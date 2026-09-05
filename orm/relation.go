package orm

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// RelationOptions controls predictable loader batching. Zero uses PostgreSQL's
// parameter limit. Smaller values are useful for application-specific budgets.
type RelationOptions struct {
	ParameterLimit int
}

func (options RelationOptions) parameterLimit() (int, error) {
	if options.ParameterLimit == 0 {
		return PostgreSQLParameterLimit, nil
	}
	if options.ParameterLimit < 1 || options.ParameterLimit > PostgreSQLParameterLimit {
		return 0, fmt.Errorf("orm: relation parameter limit must be between 1 and %d", PostgreSQLParameterLimit)
	}
	return options.ParameterLimit, nil
}

type relationDefinition[P, T any, K comparable] struct {
	parent       Table[P]
	target       Table[T]
	parentColumn identifier
	targetColumn identifier
	parentKey    func(P) (K, bool)
	targetKey    func(T) (K, bool)
	limit        int
}

func (definition relationDefinition[P, T, K]) validate() error {
	if err := definition.parent.validate(); err != nil {
		return err
	}
	if err := definition.target.validate(); err != nil {
		return err
	}
	if definition.parentColumn.name == "" || definition.targetColumn.name == "" ||
		definition.parentKey == nil || definition.targetKey == nil ||
		definition.limit < 1 || definition.limit > PostgreSQLParameterLimit {
		return fmt.Errorf("orm: invalid relation definition")
	}
	return nil
}

func newRelationDefinition[P, T, PV, TV any, K comparable](
	parent Table[P],
	target Table[T],
	parentColumn Column[P, PV],
	targetColumn Column[T, TV],
	parentKey func(P) (K, bool),
	targetKey func(T) (K, bool),
	options RelationOptions,
) (relationDefinition[P, T, K], error) {
	if err := parent.validate(); err != nil {
		return relationDefinition[P, T, K]{}, err
	}
	if err := target.validate(); err != nil {
		return relationDefinition[P, T, K]{}, err
	}
	if parentColumn.table != parent.name || parentColumn.identity != parent.identity ||
		targetColumn.table != target.name || targetColumn.identity != target.identity {
		return relationDefinition[P, T, K]{}, fmt.Errorf("orm: relation columns do not belong to their declared tables")
	}
	if parentKey == nil || targetKey == nil {
		return relationDefinition[P, T, K]{}, fmt.Errorf("orm: relation requires explicit key callbacks")
	}
	limit, err := options.parameterLimit()
	if err != nil {
		return relationDefinition[P, T, K]{}, err
	}
	return relationDefinition[P, T, K]{
		parent: parent, target: target, parentColumn: parentColumn.name,
		targetColumn: targetColumn.name, parentKey: parentKey, targetKey: targetKey,
		limit: limit,
	}, nil
}

// BelongsTo explicitly maps a parent foreign key to one target key.
type BelongsTo[P, T any, K comparable] struct {
	definition relationDefinition[P, T, K]
	assign     func(*P, *T)
}

func NewBelongsTo[P, T, PV, TV any, K comparable](
	parent Table[P],
	target Table[T],
	parentForeignKey Column[P, PV],
	targetKeyColumn Column[T, TV],
	parentKey func(P) (K, bool),
	targetKey func(T) (K, bool),
	assign func(*P, *T),
	options RelationOptions,
) (BelongsTo[P, T, K], error) {
	definition, err := newRelationDefinition(
		parent, target, parentForeignKey, targetKeyColumn, parentKey, targetKey, options,
	)
	if err != nil {
		return BelongsTo[P, T, K]{}, err
	}
	if assign == nil {
		return BelongsTo[P, T, K]{}, fmt.Errorf("orm: belongs-to requires an assign callback")
	}
	return BelongsTo[P, T, K]{definition: definition, assign: assign}, nil
}

