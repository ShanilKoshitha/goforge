# Typed ORM

GoForge's ORM is generated, typed Go over `database/sql`. Models, migrations,
predicates, and execution stay visible in the application. There is no runtime
model registry or string-based query language.

## Generate a model

Create the relationship target first, then the model that refers to it:

```sh
forge make:model Customer --field name:string
forge make:model Invoice --field number:string --field total_cents:integer --field paid:boolean --belongs-to customer:Customer
forge migrate
```

The field grammar is
`<name>:<string|text|integer|boolean>[:required|nullable]`. Fields are required
by default. Options may be repeated and their declaration order is preserved.
A belongs-to specification is `<name>:<ExistingModel>`; its target must already
exist with the conventional required `ID` primary key so the generator can
validate its type and create a real foreign key. For an application-owned model
with a custom primary key, write the foreign-key field and relationship tag in
ordinary Go, add the reviewed SQL constraint, then run `forge orm:generate`.

Each command writes application-owned source:

- `internal/models/customer.go` or `internal/models/invoice.go` contains the
  ordinary Go type and `forge` tags.
- `database/migrations/*_create_customers.{up,down}.sql` or the matching
  invoices migration contains the PostgreSQL schema.
- `internal/models/zz_orm_gen.go` is the replaceable generated store. Do not
  edit this file by hand; regenerate it from the model declarations.

Generators refuse to overwrite the model or migration files. The model and SQL
migration are the source you own after generation.

## Use the generated store

The examples below assume the generated application's module path is
`example.com/acme`; replace that import with the module from its `go.mod`.
Both `*sql.DB` and `*sql.Tx` implement `orm.Executor`.

```go
package billing

import (
	"context"
	"database/sql"

	"example.com/acme/internal/models"
)

func findInvoice(ctx context.Context, db *sql.DB, id int64) (models.Invoice, error) {
	store := models.NewStore(db)
	return store.Invoices().Select().
		Where(models.InvoiceColumns.ID.Eq(id)).
		First(ctx, db)
}
```

`First` returns an error matching `orm.ErrNotFound` when no row matches. The
terminal operation receives the executor explicitly; this keeps transaction
use visible at each database call.

### Select, filter, order, and page

Predicates and order clauses are typed to their model. Multiple predicates in
`Where` are combined with `AND`.

```go
func openInvoices(ctx context.Context, db *sql.DB, page int) ([]models.Invoice, error) {
	if page < 1 {
		page = 1
	}

	const pageSize = 25
	store := models.NewStore(db)
	return store.Invoices().Select().
		Where(
			models.InvoiceColumns.Paid.Eq(false),
			models.InvoiceColumns.TotalCents.Gt(int64(0)),
		).
		OrderBy(models.InvoiceColumns.ID.Desc()).
		Limit(pageSize).
		Offset((page - 1) * pageSize).
		All(ctx, db)
}
```

Use `orm.And`, `orm.Or`, and the typed string helpers when a filter needs
grouping or pattern matching:

```go
query := store.Invoices().Select().Where(
	orm.Or(
		models.InvoiceColumns.Number.Eq("INV-1001"),
		orm.Like(models.InvoiceColumns.Number, "OVERDUE-%"),
	),
)
```

Import `github.com/ShanilKoshitha/goforge/orm` for the `orm` identifiers in
this guide.

### Count and exists

`Count` and `Exists` use the same visible filter. Ordering, pagination, and row
locks do not change their aggregate SQL.

```go
unpaid := store.Invoices().Select().Where(models.InvoiceColumns.Paid.Eq(false))

count, err := unpaid.Count(ctx, db)
if err != nil {
	return err
}

exists, err := unpaid.Where(
	models.InvoiceColumns.CustomerID.Eq(customerID),
).Exists(ctx, db)
```

### Create

Generated create inputs contain writable columns. Generated IDs and timestamps
are returned by PostgreSQL and protected fields are not exposed on the input.

```go
invoice, err := store.Invoices().Create(models.InvoiceCreateInput{
	CustomerID: customerID,
	Number:     "INV-1001",
	TotalCents: int64(12_500),
	Paid:       false,
}).One(ctx, db)
```

Nullable fields and fields with database defaults use `orm.Field`. Its zero
value omits the column, `orm.Value(value)` supplies a value,
`orm.Null[T]()` writes SQL `NULL`, and `orm.Default[T]()` asks PostgreSQL for
the column default.

### Partial update and optimistic versions

Every member of a generated changes struct is an `orm.Field`, so omitted
members are unchanged. `orm.Value(false)` and `orm.Value(int64(0))` remain
present values.

