package orm

import (
	"fmt"
	"reflect"
)

// PostgreSQLParameterLimit is the maximum number of bind parameters in one
// PostgreSQL statement. Builders reject larger statements before execution.
const PostgreSQLParameterLimit = 65_535

// Statement is immutable inspectable SQL produced by a builder.
type Statement struct {
	query string
	args  []any
}

func newStatement(query string, args []any) Statement {
	return Statement{query: query, args: cloneArgs(args)}
}

func newCompiledStatement(query string, args []any) (Statement, error) {
	if len(args) > PostgreSQLParameterLimit {
		return Statement{}, fmt.Errorf("orm: statement has %d parameters; PostgreSQL limit is %d", len(args), PostgreSQLParameterLimit)
	}
	return newStatement(query, args), nil
}

// SQL returns the compiled PostgreSQL statement.
func (statement Statement) SQL() string { return statement.query }

// Args returns a copy of the parameter list.
func (statement Statement) Args() []any {
	return cloneArgs(statement.args)
}

func cloneArgs(args []any) []any {
	result := make([]any, len(args))
	for index, value := range args {
		result[index] = freezeArg(value)
	}
	return result
}

func freezeArg(value any) any {
	if value == nil {
		return nil
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Slice && reflected.Type().Elem().Kind() == reflect.Uint8 {
		if reflected.IsNil() {
			return value
		}
		copy := reflect.MakeSlice(reflected.Type(), reflected.Len(), reflected.Len())
		reflect.Copy(copy, reflected)
		return copy.Interface()
	}
	return value
}

func isNilValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