func MustBelongsTo[P, T, PV, TV any, K comparable](
	parent Table[P], target Table[T], parentForeignKey Column[P, PV], targetKeyColumn Column[T, TV],
	parentKey func(P) (K, bool), targetKey func(T) (K, bool), assign func(*P, *T), options RelationOptions,
) BelongsTo[P, T, K] {
	relation, err := NewBelongsTo(parent, target, parentForeignKey, targetKeyColumn, parentKey, targetKey, assign, options)
	if err != nil {
		panic(err)
	}
	return relation
}

// Build returns every statement Load would issue for the supplied materialized
// parent page. Parent pagination therefore always occurs before relation I/O.
func (relation BelongsTo[P, T, K]) Build(parents []P, scope SelectBuilder[T]) ([]Statement, error) {
	if err := relation.definition.validate(); err != nil {
		return nil, err
	}
	if relation.assign == nil {
		return nil, fmt.Errorf("orm: invalid belongs-to relation")
	}
	keys := uniqueKeys(parents, relation.definition.parentKey)
	return buildScopedRelationStatements(relation.definition, keys, scope)
}

func (relation BelongsTo[P, T, K]) Load(ctx context.Context, executor Executor, parents []P, scope SelectBuilder[T]) ([]P, error) {
	if err := validateExecutor(executor, false); err != nil {
		return nil, err
	}
	result := append([]P(nil), parents...)
	statements, err := relation.Build(result, scope)
	if err != nil {
		return nil, err
	}
	byKey := make(map[K]T)
	for _, statement := range statements {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		items, err := queryModels(ctx, executor, relation.definition.target, statement)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			key, ok := relation.definition.targetKey(item)
			if !ok {
				return nil, fmt.Errorf("orm: belongs-to target returned no relation key")
			}
			if _, exists := byKey[key]; exists {
				return nil, fmt.Errorf("orm: belongs-to target key is not unique")
			}
			byKey[key] = item
		}
	}
	for index := range result {
		key, ok := relation.definition.parentKey(result[index])
		if !ok {
			relation.assign(&result[index], nil)
			continue
		}
		item, exists := byKey[key]
		if !exists {
			relation.assign(&result[index], nil)
			continue
		}
		copy := item
		relation.assign(&result[index], &copy)
	}
	return result, nil
}

// HasOne explicitly maps a parent key to at most one target foreign key.
type HasOne[P, T any, K comparable] struct {
	definition relationDefinition[P, T, K]
	assign     func(*P, *T)
}

func NewHasOne[P, T, PV, TV any, K comparable](
	parent Table[P], target Table[T], parentKeyColumn Column[P, PV], targetForeignKey Column[T, TV],
	parentKey func(P) (K, bool), targetForeignKeyValue func(T) (K, bool), assign func(*P, *T), options RelationOptions,
) (HasOne[P, T, K], error) {
	definition, err := newRelationDefinition(
		parent, target, parentKeyColumn, targetForeignKey, parentKey, targetForeignKeyValue, options,
	)
	if err != nil {
		return HasOne[P, T, K]{}, err
	}
	if assign == nil {
		return HasOne[P, T, K]{}, fmt.Errorf("orm: has-one requires an assign callback")
	}
	return HasOne[P, T, K]{definition: definition, assign: assign}, nil
}

func MustHasOne[P, T, PV, TV any, K comparable](
	parent Table[P], target Table[T], parentKeyColumn Column[P, PV], targetForeignKey Column[T, TV],
	parentKey func(P) (K, bool), targetForeignKeyValue func(T) (K, bool), assign func(*P, *T), options RelationOptions,
) HasOne[P, T, K] {
	relation, err := NewHasOne(parent, target, parentKeyColumn, targetForeignKey, parentKey, targetForeignKeyValue, assign, options)
	if err != nil {
		panic(err)
	}
	return relation
}

