package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/ShanilKoshitha/goforge/job"
)

var identifierPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
var jobNamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*\.v[1-9][0-9]*$`)
var queuePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

// Option configures validated PostgreSQL identifiers.
type Option func(*config) error

type config struct {
	jobsTable   string
	failedTable string
	maxPayload  int
	maxError    int
}

// WithLimits changes defensive encoded payload and diagnostic bounds.
func WithLimits(maxPayloadBytes, maxErrorBytes int) Option {
	return func(config *config) error {
		if maxPayloadBytes < 1 || maxErrorBytes < 1 {
			return fmt.Errorf("job/postgres: limits must be positive")
		}
		config.maxPayload = maxPayloadBytes
		config.maxError = maxErrorBytes
		return nil
	}
}

// WithTables selects application-owned table names.
func WithTables(jobsTable, failedTable string) Option {
	return func(config *config) error {
		if !validIdentifier(jobsTable) || !validIdentifier(failedTable) {
			return fmt.Errorf("job/postgres: invalid table names %q and %q", jobsTable, failedTable)
		}
		config.jobsTable = jobsTable
		config.failedTable = failedTable
		return nil
	}
}

// Store owns queue operations but not the supplied database handle.
type Store struct {
	db          *sql.DB
	jobsTable   string
	failedTable string
	maxPayload  int
	maxError    int
}

// New creates a PostgreSQL store. The caller retains database ownership.
func New(db *sql.DB, options ...Option) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("job/postgres: database is required")
	}
	config := config{jobsTable: "goforge_jobs", failedTable: "goforge_failed_jobs", maxPayload: job.DefaultMaxPayloadBytes, maxError: job.DefaultMaxErrorBytes}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("job/postgres: nil option")
		}
		if err := option(&config); err != nil {
			return nil, err
		}
	}
	return &Store{db: db, jobsTable: quote(config.jobsTable), failedTable: quote(config.failedTable), maxPayload: config.maxPayload, maxError: config.maxError}, nil
}

func validIdentifier(value string) bool {
	return len(value) > 0 && len(value) <= 63 && identifierPattern.MatchString(value)
}

func quote(value string) string { return `"` + value + `"` }

// Enqueue inserts through the explicitly supplied DB or transaction.
func (store *Store) Enqueue(ctx context.Context, executor job.Executor, request job.EnqueueRequest) (job.DispatchResult, error) {
	if err := job.ValidateExecutor(executor); err != nil {
		return job.DispatchResult{}, fmt.Errorf("enqueue: %w", err)
	}
	if len(request.Name) == 0 || len(request.Name) > job.MaxNameBytes || !jobNamePattern.MatchString(request.Name) ||
		len(request.Queue) == 0 || len(request.Queue) > job.MaxQueueBytes || !queuePattern.MatchString(request.Queue) {
		return job.DispatchResult{}, fmt.Errorf("enqueue: invalid name or queue")
	}
	if len(request.Payload) == 0 || len(request.Payload) > store.maxPayload || !json.Valid(request.Payload) {
		return job.DispatchResult{}, fmt.Errorf("enqueue: invalid or oversized payload")
	}
	if len(request.DedupKey) > job.MaxDedupKeyBytes {
		return job.DispatchResult{}, fmt.Errorf("enqueue: oversized deduplication key")
	}
	if request.MaxAttempts < 1 || request.MaxAttempts > 1000 || request.Timeout < time.Millisecond || request.Timeout > 24*time.Hour ||
		len(request.Backoff) == 0 || len(request.Backoff) > 1000 || request.Delay < 0 || request.Delay > 365*24*time.Hour ||
		(request.Delay > 0 && request.Delay < time.Millisecond) || (request.At != nil && request.At.IsZero()) ||
		(request.At != nil && request.Delay != 0) || request.Priority < -32768 || request.Priority > 32767 {
		return job.DispatchResult{}, fmt.Errorf("enqueue: invalid execution policy")
	}
	for _, delay := range request.Backoff {
		if delay < 0 || delay > 30*24*time.Hour || (delay > 0 && delay < time.Millisecond) {
			return job.DispatchResult{}, fmt.Errorf("enqueue: invalid backoff")
		}
	}
	backoff := durationsToMilliseconds(request.Backoff)
	var at any
	if request.At != nil {
		at = *request.At
	}
	arguments := []any{
		string(request.ID), request.Queue, request.Name, []byte(request.Payload), request.Priority,
		request.Delay.Milliseconds(), at, request.MaxAttempts, request.Timeout.Milliseconds(), backoff,
	}
	if request.DedupKey == "" {
		query := fmt.Sprintf(`WITH db_clock AS (SELECT clock_timestamp() AS now)
