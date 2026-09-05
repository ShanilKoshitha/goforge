package orm

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound            = errors.New("orm: record not found")
	ErrUnsafeMutation      = errors.New("orm: mutation requires a predicate or AllRows")
	ErrStale               = errors.New("orm: stale record")
	ErrTransactionRequired = errors.New("orm: row locking requires a transaction executor")
	ErrUnique              = errors.New("orm: unique constraint violation")
	ErrForeignKey          = errors.New("orm: foreign key constraint violation")
	ErrNotNull             = errors.New("orm: not-null constraint violation")
	ErrCheck               = errors.New("orm: check constraint violation")
	ErrSerialization       = errors.New("orm: serialization failure")
	ErrDeadlock            = errors.New("orm: deadlock detected")
)

// Operation identifies a persistence operation without depending on a driver.
type Operation string

const (
	OperationSelect Operation = "select"
	OperationInsert Operation = "insert"
	OperationUpdate Operation = "update"
	OperationDelete Operation = "delete"
)

// PersistenceError adds operation and table context while preserving Cause for
// errors.Is and errors.As.
type PersistenceError struct {
	Operation Operation
	Table     string
	Cause     error
}

func (err *PersistenceError) Error() string {
	if err == nil {
		return "<nil>"
	}
	switch err.Cause.(type) {
	case *ConstraintError, *TransientError:
		return fmt.Sprintf("orm: %s %s failed: %v", err.Operation, err.Table, err.Cause)
	}
	return fmt.Sprintf("orm: %s %s failed", err.Operation, err.Table)
}

func (err *PersistenceError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// ConstraintKind classifies database constraint failures. Driver adapters may
// wrap their native errors in ConstraintError without discarding driver detail.
type ConstraintKind string

const (
	ConstraintUnique     ConstraintKind = "unique"
	ConstraintForeignKey ConstraintKind = "foreign_key"
	ConstraintNotNull    ConstraintKind = "not_null"
	ConstraintCheck      ConstraintKind = "check"
)

// ConstraintError is the driver-neutral shape for a constraint failure.
type ConstraintError struct {
	Kind       ConstraintKind
	Constraint string
	Code       string
	Cause      error
}

func (err *ConstraintError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.Constraint == "" {
		if err.Code != "" {
			return fmt.Sprintf("orm: %s constraint violation (%s)", err.Kind, err.Code)
		}
		return fmt.Sprintf("orm: %s constraint violation", err.Kind)
	}
	if err.Code != "" {
		return fmt.Sprintf("orm: %s constraint %s violated (%s)", err.Kind, err.Constraint, err.Code)
	}
	return fmt.Sprintf("orm: %s constraint %s violated", err.Kind, err.Constraint)
}

func (err *ConstraintError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// Is exposes a stable errors.Is target while Unwrap retains the native driver
// error for errors.As.
func (err *ConstraintError) Is(target error) bool {
	if err == nil {
		return false
	}
	switch err.Kind {
	case ConstraintUnique:
		return target == ErrUnique
	case ConstraintForeignKey:
		return target == ErrForeignKey
	case ConstraintNotNull:
		return target == ErrNotNull
	case ConstraintCheck:
		return target == ErrCheck
	default:
		return false
	}
}

// TransientKind classifies persistence failures callers may safely choose to
// retry at an application transaction boundary.
type TransientKind string

const (
	TransientSerialization TransientKind = "serialization"
	TransientDeadlock      TransientKind = "deadlock"
)

// TransientError retains the driver error and provides a stable retry class.
type TransientError struct {
	Kind  TransientKind
	Code  string
	Cause error
}

func (err *TransientError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.Code != "" {
		return fmt.Sprintf("orm: transient %s failure (%s)", err.Kind, err.Code)
	}
	return fmt.Sprintf("orm: transient %s failure", err.Kind)
}

func (err *TransientError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

func (err *TransientError) Is(target error) bool {
	if err == nil {
		return false
	}
	switch err.Kind {
	case TransientSerialization:
		return target == ErrSerialization
	case TransientDeadlock:
		return target == ErrDeadlock
	default:
		return false
	}
}
