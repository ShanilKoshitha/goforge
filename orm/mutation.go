package orm

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Assignment is created from a typed Column and therefore cannot assign a
// value of the wrong Go type or a column belonging to another model type.
type Assignment[M any] struct {
	table    identifier
	identity *tableIdentity
	column   identifier
	kind     assignmentKind
	value    any
}

type assignmentKind uint8

const (
	assignmentOmitted assignmentKind = iota
	assignmentValue
	assignmentNull
	assignmentDefault
	assignmentCurrentTime
	assignmentIncrement
)

func (column Column[M, V]) Set(value V) Assignment[M] {
	return Assignment[M]{
		table: column.table, identity: column.identity, column: column.name,
		kind: assignmentValue, value: freezeArg(value),
	}
}

// From turns a partial Field into an assignment. Omitted fields are ignored.
func (column Column[M, V]) From(field Field[V]) Assignment[M] {
	assignment := Assignment[M]{table: column.table, identity: column.identity, column: column.name}
	switch field.state {
	case FieldOmitted:
		assignment.kind = assignmentOmitted
	case FieldNull:
		assignment.kind = assignmentNull
	case FieldValue:
		assignment.kind = assignmentValue
		assignment.value = freezeArg(field.value)
	case FieldDefault:
		assignment.kind = assignmentDefault
	}
	return assignment
}

// SetCurrentTime is the only built-in clock expression and is available only
// for time.Time columns. It never accepts SQL text.
func SetCurrentTime[M any](column Column[M, time.Time]) Assignment[M] {
	return Assignment[M]{table: column.table, identity: column.identity, column: column.name, kind: assignmentCurrentTime}
}

// Number is the set of values accepted by Increment.
type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// Increment creates a column arithmetic expression without accepting raw SQL.
// The delta remains a bound argument.
func Increment[M any, V Number](column Column[M, V], delta V) Assignment[M] {
	return Assignment[M]{
		table: column.table, identity: column.identity, column: column.name,
		kind: assignmentIncrement, value: freezeArg(delta),
	}
}

// VersionGuard is a generated-only optimistic-version predicate and increment
// pair. Construct it with ExpectVersion rather than assembling the two halves
// independently.
type VersionGuard[M any] struct {
	predicate  Predicate[M]
	assignment Assignment[M]
}

// ExpectVersion requires the current version and increments it by one in the
// same UPDATE statement. The expected value remains a bound argument.
func ExpectVersion[M any, V Number](column Column[M, V], expected V) VersionGuard[M] {
	return VersionGuard[M]{predicate: column.Eq(expected), assignment: Increment(column, V(1))}
}

type InsertBuilder[M any] struct {
	table       Table[M]
	assignments []Assignment[M]
	err         error
}

func Insert[M any](table Table[M]) InsertBuilder[M] { return InsertBuilder[M]{table: table} }

func (builder InsertBuilder[M]) Values(assignments ...Assignment[M]) InsertBuilder[M] {
	builder.assignments = appendCopy(builder.assignments, assignments...)
	return builder
}

// InsertRow is one explicitly shaped row in a bulk insert. Row copies the
// assignment list so builders remain immutable when callers reuse slices.
type InsertRow[M any] struct {
	assignments []Assignment[M]
}

// Row constructs one typed bulk-insert row.
func Row[M any](assignments ...Assignment[M]) InsertRow[M] {
	return InsertRow[M]{assignments: append([]Assignment[M](nil), assignments...)}
}

// BulkInsertBuilder compiles one PostgreSQL multi-row INSERT ... RETURNING.
type BulkInsertBuilder[M any] struct {
	table Table[M]
	rows  []InsertRow[M]
	err   error
}

// Rows starts a bulk insert. It is intentionally incompatible with Values on
// the same builder so the distinction between a single row and a row set stays
// visible at the call site.
func (builder InsertBuilder[M]) Rows(rows ...InsertRow[M]) BulkInsertBuilder[M] {
	bulk := BulkInsertBuilder[M]{table: builder.table, rows: append([]InsertRow[M](nil), rows...), err: builder.err}
	if len(builder.assignments) != 0 {
		bulk.err = fmt.Errorf("orm: bulk Rows cannot be combined with single-row Values")
	}
	return bulk
}

