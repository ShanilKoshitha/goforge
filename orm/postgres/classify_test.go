package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ShanilKoshitha/goforge/orm"
	ormpostgres "github.com/ShanilKoshitha/goforge/orm/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestClassifyConstraintSQLStates(t *testing.T) {
	tests := []struct {
		name       string
		code       string
		constraint string
		column     string
		kind       orm.ConstraintKind
		target     error
		wantName   string
	}{
		{name: "unique", code: "23505", constraint: "users_email_key", kind: orm.ConstraintUnique, target: orm.ErrUnique, wantName: "users_email_key"},
		{name: "foreign key", code: "23503", constraint: "issues_user_id_fkey", kind: orm.ConstraintForeignKey, target: orm.ErrForeignKey, wantName: "issues_user_id_fkey"},
		{name: "not null constraint", code: "23502", constraint: "users_name_required", column: "name", kind: orm.ConstraintNotNull, target: orm.ErrNotNull, wantName: "users_name_required"},
		{name: "not null column fallback", code: "23502", column: "name", kind: orm.ConstraintNotNull, target: orm.ErrNotNull, wantName: "name"},
		{name: "check", code: "23514", constraint: "issues_name_check", kind: orm.ConstraintCheck, target: orm.ErrCheck, wantName: "issues_name_check"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			native := sensitivePostgresError(test.code)
			native.ConstraintName = test.constraint
			native.ColumnName = test.column
			classified := ormpostgres.Classify(native)

			if !errors.Is(classified, test.target) {
				t.Fatalf("errors.Is(%v) = false", test.target)
			}
			if !errors.Is(classified, native) {
				t.Fatal("native PostgreSQL cause was not preserved")
			}
			var constraintError *orm.ConstraintError
			if !errors.As(classified, &constraintError) {
				t.Fatalf("classified error type = %T", classified)
			}
			if constraintError.Kind != test.kind || constraintError.Constraint != test.wantName {
				t.Fatalf("constraint = %q kind=%q", constraintError.Constraint, constraintError.Kind)
			}
			assertNativeAndSafe(t, classified, native)
		})
	}
}

func TestClassifyTransientSQLStates(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		kind   orm.TransientKind
		target error
	}{
		{name: "serialization", code: "40001", kind: orm.TransientSerialization, target: orm.ErrSerialization},
		{name: "deadlock", code: "40P01", kind: orm.TransientDeadlock, target: orm.ErrDeadlock},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			native := sensitivePostgresError(test.code)
			classified := ormpostgres.Classify(native)

			if !errors.Is(classified, test.target) || !errors.Is(classified, native) {
				t.Fatalf("classification did not preserve targets: %v", classified)
			}
			var transient *orm.TransientError
			if !errors.As(classified, &transient) || transient.Kind != test.kind {
				t.Fatalf("transient classification = %#v", transient)
			}
			assertNativeAndSafe(t, classified, native)
		})
	}
}

func TestClassifyPreservesPersistenceContextAsOutermostError(t *testing.T) {
	native := sensitivePostgresError("23505")
	native.ConstraintName = "users_email_key"
	persistence := &orm.PersistenceError{
		Operation: orm.OperationInsert,
		Table:     "users",
		Cause:     fmt.Errorf("execute insert: %w", native),
	}

	classified := ormpostgres.Classify(persistence)
	outer, ok := classified.(*orm.PersistenceError)
	if !ok {
		t.Fatalf("outer error = %T, want *orm.PersistenceError", classified)
	}
	if outer.Operation != persistence.Operation || outer.Table != persistence.Table {
		t.Fatalf("persistence context = %q %q", outer.Operation, outer.Table)
	}
	if !errors.Is(classified, orm.ErrUnique) || !errors.Is(classified, native) {
		t.Fatalf("nested classification chain is incomplete: %v", classified)
	}
	var constraintError *orm.ConstraintError
	var foundNative *pgconn.PgError
	if !errors.As(classified, &constraintError) || !errors.As(classified, &foundNative) || foundNative != native {
		t.Fatal("errors.As did not preserve classified and native errors")
	}
	message := classified.Error()
	for _, safeContext := range []string{"insert", "users", "users_email_key", "23505"} {
		if !strings.Contains(message, safeContext) {
			t.Fatalf("message %q lacks safe context %q", message, safeContext)
		}
	}
	assertDisclosureSafe(t, message)
}