INSERT INTO %s
    (id, queue, name, payload, priority, available_at, attempts, max_attempts,
     timeout_ms, backoff_ms, lease_generation, created_at, updated_at)
SELECT $1::uuid, $2, $3, $4::jsonb, $5,
       CASE WHEN $7::timestamptz IS NOT NULL THEN $7::timestamptz
            ELSE db_clock.now + ($6::bigint * interval '1 millisecond') END,
       0, $8, $9, $10::bigint[], 0, db_clock.now, db_clock.now
FROM db_clock
RETURNING id::text`, store.jobsTable)
		var id string
		if err := executor.QueryRowContext(ctx, query, arguments...).Scan(&id); err != nil {
			return job.DispatchResult{}, fmt.Errorf("enqueue: %w", err)
		}
		return job.DispatchResult{ID: job.ID(id), Enqueued: true}, nil
	}
	arguments = append(arguments, request.DedupKey)
	query := fmt.Sprintf(`WITH db_clock AS (SELECT clock_timestamp() AS now)
INSERT INTO %s
    (id, queue, name, payload, priority, available_at, attempts, max_attempts,
     timeout_ms, backoff_ms, lease_generation, dedup_key, created_at, updated_at)
SELECT $1::uuid, $2, $3, $4::jsonb, $5,
       CASE WHEN $7::timestamptz IS NOT NULL THEN $7::timestamptz
            ELSE db_clock.now + ($6::bigint * interval '1 millisecond') END,
       0, $8, $9, $10::bigint[], 0, $11, db_clock.now, db_clock.now
FROM db_clock
ON CONFLICT (queue, name, dedup_key) WHERE dedup_key IS NOT NULL
DO UPDATE SET dedup_key = EXCLUDED.dedup_key
RETURNING id::text`, store.jobsTable)
	var id string
	if err := executor.QueryRowContext(ctx, query, arguments...).Scan(&id); err != nil {
		return job.DispatchResult{}, fmt.Errorf("enqueue deduplicated: %w", err)
	}
	return job.DispatchResult{ID: job.ID(id), Enqueued: id == string(request.ID)}, nil
}

// Claim atomically leases only known, due jobs and returns after the statement commits.
func (store *Store) Claim(ctx context.Context, request job.ClaimRequest) ([]job.Delivery, error) {
	if len(request.Queues) == 0 || len(request.Names) == 0 || request.Limit < 1 || request.Limit > 10000 || request.WorkerID == "" || request.LeaseDuration < time.Millisecond {
		return nil, fmt.Errorf("claim: invalid request")
	}
	if len(request.WorkerID) > 128 {
		return nil, fmt.Errorf("claim: invalid worker id")
	}
	for _, queue := range request.Queues {
		if len(queue) > job.MaxQueueBytes || !queuePattern.MatchString(queue) {
			return nil, fmt.Errorf("claim: invalid queue")
		}
	}
	for _, name := range request.Names {
		if len(name) > job.MaxNameBytes || !jobNamePattern.MatchString(name) {
			return nil, fmt.Errorf("claim: invalid name")
		}
	}
	query := fmt.Sprintf(`WITH db_clock AS (
    SELECT clock_timestamp() AS now
), candidates AS (
    SELECT queued.id
    FROM %s AS queued, db_clock
    WHERE queued.queue = ANY($1::text[])
      AND queued.name = ANY($2::text[])
      AND queued.available_at <= db_clock.now
      AND queued.attempts < queued.max_attempts
    ORDER BY queued.priority DESC, queued.available_at, queued.id
    FOR UPDATE OF queued SKIP LOCKED
    LIMIT $3
)
UPDATE %s AS queued
SET attempts = queued.attempts + 1,
    lease_owner = $4,
    lease_generation = queued.lease_generation + 1,
    leased_at = db_clock.now,
    lease_expires_at = db_clock.now + ($5::bigint * interval '1 millisecond'),
    available_at = db_clock.now + ($5::bigint * interval '1 millisecond'),
    updated_at = db_clock.now
