// Package postgres adapts PostgreSQL driver errors to GoForge ORM errors.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/ShanilKoshitha/goforge/orm"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	codeUnique        = "23505"
	codeForeignKey    = "23503"
	codeNotNull       = "23502"
	codeCheck         = "23514"
	codeSerialization = "40001"
	codeDeadlock      = "40P01"
)

// Classify converts known PostgreSQL SQLSTATEs to driver-neutral ORM errors.
//
// When err is a PersistenceError, the persistence error remains outermost and
// the classified error replaces its cause. The resulting chain is therefore:
//
//	PersistenceError -> ConstraintError/TransientError -> safeCause -> PgError
//
// This preserves operation and table context together with errors.Is/errors.As
// access to the native error. safeCause prevents PostgreSQL messages and detail
// fields, which may contain submitted values, from reaching Error output.
// Context errors, unknown errors, and nil pass through unchanged.
func Classify(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	classified, changed := classify(err)
	if !changed {
		return err
	}
	return classified
}

func classify(err error) (error, bool) {
	switch typed := err.(type) {
	case *orm.PersistenceError:
		cause, changed := classify(typed.Cause)
		if !changed {
			return err, false
		}
		return &orm.PersistenceError{
			Operation: typed.Operation,
			Table:     typed.Table,
			Cause:     cause,
		}, true
	case *orm.ConstraintError, *orm.TransientError:
		return err, false
	case interface{ Unwrap() []error }:
		causes := typed.Unwrap()
		classified := make([]error, len(causes))
		changed := false
		for index, cause := range causes {
			classified[index], changed = classifyOne(cause, changed)
		}
		if !changed {
			return err, false
		}
		return errors.Join(classified...), true
	}
	if wrapped := errors.Unwrap(err); wrapped != nil {
		classified, changed := classify(wrapped)
		if changed {
			// Driver-neutral classification becomes the visible wrapper. The
			// original native cause remains below safeCause, so errors.Is/As
			// still work without reusing a possibly disclosure-bearing message.
			return classified, true
		}
	}

	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return err, false
	}

	cause := &safeCause{code: postgresError.Code, cause: err}
	switch postgresError.Code {
	case codeUnique:
		return constraintError(orm.ConstraintUnique, postgresError.ConstraintName, cause), true
	case codeForeignKey:
		return constraintError(orm.ConstraintForeignKey, postgresError.ConstraintName, cause), true
	case codeNotNull:
		name := postgresError.ConstraintName
		if name == "" {
			name = postgresError.ColumnName
		}
		return constraintError(orm.ConstraintNotNull, name, cause), true
	case codeCheck:
		return constraintError(orm.ConstraintCheck, postgresError.ConstraintName, cause), true
	case codeSerialization:
		return &orm.TransientError{Kind: orm.TransientSerialization, Code: postgresError.Code, Cause: cause}, true
	case codeDeadlock:
		return &orm.TransientError{Kind: orm.TransientDeadlock, Code: postgresError.Code, Cause: cause}, true
	default:
		return err, false
	}
}

func classifyOne(err error, alreadyChanged bool) (error, bool) {
	classified, changed := classify(err)
	return classified, alreadyChanged || changed
}

func constraintError(kind orm.ConstraintKind, name string, cause error) error {
	var postgresError *pgconn.PgError
	_ = errors.As(cause, &postgresError)
	code := ""
	if postgresError != nil {
		code = postgresError.Code
	}
	return &orm.ConstraintError{Kind: kind, Constraint: name, Code: code, Cause: cause}
}

type safeCause struct {
	code  string
	cause error
}

func (err *safeCause) Error() string {
	return fmt.Sprintf("postgres: SQLSTATE %s", err.code)
}

func (err *safeCause) Unwrap() error { return err.cause }
