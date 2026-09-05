// Package database adds small, explicit helpers around database/sql.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Transaction commits when work succeeds and rolls back on error or panic.
// The callback must not commit or roll back the transaction itself.
func Transaction(ctx context.Context, db *sql.DB, options *sql.TxOptions, work func(*sql.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, options)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = tx.Rollback()
			panic(recovered)
		}
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("rollback transaction: %w", rollbackErr))
			}
		}
	}()
	if err = work(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