FROM candidates, db_clock
WHERE queued.id = candidates.id
RETURNING queued.id::text, queued.queue, queued.name, queued.payload, queued.priority,
          queued.attempts, queued.max_attempts, queued.timeout_ms,
          array_to_json(queued.backoff_ms)::text,
          COALESCE(queued.dedup_key, ''), queued.available_at, queued.leased_at,
          queued.lease_expires_at, queued.lease_generation`, store.jobsTable, store.jobsTable)
	rows, err := store.db.QueryContext(ctx, query, request.Queues, request.Names, request.Limit, request.WorkerID, request.LeaseDuration.Milliseconds())
	if err != nil {
		return nil, fmt.Errorf("claim: %w", err)
	}
	defer rows.Close()
	deliveries := make([]job.Delivery, 0, request.Limit)
	for rows.Next() {
		var delivery job.Delivery
		var id string
		var payload []byte
		var timeoutMS int64
		var backoffJSON string
		var generation int64
		if err := rows.Scan(
			&id, &delivery.Queue, &delivery.Name, &payload, &delivery.Priority,
			&delivery.Attempt, &delivery.MaxAttempts, &timeoutMS, &backoffJSON,
			&delivery.DedupKey, &delivery.AvailableAt, &delivery.LeasedAt,
			&delivery.LeaseExpires, &generation,
		); err != nil {
			return nil, fmt.Errorf("scan claim: %w", err)
		}
		delivery.ID = job.ID(id)
		delivery.Payload = append([]byte(nil), payload...)
		delivery.Timeout = time.Duration(timeoutMS) * time.Millisecond
		delivery.Backoff, err = decodeDurations(backoffJSON)
		if err != nil {
			return nil, fmt.Errorf("scan claim backoff: %w", err)
		}
		delivery.Lease = job.Lease{JobID: delivery.ID, WorkerID: request.WorkerID, Generation: generation}
		if err := store.validateDelivery(delivery); err != nil {
			return nil, fmt.Errorf("scan claim: %w", err)
		}
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claims: %w", err)
	}
	return deliveries, nil
}

// Heartbeat extends a live fenced lease using the database clock.
func (store *Store) Heartbeat(ctx context.Context, lease job.Lease, duration time.Duration) (bool, error) {
	if err := validateLease(lease); err != nil || duration < time.Millisecond {
		return false, fmt.Errorf("heartbeat: invalid lease or duration")
	}
	query := fmt.Sprintf(`WITH db_clock AS (SELECT clock_timestamp() AS now)
UPDATE %s AS queued
SET lease_expires_at = db_clock.now + ($4::bigint * interval '1 millisecond'),
    available_at = db_clock.now + ($4::bigint * interval '1 millisecond'),
    updated_at = db_clock.now
FROM db_clock
WHERE queued.id = $1::uuid AND queued.lease_owner = $2
  AND queued.lease_generation = $3 AND queued.lease_expires_at > db_clock.now`, store.jobsTable)
	return store.execFenced(ctx, query, lease, duration.Milliseconds())
}

// Ack deletes a successfully completed fenced job.
func (store *Store) Ack(ctx context.Context, lease job.Lease) (bool, error) {
	if err := validateLease(lease); err != nil {
		return false, err
	}
	query := fmt.Sprintf(`DELETE FROM %s
WHERE id = $1::uuid AND lease_owner = $2 AND lease_generation = $3
  AND lease_expires_at > clock_timestamp()`, store.jobsTable)
	return store.execFenced(ctx, query, lease)
}

// Retry releases a fenced job at a database-clock-relative delay.
func (store *Store) Retry(ctx context.Context, request job.RetryRequest) (bool, error) {
	if err := validateLease(request.Lease); err != nil || request.Delay < 0 || len(request.ErrorKind) > 64 || len(request.Error) > store.maxError {
		return false, fmt.Errorf("retry: invalid request")
	}
	query := fmt.Sprintf(`WITH db_clock AS (SELECT clock_timestamp() AS now)
