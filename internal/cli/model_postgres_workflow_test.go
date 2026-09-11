package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestGeneratedModelPostgresWorkflow(t *testing.T) {
	databaseURL := os.Getenv("GOFORGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOFORGE_TEST_DATABASE_URL is not set")
	}

	root := projectRoot(t)
	scratch := t.TempDir()
	forgeBinary := filepath.Join(scratch, "forge")
	acceptanceBinary := filepath.Join(scratch, "model-acceptance")
	if runtime.GOOS == "windows" {
		forgeBinary += ".exe"
		acceptanceBinary += ".exe"
	}
	baseEnvironment := append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	if output, err := modelWorkflowCommand(root, baseEnvironment, "go", "build", "-o", forgeBinary, "./cmd/forge"); err != nil {
		t.Fatalf("build forge CLI: %v\n%s", err, output)
	}

	directory := filepath.Join(scratch, "models")
	if output, err := modelWorkflowCommand(scratch, baseEnvironment, forgeBinary, "new", "models", "--module", "example.com/issueboard", "--replace", root); err != nil {
		t.Fatalf("forge new: %v\n%s", err, output)
	}
	commands := [][]string{
		{"make:model", "Customer", "--field", "name:string"},
		{"make:model", "Invoice", "--field", "number:string", "--field", "total_cents:integer", "--field", "paid:boolean", "--field", "notes:text:nullable", "--belongs-to", "customer:Customer"},
	}
	for _, arguments := range commands {
		if output, err := modelWorkflowCommand(directory, baseEnvironment, forgeBinary, arguments...); err != nil {
			t.Fatalf("forge %s: %v\n%s", strings.Join(arguments, " "), err, output)
		}
	}
	if output, err := modelWorkflowCommand(directory, baseEnvironment, forgeBinary, "orm:generate", "--check"); err != nil {
		t.Fatalf("generated ORM is stale: %v\n%s", err, output)
	}
	for _, command := range [][]string{
		{"test", "./..."},
		{"vet", "./..."},
	} {
		if output, err := modelWorkflowCommand(directory, baseEnvironment, "go", command...); err != nil {
			t.Fatalf("generated application go %s: %v\n%s", strings.Join(command, " "), err, output)
		}
	}
	if output, err := modelWorkflowCommand(directory, baseEnvironment, forgeBinary, "build"); err != nil {
		t.Fatalf("forge build: %v\n%s", err, output)
	}

	acceptancePath := filepath.Join(directory, ".forge", "model_acceptance.go")
	if err := os.WriteFile(acceptancePath, []byte(modelPostgresAcceptanceProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := modelWorkflowCommand(directory, baseEnvironment, "go", "build", "-o", acceptanceBinary, "./.forge/model_acceptance.go"); err != nil {
		t.Fatalf("build model acceptance helper: %v\n%s", err, output)
	}

	schema := fmt.Sprintf("goforge_models_%d", time.Now().UnixNano())
	adminPath := filepath.Join(directory, ".forge", "acceptance_db.go")
	if err := os.WriteFile(adminPath, []byte(postgresAdminProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	adminEnvironment := append(baseEnvironment, "DATABASE_URL="+databaseURL)
	if output, err := modelWorkflowCommand(directory, adminEnvironment, "go", "run", "./.forge/acceptance_db.go", "create", schema); err != nil {
		t.Fatalf("create isolated PostgreSQL schema: %v\n%s", err, output)
	}
	t.Cleanup(func() {
		if output, err := modelWorkflowCommand(directory, adminEnvironment, "go", "run", "./.forge/acceptance_db.go", "drop", schema); err != nil {
			t.Errorf("drop isolated PostgreSQL schema: %v\n%s", err, output)
		}
	})
	isolatedURL, err := postgresSchemaURL(databaseURL, schema)
	if err != nil {
		t.Fatal(err)
	}
	environment := append(baseEnvironment, "DATABASE_URL="+isolatedURL, "APP_ENV=local")
	if output, err := modelWorkflowCommand(directory, environment, forgeBinary, "migrate"); err != nil {
		t.Fatalf("migrate generated model application: %v\n%s", err, output)
	}
	if output, err := modelWorkflowCommand(directory, environment, acceptanceBinary); err != nil {
		t.Fatalf("generated model PostgreSQL acceptance: %v\n%s", err, output)
	} else if !strings.Contains(output, "model_postgres_acceptance=passed") {
		t.Fatalf("generated model acceptance omitted success marker: %s", output)
	}
}

func modelWorkflowCommand(directory string, environment []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = directory
	command.Env = environment
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return string(output), fmt.Errorf("command %s exceeded five minutes: %w", name, ctx.Err())
	}
	return string(output), err
}

const modelPostgresAcceptanceProgram = `package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"example.com/issueboard/internal/models"
	"github.com/ShanilKoshitha/goforge/orm"
	ormpostgres "github.com/ShanilKoshitha/goforge/orm/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	must(err)
	defer db.Close()
	must(db.PingContext(ctx))

	store := models.NewStore(db)
	alpha := createCustomer(ctx, store, "Alpha")
	beta := createCustomer(ctx, store, "Beta")
	first := createInvoice(ctx, store, "INV-100", 100, false, alpha.ID)
	second := createInvoiceWithNotes(ctx, store, "INV-300", 300, false, alpha.ID, orm.Null[*string]())
	third := createInvoiceWithNotes(ctx, store, "INV-200", 200, false, beta.ID, orm.Value(pointer("concrete note")))
	assert(first.Notes == nil && second.Notes == nil && third.Notes != nil && *third.Notes == "concrete note", "nullable create Go values")
	var omittedSQL, nullSQL, concreteSQL sql.NullString
	must(db.QueryRowContext(ctx, ` + "`" + `SELECT
		(SELECT notes FROM invoices WHERE id = $1),
		(SELECT notes FROM invoices WHERE id = $2),
		(SELECT notes FROM invoices WHERE id = $3)` + "`" + `, first.ID, second.ID, third.ID).Scan(&omittedSQL, &nullSQL, &concreteSQL))
	assert(!omittedSQL.Valid && !nullSQL.Valid && concreteSQL.Valid && concreteSQL.String == "concrete note", "nullable create SQL values")

	found, err := store.Invoices().Select().Where(models.InvoiceColumns.ID.Eq(second.ID)).First(ctx, store.Invoices().Executor)
	must(err)
	assert(found.Number == "INV-300" && found.CustomerID == alpha.ID, "typed find")

	filtered, err := store.Invoices().Select().
		Where(models.InvoiceColumns.Paid.Eq(false)).
		OrderBy(models.InvoiceColumns.TotalCents.Desc(), models.InvoiceColumns.ID.Asc()).
		Limit(1).Offset(1).
		All(ctx, store.Invoices().Executor)
	must(err)
	assert(len(filtered) == 1 && filtered[0].ID == third.ID, "typed filter/order/pagination")
	count, err := store.Invoices().Select().Where(models.InvoiceColumns.CustomerID.Eq(alpha.ID)).Count(ctx, store.Invoices().Executor)
	must(err)
	assert(count == 2, "typed count")
	exists, err := store.Invoices().Select().Where(models.InvoiceColumns.Number.Eq("INV-100")).Exists(ctx, store.Invoices().Executor)
	must(err)
	assert(exists, "typed exists")

	statement, err := store.Invoices().Select().
		Where(models.InvoiceColumns.CustomerID.Eq(alpha.ID), models.InvoiceColumns.Paid.Eq(false)).
		OrderBy(models.InvoiceColumns.TotalCents.Desc()).Limit(2).Build()
	must(err)
	assert(strings.Contains(statement.SQL(), ` + "`" + `FROM "invoices"` + "`" + `) && strings.Contains(statement.SQL(), ` + "`" + `ORDER BY "invoices"."total_cents" DESC` + "`" + `), "inspectable statement SQL")
	assert(len(statement.Args()) == 3, "inspectable statement arguments")

	updated, err := store.Invoices().Update(models.InvoiceChanges{Paid: orm.Value(true)}).
		Where(models.InvoiceColumns.ID.Eq(first.ID)).
		OptimisticVersion(orm.ExpectVersion(models.InvoiceColumns.Version, first.Version)).
		StaleOnZero().
		One(ctx, store.Invoices().Executor)
	must(err)
	assert(updated.Paid && updated.Number == first.Number && updated.TotalCents == first.TotalCents && updated.Notes == nil && updated.Version == first.Version+1, "partial optimistic update")
	preserved, err := store.Invoices().Update(models.InvoiceChanges{Paid: orm.Value(true)}).
		Where(models.InvoiceColumns.ID.Eq(third.ID)).One(ctx, store.Invoices().Executor)
	must(err)
	assert(preserved.Notes != nil && *preserved.Notes == "concrete note", "partial update preserves nullable value")
	set, err := store.Invoices().Update(models.InvoiceChanges{Notes: orm.Value(pointer("set note"))}).
		Where(models.InvoiceColumns.ID.Eq(first.ID)).One(ctx, store.Invoices().Executor)
	must(err)
	assert(set.Notes != nil && *set.Notes == "set note", "partial update sets nullable value")
	cleared, err := store.Invoices().Update(models.InvoiceChanges{
		Notes: orm.Null[*string](),
	}).
		Where(models.InvoiceColumns.ID.Eq(third.ID)).One(ctx, store.Invoices().Executor)
	must(err)
	assert(cleared.Notes == nil, "partial update clears nullable value")
	var setSQL, clearedSQL sql.NullString
	must(db.QueryRowContext(ctx, ` + "`" + `SELECT
		(SELECT notes FROM invoices WHERE id = $1),
		(SELECT notes FROM invoices WHERE id = $2)` + "`" + `, first.ID, third.ID).Scan(&setSQL, &clearedSQL))
	assert(setSQL.Valid && setSQL.String == "set note" && !clearedSQL.Valid, "nullable partial update SQL values")
	_, err = store.Invoices().Update(models.InvoiceChanges{Number: orm.Value("stale")} ).
		Where(models.InvoiceColumns.ID.Eq(first.ID)).
		OptimisticVersion(orm.ExpectVersion(models.InvoiceColumns.Version, first.Version)).
		StaleOnZero().
		One(ctx, store.Invoices().Executor)
	assert(errors.Is(err, orm.ErrStale), "stale update classification")

	var observedQueries atomic.Int64
	observed := orm.ObserveExecutor(db, func(_ context.Context, event orm.StatementEvent, _ orm.Statement) {
		if event == orm.StatementQuery {
			observedQueries.Add(1)
		}
	})
	observedStore := models.NewStore(observed)
	parents, err := observedStore.Invoices().Select().OrderBy(models.InvoiceColumns.ID.Asc()).All(ctx, observedStore.Invoices().Executor)
	must(err)
	parents, err = observedStore.Invoices().LoadCustomer(ctx, parents, observedStore.Customers().Select())
	must(err)
	assert(len(parents) == 3 && observedQueries.Load() == 2, "batch belongs-to query budget")
	for _, invoice := range parents {
		assert(invoice.Customer != nil && invoice.Customer.ID == invoice.CustomerID, "batch belongs-to mapping")
	}

	rollback, err := db.BeginTx(ctx, nil)
	must(err)
	rollbackStore := models.NewStore(rollback)
	rolledBack := createCustomer(ctx, rollbackStore, "Rolled back")
	assert(rolledBack.ID > 0, "transaction rollback insert")
	must(rollback.Rollback())
	rolledBackExists, err := store.Customers().Select().Where(models.CustomerColumns.Name.Eq("Rolled back")).Exists(ctx, store.Customers().Executor)
	must(err)
	assert(!rolledBackExists, "transaction rollback")

	commit, err := db.BeginTx(ctx, nil)
	must(err)
	commitStore := models.NewStore(commit)
	committed := createCustomer(ctx, commitStore, "Committed")
	committedInvoice := createInvoice(ctx, commitStore, "INV-COMMIT", 900, true, committed.ID)
	var rawCount int
	must(commit.QueryRowContext(ctx, "SELECT COUNT(*) FROM invoices WHERE customer_id = $1", committed.ID).Scan(&rawCount))
	assert(rawCount == 1 && committedInvoice.CustomerID == committed.ID, "raw SQL with generated transaction")
	must(commit.Commit())
	committedExists, err := store.Invoices().Select().Where(models.InvoiceColumns.ID.Eq(committedInvoice.ID)).Exists(ctx, store.Invoices().Executor)
	must(err)
	assert(committedExists, "transaction commit")

	_, err = store.Customers().Create(models.CustomerCreateInput{Name: "duplicate-id"}, models.CustomerColumns.ID.Set(alpha.ID)).One(ctx, store.Customers().Executor)
	classified := ormpostgres.Classify(err)
	assert(errors.Is(classified, orm.ErrUnique), "unique constraint classification")
	var constraint *orm.ConstraintError
	assert(errors.As(classified, &constraint) && constraint.Constraint != "", "constraint detail")
	_, err = store.Invoices().Create(models.InvoiceCreateInput{Number: "bad-fk", TotalCents: 1, Paid: false, CustomerID: alpha.ID + 1_000_000}).One(ctx, store.Invoices().Executor)
	assert(errors.Is(ormpostgres.Classify(err), orm.ErrForeignKey), "foreign-key constraint classification")

	if _, err := store.Invoices().Delete().Build(); !errors.Is(err, orm.ErrUnsafeMutation) {
		panic("unscoped delete was not rejected")
	}
	_, err = store.Customers().Delete().Where(models.CustomerColumns.ID.Eq(alpha.ID)).Exec(ctx, store.Customers().Executor)
	assert(errors.Is(ormpostgres.Classify(err), orm.ErrForeignKey), "referenced customer safe delete")
	deleted, err := store.Invoices().Delete().Where(models.InvoiceColumns.ID.Eq(second.ID)).Exec(ctx, store.Invoices().Executor)
	must(err)
	assert(deleted == 1, "scoped delete")
	deleted, err = store.Invoices().Delete().Where(models.InvoiceColumns.ID.Eq(second.ID)).Exec(ctx, store.Invoices().Executor)
	must(err)
	assert(deleted == 0, "scoped delete missing row")

	canceled, stop := context.WithCancel(context.Background())
	stop()
	_, err = store.Invoices().Select().Where(models.InvoiceColumns.ID.Eq(third.ID)).First(canceled, store.Invoices().Executor)
	assert(errors.Is(err, context.Canceled), "query cancellation")

	fmt.Println("model_postgres_acceptance=passed")
}

func createCustomer(ctx context.Context, store models.Store, name string) models.Customer {
	item, err := store.Customers().Create(models.CustomerCreateInput{Name: name}).One(ctx, store.Customers().Executor)
	must(err)
	return item
}

func createInvoice(ctx context.Context, store models.Store, number string, total int64, paid bool, customerID int64) models.Invoice {
	return createInvoiceWithNotes(ctx, store, number, total, paid, customerID, orm.Field[*string]{})
}

func createInvoiceWithNotes(ctx context.Context, store models.Store, number string, total int64, paid bool, customerID int64, notes orm.Field[*string]) models.Invoice {
	item, err := store.Invoices().Create(models.InvoiceCreateInput{
		Number: number, TotalCents: total, Paid: paid, Notes: notes, CustomerID: customerID,
	}).One(ctx, store.Invoices().Executor)
	must(err)
	return item
}

func pointer[T any](value T) *T {
	return &value
}

func assert(ok bool, label string) {
	if !ok {
		panic(label)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
`
