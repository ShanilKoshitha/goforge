package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/ShanilKoshitha/goforge/database"
	"github.com/ShanilKoshitha/goforge/job"
	"github.com/ShanilKoshitha/goforge/schedule"
)

const overlapPolicy = "forbid"

var (
	scheduleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*\.v[1-9][0-9]*$`)
	fingerprintPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	jobNamePattern      = regexp.MustCompile(`^[a-z][a-z0-9._-]*\.v[1-9][0-9]*$`)
	queuePattern        = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
)

type durableSchedule struct {
	name             string
	fingerprint      string
	expression       string
	timeZone         string
	jobName          string
	queue            string
	misfireGrace     time.Duration
	overlapPolicy    string
	nextRunAt        time.Time
	lastEvaluatedAt  sql.NullTime
	lastCursorAt     sql.NullTime
	lastOccurrenceAt sql.NullTime
	lastJobID        sql.NullString
	lastOutcome      sql.NullString
	createdAt        time.Time
	updatedAt        time.Time
}

// Materialize initializes and evaluates one code-owned definition. Dispatch
// and cursor advancement occur in the same database transaction.
func (store *Store) Materialize(ctx context.Context, definition schedule.Definition, materialize schedule.MaterializeFunc) (schedule.Result, error) {
	if err := validateDefinition(definition); err != nil {
		return schedule.Result{}, err
	}
	if materialize == nil {
		return schedule.Result{}, fmt.Errorf("schedule/postgres: materialize callback is required")
	}
	if err := ctx.Err(); err != nil {
		return schedule.Result{}, err
	}

	result := schedule.Result{Name: definition.Name(), Outcome: schedule.OutcomeNotDue}
	err := database.Transaction(ctx, store.db, nil, func(tx *sql.Tx) error {
		stored, found, err := store.lock(ctx, tx, definition.Name())
		if err != nil {
			return err
		}
		if !found {
			exists, err := store.exists(ctx, tx, definition.Name())
			if err != nil {
				return err
			}
			if exists {
				result.Outcome = schedule.OutcomeContended
				return nil
			}
			if err := store.initialize(ctx, tx, definition); err != nil {
				return err
			}
			stored, found, err = store.lock(ctx, tx, definition.Name())
			if err != nil {
				return err
			}
			if !found {
				result.Outcome = schedule.OutcomeContended
				return nil
			}
		}
		if err := stored.matches(definition); err != nil {
			return err
		}

		now, err := databaseClock(ctx, tx)
		if err != nil {
			return err
		}
		if stored.nextRunAt.After(now) {
			return nil
		}

		nextRunAt, err := nextAfter(definition, now)
		if err != nil {
			return err
		}
		windowStart := now.Add(-definition.MisfireGrace())
		if stored.nextRunAt.After(windowStart) {
			windowStart = stored.nextRunAt
		}
		occurrenceAt, eligible, err := latestOccurrence(definition.Next, windowStart, now)
		if err != nil {
			return fmt.Errorf("schedule/postgres: select occurrence for %s: %w", definition.Name(), err)
		}
		if !eligible {
			if err := store.advance(ctx, tx, stored, nextRunAt, now, time.Time{}, "", schedule.OutcomeMisfireSkipped); err != nil {
				return err
			}
			result.Outcome = schedule.OutcomeMisfireSkipped
			return nil
		}

		dispatched, err := materialize(ctx, tx, schedule.Occurrence{ScheduledAt: occurrenceAt})
		if err != nil {
			return fmt.Errorf("schedule/postgres: dispatch %s at %s: %w", definition.Name(), occurrenceAt.Format(time.RFC3339), err)
		}
		if dispatched.ID == "" || len(dispatched.ID) > 1024 {
			return fmt.Errorf("schedule/postgres: dispatch %s returned an invalid job id", definition.Name())
		}
		outcome := schedule.OutcomeOverlapSuppressed
		if dispatched.Enqueued {
			outcome = schedule.OutcomeEnqueued
		}
		if err := store.advance(ctx, tx, stored, nextRunAt, now, occurrenceAt, dispatched.ID, outcome); err != nil {
			return err
		}
		result.Outcome = outcome
		result.ScheduledAt = occurrenceAt
		result.JobID = dispatched.ID
		return nil
	})
	if err != nil {
		return schedule.Result{}, err
	}
	return result, nil
}

// List reports only the supplied code-owned definitions. It never initializes
// state and never activates durable rows absent from the supplied registry.
func (store *Store) List(ctx context.Context, definitions []schedule.Definition) ([]schedule.Status, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ordered := append([]schedule.Definition(nil), definitions...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name() < ordered[j].Name() })
	seen := make(map[string]struct{}, len(ordered))
	statuses := make([]schedule.Status, 0, len(ordered))
	for _, definition := range ordered {
		if err := validateDefinition(definition); err != nil {
			return nil, err
		}
		if _, exists := seen[definition.Name()]; exists {
			return nil, fmt.Errorf("schedule/postgres: duplicate definition %s", definition.Name())
		}
		seen[definition.Name()] = struct{}{}
		status := statusFromDefinition(definition)
		stored, found, err := store.find(ctx, store.db, definition.Name(), "")
		if err != nil {
			return nil, err
		}
		if found {
			if err := stored.matches(definition); err != nil {
				return nil, err
			}
			status = stored.status(definition)
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func validateDefinition(definition schedule.Definition) error {
	if len(definition.Name()) == 0 || len(definition.Name()) > schedule.MaxNameBytes || !scheduleNamePattern.MatchString(definition.Name()) {
		return fmt.Errorf("schedule/postgres: valid definition is required")
	}
	if len(definition.Expression()) == 0 || len(definition.Expression()) > 256 ||
		len(definition.TimeZone()) == 0 || len(definition.TimeZone()) > 128 ||
		len(definition.JobName()) == 0 || len(definition.JobName()) > job.MaxNameBytes || !jobNamePattern.MatchString(definition.JobName()) ||
		len(definition.Queue()) == 0 || len(definition.Queue()) > job.MaxQueueBytes || !queuePattern.MatchString(definition.Queue()) ||
		len(definition.Fingerprint()) != 64 || !fingerprintPattern.MatchString(definition.Fingerprint()) ||
		definition.MisfireGrace() < time.Minute || definition.MisfireGrace() > 365*24*time.Hour || definition.MisfireGrace()%time.Minute != 0 {
		return fmt.Errorf("schedule/postgres: invalid durable definition %s", definition.Name())
	}
	return nil
}

func (store *Store) initialize(ctx context.Context, tx *sql.Tx, definition schedule.Definition) error {
	now, err := databaseClock(ctx, tx)
	if err != nil {
		return err
	}
	currentMinute := now.Truncate(time.Minute)
	cursor := definition.Next(currentMinute.Add(-time.Nanosecond)).UTC()
	if cursor.IsZero() || !cursor.Equal(cursor.Truncate(time.Minute)) || cursor.Before(currentMinute) {
		return fmt.Errorf("schedule/postgres: %s returned an invalid initialization occurrence", definition.Name())
	}
	if !cursor.Equal(currentMinute) {
		cursor, err = nextAfter(definition, now)
		if err != nil {
			return err
		}
	}
	query := fmt.Sprintf(`INSERT INTO %s
    (name, fingerprint, cron_expression, time_zone, job_name, queue,
     misfire_grace_ms, overlap_policy, next_run_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
ON CONFLICT (name) DO NOTHING`, store.table)
	_, err = tx.ExecContext(ctx, query,
		definition.Name(), definition.Fingerprint(), definition.Expression(), definition.TimeZone(),
		definition.JobName(), definition.Queue(), definition.MisfireGrace().Milliseconds(), overlapPolicy,
		cursor, now,
	)
	if err != nil {
		return fmt.Errorf("schedule/postgres: initialize %s: %w", definition.Name(), err)
	}
	return nil
}

func (store *Store) lock(ctx context.Context, tx *sql.Tx, name string) (durableSchedule, bool, error) {
	return store.find(ctx, tx, name, " FOR UPDATE SKIP LOCKED")
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (store *Store) find(ctx context.Context, executor rowQuerier, name, suffix string) (durableSchedule, bool, error) {
	query := fmt.Sprintf(`SELECT name, fingerprint, cron_expression, time_zone, job_name, queue,
       misfire_grace_ms, overlap_policy, next_run_at,
       last_evaluated_at, last_cursor_at, last_occurrence_at,
       COALESCE(last_job_id, ''), COALESCE(last_outcome, ''),
       created_at, updated_at
FROM %s WHERE name = $1%s`, store.table, suffix)
	var stored durableSchedule
	var graceMS int64
	err := executor.QueryRowContext(ctx, query, name).Scan(
		&stored.name, &stored.fingerprint, &stored.expression, &stored.timeZone,
		&stored.jobName, &stored.queue, &graceMS, &stored.overlapPolicy, &stored.nextRunAt,
		&stored.lastEvaluatedAt, &stored.lastCursorAt, &stored.lastOccurrenceAt,
		&stored.lastJobID, &stored.lastOutcome, &stored.createdAt, &stored.updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return durableSchedule{}, false, nil
	}
	if err != nil {
		return durableSchedule{}, false, fmt.Errorf("schedule/postgres: find %s: %w", name, err)
	}
	stored.misfireGrace = time.Duration(graceMS) * time.Millisecond
	return stored, true, nil
}

func (store *Store) exists(ctx context.Context, executor rowQuerier, name string) (bool, error) {
	query := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM %s WHERE name = $1)`, store.table)
	var exists bool
	if err := executor.QueryRowContext(ctx, query, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("schedule/postgres: inspect contention for %s: %w", name, err)
	}
	return exists, nil
}