UPDATE %s AS queued
SET available_at = db_clock.now + ($4::bigint * interval '1 millisecond'),
    lease_owner = NULL, leased_at = NULL, lease_expires_at = NULL,
    last_error_kind = $5, last_error = $6, updated_at = db_clock.now
FROM db_clock
WHERE queued.id = $1::uuid AND queued.lease_owner = $2
  AND queued.lease_generation = $3 AND queued.lease_expires_at > db_clock.now`, store.jobsTable)
	return store.execFenced(ctx, query, request.Lease, request.Delay.Milliseconds(), request.ErrorKind, request.Error)
}

// Release immediately releases a fenced job without changing its attempt count.
func (store *Store) Release(ctx context.Context, lease job.Lease) (bool, error) {
	if err := validateLease(lease); err != nil {
		return false, err
	}
	query := fmt.Sprintf(`WITH db_clock AS (SELECT clock_timestamp() AS now)
UPDATE %s AS queued
SET available_at = db_clock.now, lease_owner = NULL, leased_at = NULL,
    lease_expires_at = NULL, updated_at = db_clock.now
FROM db_clock
WHERE queued.id = $1::uuid AND queued.lease_owner = $2
  AND queued.lease_generation = $3 AND queued.lease_expires_at > db_clock.now`, store.jobsTable)
	return store.execFenced(ctx, query, lease)
}

// Fail atomically moves a fenced job to terminal failures.
func (store *Store) Fail(ctx context.Context, request job.FailureRequest) (bool, error) {
	if err := validateLease(request.Lease); err != nil || len(request.Kind) == 0 || len(request.Kind) > 64 || len(request.Error) > store.maxError {
		return false, fmt.Errorf("fail: invalid request")
	}
	query := fmt.Sprintf(`WITH db_clock AS (
    SELECT clock_timestamp() AS now
), removed AS (
    DELETE FROM %s AS queued USING db_clock
    WHERE queued.id = $1::uuid AND queued.lease_owner = $2
      AND queued.lease_generation = $3 AND queued.lease_expires_at > db_clock.now
    RETURNING queued.*
)
INSERT INTO %s
    (id, queue, name, payload, priority, attempts, max_attempts, timeout_ms,
     backoff_ms, dedup_key, failure_kind, failure_message, created_at, failed_at)
SELECT removed.id, removed.queue, removed.name, removed.payload, removed.priority,
       removed.attempts, removed.max_attempts, removed.timeout_ms, removed.backoff_ms,
       removed.dedup_key, $4, $5, removed.created_at, db_clock.now
