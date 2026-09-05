package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ShanilKoshitha/goforge/job"
)

// ListFailed returns newest payload-free failure summaries first.
func (store *Store) ListFailed(ctx context.Context, limit int) ([]job.FailedJob, error) {
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("job/postgres: failed-job limit must be between 1 and 1000")
	}
	query := fmt.Sprintf(`SELECT id::text, queue, name, priority, attempts, max_attempts,
	       timeout_ms, array_to_json(backoff_ms)::text, COALESCE(dedup_key, ''), failure_kind,
       failure_message, created_at, failed_at
FROM %s ORDER BY failed_at DESC, id LIMIT $1`, store.failedTable)
	rows, err := store.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, wrap("list failed", err)
	}
	defer rows.Close()
	failed := make([]job.FailedJob, 0)
	for rows.Next() {
		var item job.FailedJob
		var id string
		var timeoutMS int64
		var backoffJSON string
		if err := rows.Scan(
			&id, &item.Queue, &item.Name, &item.Priority, &item.Attempts,
			&item.MaxAttempts, &timeoutMS, &backoffJSON, &item.DedupKey,
			&item.FailureKind, &item.FailureMessage, &item.CreatedAt, &item.FailedAt,
		); err != nil {
			return nil, wrap("scan failed", err)
		}
		item.ID = job.ID(id)
		item.Timeout = time.Duration(timeoutMS) * time.Millisecond
		item.Backoff, err = decodeDurations(backoffJSON)
		if err != nil {
			return nil, wrap("scan failed backoff", err)
		}
		if err := store.validateFailed(item, nil); err != nil {
			return nil, wrap("scan failed", err)
		}
		failed = append(failed, item)
	}
	if err := rows.Err(); err != nil {
		return nil, wrap("iterate failed", err)
	}
	return failed, nil
}

// FindFailed explicitly retrieves one terminal job including its payload.
func (store *Store) FindFailed(ctx context.Context, id job.ID) (job.FailedJobDetail, bool, error) {
	query := fmt.Sprintf(`SELECT id::text, queue, name, payload, priority, attempts, max_attempts,
	       timeout_ms, array_to_json(backoff_ms)::text, COALESCE(dedup_key, ''), failure_kind,
	       failure_message, created_at, failed_at
FROM %s WHERE id = $1::uuid`, store.failedTable)
	var detail job.FailedJobDetail
	var scannedID string
	var payload []byte
	var timeoutMS int64
	var backoffJSON string
	err := store.db.QueryRowContext(ctx, query, string(id)).Scan(
		&scannedID, &detail.Queue, &detail.Name, &payload, &detail.Priority,
		&detail.Attempts, &detail.MaxAttempts, &timeoutMS, &backoffJSON,
		&detail.DedupKey, &detail.FailureKind, &detail.FailureMessage,
		&detail.CreatedAt, &detail.FailedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return job.FailedJobDetail{}, false, nil
	}
	if err != nil {
		return job.FailedJobDetail{}, false, wrap("find failed", err)
	}
	detail.ID = job.ID(scannedID)
	detail.Payload = append([]byte(nil), payload...)
	detail.Timeout = time.Duration(timeoutMS) * time.Millisecond
	detail.Backoff, err = decodeDurations(backoffJSON)
	if err != nil {
		return job.FailedJobDetail{}, false, wrap("scan failed backoff", err)
	}
	if err := store.validateFailed(detail.FailedJob, detail.Payload); err != nil {
		return job.FailedJobDetail{}, false, wrap("scan failed", err)
	}
	return detail, true, nil
}

func (store *Store) validateFailed(item job.FailedJob, payload []byte) error {
	if len(item.Queue) == 0 || len(item.Queue) > job.MaxQueueBytes || !queuePattern.MatchString(item.Queue) ||
		len(item.Name) == 0 || len(item.Name) > job.MaxNameBytes || !jobNamePattern.MatchString(item.Name) ||
		len(item.DedupKey) > job.MaxDedupKeyBytes || item.Attempts < 0 || item.MaxAttempts < 1 || item.MaxAttempts > 1000 ||
		item.Attempts > item.MaxAttempts || item.Timeout < time.Millisecond || item.Timeout > 24*time.Hour ||
		len(item.Backoff) == 0 || len(item.Backoff) > 1000 || len(item.FailureKind) == 0 || len(item.FailureKind) > 64 ||
		len(item.FailureMessage) > store.maxError || item.CreatedAt.IsZero() || item.FailedAt.IsZero() {
		return fmt.Errorf("invalid durable failure")
	}
	for _, delay := range item.Backoff {
		if delay < 0 || delay > 30*24*time.Hour {
			return fmt.Errorf("invalid durable failure backoff")
		}
	}
	if payload != nil && (len(payload) == 0 || len(payload) > store.maxPayload || !json.Valid(payload)) {
		return fmt.Errorf("invalid durable failure payload")
	}
	return nil
}

// RetryFailed atomically restores one failed job with attempts reset.
func (store *Store) RetryFailed(ctx context.Context, id job.ID) (bool, error) {
	query := fmt.Sprintf(`WITH db_clock AS (
    SELECT clock_timestamp() AS now
), removed AS (
    DELETE FROM %s WHERE id = $1::uuid RETURNING *
)
INSERT INTO %s
    (id, queue, name, payload, priority, available_at, attempts, max_attempts,
     timeout_ms, backoff_ms, lease_generation, dedup_key, created_at, updated_at)
SELECT removed.id, removed.queue, removed.name, removed.payload, removed.priority,
       db_clock.now, 0, removed.max_attempts, removed.timeout_ms, removed.backoff_ms,
       0, removed.dedup_key, removed.created_at, db_clock.now
FROM removed, db_clock
RETURNING id`, store.failedTable, store.jobsTable)
	var restored string
	err := store.db.QueryRowContext(ctx, query, string(id)).Scan(&restored)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, wrap("retry failed", err)
	}
	return true, nil
}

// ForgetFailed permanently deletes one terminal failure.
func (store *Store) ForgetFailed(ctx context.Context, id job.ID) (bool, error) {
	query := fmt.Sprintf(`DELETE FROM %s WHERE id = $1::uuid`, store.failedTable)
	result, err := store.db.ExecContext(ctx, query, string(id))
	if err != nil {
		return false, wrap("forget failed", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, wrap("count forgotten", err)
	}
	return count == 1, nil
}

var _ job.FailedJobInspector = (*Store)(nil)
