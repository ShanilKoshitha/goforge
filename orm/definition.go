package orm

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"regexp"
)

// Executor is implemented by both *sql.DB and *sql.Tx.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

var (
	_ Executor = (*sql.DB)(nil)
	_ Executor = (*sql.Tx)(nil)
)

// RowScanner is the common scanning surface of *sql.Row and *sql.Rows.
type RowScanner interface {
	Scan(...any) error
}

var identifierPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

type identifier struct{ name string }

// tableIdentity deliberately has non-zero size. The Go spec permits distinct
// zero-sized allocations to have the same address, which would collapse the
// provenance of independently generated descriptors for the same SQL table.
type tableIdentity struct{ marker byte }

func newIdentifier(name string) (identifier, error) {
	if len(name) == 0 || len(name) > 63 || !identifierPattern.MatchString(name) {
		return identifier{}, fmt.Errorf("orm: invalid generated identifier %q", name)
	}
	return identifier{name: name}, nil
}

func (value identifier) quoted() string { return `"` + value.name + `"` }

// Mapper explicitly describes how selected columns become a model. Generated
// declarations should build it once with NewMapper or MustMapper.
type Mapper[M any] struct {
	columns []identifier
	scan    func(RowScanner) (M, error)
}

// NewMapper validates generated column names and rejects duplicates.
func NewMapper[M any](columns []string, scan func(RowScanner) (M, error)) (Mapper[M], error) {
	if len(columns) == 0 {
		return Mapper[M]{}, fmt.Errorf("orm: mapper requires at least one column")
	}
	if scan == nil {
		return Mapper[M]{}, fmt.Errorf("orm: mapper requires a scan function")
	}
	result := Mapper[M]{columns: make([]identifier, 0, len(columns)), scan: scan}
	seen := make(map[string]struct{}, len(columns))
	for _, name := range columns {
		column, err := newIdentifier(name)
		if err != nil {
			return Mapper[M]{}, err
		}
		if _, exists := seen[name]; exists {
			return Mapper[M]{}, fmt.Errorf("orm: duplicate generated column %q", name)
		}
		seen[name] = struct{}{}
		result.columns = append(result.columns, column)
	}
	return result, nil
}

// MustMapper is intended for package-level generated declarations.
func MustMapper[M any](columns []string, scan func(RowScanner) (M, error)) Mapper[M] {
	mapper, err := NewMapper(columns, scan)
	if err != nil {
		panic(err)
	}
	return mapper
}

// Table binds a validated generated table name to an explicit mapper.
type Table[M any] struct {
	name     identifier
	mapper   Mapper[M]
	identity *tableIdentity
}

// NewTable validates a generated table definition.
func NewTable[M any](name string, mapper Mapper[M]) (Table[M], error) {
	tableName, err := newIdentifier(name)
	if err != nil {
		return Table[M]{}, err
	}
	if len(mapper.columns) == 0 || mapper.scan == nil {
		return Table[M]{}, fmt.Errorf("orm: table %s requires a valid mapper", name)
	}
	return Table[M]{name: tableName, mapper: mapper, identity: &tableIdentity{}}, nil
}

// MustTable is intended for package-level generated declarations.
func MustTable[M any](name string, mapper Mapper[M]) Table[M] {
	table, err := NewTable(name, mapper)
	if err != nil {
		panic(err)
	}
	return table
}

// Name exposes the validated table name for diagnostics and generated code.
func (table Table[M]) Name() string { return table.name.name }

func (table Table[M]) validate() error {
	if table.name.name == "" || len(table.mapper.columns) == 0 || table.mapper.scan == nil || table.identity == nil {
		return fmt.Errorf("orm: invalid table definition")
	}
	return nil
}

// Column is a typed reference to a column belonging to one generated model.
type Column[M, V any] struct {
	table    identifier
	identity *tableIdentity
	name     identifier
}

// NewColumn validates a generated column and requires it to be mapped by table.
func NewColumn[M, V any](table Table[M], name string) (Column[M, V], error) {
	columnName, err := newIdentifier(name)
	if err != nil {
		return Column[M, V]{}, err
	}
	for _, mapped := range table.mapper.columns {
		if mapped.name == name {
			return Column[M, V]{table: table.name, identity: table.identity, name: columnName}, nil
		}
	}
	return Column[M, V]{}, fmt.Errorf("orm: column %q is not mapped by table %s", name, table.Name())
}

// MustColumn is intended for package-level generated declarations.
func MustColumn[M, V any](table Table[M], name string) Column[M, V] {
	column, err := NewColumn[M, V](table, name)
	if err != nil {
		panic(err)
	}
	return column
}

func (column Column[M, V]) qualified() string {
	return column.table.quoted() + "." + column.name.quoted()
}

type transactionCarrier interface{ carriesORMTransaction() bool }
type executorValidator interface{ validORMExecutor() bool }

func validateExecutor(executor Executor, requireTransaction bool) error {
	if executor == nil {
		return fmt.Errorf("orm: executor is required")
	}
	value := reflect.ValueOf(executor)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return fmt.Errorf("orm: executor is required")
		}
	}
	if validator, ok := executor.(executorValidator); ok && !validator.validORMExecutor() {
		return fmt.Errorf("orm: executor is required")
	}
	if !requireTransaction {
		return nil
	}
	switch typed := executor.(type) {
	case *sql.Tx:
		if typed != nil {
			return nil
		}
	case transactionCarrier:
		if typed.carriesORMTransaction() {
			return nil
		}
	}
	return ErrTransactionRequired
}
