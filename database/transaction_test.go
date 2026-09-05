package database_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ShanilKoshitha/goforge/database"
)

var driverSequence atomic.Uint64

type transactionState struct {
	mu          sync.Mutex
	commits     int
	rollbacks   int
	commitErr   error
	rollbackErr error
}

type transactionDriver struct{ state *transactionState }

func (driverValue transactionDriver) Open(string) (driver.Conn, error) {
	return &transactionConn{state: driverValue.state}, nil
}

type transactionConn struct{ state *transactionState }

func (connection *transactionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported")
}
func (connection *transactionConn) Close() error { return nil }
func (connection *transactionConn) Begin() (driver.Tx, error) {
	return connection.BeginTx(context.Background(), driver.TxOptions{})
}
func (connection *transactionConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return &transactionTx{state: connection.state}, nil
}

type transactionTx struct{ state *transactionState }

func (transaction *transactionTx) Commit() error {
	transaction.state.mu.Lock()
	defer transaction.state.mu.Unlock()
	transaction.state.commits++
	return transaction.state.commitErr
}

func (transaction *transactionTx) Rollback() error {
	transaction.state.mu.Lock()
	defer transaction.state.mu.Unlock()
	transaction.state.rollbacks++
	return transaction.state.rollbackErr
}

func openTransactionDB(t *testing.T, state *transactionState) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("goforge-transaction-test-%d", driverSequence.Add(1))
	sql.Register(name, transactionDriver{state: state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestTransactionCommitsSuccessfulWork(t *testing.T) {
	state := &transactionState{}
	err := database.Transaction(context.Background(), openTransactionDB(t, state), nil, func(*sql.Tx) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if state.commits != 1 || state.rollbacks != 0 {
		t.Fatalf("commits=%d rollbacks=%d", state.commits, state.rollbacks)
	}
}

func TestTransactionReturnsCommitFailure(t *testing.T) {
	commitErr := errors.New("commit failed")
	state := &transactionState{commitErr: commitErr}
	err := database.Transaction(context.Background(), openTransactionDB(t, state), nil, func(*sql.Tx) error { return nil })
	if !errors.Is(err, commitErr) {
		t.Fatalf("expected commit failure, got %v", err)
	}
	if state.commits != 1 {
		t.Fatalf("expected one commit, got %d", state.commits)
	}
}

func TestTransactionReturnsWorkAndRollbackFailures(t *testing.T) {
	workErr := errors.New("work failed")
	rollbackErr := errors.New("rollback failed")
	state := &transactionState{rollbackErr: rollbackErr}
	err := database.Transaction(context.Background(), openTransactionDB(t, state), nil, func(*sql.Tx) error { return workErr })
	if !errors.Is(err, workErr) || !errors.Is(err, rollbackErr) {
		t.Fatalf("expected joined work and rollback failures, got %v", err)
	}
	if state.rollbacks != 1 {
		t.Fatalf("expected one rollback, got %d", state.rollbacks)
	}
}

func TestTransactionRollsBackAndRepanics(t *testing.T) {
	state := &transactionState{}
	deferred := false
	func() {
		defer func() {
			deferred = true
			if recovered := recover(); recovered != "boom" {
				t.Fatalf("expected original panic, got %v", recovered)
			}
		}()
		_ = database.Transaction(context.Background(), openTransactionDB(t, state), nil, func(*sql.Tx) error {
			panic("boom")
		})
	}()
	if !deferred || state.rollbacks != 1 {
		t.Fatalf("panic was not propagated after rollback; rollbacks=%d", state.rollbacks)
	}
}
