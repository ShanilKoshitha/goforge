package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ShanilKoshitha/goforge/session"
	"github.com/jackc/pgx/v5/pgconn"
)

type PostgresSessionStore struct{ DB *sql.DB }

var _ session.Store = (*PostgresSessionStore)(nil)

func NewPostgresSessionStore(db *sql.DB) *PostgresSessionStore {
	return &PostgresSessionStore{DB: db}
}

func (store *PostgresSessionStore) Get(ctx context.Context, id string) ([]byte, error) {
	var payload []byte
	err := store.DB.QueryRowContext(ctx,
		"SELECT payload FROM sessions WHERE id = $1 AND expires_at > NOW()", id,
	).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		if _, cleanupErr := store.DB.ExecContext(ctx, "DELETE FROM sessions WHERE id = $1 AND expires_at <= NOW()", id); cleanupErr != nil {
			return nil, fmt.Errorf("remove expired PostgreSQL session: %w", cleanupErr)
		}
		return nil, session.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load PostgreSQL session: %w", err)
	}
	return payload, nil
}

func (store *PostgresSessionStore) Create(ctx context.Context, id string, payload []byte, expiresAt time.Time) (err error) {
	transaction, err := store.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin PostgreSQL session creation: %w", err)
	}
	defer func() {
		if rollbackErr := transaction.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) && err == nil {
			err = fmt.Errorf("rollback PostgreSQL session creation: %w", rollbackErr)
		}
	}()

	if _, err := transaction.ExecContext(ctx,
		"DELETE FROM sessions WHERE id = $1 AND expires_at <= NOW()", id,
	); err != nil {
		return fmt.Errorf("remove expired PostgreSQL session: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
		DELETE FROM sessions WHERE id IN (
			SELECT id FROM sessions
			WHERE expires_at <= NOW() AND id <> $1
			ORDER BY expires_at
			LIMIT 100
		)
	`, id); err != nil {
		return fmt.Errorf("prune expired PostgreSQL sessions: %w", err)
	}
	if _, err := transaction.ExecContext(ctx,
		"INSERT INTO sessions (id, payload, expires_at) VALUES ($1, $2, $3)",
		id, payload, expiresAt,
	); err != nil {
		return postgresSessionWriteError("create", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit PostgreSQL session creation: %w", err)
	}
	return nil
}

func (store *PostgresSessionStore) Update(ctx context.Context, id string, payload []byte, expiresAt time.Time) error {
	result, err := store.DB.ExecContext(ctx,
		"UPDATE sessions SET payload = $2, expires_at = $3 WHERE id = $1 AND expires_at > NOW()",
		id, payload, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("update PostgreSQL session: %w", err)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect PostgreSQL session update: %w", err)
	}
	if updated == 0 {
		if _, cleanupErr := store.DB.ExecContext(ctx, "DELETE FROM sessions WHERE id = $1 AND expires_at <= NOW()", id); cleanupErr != nil {
			return fmt.Errorf("remove expired PostgreSQL session: %w", cleanupErr)
		}
		return fmt.Errorf("update PostgreSQL session: %w", session.ErrNotFound)
	}
	return nil
}

func (store *PostgresSessionStore) Rotate(ctx context.Context, oldID, newID string, payload []byte, expiresAt time.Time) (err error) {
	transaction, err := store.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin PostgreSQL session rotation: %w", err)
	}
	defer func() {
		if rollbackErr := transaction.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) && err == nil {
			err = fmt.Errorf("rollback PostgreSQL session rotation: %w", rollbackErr)
		}
	}()

	result, err := transaction.ExecContext(ctx,
		"DELETE FROM sessions WHERE id = $1 AND expires_at > NOW()", oldID,
	)
	if err != nil {
		return fmt.Errorf("remove old PostgreSQL session: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect PostgreSQL session rotation: %w", err)
	}
	if removed == 0 {
		return fmt.Errorf("rotate PostgreSQL session: %w", session.ErrNotFound)
	}
	if _, err := transaction.ExecContext(ctx,
		"INSERT INTO sessions (id, payload, expires_at) VALUES ($1, $2, $3)",
		newID, payload, expiresAt,
	); err != nil {
		return postgresSessionWriteError("rotate", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit PostgreSQL session rotation: %w", err)
	}
	return nil
}

func (store *PostgresSessionStore) Delete(ctx context.Context, id string) error {
	_, err := store.DB.ExecContext(ctx, "DELETE FROM sessions WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete PostgreSQL session: %w", err)
	}
	return nil
}

func postgresSessionWriteError(action string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return fmt.Errorf("%s PostgreSQL session: %w", action, session.ErrExists)
	}
	return fmt.Errorf("%s PostgreSQL session: %w", action, err)
}