// Build returns deterministic multi-row PostgreSQL SQL. Every row must have
// the same present column set; assignment order after the first row may differ.
// Omitted fields do not participate in the shape, while Null and Default do.
func (builder BulkInsertBuilder[M]) Build() (Statement, error) {
	if builder.err != nil {
		return Statement{}, builder.err
	}
	if err := builder.table.validate(); err != nil {
		return Statement{}, err
	}
	if len(builder.rows) == 0 {
		return Statement{}, fmt.Errorf("orm: bulk insert requires at least one row")
	}

	shaped := make([][]Assignment[M], len(builder.rows))
	for index, row := range builder.rows {
		assignments, err := includedAssignments(builder.table.name, builder.table.identity, row.assignments)
		if err != nil {
			return Statement{}, fmt.Errorf("orm: bulk insert row %d: %w", index, err)
		}
		for _, assignment := range assignments {
			if assignment.kind == assignmentIncrement {
				return Statement{}, fmt.Errorf("orm: bulk insert row %d: increment is not valid in an insert", index)
			}
		}
		shaped[index] = assignments
	}
	if len(shaped[0]) == 0 {
		if len(shaped) != 1 {
			return Statement{}, fmt.Errorf("orm: multi-row insert requires at least one present column")
		}
		return Insert(builder.table).Build()
	}

	columns := make([]identifier, len(shaped[0]))
	for index, assignment := range shaped[0] {
		columns[index] = assignment.column
	}
	for rowIndex := 1; rowIndex < len(shaped); rowIndex++ {
		if len(shaped[rowIndex]) != len(columns) {
			return Statement{}, fmt.Errorf("orm: bulk insert row %d has an incompatible column shape", rowIndex)
		}
		byColumn := make(map[string]Assignment[M], len(shaped[rowIndex]))
		for _, assignment := range shaped[rowIndex] {
			byColumn[assignment.column.name] = assignment
		}
		reordered := make([]Assignment[M], len(columns))
		for columnIndex, column := range columns {
			assignment, ok := byColumn[column.name]
			if !ok {
				return Statement{}, fmt.Errorf("orm: bulk insert row %d has an incompatible column shape", rowIndex)
			}
			reordered[columnIndex] = assignment
		}
		shaped[rowIndex] = reordered
	}

	compiler := &sqlCompiler{}
	var query strings.Builder
	query.WriteString("INSERT INTO ")
	query.WriteString(builder.table.name.quoted())
	query.WriteString(" (")
	for index, column := range columns {
		if index > 0 {
			query.WriteString(", ")
		}
		query.WriteString(column.quoted())
	}
	query.WriteString(") VALUES ")
	for rowIndex, assignments := range shaped {
		if rowIndex > 0 {
			query.WriteString(", ")
		}
		query.WriteByte('(')
		for columnIndex, assignment := range assignments {
			if columnIndex > 0 {
				query.WriteString(", ")
			}
			writeAssignmentValue(&query, compiler, assignment)
		}
		query.WriteByte(')')
	}
	query.WriteString(" RETURNING ")
	writeMappedColumns(&query, builder.table)
	return newCompiledStatement(query.String(), compiler.args)
}

// All inserts all rows atomically in one statement and maps RETURNING rows.
func (builder BulkInsertBuilder[M]) All(ctx context.Context, executor Executor) ([]M, error) {
	if err := validateExecutor(executor, false); err != nil {
		return nil, err
	}
	statement, err := builder.Build()
	if err != nil {
		return nil, err
	}
	return queryModelsFor(ctx, executor, builder.table, statement, OperationInsert)
}

func (builder InsertBuilder[M]) Build() (Statement, error) {
	if builder.err != nil {
		return Statement{}, builder.err
	}
	if err := builder.table.validate(); err != nil {
		return Statement{}, err
	}
	assignments, err := includedAssignments(builder.table.name, builder.table.identity, builder.assignments)
	if err != nil {
		return Statement{}, err
	}
	compiler := &sqlCompiler{}
	var query strings.Builder
	query.WriteString("INSERT INTO ")
	query.WriteString(builder.table.name.quoted())
	if len(assignments) == 0 {
		query.WriteString(" DEFAULT VALUES")
	} else {
		query.WriteString(" (")
		for index, assignment := range assignments {
			if index > 0 {
				query.WriteString(", ")
			}
			query.WriteString(assignment.column.quoted())
		}
		query.WriteString(") VALUES (")
		for index, assignment := range assignments {
			if index > 0 {
				query.WriteString(", ")
			}
			if assignment.kind == assignmentIncrement {
				return Statement{}, fmt.Errorf("orm: increment is not valid in an insert")
			}
			writeAssignmentValue(&query, compiler, assignment)
		}
		query.WriteByte(')')
	}
	query.WriteString(" RETURNING ")
	writeMappedColumns(&query, builder.table)
	return newCompiledStatement(query.String(), compiler.args)
}

