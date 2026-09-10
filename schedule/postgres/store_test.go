package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/job"
	"github.com/ShanilKoshitha/goforge/schedule"
)

type testPayload struct {
	Value string `json:"value"`
}

func testDefinition(t *testing.T, name, expression string, grace time.Duration, value string) schedule.Definition {
	t.Helper()
	target := job.MustDefine[testPayload]("tests.scheduled.v1", job.Policy{
		Queue: "default", MaxAttempts: 2, Timeout: time.Second, Backoff: []time.Duration{time.Second},
	})
	definition, err := schedule.Define(name, expression, target, schedule.Static(testPayload{Value: value}), schedule.MisfireGrace(grace))
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func TestSchemaIsPlainInspectablePostgreSQL(t *testing.T) {
	for _, fragment := range []string{
		"CREATE TABLE goforge_schedules", "fingerprint text NOT NULL",
		"next_run_at timestamptz", "last_cursor_at timestamptz",
		"overlap_policy = 'forbid'", "goforge_schedules_due_idx",
	} {
		if !strings.Contains(Schema, fragment) {
			t.Fatalf("schema is missing %q", fragment)
		}
	}
	lower := strings.ToLower(Schema)
	if strings.Contains(lower, "create extension") || strings.Contains(lower, "foreign key") || strings.Contains(lower, "references goforge_jobs") || strings.Contains(lower, "last_job_id uuid") {
		t.Fatal("schedule schema unexpectedly couples to an extension, queue table, or UUID job IDs")
	}
}

func TestStoreConfigurationIsDefensive(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("expected nil database error")
	}
	db := &sql.DB{}
	for _, options := range [][]Option{{WithTable("bad-name")}, {WithTable(strings.Repeat("a", 64))}, {nil}} {
		if _, err := New(db, options...); err == nil {
			t.Fatalf("expected invalid options error for %+v", options)
		}
	}
	if _, err := New(db, WithTable("tenant_schedules")); err != nil {
		t.Fatal(err)
	}
}

