package orm

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type lockMode uint8

const (
	lockNone lockMode = iota
	lockUpdate
	lockShare
)

// SelectBuilder is an immutable query for one mapped model.
type SelectBuilder[M any] struct {
	table      Table[M]
	predicates []Predicate[M]
	orders     []Order[M]
	limit      *int
	offset     *int
	lock       lockMode
	err        error
}

func Select[M any](table Table[M]) SelectBuilder[M] { return SelectBuilder[M]{table: table} }

func (builder SelectBuilder[M]) Where(predicates ...Predicate[M]) SelectBuilder[M] {
	builder.predicates = appendCopy(builder.predicates, predicates...)
	return builder
}

func (builder SelectBuilder[M]) OrderBy(orders ...Order[M]) SelectBuilder[M] {
	builder.orders = appendCopy(builder.orders, orders...)
	return builder
}

func (builder SelectBuilder[M]) Limit(limit int) SelectBuilder[M] {
	if limit < 0 {
		builder.err = errors.Join(builder.err, fmt.Errorf("orm: limit must not be negative"))
		return builder
	}
	builder.limit = intPointer(limit)
	return builder
}

func (builder SelectBuilder[M]) Offset(offset int) SelectBuilder[M] {
	if offset < 0 {
		builder.err = errors.Join(builder.err, fmt.Errorf("orm: offset must not be negative"))
		return builder
	}
	builder.offset = intPointer(offset)
	return builder
}

func (builder SelectBuilder[M]) ForUpdate() SelectBuilder[M] {
	if builder.lock == lockShare {
		builder.err = errors.Join(builder.err, fmt.Errorf("orm: conflicting row locks"))
	}
	builder.lock = lockUpdate
	return builder
}

func (builder SelectBuilder[M]) ForShare() SelectBuilder[M] {
	if builder.lock == lockUpdate {
		builder.err = errors.Join(builder.err, fmt.Errorf("orm: conflicting row locks"))
	}
	builder.lock = lockShare
	return builder
}

// Build returns inspectable PostgreSQL SQL and a copied parameter list.
func (builder SelectBuilder[M]) Build() (Statement, error) {
	return builder.buildSelect(false)
}

func (builder SelectBuilder[M]) buildSelect(first bool) (Statement, error) {
	if builder.err != nil {
		return Statement{}, builder.err
	}
	if err := builder.table.validate(); err != nil {
		return Statement{}, err
	}
	compiler := &sqlCompiler{}
	var query strings.Builder
	query.WriteString("SELECT ")
	writeMappedColumns(&query, builder.table)
	query.WriteString(" FROM ")
	query.WriteString(builder.table.name.quoted())
	if err := writePredicates(&query, compiler, builder.table.name, builder.table.identity, builder.predicates); err != nil {
		return Statement{}, err
	}
	if len(builder.orders) > 0 {
		if err := writeOrders(&query, builder.table.name, builder.table.identity, builder.orders); err != nil {
			return Statement{}, err
		}
	}
	limit := builder.limit
	if first {
		limit = intPointer(1)
	}
	if limit != nil {
		query.WriteString(" LIMIT ")
		query.WriteString(compiler.bind(*limit))
	}
	if builder.offset != nil {
		query.WriteString(" OFFSET ")
		query.WriteString(compiler.bind(*builder.offset))
	}
	switch builder.lock {
	case lockUpdate:
		query.WriteString(" FOR UPDATE")
	case lockShare:
		query.WriteString(" FOR SHARE")
	}
	return newCompiledStatement(query.String(), compiler.args)
}

// BuildCount returns the filtered count query; ordering, pagination, and locks
// intentionally do not affect the total.
func (builder SelectBuilder[M]) BuildCount() (Statement, error) {
	return builder.buildAggregate("COUNT(*)")
}

// BuildExists returns the filtered existence query; ordering, pagination, and
// locks intentionally do not affect existence.
func (builder SelectBuilder[M]) BuildExists() (Statement, error) {
	if builder.err != nil {
		return Statement{}, builder.err
	}
	if err := builder.table.validate(); err != nil {
		return Statement{}, err
	}
	compiler := &sqlCompiler{}
	var inner strings.Builder
	inner.WriteString("SELECT 1 FROM ")
	inner.WriteString(builder.table.name.quoted())
	if err := writePredicates(&inner, compiler, builder.table.name, builder.table.identity, builder.predicates); err != nil {
		return Statement{}, err
	}
	return newCompiledStatement("SELECT EXISTS ("+inner.String()+")", compiler.args)
}

