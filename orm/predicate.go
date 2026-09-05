package orm

import (
	"fmt"
	"strings"
)

type predicateKind uint8

const (
	predicateCompare predicateKind = iota
	predicateIn
	predicateNull
	predicateConstant
	predicateAnd
	predicateOr
	predicateNot
)

// Predicate is tied at compile time to one model type.
type Predicate[M any] struct {
	table    identifier
	identity *tableIdentity
	column   identifier
	kind     predicateKind
	op       string
	args     []any
	truth    bool
	children []Predicate[M]
}

func (column Column[M, V]) compare(operator string, value V) Predicate[M] {
	return Predicate[M]{
		table: column.table, identity: column.identity, column: column.name, kind: predicateCompare,
		op: operator, args: []any{freezeArg(value)},
	}
}

func (column Column[M, V]) Eq(value V) Predicate[M]    { return column.compare("=", value) }
func (column Column[M, V]) NotEq(value V) Predicate[M] { return column.compare("<>", value) }
func (column Column[M, V]) Lt(value V) Predicate[M]    { return column.compare("<", value) }
func (column Column[M, V]) Lte(value V) Predicate[M]   { return column.compare("<=", value) }
func (column Column[M, V]) Gt(value V) Predicate[M]    { return column.compare(">", value) }
func (column Column[M, V]) Gte(value V) Predicate[M]   { return column.compare(">=", value) }

// In copies values so subsequent caller mutation cannot change the predicate.
// An empty set compiles to FALSE.
func (column Column[M, V]) In(values ...V) Predicate[M] {
	if len(values) == 0 {
		return Predicate[M]{table: column.table, identity: column.identity, kind: predicateConstant, truth: false}
	}
	args := make([]any, len(values))
	for index, value := range values {
		args[index] = freezeArg(value)
	}
	return Predicate[M]{table: column.table, identity: column.identity, column: column.name, kind: predicateIn, args: args}
}

func (column Column[M, V]) IsNull() Predicate[M] {
	return Predicate[M]{table: column.table, identity: column.identity, column: column.name, kind: predicateNull, op: "IS NULL"}
}

func (column Column[M, V]) IsNotNull() Predicate[M] {
	return Predicate[M]{table: column.table, identity: column.identity, column: column.name, kind: predicateNull, op: "IS NOT NULL"}
}

// Like is available only for string columns.
func Like[M any](column Column[M, string], pattern string) Predicate[M] {
	return Predicate[M]{
		table: column.table, identity: column.identity, column: column.name, kind: predicateCompare,
		op: "LIKE", args: []any{pattern},
	}
}

// And groups predicates with explicit parentheses. An empty group is invalid.
func And[M any](predicates ...Predicate[M]) Predicate[M] {
	return groupedPredicate(predicateAnd, predicates)
}

// Or groups predicates with explicit parentheses. An empty group is invalid.
func Or[M any](predicates ...Predicate[M]) Predicate[M] {
	return groupedPredicate(predicateOr, predicates)
}

// Not negates one predicate with explicit parentheses.
func Not[M any](predicate Predicate[M]) Predicate[M] {
	return Predicate[M]{table: predicate.table, identity: predicate.identity, kind: predicateNot, children: []Predicate[M]{predicate}}
}

func groupedPredicate[M any](kind predicateKind, predicates []Predicate[M]) Predicate[M] {
	result := Predicate[M]{kind: kind, children: append([]Predicate[M](nil), predicates...)}
	if len(predicates) > 0 {
		result.table = predicates[0].table
		result.identity = predicates[0].identity
	}
	return result
}

type sqlCompiler struct{ args []any }

func (compiler *sqlCompiler) bind(value any) string {
	compiler.args = append(compiler.args, value)
	return fmt.Sprintf("$%d", len(compiler.args))
}

func (predicate Predicate[M]) compile(compiler *sqlCompiler, table identifier, identity *tableIdentity) (string, error) {
	if table.name == "" || predicate.table.name == "" || identity == nil || predicate.identity == nil {
		return "", fmt.Errorf("orm: invalid predicate")
	}
	if predicate.table != table || predicate.identity != identity {
		return "", fmt.Errorf("orm: predicate belongs to a different table")
	}
	if predicate.kind == predicateConstant {
		if predicate.truth {
			return "TRUE", nil
		}
		return "FALSE", nil
	}
	qualified := predicate.table.quoted() + "." + predicate.column.quoted()
	switch predicate.kind {
	case predicateCompare:
		if len(predicate.args) != 1 {
			return "", fmt.Errorf("orm: invalid comparison predicate")
		}
		if isNilValue(predicate.args[0]) {
			return "", fmt.Errorf("orm: comparison value is nil; use IsNull or IsNotNull")
		}
		return qualified + " " + predicate.op + " " + compiler.bind(predicate.args[0]), nil
	case predicateIn:
		placeholders := make([]byte, 0, len(predicate.args)*4)
		for index, value := range predicate.args {
			if isNilValue(value) {
				return "", fmt.Errorf("orm: IN value is nil; use an explicit null predicate")
			}
			if index > 0 {
				placeholders = append(placeholders, ',', ' ')
			}
			placeholders = append(placeholders, compiler.bind(value)...)
		}
		return qualified + " IN (" + string(placeholders) + ")", nil
	case predicateNull:
		return qualified + " " + predicate.op, nil
	case predicateAnd, predicateOr:
		if len(predicate.children) == 0 {
			return "", fmt.Errorf("orm: predicate group must not be empty")
		}
		separator := " AND "
		if predicate.kind == predicateOr {
			separator = " OR "
		}
		parts := make([]string, 0, len(predicate.children))
		for _, child := range predicate.children {
			compiled, err := child.compile(compiler, table, identity)
			if err != nil {
				return "", err
			}
			parts = append(parts, compiled)
		}
		return "(" + strings.Join(parts, separator) + ")", nil
	case predicateNot:
		if len(predicate.children) != 1 {
			return "", fmt.Errorf("orm: NOT requires one predicate")
		}
		compiled, err := predicate.children[0].compile(compiler, table, identity)
		if err != nil {
			return "", err
		}
		return "NOT (" + compiled + ")", nil
	default:
		return "", fmt.Errorf("orm: invalid predicate")
	}
}

// Order is tied at compile time to one model type.
type Order[M any] struct {
	table     identifier
	identity  *tableIdentity
	column    identifier
	direction string
}

func (column Column[M, V]) Asc() Order[M] {
	return Order[M]{table: column.table, identity: column.identity, column: column.name, direction: "ASC"}
}

func (column Column[M, V]) Desc() Order[M] {
	return Order[M]{table: column.table, identity: column.identity, column: column.name, direction: "DESC"}
}