func TestLatestOccurrenceUsesBoundedBinarySearchAndInclusiveBounds(t *testing.T) {
	end := time.Date(2026, 9, 9, 12, 34, 37, 0, time.UTC)
	start := end.Add(-365 * 24 * time.Hour)
	var calls atomic.Int64
	everyMinute := func(after time.Time) time.Time {
		calls.Add(1)
		return after.Truncate(time.Minute).Add(time.Minute)
	}
	got, found, err := latestOccurrence(everyMinute, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if !found || !got.Equal(end.Truncate(time.Minute)) {
		t.Fatalf("latest=%s found=%v, want %s", got, found, end.Truncate(time.Minute))
	}
	if count := calls.Load(); count > 24 {
		t.Fatalf("365-day every-minute search made %d Next calls", count)
	}

	boundary := end.Truncate(time.Minute)
	got, found, err = latestOccurrence(everyMinute, boundary, boundary)
	if err != nil || !found || !got.Equal(boundary) {
		t.Fatalf("inclusive single-minute result=%s found=%v err=%v", got, found, err)
	}

	nextDay := func(after time.Time) time.Time { return after.UTC().Truncate(24 * time.Hour).Add(24 * time.Hour) }
	if got, found, err := latestOccurrence(nextDay, boundary, boundary.Add(30*time.Second)); err != nil || found || !got.IsZero() {
		t.Fatalf("empty window result=%s found=%v err=%v", got, found, err)
	}
}

func TestLatestOccurrenceRejectsBrokenNextFunction(t *testing.T) {
	start := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	_, _, err := latestOccurrence(func(time.Time) time.Time { return start.Add(-time.Minute) }, start, start.Add(time.Hour))
	if err == nil || !strings.Contains(err.Error(), "monotone") {
		t.Fatalf("broken Next error = %v", err)
	}
}

func TestMaterializeTransactionCommitsQueueAndCursorTogether(t *testing.T) {
	definition := testDefinition(t, "tests.every-minute.v1", "* * * * *", 15*time.Minute, "commit")
	now := time.Date(2026, 9, 9, 12, 34, 37, 0, time.UTC)
	db, state := openScriptDatabase(t, definition, now)
	store, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	id := job.ID("11111111-1111-4111-8111-111111111111")
	result, err := store.Materialize(context.Background(), definition, func(ctx context.Context, executor job.Executor, occurrence schedule.Occurrence) (job.DispatchResult, error) {
		if _, err := executor.ExecContext(ctx, "INSERT QUEUE"); err != nil {
			return job.DispatchResult{}, err
		}
		if !occurrence.ScheduledAt.Equal(now.Truncate(time.Minute)) {
			t.Fatalf("occurrence=%s", occurrence.ScheduledAt)
		}
		return job.DispatchResult{ID: id, Enqueued: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != schedule.OutcomeEnqueued || result.JobID != id {
		t.Fatalf("result=%+v", result)
	}
	snapshot := state.read()
	if snapshot.queueCount != 1 || snapshot.row.lastOutcome != string(schedule.OutcomeEnqueued) || !snapshot.row.nextRunAt.Equal(definition.Next(now)) {
		t.Fatalf("committed snapshot=%+v", snapshot)
	}
}

func TestMaterializeInitializesAtTheCurrentEligibleMinute(t *testing.T) {
	definition := testDefinition(t, "tests.initialize.v1", "* * * * *", 15*time.Minute, "initialize")
	now := time.Date(2026, 9, 9, 12, 34, 37, 0, time.UTC)
	db, state := openScriptDatabase(t, definition, now)
	state.mu.Lock()
	state.snapshot = scriptSnapshot{}
	state.mu.Unlock()
	store, _ := New(db)
	result, err := store.Materialize(context.Background(), definition, func(_ context.Context, _ job.Executor, occurrence schedule.Occurrence) (job.DispatchResult, error) {
		if !occurrence.ScheduledAt.Equal(now.Truncate(time.Minute)) {
			t.Fatalf("first occurrence=%s, want current minute", occurrence.ScheduledAt)
		}
		return job.DispatchResult{ID: "12121212-1212-4212-8212-121212121212", Enqueued: true}, nil
	})
	if err != nil || result.Outcome != schedule.OutcomeEnqueued {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	snapshot := state.read()
	if !snapshot.rowExists || !snapshot.row.nextRunAt.Equal(definition.Next(now)) {
		t.Fatalf("initialized snapshot=%+v", snapshot)
	}
}

func TestMaterializeDoesNotReplayHistoryWhenFirstObserved(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 34, 37, 0, time.UTC)
	definition := testDefinition(t, "tests.new-hourly.v1", "24 * * * *", 15*time.Minute, "new")
	db, state := openScriptDatabase(t, definition, now)
	state.mu.Lock()
	state.snapshot = scriptSnapshot{}
	state.mu.Unlock()
	store, _ := New(db)
	called := false
	result, err := store.Materialize(context.Background(), definition, func(context.Context, job.Executor, schedule.Occurrence) (job.DispatchResult, error) {
		called = true
		return job.DispatchResult{}, errors.New("new schedule replayed history")
	})
	if err != nil || called || result.Outcome != schedule.OutcomeNotDue {
		t.Fatalf("result=%+v called=%v err=%v", result, called, err)
	}
	next := state.read().row.nextRunAt
	if !next.Equal(time.Date(2026, 9, 9, 13, 24, 0, 0, time.UTC)) {
		t.Fatalf("initial cursor=%s", next)
	}
}

func TestMaterializeRecordsActiveOverlapSuppression(t *testing.T) {
	definition := testDefinition(t, "tests.overlap.v1", "* * * * *", 15*time.Minute, "overlap")
	now := time.Date(2026, 9, 9, 12, 34, 37, 0, time.UTC)
	db, state := openScriptDatabase(t, definition, now)
	store, _ := New(db)
	id := job.ID("opaque:queue/provider/id-13")
	result, err := store.Materialize(context.Background(), definition, func(context.Context, job.Executor, schedule.Occurrence) (job.DispatchResult, error) {
		return job.DispatchResult{ID: id, Enqueued: false}, nil
	})
	if err != nil || result.Outcome != schedule.OutcomeOverlapSuppressed || result.JobID != id {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	snapshot := state.read()
	if snapshot.row.lastOutcome != string(schedule.OutcomeOverlapSuppressed) || snapshot.row.lastJobID != string(id) {
		t.Fatalf("durable opaque-id result=%+v", snapshot.row)
	}
}

func TestMaterializeRejectsOversizedOpaqueJobID(t *testing.T) {
	definition := testDefinition(t, "tests.job-id-bound.v1", "* * * * *", 15*time.Minute, "bound")
	now := time.Date(2026, 9, 9, 12, 34, 37, 0, time.UTC)
	db, state := openScriptDatabase(t, definition, now)
	store, _ := New(db)
	before := state.read()
	_, err := store.Materialize(context.Background(), definition, func(context.Context, job.Executor, schedule.Occurrence) (job.DispatchResult, error) {
		return job.DispatchResult{ID: job.ID(strings.Repeat("x", 1025)), Enqueued: true}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "invalid job id") {
		t.Fatalf("oversized job id error=%v", err)
	}
	after := state.read()
	if !after.row.nextRunAt.Equal(before.row.nextRunAt) || after.row.lastJobID != "" {
		t.Fatalf("oversized job id advanced state: before=%+v after=%+v", before, after)
	}
}

func TestMaterializeRollsBackCallbackAndAdvanceFailures(t *testing.T) {
	definition := testDefinition(t, "tests.rollback.v1", "* * * * *", 15*time.Minute, "rollback")
	now := time.Date(2026, 9, 9, 12, 34, 37, 0, time.UTC)
	tests := []struct {
		name      string
		updateErr error
		callback  func(context.Context, job.Executor) (job.DispatchResult, error)
	}{
		{
			name: "callback error",
			callback: func(ctx context.Context, executor job.Executor) (job.DispatchResult, error) {
				_, _ = executor.ExecContext(ctx, "INSERT QUEUE")
				return job.DispatchResult{}, errors.New("payload factory failed")
			},
		},
		{
			name:      "advance error",
			updateErr: errors.New("advance rejected"),
			callback: func(ctx context.Context, executor job.Executor) (job.DispatchResult, error) {
				_, _ = executor.ExecContext(ctx, "INSERT QUEUE")
				return job.DispatchResult{ID: "22222222-2222-4222-8222-222222222222", Enqueued: true}, nil
			},
		},
		{
			name: "cancellation",
			callback: func(ctx context.Context, executor job.Executor) (job.DispatchResult, error) {
				_, _ = executor.ExecContext(ctx, "INSERT QUEUE")
				return job.DispatchResult{}, context.Canceled
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, state := openScriptDatabase(t, definition, now)
			state.updateErr = test.updateErr
			store, _ := New(db)
			before := state.read()
			_, err := store.Materialize(context.Background(), definition, func(ctx context.Context, executor job.Executor, _ schedule.Occurrence) (job.DispatchResult, error) {
				return test.callback(ctx, executor)
			})
			if err == nil {
				t.Fatal("expected materialize error")
			}
			after := state.read()
			if after.queueCount != before.queueCount || !after.row.nextRunAt.Equal(before.row.nextRunAt) || after.row.lastOutcome != before.row.lastOutcome {
				t.Fatalf("transaction was not rolled back: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestMaterializeRollsBackCallbackPanic(t *testing.T) {
	definition := testDefinition(t, "tests.panic.v1", "* * * * *", 15*time.Minute, "panic")
	now := time.Date(2026, 9, 9, 12, 34, 37, 0, time.UTC)
	db, state := openScriptDatabase(t, definition, now)
	store, _ := New(db)
	before := state.read()
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected callback panic")
			}
		}()
		_, _ = store.Materialize(context.Background(), definition, func(ctx context.Context, executor job.Executor, _ schedule.Occurrence) (job.DispatchResult, error) {
			_, _ = executor.ExecContext(ctx, "INSERT QUEUE")
			panic("handler setup panic")
		})
	}()
	after := state.read()
	if after.queueCount != before.queueCount || !after.row.nextRunAt.Equal(before.row.nextRunAt) {
		t.Fatalf("panic transaction was not rolled back: before=%+v after=%+v", before, after)
	}
}

func TestMaterializeCommitFailurePublishesNeitherQueueNorCursor(t *testing.T) {
	definition := testDefinition(t, "tests.commit-failure.v1", "* * * * *", 15*time.Minute, "commit failure")
	now := time.Date(2026, 9, 9, 12, 34, 37, 0, time.UTC)
	db, state := openScriptDatabase(t, definition, now)
	state.commitErr = errors.New("connection lost during commit")
	store, _ := New(db)
	before := state.read()
	_, err := store.Materialize(context.Background(), definition, func(ctx context.Context, executor job.Executor, _ schedule.Occurrence) (job.DispatchResult, error) {
		if _, writeErr := executor.ExecContext(ctx, "INSERT QUEUE"); writeErr != nil {
			return job.DispatchResult{}, writeErr
		}
		return job.DispatchResult{ID: "24242424-2424-4424-8424-242424242424", Enqueued: true}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "commit transaction") {
		t.Fatalf("commit failure error=%v", err)
	}
	after := state.read()
	if after.queueCount != before.queueCount || !after.row.nextRunAt.Equal(before.row.nextRunAt) || after.row.lastOutcome != before.row.lastOutcome {
		t.Fatalf("commit failure published state: before=%+v after=%+v", before, after)
	}
}

func TestListIsReadOnlySortedAndFailsClosedOnDrift(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 34, 37, 0, time.UTC)
	storedDefinition := testDefinition(t, "tests.stored.v1", "* * * * *", 15*time.Minute, "stored")
	missingDefinition := testDefinition(t, "tests.missing.v1", "0 * * * *", time.Hour, "missing")
	db, state := openScriptDatabase(t, storedDefinition, now)
	store, _ := New(db)
	before := state.read()
	statuses, err := store.List(context.Background(), []schedule.Definition{storedDefinition, missingDefinition})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0].Name != missingDefinition.Name() || statuses[1].Name != storedDefinition.Name() {
		t.Fatalf("statuses=%+v", statuses)
	}
	if !statuses[0].NextRunAt.IsZero() || statuses[1].NextRunAt.IsZero() {
		t.Fatalf("missing/stored durable state was not distinguished: %+v", statuses)
	}
	after := state.read()
	if after.queueCount != before.queueCount || !after.row.nextRunAt.Equal(before.row.nextRunAt) {
		t.Fatal("List mutated durable state")
	}

	conflict := testDefinition(t, storedDefinition.Name(), "*/2 * * * *", 15*time.Minute, "stored")
	if _, err := store.List(context.Background(), []schedule.Definition{conflict}); !errors.Is(err, schedule.ErrDefinitionConflict) {
		t.Fatalf("fingerprint conflict error=%v", err)
	}
}

var scriptDriverSequence atomic.Uint64

type scriptRow struct {
	name, fingerprint, expression, timeZone, jobName, queue, overlapPolicy string
	misfireGrace                                                           time.Duration
	nextRunAt                                                              time.Time
	lastEvaluatedAt, lastCursorAt, lastOccurrenceAt                        *time.Time
	lastJobID, lastOutcome                                                 string
	createdAt, updatedAt                                                   time.Time
}

type scriptSnapshot struct {
	row        scriptRow
	rowExists  bool
	queueCount int
}

type scriptState struct {
	mu        sync.Mutex
	snapshot  scriptSnapshot
	now       time.Time
	updateErr error
	commitErr error
}

func (state *scriptState) read() scriptSnapshot {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.snapshot
}

type scriptDriver struct{ state *scriptState }

func (driverValue scriptDriver) Open(string) (driver.Conn, error) {
	return &scriptConn{state: driverValue.state}, nil
}

type scriptConn struct {
	state   *scriptState
	working *scriptSnapshot
}

func (*scriptConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported")
}
func (*scriptConn) Close() error { return nil }
func (connection *scriptConn) Begin() (driver.Tx, error) {
	return connection.BeginTx(context.Background(), driver.TxOptions{})
}
func (connection *scriptConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	connection.state.mu.Lock()
	copy := connection.state.snapshot
	connection.state.mu.Unlock()
	connection.working = &copy
	return &scriptTx{connection: connection}, nil
}

func (connection *scriptConn) snapshot() *scriptSnapshot {
	if connection.working != nil {
		return connection.working
	}
	return &connection.state.snapshot
}

func (connection *scriptConn) ExecContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	snapshot := connection.snapshot()
	switch {
	case query == "INSERT QUEUE":
		snapshot.queueCount++
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(query, "INSERT INTO \"goforge_schedules\""):
		if snapshot.rowExists {
			return driver.RowsAffected(0), nil
		}
		now := arguments[9].Value.(time.Time)
		snapshot.rowExists = true
		snapshot.row = scriptRow{
			name: arguments[0].Value.(string), fingerprint: arguments[1].Value.(string),
			expression: arguments[2].Value.(string), timeZone: arguments[3].Value.(string),
			jobName: arguments[4].Value.(string), queue: arguments[5].Value.(string),
			misfireGrace:  time.Duration(arguments[6].Value.(int64)) * time.Millisecond,
			overlapPolicy: arguments[7].Value.(string), nextRunAt: arguments[8].Value.(time.Time),
			createdAt: now, updatedAt: now,
		}
		return driver.RowsAffected(1), nil
	case strings.HasPrefix(query, "UPDATE \"goforge_schedules\""):
		if connection.state.updateErr != nil {
			return nil, connection.state.updateErr
		}
		if !snapshot.rowExists || snapshot.row.name != arguments[0].Value.(string) ||
			snapshot.row.fingerprint != arguments[1].Value.(string) ||
			!snapshot.row.nextRunAt.Equal(arguments[4].Value.(time.Time)) {
			return driver.RowsAffected(0), nil
		}
		snapshot.row.nextRunAt = arguments[2].Value.(time.Time)
		evaluated := arguments[3].Value.(time.Time)
		cursor := arguments[4].Value.(time.Time)
		snapshot.row.lastEvaluatedAt = &evaluated
		snapshot.row.lastCursorAt = &cursor
		if arguments[5].Value != nil {
			occurrence := arguments[5].Value.(time.Time)
			snapshot.row.lastOccurrenceAt = &occurrence
		}
		if arguments[6].Value != nil {
			snapshot.row.lastJobID = arguments[6].Value.(string)
		}
		snapshot.row.lastOutcome = arguments[7].Value.(string)
		snapshot.row.updatedAt = evaluated
		return driver.RowsAffected(1), nil
	default:
		return nil, fmt.Errorf("unexpected exec: %s", query)
	}
}

func (connection *scriptConn) QueryContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Rows, error) {
	snapshot := connection.snapshot()
	switch {
	case query == "SELECT clock_timestamp()":
		return &scriptRows{columns: []string{"clock_timestamp"}, values: [][]driver.Value{{connection.state.now}}}, nil
	case strings.HasPrefix(query, "SELECT EXISTS"):
		return &scriptRows{columns: []string{"exists"}, values: [][]driver.Value{{snapshot.rowExists}}}, nil
	case strings.HasPrefix(query, "SELECT name, fingerprint"):
		if !snapshot.rowExists || snapshot.row.name != arguments[0].Value.(string) {
			return &scriptRows{columns: scriptColumns()}, nil
		}
		row := snapshot.row
		return &scriptRows{columns: scriptColumns(), values: [][]driver.Value{{
			row.name, row.fingerprint, row.expression, row.timeZone, row.jobName, row.queue,
			row.misfireGrace.Milliseconds(), row.overlapPolicy, row.nextRunAt,
			nullTimeValue(row.lastEvaluatedAt), nullTimeValue(row.lastCursorAt), nullTimeValue(row.lastOccurrenceAt),
			row.lastJobID, row.lastOutcome, row.createdAt, row.updatedAt,
		}}}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

type scriptTx struct{ connection *scriptConn }

func (transaction *scriptTx) Commit() error {
	if transaction.connection.state.commitErr != nil {
		transaction.connection.working = nil
		return transaction.connection.state.commitErr
	}
	transaction.connection.state.mu.Lock()
	transaction.connection.state.snapshot = *transaction.connection.working
	transaction.connection.state.mu.Unlock()
	transaction.connection.working = nil
	return nil
}

func (transaction *scriptTx) Rollback() error {
	transaction.connection.working = nil
	return nil
}

type scriptRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (rows *scriptRows) Columns() []string { return rows.columns }
func (*scriptRows) Close() error           { return nil }
func (rows *scriptRows) Next(destination []driver.Value) error {
	if rows.index >= len(rows.values) {
		return io.EOF
	}
	copy(destination, rows.values[rows.index])
	rows.index++
	return nil
}

func scriptColumns() []string {
	return []string{
		"name", "fingerprint", "cron_expression", "time_zone", "job_name", "queue",
		"misfire_grace_ms", "overlap_policy", "next_run_at", "last_evaluated_at",
		"last_cursor_at", "last_occurrence_at", "last_job_id", "last_outcome", "created_at", "updated_at",
	}
}

func nullTimeValue(value *time.Time) driver.Value {
	if value == nil {
		return nil
	}
	return *value
}

func openScriptDatabase(t *testing.T, definition schedule.Definition, now time.Time) (*sql.DB, *scriptState) {
	t.Helper()
	cursor := now.Truncate(time.Minute)
	state := &scriptState{
		now: now,
		snapshot: scriptSnapshot{rowExists: true, row: scriptRow{
			name: definition.Name(), fingerprint: definition.Fingerprint(), expression: definition.Expression(),
			timeZone: definition.TimeZone(), jobName: definition.JobName(), queue: definition.Queue(),
			misfireGrace: definition.MisfireGrace(), overlapPolicy: overlapPolicy,
			nextRunAt: cursor, createdAt: now.Add(-time.Hour), updatedAt: now.Add(-time.Hour),
		}},
	}
	name := fmt.Sprintf("goforge-schedule-script-%d", scriptDriverSequence.Add(1))
	sql.Register(name, scriptDriver{state: state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db, state
}