// One inserts and maps the returned model.
func (builder InsertBuilder[M]) One(ctx context.Context, executor Executor) (M, error) {
	if err := validateExecutor(executor, false); err != nil {
		var zero M
		return zero, err
	}
	statement, err := builder.Build()
	if err != nil {
		var zero M
		return zero, err
	}
	item, err := builder.table.mapper.scan(executor.QueryRowContext(ctx, statement.query, statement.args...))
	if err != nil {
		var zero M
		return zero, persistenceError(OperationInsert, builder.table.Name(), err)
	}
	return item, nil
}

type UpdateBuilder[M any] struct {
	table       Table[M]
	assignments []Assignment[M]
	predicates  []Predicate[M]
	allRows     bool
	staleOnZero bool
	err         error
}

func Update[M any](table Table[M]) UpdateBuilder[M] { return UpdateBuilder[M]{table: table} }

func (builder UpdateBuilder[M]) Set(assignments ...Assignment[M]) UpdateBuilder[M] {
	builder.assignments = appendCopy(builder.assignments, assignments...)
	return builder
}

func (builder UpdateBuilder[M]) Where(predicates ...Predicate[M]) UpdateBuilder[M] {
	builder.predicates = appendCopy(builder.predicates, predicates...)
	return builder
}

// OptimisticVersion atomically adds both halves of an ExpectVersion guard to
// this immutable builder. It deliberately does not enable StaleOnZero: a zero
// match may be caused by any other predicate, including tenant or ownership
// scope. Generated repositories may add StaleOnZero only after they have made
// that distinction explicitly.
func (builder UpdateBuilder[M]) OptimisticVersion(guard VersionGuard[M]) UpdateBuilder[M] {
	builder.assignments = appendCopy(builder.assignments, guard.assignment)
	builder.predicates = appendCopy(builder.predicates, guard.predicate)
	return builder
}

// AllRows explicitly allows a mutation without predicates.
func (builder UpdateBuilder[M]) AllRows() UpdateBuilder[M] {
	builder.allRows = true
	return builder
}

// StaleOnZero makes a zero-row optimistic update return ErrStale. Generated
// owner-scoped repositories must preserve wrong-owner and missing rows as
// ErrNotFound rather than enabling this directly on an ownership predicate.
func (builder UpdateBuilder[M]) StaleOnZero() UpdateBuilder[M] {
	builder.staleOnZero = true
	return builder
}

func (builder UpdateBuilder[M]) Build() (Statement, error) {
	if builder.err != nil {
		return Statement{}, builder.err
	}
	if err := builder.table.validate(); err != nil {
		return Statement{}, err
	}
	if len(builder.predicates) == 0 && !builder.allRows {
		return Statement{}, ErrUnsafeMutation
	}
	assignments, err := includedAssignments(builder.table.name, builder.table.identity, builder.assignments)
	if err != nil {
		return Statement{}, err
	}
	if len(assignments) == 0 {
		return Statement{}, fmt.Errorf("orm: update requires at least one present assignment")
	}
	compiler := &sqlCompiler{}
	var query strings.Builder
	query.WriteString("UPDATE ")
	query.WriteString(builder.table.name.quoted())
	query.WriteString(" SET ")
	for index, assignment := range assignments {
		if index > 0 {
			query.WriteString(", ")
		}
		query.WriteString(assignment.column.quoted())
		query.WriteString(" = ")
		writeAssignmentValue(&query, compiler, assignment)
	}
	if err := writePredicates(&query, compiler, builder.table.name, builder.table.identity, builder.predicates); err != nil {
		return Statement{}, err
	}
	return newCompiledStatement(query.String(), compiler.args)
}

// BuildReturning adds an atomic PostgreSQL RETURNING clause for the mapped
// model. Generated repositories use this instead of update-then-select.
func (builder UpdateBuilder[M]) BuildReturning() (Statement, error) {
	statement, err := builder.Build()
	if err != nil {
		return Statement{}, err
	}
	var query strings.Builder
	query.WriteString(statement.query)
	query.WriteString(" RETURNING ")
	writeMappedColumns(&query, builder.table)
	return newCompiledStatement(query.String(), statement.args)
}

// One updates and returns one model atomically. The caller must supply predicates
// that identify at most one row; PostgreSQL applies the update to every match
// before database/sql returns the first RETURNING row. A no-row result is
// ErrNotFound, or ErrStale only when StaleOnZero was explicitly selected by
// generated code.
func (builder UpdateBuilder[M]) One(ctx context.Context, executor Executor) (M, error) {
	if err := validateExecutor(executor, false); err != nil {
		var zero M
		return zero, err
	}
	statement, err := builder.BuildReturning()
	if err != nil {
		var zero M
		return zero, err
	}
	item, err := builder.table.mapper.scan(executor.QueryRowContext(ctx, statement.query, statement.args...))
	if err != nil {
		var zero M
		if errors.Is(err, sql.ErrNoRows) {
			cause := error(ErrNotFound)
			if builder.staleOnZero {
				cause = ErrStale
			}
			return zero, persistenceError(OperationUpdate, builder.table.Name(), cause)
		}
		return zero, persistenceError(OperationUpdate, builder.table.Name(), err)
	}
	return item, nil
}