func databaseClock(ctx context.Context, executor rowQuerier) (time.Time, error) {
	var now time.Time
	if err := executor.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("schedule/postgres: read database clock: %w", err)
	}
	return now.UTC(), nil
}

func nextAfter(definition schedule.Definition, after time.Time) (time.Time, error) {
	next := definition.Next(after).UTC()
	if next.IsZero() || !next.After(after) || !next.Equal(next.Truncate(time.Minute)) {
		return time.Time{}, fmt.Errorf("schedule/postgres: %s returned an invalid next occurrence", definition.Name())
	}
	return next, nil
}

// latestOccurrence performs O(log(minutes)) cron evaluations even at the
// maximum 365-day grace. For a UTC-minute candidate c, Next(c-1ns) <= end is
// true through the final matching minute and false afterward.
func latestOccurrence(next func(time.Time) time.Time, start, end time.Time) (time.Time, bool, error) {
	if next == nil || start.IsZero() || end.IsZero() || start.After(end) {
		return time.Time{}, false, fmt.Errorf("invalid occurrence search")
	}
	start = start.UTC()
	end = end.UTC()
	lower := start.Truncate(time.Minute)
	if lower.Before(start) {
		lower = lower.Add(time.Minute)
	}
	upper := end.Truncate(time.Minute)
	if lower.After(upper) {
		return time.Time{}, false, nil
	}
	minutes := int64(upper.Sub(lower) / time.Minute)
	low, high, best := int64(0), minutes, int64(-1)
	for low <= high {
		middle := low + (high-low)/2
		candidate := lower.Add(time.Duration(middle) * time.Minute)
		occurrence := next(candidate.Add(-time.Nanosecond)).UTC()
		if occurrence.IsZero() {
			high = middle - 1
			continue
		}
		if occurrence.Before(candidate) || !occurrence.Equal(occurrence.Truncate(time.Minute)) {
			return time.Time{}, false, fmt.Errorf("next occurrence is not a monotone whole UTC minute")
		}
		if occurrence.After(end) {
			high = middle - 1
			continue
		}
		best = middle
		low = middle + 1
	}
	if best < 0 {
		return time.Time{}, false, nil
	}
	candidate := lower.Add(time.Duration(best) * time.Minute)
	occurrence := next(candidate.Add(-time.Nanosecond)).UTC()
	if occurrence.IsZero() || occurrence.Before(start) || occurrence.After(end) ||
		occurrence.Before(candidate) || !occurrence.Equal(occurrence.Truncate(time.Minute)) {
		return time.Time{}, false, fmt.Errorf("next occurrence failed final inclusive-window validation")
	}
	return occurrence, true, nil
}