```go
updated, err := store.Invoices().Update(models.InvoiceChanges{
	Paid: orm.Value(true),
}).
	Where(models.InvoiceColumns.ID.Eq(invoiceID)).
	One(ctx, db)
```

The generated model includes a version column. A standalone update can require
and increment the expected version atomically:

```go
updated, err := store.Invoices().Update(models.InvoiceChanges{
	TotalCents: orm.Value(int64(13_000)),
}).
	Where(models.InvoiceColumns.ID.Eq(invoiceID)).
	OptimisticVersion(orm.ExpectVersion(models.InvoiceColumns.Version, expectedVersion)).
	StaleOnZero().
	One(ctx, db)
if errors.Is(err, orm.ErrStale) {
	// Reload and ask the caller to resolve the concurrent change.
}
```

Import the standard `errors` package for this check. Only opt into
`StaleOnZero` when every zero-row result really means a stale version. An
owner- or tenant-scoped repository must distinguish hidden/missing rows from a
stale visible row instead, as generated resources do.

### Safe delete

Update and delete builders reject a statement with no predicate with
`orm.ErrUnsafeMutation`:

```go
deleted, err := store.Invoices().Delete().
	Where(models.InvoiceColumns.ID.Eq(invoiceID)).
	Exec(ctx, db)
```

`deleted` is the PostgreSQL affected-row count. Whole-table mutation requires
the conspicuous `.AllRows()` escape hatch; keep that decision at a reviewed
application boundary.

## Load relationships explicitly

The generated `Invoice` has a `CustomerID` and a `Customer *Customer` field.
Selecting invoices does not populate `Customer`. Load it explicitly with a
caller-provided target query:

```go
invoices, err := store.Invoices().Select().
	Where(models.InvoiceColumns.Paid.Eq(false)).
	All(ctx, db)
if err != nil {
	return nil, err
}

invoices, err = store.Invoices().LoadCustomer(
	ctx,
	invoices,
	store.Customers().Select(),
)
if err != nil {
	return nil, err
}
```

The loader batches the target lookup rather than issuing one query per invoice.
Its target query remains visible and can carry required authorization or tenant
predicates. Relationships never lazy-load when a Go field is read.

## Transactions and handwritten SQL

Use the standard library transaction boundary. Pass the same `*sql.Tx` to the
store and each terminal operation; handwritten SQL can participate in that
same transaction.

```go
func createAndCount(
	ctx context.Context,
	db *sql.DB,
	customerID int64,
) (models.Invoice, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return models.Invoice{}, err
	}
	defer tx.Rollback()

	store := models.NewStore(tx)
	invoice, err := store.Invoices().Create(models.InvoiceCreateInput{
		CustomerID: customerID,
		Number:     "INV-1002",
		TotalCents: int64(8_500),
		Paid:       false,
	}).One(ctx, tx)
	if err != nil {
		return models.Invoice{}, err
	}

	var customerInvoiceCount int64
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM invoices WHERE customer_id = $1`,
		customerID,
	).Scan(&customerInvoiceCount)
	if err != nil {
		return models.Invoice{}, err
	}

	if err := tx.Commit(); err != nil {
		return models.Invoice{}, err
	}
	return invoice, nil
}
```

For row locks, add `.ForUpdate()` or `.ForShare()` to a select and execute it
with `*sql.Tx`; the ORM rejects row locking through `*sql.DB`. For any query the
typed builders do not express, use `QueryContext`, `QueryRowContext`, or
`ExecContext` directly. Builders also expose `Build()` so application code and
tests can inspect the parameterized PostgreSQL `Statement.SQL()` and copied
`Statement.Args()` without executing it.

## Regenerate and check

When you edit a model, update its migration (or add a new migration for an
already-deployed database), then regenerate the typed store:

```sh
forge orm:generate
forge orm:generate --check
```

`--check` is non-mutating and fails when `internal/models/zz_orm_gen.go` differs
from the model declarations. `forge test` and `forge build` run this check as a
preflight.

## Ownership and deliberate non-goals

The generated model, tags, migrations, repositories, and calling services are
application code. Edit or replace them, write SQL directly, or use
`database/sql` alongside the typed builders. `make:model` does not add an HTTP
endpoint, a `user_id`, or an authorization policy. Add explicit scope
predicates in your repository, or use `make:resource` when the generated
authenticated JSON and browser workflow is the right starting point.

The ORM deliberately does not provide:

- Active Record methods such as `invoice.Save()`;
- lazy relationship loading, identity maps, or an implicit unit of work;
- runtime reflection or model discovery; or
- automatic migration generation by diffing Go structs against a live schema.

Schema changes remain reviewed SQL migrations. Database execution remains an
explicit call against a visible `*sql.DB` or `*sql.Tx`.