func (builder UpdateBuilder[M]) Exec(ctx context.Context, executor Executor) (int64, error) {
	if err := validateExecutor(executor, false); err != nil {
		return 0, err
	}
	statement, err := builder.Build()
	if err != nil {
		return 0, err
	}
	result, err := executor.ExecContext(ctx, statement.query, statement.args...)
	if err != nil {
		return 0, persistenceError(OperationUpdate, builder.table.Name(), err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, persistenceError(OperationUpdate, builder.table.Name(), err)
	}
	if rows == 0 && builder.staleOnZero {
		return 0, persistenceError(OperationUpdate, builder.table.Name(), ErrStale)
	}
	return rows, nil
}

type DeleteBuilder[M any] struct {
	table      Table[M]
	predicates []Predicate[M]
	allRows    bool
	err        error
}

func Delete[M any](table Table[M]) DeleteBuilder[M] { return DeleteBuilder[M]{table: table} }

func (builder DeleteBuilder[M]) Where(predicates ...Predicate[M]) DeleteBuilder[M] {
	builder.predicates = appendCopy(builder.predicates, predicates...)
	return builder
}

func (builder DeleteBuilder[M]) AllRows() DeleteBuilder[M] {
	builder.allRows = true
	return builder
}

func (builder DeleteBuilder[M]) Build() (Statement, error) {
	if builder.err != nil {
		return Statement{}, builder.err
	}
	if err := builder.table.validate(); err != nil {
		return Statement{}, err
	}
	if len(builder.predicates) == 0 && !builder.allRows {
		return Statement{}, ErrUnsafeMutation
	}
	compiler := &sqlCompiler{}
	var query strings.Builder
	query.WriteString("DELETE FROM ")
	query.WriteString(builder.table.name.quoted())
	if err := writePredicates(&query, compiler, builder.table.name, builder.table.identity, builder.predicates); err != nil {
		return Statement{}, err
	}
	return newCompiledStatement(query.String(), compiler.args)
}

func (builder DeleteBuilder[M]) Exec(ctx context.Context, executor Executor) (int64, error) {
	if err := validateExecutor(executor, false); err != nil {
		return 0, err
	}
	statement, err := builder.Build()
	if err != nil {
		return 0, err
	}
	result, err := executor.ExecContext(ctx, statement.query, statement.args...)
	if err != nil {
		return 0, persistenceError(OperationDelete, builder.table.Name(), err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, persistenceError(OperationDelete, builder.table.Name(), err)
	}
	return rows, nil
}

func includedAssignments[M any](table identifier, identity *tableIdentity, assignments []Assignment[M]) ([]Assignment[M], error) {
	result := make([]Assignment[M], 0, len(assignments))
	seen := make(map[string]struct{}, len(assignments))
	for _, assignment := range assignments {
		if assignment.table != table || assignment.identity != identity || identity == nil {
			return nil, fmt.Errorf("orm: assignment belongs to a different table")
		}
		if assignment.kind == assignmentOmitted {
			continue
		}
		if assignment.kind < assignmentValue || assignment.kind > assignmentIncrement {
			return nil, fmt.Errorf("orm: invalid field state")
		}
		if assignment.kind == assignmentValue && isNilValue(assignment.value) {
			return nil, fmt.Errorf("orm: assignment value is nil; use Null or Default")
		}
		if _, exists := seen[assignment.column.name]; exists {
			return nil, fmt.Errorf("orm: duplicate assignment for column %s", assignment.column.name)
		}
		seen[assignment.column.name] = struct{}{}
		result = append(result, assignment)
	}
	return result, nil
}

func writeAssignmentValue[M any](query *strings.Builder, compiler *sqlCompiler, assignment Assignment[M]) {
	switch assignment.kind {
	case assignmentNull:
		query.WriteString(compiler.bind(nil))
	case assignmentDefault:
		query.WriteString("DEFAULT")
	case assignmentCurrentTime:
		query.WriteString("CURRENT_TIMESTAMP")
	case assignmentIncrement:
		query.WriteString(assignment.column.quoted())
		query.WriteString(" + ")
		query.WriteString(compiler.bind(assignment.value))
	default:
		query.WriteString(compiler.bind(assignment.value))
	}
}