func (store *Store) advance(
	ctx context.Context,
	tx *sql.Tx,
	stored durableSchedule,
	nextRunAt time.Time,
	evaluatedAt time.Time,
	occurrenceAt time.Time,
	jobID job.ID,
	outcome schedule.Outcome,
) error {
	var occurrence any
	if !occurrenceAt.IsZero() {
		occurrence = occurrenceAt
	}
	var durableJobID any
	if jobID != "" {
		durableJobID = string(jobID)
	}
	query := fmt.Sprintf(`UPDATE %s
SET next_run_at = $3,
    last_evaluated_at = $4,
    last_cursor_at = $5,
    last_occurrence_at = $6,
    last_job_id = $7,
    last_outcome = $8,
    updated_at = $4
WHERE name = $1 AND fingerprint = $2 AND next_run_at = $5`, store.table)
	result, err := tx.ExecContext(ctx, query,
		stored.name, stored.fingerprint, nextRunAt, evaluatedAt, stored.nextRunAt,
		occurrence, durableJobID, string(outcome),
	)
	if err != nil {
		return fmt.Errorf("schedule/postgres: advance %s: %w", stored.name, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("schedule/postgres: confirm advance %s: %w", stored.name, err)
	}
	if affected != 1 {
		return fmt.Errorf("schedule/postgres: advance %s affected %d rows", stored.name, affected)
	}
	return nil
}

func (stored durableSchedule) matches(definition schedule.Definition) error {
	if stored.fingerprint != definition.Fingerprint() ||
		stored.expression != definition.Expression() ||
		stored.timeZone != definition.TimeZone() ||
		stored.jobName != definition.JobName() ||
		stored.queue != definition.Queue() ||
		stored.misfireGrace != definition.MisfireGrace() ||
		stored.overlapPolicy != overlapPolicy {
		return fmt.Errorf("%w: %s stores fingerprint %s, code has %s", schedule.ErrDefinitionConflict, definition.Name(), stored.fingerprint, definition.Fingerprint())
	}
	return nil
}

func statusFromDefinition(definition schedule.Definition) schedule.Status {
	return schedule.Status{
		Name: definition.Name(), Expression: definition.Expression(), TimeZone: definition.TimeZone(),
		JobName: definition.JobName(), Queue: definition.Queue(), MisfireGrace: definition.MisfireGrace(),
		Fingerprint: definition.Fingerprint(),
	}
}

func (stored durableSchedule) status(definition schedule.Definition) schedule.Status {
	status := statusFromDefinition(definition)
	status.NextRunAt = stored.nextRunAt.UTC()
	status.LastEvaluatedAt = nullTimePointer(stored.lastEvaluatedAt)
	status.LastCursorAt = nullTimePointer(stored.lastCursorAt)
	status.LastOccurrenceAt = nullTimePointer(stored.lastOccurrenceAt)
	status.LastJobID = job.ID(stored.lastJobID.String)
	status.LastOutcome = schedule.Outcome(stored.lastOutcome.String)
	status.CreatedAt = timePointer(stored.createdAt)
	status.UpdatedAt = timePointer(stored.updatedAt)
	return status
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	copy := value.Time.UTC()
	return &copy
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value.UTC()
	return &copy
}