func (builder SelectBuilder[M]) buildAggregate(expression string) (Statement, error) {
	if builder.err != nil {
		return Statement{}, builder.err
	}
	if err := builder.table.validate(); err != nil {
		return Statement{}, err
	}
	compiler := &sqlCompiler{}
	var query strings.Builder
	query.WriteString("SELECT ")
	query.WriteString(expression)
	query.WriteString(" FROM ")
	query.WriteString(builder.table.name.quoted())
	if err := writePredicates(&query, compiler, builder.table.name, builder.table.identity, builder.predicates); err != nil {
		return Statement{}, err
	}
	return newCompiledStatement(query.String(), compiler.args)
}

func (builder SelectBuilder[M]) All(ctx context.Context, executor Executor) ([]M, error) {
	if err := validateExecutor(executor, builder.lock != lockNone); err != nil {
		return nil, err
	}
	statement, err := builder.Build()
	if err != nil {
		return nil, err
	}
	return queryModels(ctx, executor, builder.table, statement)
}

func (builder SelectBuilder[M]) First(ctx context.Context, executor Executor) (M, error) {
	if err := validateExecutor(executor, builder.lock != lockNone); err != nil {
		var zero M
		return zero, err
	}
	statement, err := builder.buildSelect(true)
	if err != nil {
		var zero M
		return zero, err
	}
	items, err := queryModels(ctx, executor, builder.table, statement)
	if err != nil {
		var zero M
		return zero, err
	}
	if len(items) == 0 {
		var zero M
		return zero, persistenceError(OperationSelect, builder.table.Name(), ErrNotFound)
	}
	return items[0], nil
}

func (builder SelectBuilder[M]) Count(ctx context.Context, executor Executor) (int64, error) {
	if err := validateExecutor(executor, false); err != nil {
		return 0, err
	}
	statement, err := builder.BuildCount()
	if err != nil {
		return 0, err
	}
	var count int64
	if err := executor.QueryRowContext(ctx, statement.query, statement.args...).Scan(&count); err != nil {
		return 0, persistenceError(OperationSelect, builder.table.Name(), err)
	}
	return count, nil
}

func (builder SelectBuilder[M]) Exists(ctx context.Context, executor Executor) (bool, error) {
	if err := validateExecutor(executor, false); err != nil {
		return false, err
	}
	statement, err := builder.BuildExists()
	if err != nil {
		return false, err
	}
	var exists bool
	if err := executor.QueryRowContext(ctx, statement.query, statement.args...).Scan(&exists); err != nil {
		return false, persistenceError(OperationSelect, builder.table.Name(), err)
	}
	return exists, nil
}

func queryModels[M any](ctx context.Context, executor Executor, table Table[M], statement Statement) (items []M, err error) {
	return queryModelsFor(ctx, executor, table, statement, OperationSelect)
}

func queryModelsFor[M any](ctx context.Context, executor Executor, table Table[M], statement Statement, operation Operation) (items []M, err error) {
	if err := validateExecutor(executor, false); err != nil {
		return nil, err
	}
	rows, err := executor.QueryContext(ctx, statement.query, statement.args...)
	if err != nil {
		return nil, persistenceError(operation, table.Name(), err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, persistenceError(operation, table.Name(), closeErr))
		}
	}()
	items = make([]M, 0)
	for rows.Next() {
		item, scanErr := table.mapper.scan(rows)
		if scanErr != nil {
			return nil, persistenceError(operation, table.Name(), scanErr)
		}
		items = append(items, item)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, persistenceError(operation, table.Name(), rowsErr)
	}
	return items, nil
}

func writeMappedColumns[M any](query *strings.Builder, table Table[M]) {
	for index, column := range table.mapper.columns {
		if index > 0 {
			query.WriteString(", ")
		}
		query.WriteString(table.name.quoted())
		query.WriteByte('.')
		query.WriteString(column.quoted())
	}
}

func writePredicates[M any](query *strings.Builder, compiler *sqlCompiler, table identifier, identity *tableIdentity, predicates []Predicate[M]) error {
	if len(predicates) == 0 {
		return nil
	}
	query.WriteString(" WHERE ")
	for index, predicate := range predicates {
		compiled, err := predicate.compile(compiler, table, identity)
		if err != nil {
			return err
		}
		if index > 0 {
			query.WriteString(" AND ")
		}
		query.WriteString(compiled)
	}
	return nil
}

func appendCopy[T any](current []T, values ...T) []T {
	result := make([]T, len(current), len(current)+len(values))
	copy(result, current)
	return append(result, values...)
}

func intPointer(value int) *int { return &value }

func persistenceError(operation Operation, table string, cause error) error {
	return &PersistenceError{Operation: operation, Table: table, Cause: cause}
}