func (relation HasOne[P, T, K]) Build(parents []P, scope SelectBuilder[T]) ([]Statement, error) {
	if err := relation.definition.validate(); err != nil {
		return nil, err
	}
	if relation.assign == nil {
		return nil, fmt.Errorf("orm: invalid has-one relation")
	}
	keys := uniqueKeys(parents, relation.definition.parentKey)
	return buildScopedRelationStatements(relation.definition, keys, scope)
}

func (relation HasOne[P, T, K]) Load(ctx context.Context, executor Executor, parents []P, scope SelectBuilder[T]) ([]P, error) {
	if err := validateExecutor(executor, false); err != nil {
		return nil, err
	}
	result := append([]P(nil), parents...)
	statements, err := relation.Build(result, scope)
	if err != nil {
		return nil, err
	}
	byKey := make(map[K]T)
	for _, statement := range statements {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		items, err := queryModels(ctx, executor, relation.definition.target, statement)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			key, ok := relation.definition.targetKey(item)
			if !ok {
				return nil, fmt.Errorf("orm: has-one target returned no foreign key")
			}
			if _, exists := byKey[key]; exists {
				return nil, fmt.Errorf("orm: has-one relation returned multiple targets")
			}
			byKey[key] = item
		}
	}
	for index := range result {
		key, ok := relation.definition.parentKey(result[index])
		item, exists := byKey[key]
		if !ok || !exists {
			relation.assign(&result[index], nil)
			continue
		}
		copy := item
		relation.assign(&result[index], &copy)
	}
	return result, nil
}

// HasMany explicitly maps a parent key to an ordered target collection.
type HasMany[P, T any, K comparable] struct {
	definition relationDefinition[P, T, K]
	assign     func(*P, []T)
}

func NewHasMany[P, T, PV, TV any, K comparable](
	parent Table[P], target Table[T], parentKeyColumn Column[P, PV], targetForeignKey Column[T, TV],
	parentKey func(P) (K, bool), targetForeignKeyValue func(T) (K, bool), assign func(*P, []T), options RelationOptions,
) (HasMany[P, T, K], error) {
	definition, err := newRelationDefinition(
		parent, target, parentKeyColumn, targetForeignKey, parentKey, targetForeignKeyValue, options,
	)
	if err != nil {
		return HasMany[P, T, K]{}, err
	}
	if assign == nil {
		return HasMany[P, T, K]{}, fmt.Errorf("orm: has-many requires an assign callback")
	}
	return HasMany[P, T, K]{definition: definition, assign: assign}, nil
}

func MustHasMany[P, T, PV, TV any, K comparable](
	parent Table[P], target Table[T], parentKeyColumn Column[P, PV], targetForeignKey Column[T, TV],
	parentKey func(P) (K, bool), targetForeignKeyValue func(T) (K, bool), assign func(*P, []T), options RelationOptions,
) HasMany[P, T, K] {
	relation, err := NewHasMany(parent, target, parentKeyColumn, targetForeignKey, parentKey, targetForeignKeyValue, assign, options)
	if err != nil {
		panic(err)
	}
	return relation
}

func (relation HasMany[P, T, K]) Build(parents []P, scope SelectBuilder[T]) ([]Statement, error) {
	if err := relation.definition.validate(); err != nil {
		return nil, err
	}
	if relation.assign == nil {
		return nil, fmt.Errorf("orm: invalid has-many relation")
	}
	keys := uniqueKeys(parents, relation.definition.parentKey)
	return buildScopedRelationStatements(relation.definition, keys, scope)
}

func (relation HasMany[P, T, K]) Load(ctx context.Context, executor Executor, parents []P, scope SelectBuilder[T]) ([]P, error) {
	if err := validateExecutor(executor, false); err != nil {
		return nil, err
	}
	result := append([]P(nil), parents...)
	statements, err := relation.Build(result, scope)
	if err != nil {
		return nil, err
	}
	byKey := make(map[K][]T)
	for _, statement := range statements {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		items, err := queryModels(ctx, executor, relation.definition.target, statement)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			key, ok := relation.definition.targetKey(item)
			if !ok {
				return nil, fmt.Errorf("orm: has-many target returned no foreign key")
			}
			byKey[key] = append(byKey[key], item)
		}
	}
	for index := range result {
		key, ok := relation.definition.parentKey(result[index])
		if !ok {
			relation.assign(&result[index], nil)
			continue
		}
		relation.assign(&result[index], append([]T(nil), byKey[key]...))
	}
	return result, nil
}

