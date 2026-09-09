package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/ShanilKoshitha/goforge/job"
	jobpostgres "github.com/ShanilKoshitha/goforge/job/postgres"
	"github.com/ShanilKoshitha/goforge/schedule"
)

type postgresPayload struct {
	Value string `json:"value"`
}

func TestPostgresDurableScheduleWorkflow(t *testing.T) {
	databaseURL := os.Getenv("GOFORGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOFORGE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(32)

	suffix := strings.ReplaceAll(time.Now().UTC().Format("150405.000000000"), ".", "")
	schedulesTable := "goforge_schedules_" + suffix
	jobsTable := "goforge_jobs_schedule_" + suffix
	failedTable := "goforge_failed_schedule_" + suffix
	scheduleSchema := strings.ReplaceAll(Schema, "goforge_schedules", schedulesTable)
	jobSchema := strings.ReplaceAll(jobpostgres.Schema, "goforge_failed_jobs", failedTable)
	jobSchema = strings.ReplaceAll(jobSchema, "goforge_jobs", jobsTable)
	if _, err := db.ExecContext(ctx, jobSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, scheduleSchema); err != nil {
		t.Fatal(err)
	}
	drop := fmt.Sprintf(`DROP TABLE IF EXISTS "%s"; DROP TABLE IF EXISTS "%s"; DROP TABLE IF EXISTS "%s";`, schedulesTable, failedTable, jobsTable)
	defer db.ExecContext(context.Background(), drop) //nolint:errcheck

	store, err := New(db, WithTable(schedulesTable))
	if err != nil {
		t.Fatal(err)
	}
	queueStore, err := jobpostgres.New(db, jobpostgres.WithTables(jobsTable, failedTable))
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := job.NewDispatcher(queueStore, db, job.DispatcherConfig{})
	if err != nil {
		t.Fatal(err)
	}
	target := job.MustDefine[postgresPayload]("tests.schedule-target.v1", job.Policy{
		Queue: "default", MaxAttempts: 2, Timeout: time.Second, Backoff: []time.Duration{time.Second},
	})
	everyMinute := schedule.MustDefine(
		"tests.every-minute.v1", "* * * * *", target, schedule.Static(postgresPayload{Value: "tick"}),
		schedule.MisfireGrace(15*time.Minute),
	)
	materialize := func(definition schedule.Definition, key string) schedule.MaterializeFunc {
		return func(callbackContext context.Context, executor job.Executor, _ schedule.Occurrence) (job.DispatchResult, error) {
			return target.Dispatch(callbackContext, dispatcher.Using(executor), postgresPayload{Value: definition.Name()}, job.Deduplicate(key))
		}
	}
	everyMinuteKey := "goforge:schedule:" + everyMinute.Name()

	t.Run("competing first run and restart cursor", func(t *testing.T) {
		const competitors = 16
		beforeRun := postgresClock(t, ctx, db)
		start := make(chan struct{})
		results := make(chan schedule.Result, competitors)
		errors := make(chan error, competitors)
		var wait sync.WaitGroup
		for index := 0; index < competitors; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				result, err := store.Materialize(ctx, everyMinute, materialize(everyMinute, everyMinuteKey))
				if err != nil {
					errors <- err
					return
				}
				results <- result
			}()
		}
		close(start)
		wait.Wait()
		close(results)
		close(errors)
		for err := range errors {
			t.Fatal(err)
		}
		enqueued := 0
		var first schedule.Result
		for result := range results {
			if result.Outcome == schedule.OutcomeEnqueued {
				enqueued++
				first = result
			}
		}
		if enqueued != 1 {
			t.Fatalf("competing schedulers enqueued %d jobs", enqueued)
		}
		afterRun := postgresClock(t, ctx, db)
		assertOccurrenceInDatabaseWindow(t, first.ScheduledAt, beforeRun, afterRun)
		if count := countJobsByKey(t, ctx, db, jobsTable, everyMinuteKey); count != 1 {
			t.Fatalf("durable jobs=%d", count)
		}

		restarted, err := New(db, WithTable(schedulesTable))
		if err != nil {
			t.Fatal(err)
		}
		called := false
		result, err := restarted.Materialize(ctx, everyMinute, func(context.Context, job.Executor, schedule.Occurrence) (job.DispatchResult, error) {
			called = true
			return job.DispatchResult{}, nil
		})
		if err != nil || called || result.Outcome != schedule.OutcomeNotDue {
			t.Fatalf("restart result=%+v called=%v err=%v", result, called, err)
		}
		if count := countJobsByKey(t, ctx, db, jobsTable, everyMinuteKey); count != 1 {
			t.Fatalf("restart duplicated jobs: %d", count)
		}
	})

	t.Run("List is read-only and operational", func(t *testing.T) {
		before := scheduleRowCount(t, ctx, db, schedulesTable)
		missing := schedule.MustDefine("tests.missing.v1", "0 * * * *", target, schedule.Static(postgresPayload{}), schedule.MisfireGrace(time.Hour))
		statuses, err := store.List(ctx, []schedule.Definition{everyMinute, missing})
		if err != nil {
			t.Fatal(err)
		}
		if len(statuses) != 2 || statuses[0].Name != everyMinute.Name() || statuses[1].Name != missing.Name() {
			t.Fatalf("statuses=%+v", statuses)
		}
		stored := statuses[0]
		if stored.NextRunAt.IsZero() || stored.CreatedAt == nil || stored.UpdatedAt == nil || stored.LastCursorAt == nil || stored.LastOutcome != schedule.OutcomeEnqueued {
			t.Fatalf("stored status=%+v", stored)
		}
		if !statuses[1].NextRunAt.IsZero() || statuses[1].CreatedAt != nil {
			t.Fatalf("missing definition acquired durable state: %+v", statuses[1])
		}
		if after := scheduleRowCount(t, ctx, db, schedulesTable); after != before {
			t.Fatalf("List changed row count %d -> %d", before, after)
		}
	})

	t.Run("opaque replacement-store job ids remain durable", func(t *testing.T) {
		opaqueDefinition := schedule.MustDefine(
			"tests.opaque-id.v1", "* * * * *", target, schedule.Static(postgresPayload{Value: "opaque"}),
			schedule.MisfireGrace(15*time.Minute),
		)
		opaqueID := job.ID("provider:jobs/opaque-id-42")
		result, err := store.Materialize(ctx, opaqueDefinition, func(context.Context, job.Executor, schedule.Occurrence) (job.DispatchResult, error) {
			return job.DispatchResult{ID: opaqueID, Enqueued: true}, nil
		})
		if err != nil || result.Outcome != schedule.OutcomeEnqueued || result.JobID != opaqueID {
			t.Fatalf("opaque result=%+v err=%v", result, err)
		}
		status := oneStatus(t, ctx, store, opaqueDefinition)
		if status.LastJobID != opaqueID {
			t.Fatalf("opaque durable status=%+v", status)
		}
	})

	t.Run("active overlap is suppressed and cursor advances", func(t *testing.T) {
		forceDue(t, ctx, db, schedulesTable, everyMinute.Name(), 2*time.Minute)
		result, err := store.Materialize(ctx, everyMinute, materialize(everyMinute, everyMinuteKey))
		if err != nil || result.Outcome != schedule.OutcomeOverlapSuppressed || result.JobID == "" {
			t.Fatalf("overlap result=%+v err=%v", result, err)
		}
		if count := countJobsByKey(t, ctx, db, jobsTable, everyMinuteKey); count != 1 {
			t.Fatalf("overlap created %d jobs", count)
		}
		status := oneStatus(t, ctx, store, everyMinute)
		if status.LastOutcome != schedule.OutcomeOverlapSuppressed || status.LastOccurrenceAt == nil || status.LastCursorAt == nil || !status.NextRunAt.After(*status.LastCursorAt) {
			t.Fatalf("overlap status=%+v", status)
		}
	})

	t.Run("misfire coalesces to newest eligible minute", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM "%s" WHERE dedup_key = $1`, jobsTable), everyMinuteKey); err != nil {
			t.Fatal(err)
		}
		forceDue(t, ctx, db, schedulesTable, everyMinute.Name(), 10*time.Minute)
		beforeRun := postgresClock(t, ctx, db)
		result, err := store.Materialize(ctx, everyMinute, materialize(everyMinute, everyMinuteKey))
		if err != nil || result.Outcome != schedule.OutcomeEnqueued {
			t.Fatalf("coalesce result=%+v err=%v", result, err)
		}
		afterRun := postgresClock(t, ctx, db)
		assertOccurrenceInDatabaseWindow(t, result.ScheduledAt, beforeRun, afterRun)
		status := oneStatus(t, ctx, store, everyMinute)
		if status.LastCursorAt == nil || status.LastOccurrenceAt == nil || !status.LastCursorAt.Before(*status.LastOccurrenceAt) {
			t.Fatalf("coalescing metadata did not preserve cursor and newest occurrence: %+v", status)
		}
	})

	t.Run("expired misfire advances without dispatch", func(t *testing.T) {
		var databaseNow time.Time
		if err := db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&databaseNow); err != nil {
			t.Fatal(err)
		}
		minute := (databaseNow.UTC().Minute() + 30) % 60
		hourly := schedule.MustDefine(
			"tests.hourly.v1", fmt.Sprintf("%d * * * *", minute), target,
			schedule.Static(postgresPayload{Value: "hourly"}), schedule.MisfireGrace(time.Minute),
		)
		calls := 0
		result, err := store.Materialize(ctx, hourly, func(context.Context, job.Executor, schedule.Occurrence) (job.DispatchResult, error) {
			calls++
			return job.DispatchResult{}, errors.New("unexpected initialization dispatch")
		})
		if err != nil || result.Outcome != schedule.OutcomeNotDue || calls != 0 {
			t.Fatalf("hourly initialization result=%+v calls=%d err=%v", result, calls, err)
		}
		oldOccurrence := hourly.Next(databaseNow.Add(-4 * time.Hour))
		if !oldOccurrence.Before(databaseNow.Add(-time.Minute)) {
			t.Fatalf("test occurrence %s is not expired relative to %s", oldOccurrence, databaseNow)
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`UPDATE "%s" SET next_run_at = $2 WHERE name = $1`, schedulesTable), hourly.Name(), oldOccurrence); err != nil {
			t.Fatal(err)
		}
		result, err = store.Materialize(ctx, hourly, func(context.Context, job.Executor, schedule.Occurrence) (job.DispatchResult, error) {
			calls++
			return job.DispatchResult{}, errors.New("expired misfire dispatched")
		})
		if err != nil || result.Outcome != schedule.OutcomeMisfireSkipped || calls != 0 {
			t.Fatalf("expired result=%+v calls=%d err=%v", result, calls, err)
		}
		status := oneStatus(t, ctx, store, hourly)
		if status.LastOutcome != schedule.OutcomeMisfireSkipped || status.LastOccurrenceAt != nil || status.LastJobID != "" || status.LastCursorAt == nil {
			t.Fatalf("expired status=%+v", status)
		}
	})

	t.Run("callback failure and state conflict roll back queue writes", func(t *testing.T) {
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`DELETE FROM "%s" WHERE dedup_key = $1`, jobsTable), everyMinuteKey); err != nil {
			t.Fatal(err)
		}
		forceDue(t, ctx, db, schedulesTable, everyMinute.Name(), 2*time.Minute)
		before := oneStatus(t, ctx, store, everyMinute)
		rollbackKey := "tests:schedule:rollback"
		_, err := store.Materialize(ctx, everyMinute, func(callbackContext context.Context, executor job.Executor, _ schedule.Occurrence) (job.DispatchResult, error) {
			if _, dispatchErr := target.Dispatch(callbackContext, dispatcher.Using(executor), postgresPayload{Value: "rollback"}, job.Deduplicate(rollbackKey)); dispatchErr != nil {
				return job.DispatchResult{}, dispatchErr
			}
			return job.DispatchResult{}, errors.New("after enqueue")
		})
		if err == nil || countJobsByKey(t, ctx, db, jobsTable, rollbackKey) != 0 {
			t.Fatalf("callback rollback err=%v", err)
		}
		after := oneStatus(t, ctx, store, everyMinute)
		if !after.NextRunAt.Equal(before.NextRunAt) || after.LastOutcome != before.LastOutcome {
			t.Fatalf("callback failure advanced state: before=%+v after=%+v", before, after)
		}

		stateKey := "tests:schedule:state-conflict"
		_, err = store.Materialize(ctx, everyMinute, func(callbackContext context.Context, executor job.Executor, _ schedule.Occurrence) (job.DispatchResult, error) {
			result, dispatchErr := target.Dispatch(callbackContext, dispatcher.Using(executor), postgresPayload{Value: "state"}, job.Deduplicate(stateKey))
			if dispatchErr != nil {
				return job.DispatchResult{}, dispatchErr
			}
			if _, updateErr := executor.ExecContext(callbackContext, fmt.Sprintf(`UPDATE "%s" SET fingerprint = repeat('0', 64) WHERE name = $1`, schedulesTable), everyMinute.Name()); updateErr != nil {
				return job.DispatchResult{}, updateErr
			}
			return result, nil
		})
		if err == nil || countJobsByKey(t, ctx, db, jobsTable, stateKey) != 0 {
			t.Fatalf("state-conflict rollback err=%v", err)
		}
		after = oneStatus(t, ctx, store, everyMinute)
		if after.Fingerprint != everyMinute.Fingerprint() || !after.NextRunAt.Equal(before.NextRunAt) {
			t.Fatalf("state conflict escaped rollback: %+v", after)
		}
	})

	t.Run("cancellation after enqueue rolls back queue and cursor", func(t *testing.T) {
		forceDue(t, ctx, db, schedulesTable, everyMinute.Name(), 2*time.Minute)
		before := oneStatus(t, ctx, store, everyMinute)
		cancelKey := "tests:schedule:cancel"
		operationContext, cancelOperation := context.WithCancel(ctx)
		_, err := store.Materialize(operationContext, everyMinute, func(callbackContext context.Context, executor job.Executor, _ schedule.Occurrence) (job.DispatchResult, error) {
			if _, dispatchErr := target.Dispatch(callbackContext, dispatcher.Using(executor), postgresPayload{Value: "cancel"}, job.Deduplicate(cancelKey)); dispatchErr != nil {
				return job.DispatchResult{}, dispatchErr
			}
			cancelOperation()
			return job.DispatchResult{}, callbackContext.Err()
		})
		cancelOperation()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error=%v", err)
		}
		if count := countJobsByKey(t, ctx, db, jobsTable, cancelKey); count != 0 {
			t.Fatalf("canceled transaction retained %d queue rows", count)
		}
		after := oneStatus(t, ctx, store, everyMinute)
		if !after.NextRunAt.Equal(before.NextRunAt) || after.LastOutcome != before.LastOutcome {
			t.Fatalf("cancellation advanced state: before=%+v after=%+v", before, after)
		}
	})

	t.Run("terminated transaction cannot orphan queue or cursor", func(t *testing.T) {
		forceDue(t, ctx, db, schedulesTable, everyMinute.Name(), 2*time.Minute)
		before := oneStatus(t, ctx, store, everyMinute)
		crashKey := "tests:schedule:connection-loss"
		_, err := store.Materialize(ctx, everyMinute, func(callbackContext context.Context, executor job.Executor, _ schedule.Occurrence) (job.DispatchResult, error) {
			result, dispatchErr := target.Dispatch(callbackContext, dispatcher.Using(executor), postgresPayload{Value: "connection-loss"}, job.Deduplicate(crashKey))
			if dispatchErr != nil {
				return job.DispatchResult{}, dispatchErr
			}
			var backendPID int
			if queryErr := executor.QueryRowContext(callbackContext, `SELECT pg_backend_pid()`).Scan(&backendPID); queryErr != nil {
				return job.DispatchResult{}, queryErr
			}
			var terminated bool
			if terminateErr := db.QueryRowContext(callbackContext, `SELECT pg_terminate_backend($1)`, backendPID).Scan(&terminated); terminateErr != nil {
				return job.DispatchResult{}, terminateErr
			}
			if !terminated {
				return job.DispatchResult{}, fmt.Errorf("backend %d was not terminated", backendPID)
			}
			return result, nil
		})
		if err == nil {
			t.Fatal("terminated transaction unexpectedly succeeded")
		}
		if count := countJobsByKey(t, ctx, db, jobsTable, crashKey); count != 0 {
			t.Fatalf("terminated transaction retained %d queue rows", count)
		}
		after := oneStatus(t, ctx, store, everyMinute)
		if !after.NextRunAt.Equal(before.NextRunAt) || after.LastOutcome != before.LastOutcome {
			t.Fatalf("connection loss advanced state: before=%+v after=%+v", before, after)
		}
	})

	t.Run("fingerprint drift fails closed", func(t *testing.T) {
		conflict := schedule.MustDefine(
			everyMinute.Name(), "*/2 * * * *", target, schedule.Static(postgresPayload{Value: "changed"}),
			schedule.MisfireGrace(15*time.Minute),
		)
		called := false
		_, err := store.Materialize(ctx, conflict, func(context.Context, job.Executor, schedule.Occurrence) (job.DispatchResult, error) {
			called = true
			return job.DispatchResult{}, nil
		})
		if !errors.Is(err, schedule.ErrDefinitionConflict) || called {
			t.Fatalf("conflict called=%v err=%v", called, err)
		}
		if _, err := store.List(ctx, []schedule.Definition{conflict}); !errors.Is(err, schedule.ErrDefinitionConflict) {
			t.Fatalf("List conflict err=%v", err)
		}
	})

	t.Run("row locks contend independently and dormant rows stay inert", func(t *testing.T) {
		other := schedule.MustDefine(
			"tests.independent.v1", "* * * * *", target, schedule.Static(postgresPayload{Value: "other"}),
			schedule.MisfireGrace(15*time.Minute),
		)
		// Initialize before holding another connection's row lock.
		if _, err := store.Materialize(ctx, other, materialize(other, "goforge:schedule:"+other.Name())); err != nil {
			t.Fatal(err)
		}
		forceDue(t, ctx, db, schedulesTable, everyMinute.Name(), time.Minute)
		forceDue(t, ctx, db, schedulesTable, other.Name(), time.Minute)
		locker, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := locker.ExecContext(ctx, fmt.Sprintf(`SELECT 1 FROM "%s" WHERE name = $1 FOR UPDATE`, schedulesTable), everyMinute.Name()); err != nil {
			_ = locker.Rollback()
			t.Fatal(err)
		}
		contended, err := store.Materialize(ctx, everyMinute, materialize(everyMinute, everyMinuteKey))
		if err != nil || contended.Outcome != schedule.OutcomeContended {
			_ = locker.Rollback()
			t.Fatalf("contended result=%+v err=%v", contended, err)
		}
		independentKey := "tests:schedule:independent-run"
		independent, err := store.Materialize(ctx, other, materialize(other, independentKey))
		if err != nil || (independent.Outcome != schedule.OutcomeEnqueued && independent.Outcome != schedule.OutcomeOverlapSuppressed) {
			_ = locker.Rollback()
			t.Fatalf("independent result=%+v err=%v", independent, err)
		}
		if err := locker.Rollback(); err != nil {
			t.Fatal(err)
		}

		statuses, err := store.List(ctx, []schedule.Definition{everyMinute})
		if err != nil || len(statuses) != 1 || statuses[0].Name != everyMinute.Name() {
			t.Fatalf("registered-only statuses=%+v err=%v", statuses, err)
		}
		if count := scheduleRowCount(t, ctx, db, schedulesTable); count < 3 {
			t.Fatalf("expected dormant durable rows, count=%d", count)
		}
	})

	t.Run("schema rejects invalid durable names", func(t *testing.T) {
		query := fmt.Sprintf(`INSERT INTO "%s"
            (name, fingerprint, cron_expression, time_zone, job_name, queue,
             misfire_grace_ms, overlap_policy, next_run_at, created_at, updated_at)
            VALUES ('not-versioned', repeat('a', 64), '* * * * *', 'UTC',
                    'tests.schedule-target.v1', 'default', 60000, 'forbid',
                    clock_timestamp(), clock_timestamp(), clock_timestamp())`, schedulesTable)
		if _, err := db.ExecContext(ctx, query); err == nil {
			t.Fatal("invalid durable schedule name passed schema constraint")
		}
	})
}

func countJobsByKey(t *testing.T, ctx context.Context, db *sql.DB, table, key string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s" WHERE dedup_key = $1`, table), key).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func postgresClock(t *testing.T, ctx context.Context, db *sql.DB) time.Time {
	t.Helper()
	var now time.Time
	if err := db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	return now.UTC()
}

func assertOccurrenceInDatabaseWindow(t *testing.T, occurrence, before, after time.Time) {
	t.Helper()
	if occurrence.Location() != time.UTC || !occurrence.Equal(occurrence.Truncate(time.Minute)) ||
		occurrence.Before(before.UTC().Truncate(time.Minute)) || occurrence.After(after.UTC().Truncate(time.Minute)) {
		t.Fatalf("occurrence %s is not a whole UTC minute within database window [%s, %s]", occurrence, before, after)
	}
}

func scheduleRowCount(t *testing.T, ctx context.Context, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM "%s"`, table)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func forceDue(t *testing.T, ctx context.Context, db *sql.DB, table, name string, ago time.Duration) {
	t.Helper()
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`UPDATE "%s" SET next_run_at = date_trunc('minute', clock_timestamp() - ($2::bigint * interval '1 millisecond')) WHERE name = $1`, table), name, ago.Milliseconds()); err != nil {
		t.Fatal(err)
	}
}

func oneStatus(t *testing.T, ctx context.Context, store *Store, definition schedule.Definition) schedule.Status {
	t.Helper()
	statuses, err := store.List(ctx, []schedule.Definition{definition})
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("statuses=%+v", statuses)
	}
	return statuses[0]
}