func TestClassifyPassesUnknownContextAndNilThrough(t *testing.T) {
	unknown := errors.New("unknown persistence failure")
	unknownPostgres := &pgconn.PgError{Code: "22001", Message: "value too long"}
	wrappedCanceled := fmt.Errorf("query interrupted: %w", context.Canceled)
	wrappedDeadline := fmt.Errorf("query interrupted: %w", context.DeadlineExceeded)

	tests := []struct {
		name string
		err  error
	}{
		{name: "nil", err: nil},
		{name: "unknown", err: unknown},
		{name: "unknown PostgreSQL", err: unknownPostgres},
		{name: "canceled", err: context.Canceled},
		{name: "wrapped canceled", err: wrappedCanceled},
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "wrapped deadline", err: wrappedDeadline},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if classified := ormpostgres.Classify(test.err); classified != test.err {
				t.Fatalf("Classify returned a different error: %T %v", classified, classified)
			}
		})
	}
}

func TestClassifyDoesNotDoubleWrapClassifiedErrors(t *testing.T) {
	native := sensitivePostgresError("40P01")
	first := ormpostgres.Classify(native)
	if second := ormpostgres.Classify(first); second != first {
		t.Fatalf("second classification changed %T", first)
	}
}

func TestClassifyFindsWrappedAndJoinedPersistenceErrors(t *testing.T) {
	native := sensitivePostgresError("23505")
	native.ConstraintName = "users_email_key"
	persistence := &orm.PersistenceError{Operation: orm.OperationInsert, Table: "users", Cause: native}

	wrapped := ormpostgres.Classify(fmt.Errorf("save user: %w", persistence))
	var foundPersistence *orm.PersistenceError
	if !errors.As(wrapped, &foundPersistence) || foundPersistence.Operation != orm.OperationInsert || foundPersistence.Table != "users" {
		t.Fatalf("wrapped persistence context was not retained: %#v", foundPersistence)
	}
	if !errors.Is(wrapped, orm.ErrUnique) || !errors.Is(wrapped, native) {
		t.Fatalf("wrapped classification chain is incomplete: %v", wrapped)
	}
	assertDisclosureSafe(t, wrapped.Error())

	other := errors.New("secondary failure")
	joined := ormpostgres.Classify(errors.Join(persistence, other))
	if !errors.Is(joined, other) || !errors.Is(joined, orm.ErrUnique) || !errors.Is(joined, native) {
		t.Fatalf("joined classification lost a cause: %v", joined)
	}
	foundPersistence = nil
	if !errors.As(joined, &foundPersistence) || foundPersistence.Table != "users" {
		t.Fatalf("joined persistence context was not retained: %#v", foundPersistence)
	}
	assertDisclosureSafe(t, joined.Error())
}

func sensitivePostgresError(code string) *pgconn.PgError {
	return &pgconn.PgError{
		Code:    code,
		Message: "failure for submitted value super-secret@example.com",
		Detail:  "Key (email)=(super-secret@example.com) already exists.",
		Hint:    "retry with super-secret@example.com",
		Where:   "unnamed portal parameter $1 = 'super-secret@example.com'",
	}
}

func assertNativeAndSafe(t *testing.T, classified error, native *pgconn.PgError) {
	t.Helper()
	var found *pgconn.PgError
	if !errors.As(classified, &found) || found != native {
		t.Fatal("errors.As did not return the native PostgreSQL error")
	}
	assertDisclosureSafe(t, classified.Error())
}

func assertDisclosureSafe(t *testing.T, message string) {
	t.Helper()
	for _, sensitive := range []string{"super-secret@example.com", "submitted value", "Key (email)", "unnamed portal"} {
		if strings.Contains(message, sensitive) {
			t.Fatalf("classified error disclosed %q: %q", sensitive, message)
		}
	}
}