FROM removed, db_clock
RETURNING id`, store.jobsTable, store.failedTable)
	var id string
	err := store.db.QueryRowContext(ctx, query, string(request.Lease.JobID), request.Lease.WorkerID, request.Lease.Generation, request.Kind, request.Error).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("fail: %w", err)
	}
	return true, nil
}

// FailExhausted atomically moves expired final-attempt leases to failures.
func (store *Store) FailExhausted(ctx context.Context, queues, names []string, limit int) ([]job.FailedJob, error) {
	if len(queues) == 0 || len(names) == 0 || limit < 1 || limit > 10000 {
		return nil, fmt.Errorf("fail exhausted: invalid request")
	}
	query := fmt.Sprintf(`WITH db_clock AS (
    SELECT clock_timestamp() AS now
), candidates AS (
    SELECT queued.id
    FROM %s AS queued, db_clock
    WHERE queued.queue = ANY($1::text[]) AND queued.name = ANY($2::text[])
      AND queued.lease_owner IS NOT NULL AND queued.available_at <= db_clock.now
      AND queued.attempts >= queued.max_attempts
    ORDER BY queued.available_at, queued.id
    FOR UPDATE OF queued SKIP LOCKED
    LIMIT $3
), removed AS (
    DELETE FROM %s AS queued USING candidates
    WHERE queued.id = candidates.id
    RETURNING queued.*
), inserted AS (
    INSERT INTO %s
        (id, queue, name, payload, priority, attempts, max_attempts, timeout_ms,
         backoff_ms, dedup_key, failure_kind, failure_message, created_at, failed_at)
    SELECT removed.id, removed.queue, removed.name, removed.payload, removed.priority,
           removed.attempts, removed.max_attempts, removed.timeout_ms, removed.backoff_ms,
           removed.dedup_key, 'lease_exhausted', 'lease expired after final attempt',
           removed.created_at, db_clock.now
    FROM removed, db_clock
    RETURNING id, queue, name, attempts
)
SELECT id::text, queue, name, attempts FROM inserted`, store.jobsTable, store.jobsTable, store.failedTable)
	rows, err := store.db.QueryContext(ctx, query, queues, names, limit)
	if err != nil {
		return nil, fmt.Errorf("fail exhausted: %w", err)
	}
	defer rows.Close()
	failed := make([]job.FailedJob, 0)
	for rows.Next() {
		var item job.FailedJob
		var id string
		if err := rows.Scan(&id, &item.Queue, &item.Name, &item.Attempts); err != nil {
			return nil, fmt.Errorf("scan exhausted: %w", err)
		}
		item.ID = job.ID(id)
		item.FailureKind = "lease_exhausted"
		failed = append(failed, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate exhausted: %w", err)
	}
	return failed, nil
}

func (store *Store) execFenced(ctx context.Context, query string, lease job.Lease, extra ...any) (bool, error) {
	arguments := []any{string(lease.JobID), lease.WorkerID, lease.Generation}
	arguments = append(arguments, extra...)
	result, err := store.db.ExecContext(ctx, query, arguments...)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return count == 1, nil
}

func durationsToMilliseconds(values []time.Duration) []int64 {
	result := make([]int64, len(values))
	for index, value := range values {
		result[index] = value.Milliseconds()
	}
	return result
}

func millisecondsToDurations(values []int64) []time.Duration {
	result := make([]time.Duration, len(values))
	for index, value := range values {
		result[index] = time.Duration(value) * time.Millisecond
	}
	return result
}

func decodeDurations(encoded string) ([]time.Duration, error) {
	var values []int64
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		return nil, err
	}
	return millisecondsToDurations(values), nil
}

func validateLease(lease job.Lease) error {
	if lease.JobID == "" || lease.WorkerID == "" || len(lease.WorkerID) > 128 || lease.Generation < 1 {
		return fmt.Errorf("job/postgres: invalid lease")
	}
	return nil
}

func (store *Store) validateDelivery(delivery job.Delivery) error {
	if len(delivery.Queue) == 0 || len(delivery.Queue) > job.MaxQueueBytes || !queuePattern.MatchString(delivery.Queue) ||
		len(delivery.Name) == 0 || len(delivery.Name) > job.MaxNameBytes || !jobNamePattern.MatchString(delivery.Name) ||
		len(delivery.Payload) == 0 || len(delivery.Payload) > store.maxPayload || !json.Valid(delivery.Payload) ||
		len(delivery.DedupKey) > job.MaxDedupKeyBytes || delivery.Attempt < 1 ||
		delivery.MaxAttempts < 1 || delivery.MaxAttempts > 1000 || delivery.Attempt > delivery.MaxAttempts ||
		delivery.Timeout < time.Millisecond || delivery.Timeout > 24*time.Hour ||
		len(delivery.Backoff) == 0 || len(delivery.Backoff) > 1000 ||
		delivery.AvailableAt.IsZero() || delivery.LeasedAt.IsZero() || delivery.LeaseExpires.IsZero() ||
		!delivery.LeaseExpires.After(delivery.LeasedAt) {
		return fmt.Errorf("invalid durable delivery")
	}
	for _, delay := range delivery.Backoff {
		if delay < 0 || delay > 30*24*time.Hour {
			return fmt.Errorf("invalid durable backoff")
		}
	}
	return validateLease(delivery.Lease)
}

var _ job.Store = (*Store)(nil)
var _ job.AdminStore = (*Store)(nil)

// Preserve errors.Is behavior across PostgreSQL wrappers.
func wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("job/postgres: %s: %w", operation, err)
}

var _ = errors.Is