func uniqueKeys[P any, K comparable](parents []P, key func(P) (K, bool)) []K {
	result := make([]K, 0, len(parents))
	seen := make(map[K]struct{}, len(parents))
	for _, parent := range parents {
		value, ok := key(parent)
		if !ok {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// relationInPredicate binds normalized relationship keys to a constructor-
// validated physical column. The column's Go wrapper type may differ from K
// (for example *int64 nullable foreign key to an int64 primary key); generated
// callbacks are responsible for normalizing both sides to K.
func relationInPredicate[M any, K comparable](table Table[M], column identifier, keys []K) Predicate[M] {
	args := make([]any, len(keys))
	for index, key := range keys {
		args[index] = freezeArg(key)
	}
	return Predicate[M]{
		table: table.name, identity: table.identity, column: column,
		kind: predicateIn, args: args,
	}
}

func buildScopedRelationStatements[P, T any, K comparable](
	definition relationDefinition[P, T, K], keys []K, scope SelectBuilder[T],
) ([]Statement, error) {
	baseArgs, err := validateRelationScope(definition.target, scope)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return []Statement{}, nil
	}
	batchSize := definition.limit - baseArgs
	if batchSize < 1 {
		return nil, fmt.Errorf("orm: relation scope exhausts the parameter limit")
	}
	statements := make([]Statement, 0, (len(keys)+batchSize-1)/batchSize)
	for start := 0; start < len(keys); start += batchSize {
		end := min(start+batchSize, len(keys))
		statement, err := scope.Where(relationInPredicate(definition.target, definition.targetColumn, keys[start:end])).Build()
		if err != nil {
			return nil, err
		}
		if len(statement.args) > definition.limit {
			return nil, fmt.Errorf("orm: relation statement exceeds the parameter limit")
		}
		statements = append(statements, statement)
	}
	return statements, nil
}

func validateRelationScope[T any](target Table[T], scope SelectBuilder[T]) (int, error) {
	if scope.table.name != target.name || scope.table.identity != target.identity {
		return 0, fmt.Errorf("orm: relation scope belongs to a different table")
	}
	if scope.limit != nil || scope.offset != nil {
		return 0, fmt.Errorf("orm: paginate parents before loading relationships; target scopes cannot paginate")
	}
	if scope.lock != lockNone {
		return 0, fmt.Errorf("orm: relation target scope cannot apply a row lock")
	}
	statement, err := scope.Build()
	if err != nil {
		return 0, err
	}
	return len(statement.args), nil
}

// ManyToMany explicitly maps parent and target keys through a generated join
// table definition. scan reads join-parent-key followed by the target columns.
type ManyToMany[P, T any, PK, TK comparable] struct {
	parent           Table[P]
	target           Table[T]
	parentKeyColumn  Column[P, PK]
	targetKeyColumn  Column[T, TK]
	joinTable        identifier
	joinParentColumn identifier
	joinTargetColumn identifier
	parentKey        func(P) (PK, bool)
	targetKey        func(T) (TK, bool)
	scan             func(RowScanner) (PK, T, error)
	assign           func(*P, []T)
	limit            int
}

func (relation ManyToMany[P, T, PK, TK]) validate() error {
	if err := relation.parent.validate(); err != nil {
		return err
	}
	if err := relation.target.validate(); err != nil {
		return err
	}
	if relation.parentKeyColumn.table != relation.parent.name ||
		relation.parentKeyColumn.identity != relation.parent.identity ||
		relation.targetKeyColumn.table != relation.target.name ||
		relation.targetKeyColumn.identity != relation.target.identity ||
		relation.joinTable.name == "" || relation.joinParentColumn.name == "" ||
		relation.joinTargetColumn.name == "" || relation.parentKey == nil ||
		relation.targetKey == nil || relation.scan == nil || relation.assign == nil ||
		relation.limit < 1 || relation.limit > PostgreSQLParameterLimit {
		return fmt.Errorf("orm: invalid many-to-many relation")
	}
	return nil
}

func NewManyToMany[P, T any, PK, TK comparable](
	parent Table[P], target Table[T], parentKeyColumn Column[P, PK], targetKeyColumn Column[T, TK],
	joinTableName, joinParentColumnName, joinTargetColumnName string,
	parentKey func(P) (PK, bool), targetKey func(T) (TK, bool),
	scan func(RowScanner) (PK, T, error), assign func(*P, []T), options RelationOptions,
) (ManyToMany[P, T, PK, TK], error) {
	if err := parent.validate(); err != nil {
		return ManyToMany[P, T, PK, TK]{}, err
	}
	if err := target.validate(); err != nil {
		return ManyToMany[P, T, PK, TK]{}, err
	}
	if parentKeyColumn.table != parent.name || parentKeyColumn.identity != parent.identity ||
		targetKeyColumn.table != target.name || targetKeyColumn.identity != target.identity {
		return ManyToMany[P, T, PK, TK]{}, fmt.Errorf("orm: many-to-many keys do not belong to their declared tables")
	}
	joinTable, err := newIdentifier(joinTableName)
	if err != nil {
		return ManyToMany[P, T, PK, TK]{}, err
	}
	joinParentColumn, err := newIdentifier(joinParentColumnName)
	if err != nil {
		return ManyToMany[P, T, PK, TK]{}, err
	}
	joinTargetColumn, err := newIdentifier(joinTargetColumnName)
	if err != nil {
		return ManyToMany[P, T, PK, TK]{}, err
	}
	if parentKey == nil || targetKey == nil || scan == nil || assign == nil {
		return ManyToMany[P, T, PK, TK]{}, fmt.Errorf("orm: many-to-many requires key, scan, and assign callbacks")
	}
	limit, err := options.parameterLimit()
	if err != nil {
		return ManyToMany[P, T, PK, TK]{}, err
	}
	return ManyToMany[P, T, PK, TK]{
		parent: parent, target: target, parentKeyColumn: parentKeyColumn, targetKeyColumn: targetKeyColumn,
		joinTable: joinTable, joinParentColumn: joinParentColumn, joinTargetColumn: joinTargetColumn,
		parentKey: parentKey, targetKey: targetKey, scan: scan, assign: assign, limit: limit,
	}, nil
}

func MustManyToMany[P, T any, PK, TK comparable](
	parent Table[P], target Table[T], parentKeyColumn Column[P, PK], targetKeyColumn Column[T, TK],
	joinTableName, joinParentColumnName, joinTargetColumnName string,
	parentKey func(P) (PK, bool), targetKey func(T) (TK, bool),
	scan func(RowScanner) (PK, T, error), assign func(*P, []T), options RelationOptions,
) ManyToMany[P, T, PK, TK] {
	relation, err := NewManyToMany(
		parent, target, parentKeyColumn, targetKeyColumn,
		joinTableName, joinParentColumnName, joinTargetColumnName,
		parentKey, targetKey, scan, assign, options,
	)
	if err != nil {
		panic(err)
	}
	return relation
}

func (relation ManyToMany[P, T, PK, TK]) Build(parents []P, scope SelectBuilder[T]) ([]Statement, error) {
	if err := relation.validate(); err != nil {
		return nil, err
	}
	keys := uniqueKeys(parents, relation.parentKey)
	baseArgs, err := validateRelationScope(relation.target, scope)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return []Statement{}, nil
	}
	batchSize := relation.limit - baseArgs
	if batchSize < 1 {
		return nil, fmt.Errorf("orm: relation scope exhausts the parameter limit")
	}
	statements := make([]Statement, 0, (len(keys)+batchSize-1)/batchSize)
	for start := 0; start < len(keys); start += batchSize {
		end := min(start+batchSize, len(keys))
		statement, err := relation.buildChunk(scope, keys[start:end])
		if err != nil {
			return nil, err
		}
		statements = append(statements, statement)
	}
	return statements, nil
}

func (relation ManyToMany[P, T, PK, TK]) buildChunk(scope SelectBuilder[T], keys []PK) (Statement, error) {
	compiler := &sqlCompiler{}
	var query strings.Builder
	query.WriteString("SELECT ")
	query.WriteString(relation.joinTable.quoted())
	query.WriteByte('.')
	query.WriteString(relation.joinParentColumn.quoted())
	query.WriteString(", ")
	writeMappedColumns(&query, relation.target)
	query.WriteString(" FROM ")
	query.WriteString(relation.target.name.quoted())
	query.WriteString(" JOIN ")
	query.WriteString(relation.joinTable.quoted())
	query.WriteString(" ON ")
	query.WriteString(relation.joinTable.quoted())
	query.WriteByte('.')
	query.WriteString(relation.joinTargetColumn.quoted())
	query.WriteString(" = ")
	query.WriteString(relation.targetKeyColumn.qualified())
	if err := writePredicates(&query, compiler, relation.target.name, relation.target.identity, scope.predicates); err != nil {
		return Statement{}, err
	}
	if len(scope.predicates) == 0 {
		query.WriteString(" WHERE ")
	} else {
		query.WriteString(" AND ")
	}
	query.WriteString(relation.joinTable.quoted())
	query.WriteByte('.')
	query.WriteString(relation.joinParentColumn.quoted())
	query.WriteString(" IN (")
	for index, key := range keys {
		if index > 0 {
			query.WriteString(", ")
		}
		query.WriteString(compiler.bind(key))
	}
	query.WriteByte(')')
	if err := writeOrders(&query, relation.target.name, relation.target.identity, scope.orders); err != nil {
		return Statement{}, err
	}
	if len(compiler.args) > relation.limit {
		return Statement{}, fmt.Errorf("orm: relation statement exceeds the parameter limit")
	}
	return newCompiledStatement(query.String(), compiler.args)
}

func (relation ManyToMany[P, T, PK, TK]) Load(ctx context.Context, executor Executor, parents []P, scope SelectBuilder[T]) ([]P, error) {
	if err := validateExecutor(executor, false); err != nil {
		return nil, err
	}
	result := append([]P(nil), parents...)
	statements, err := relation.Build(result, scope)
	if err != nil {
		return nil, err
	}
	byParent := make(map[PK][]T)
	seen := make(map[PK]map[TK]struct{})
	for _, statement := range statements {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows, err := executor.QueryContext(ctx, statement.query, statement.args...)
		if err != nil {
			return nil, persistenceError(OperationSelect, relation.target.Name(), err)
		}
		readErr := func() (err error) {
			defer func() {
				if closeErr := rows.Close(); closeErr != nil {
					err = errors.Join(err, persistenceError(OperationSelect, relation.target.Name(), closeErr))
				}
			}()
			for rows.Next() {
				parentKey, target, scanErr := relation.scan(rows)
				if scanErr != nil {
					return persistenceError(OperationSelect, relation.target.Name(), scanErr)
				}
				targetKey, ok := relation.targetKey(target)
				if !ok {
					return fmt.Errorf("orm: many-to-many target returned no key")
				}
				if seen[parentKey] == nil {
					seen[parentKey] = make(map[TK]struct{})
				}
				if _, exists := seen[parentKey][targetKey]; exists {
					continue
				}
				seen[parentKey][targetKey] = struct{}{}
				byParent[parentKey] = append(byParent[parentKey], target)
			}
			if rowsErr := rows.Err(); rowsErr != nil {
				return persistenceError(OperationSelect, relation.target.Name(), rowsErr)
			}
			return nil
		}()
		if readErr != nil {
			return nil, readErr
		}
	}
	for index := range result {
		key, ok := relation.parentKey(result[index])
		if !ok {
			relation.assign(&result[index], nil)
			continue
		}
		relation.assign(&result[index], append([]T(nil), byParent[key]...))
	}
	return result, nil
}

// BuildAttach returns an idempotent PostgreSQL join insert. A unique constraint
// on the generated join key pair makes duplicate attaches report false; other
// constraint failures are not swallowed.
func (relation ManyToMany[P, T, PK, TK]) BuildAttach(parentKey PK, targetKey TK) (Statement, error) {
	if err := relation.validate(); err != nil {
		return Statement{}, err
	}
	if isNilValue(parentKey) || isNilValue(targetKey) {
		return Statement{}, fmt.Errorf("orm: relation keys cannot be nil")
	}
	query := "INSERT INTO " + relation.joinTable.quoted() + " (" +
		relation.joinParentColumn.quoted() + ", " + relation.joinTargetColumn.quoted() +
		") VALUES ($1, $2) ON CONFLICT (" + relation.joinParentColumn.quoted() + ", " +
		relation.joinTargetColumn.quoted() + ") DO NOTHING"
	return newCompiledStatement(query, []any{parentKey, targetKey})
}

func (relation ManyToMany[P, T, PK, TK]) Attach(ctx context.Context, executor Executor, parentKey PK, targetKey TK) (bool, error) {
	if err := validateExecutor(executor, false); err != nil {
		return false, err
	}
	statement, err := relation.BuildAttach(parentKey, targetKey)
	if err != nil {
		return false, err
	}
	result, err := executor.ExecContext(ctx, statement.query, statement.args...)
	if err != nil {
		return false, persistenceError(OperationInsert, relation.joinTable.name, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, persistenceError(OperationInsert, relation.joinTable.name, err)
	}
	return rows > 0, nil
}

// BuildDetach always constrains both generated join columns.
func (relation ManyToMany[P, T, PK, TK]) BuildDetach(parentKey PK, targetKey TK) (Statement, error) {
	if err := relation.validate(); err != nil {
		return Statement{}, err
	}
	if isNilValue(parentKey) || isNilValue(targetKey) {
		return Statement{}, fmt.Errorf("orm: relation keys cannot be nil")
	}
	query := "DELETE FROM " + relation.joinTable.quoted() + " WHERE " +
		relation.joinParentColumn.quoted() + " = $1 AND " +
		relation.joinTargetColumn.quoted() + " = $2"
	return newCompiledStatement(query, []any{parentKey, targetKey})
}

func (relation ManyToMany[P, T, PK, TK]) Detach(ctx context.Context, executor Executor, parentKey PK, targetKey TK) (bool, error) {
	if err := validateExecutor(executor, false); err != nil {
		return false, err
	}
	statement, err := relation.BuildDetach(parentKey, targetKey)
	if err != nil {
		return false, err
	}
	result, err := executor.ExecContext(ctx, statement.query, statement.args...)
	if err != nil {
		return false, persistenceError(OperationDelete, relation.joinTable.name, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, persistenceError(OperationDelete, relation.joinTable.name, err)
	}
	return rows > 0, nil
}

func writeOrders[M any](query *strings.Builder, table identifier, identity *tableIdentity, orders []Order[M]) error {
	if len(orders) == 0 {
		return nil
	}
	query.WriteString(" ORDER BY ")
	for index, order := range orders {
		if order.table != table || order.identity != identity || identity == nil {
			return fmt.Errorf("orm: order belongs to a different table")
		}
		if index > 0 {
			query.WriteString(", ")
		}
		query.WriteString(order.table.quoted())
		query.WriteByte('.')
		query.WriteString(order.column.quoted())
		query.WriteByte(' ')
		query.WriteString(order.direction)
	}
	return nil
}
