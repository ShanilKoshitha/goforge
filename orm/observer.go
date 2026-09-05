package orm

import (
	"context"
	"database/sql"
)

// StatementEvent distinguishes observed queries from observed executions.
type StatementEvent uint8

const (
	StatementQuery StatementEvent = iota
	StatementExec
)

// StatementObserver receives an immutable statement immediately before it is
// sent to the wrapped Executor. It is intended for query budgets, tracing, and
// tests; it cannot change execution.
type StatementObserver func(context.Context, StatementEvent, Statement)

// ObserveExecutor wraps an Executor without changing its database/sql surface.
// A nil observer returns the original executor.
func ObserveExecutor(executor Executor, observer StatementObserver) Executor {
	if observer == nil {
		return executor
	}
	return &observedExecutor{executor: executor, observer: observer}
}

type observedExecutor struct {
	executor Executor
	observer StatementObserver
}

func (executor *observedExecutor) validORMExecutor() bool {
	return executor != nil && validateExecutor(executor.executor, false) == nil
}

func (executor *observedExecutor) carriesORMTransaction() bool {
	if executor == nil {
		return false
	}
	switch typed := executor.executor.(type) {
	case *sql.Tx:
		return typed != nil
	case transactionCarrier:
		return typed.carriesORMTransaction()
	default:
		return false
	}
}

func (executor *observedExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	executor.observer(ctx, StatementExec, newStatement(query, args))
	return executor.executor.ExecContext(ctx, query, args...)
}

func (executor *observedExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	executor.observer(ctx, StatementQuery, newStatement(query, args))
	return executor.executor.QueryContext(ctx, query, args...)
}

func (executor *observedExecutor) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	executor.observer(ctx, StatementQuery, newStatement(query, args))
	return executor.executor.QueryRowContext(ctx, query, args...)
}
