package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseMakeModelArguments(t *testing.T) {
	name, fields, relationships, err := parseMakeModelArguments([]string{
		"Invoice",
		"--field", "number:string",
		"--field=notes:text:nullable",
		"--field", "total_cents:integer:required",
		"--field", "paid:boolean",
		"--belongs-to", "customer:Customer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if name != "Invoice" || len(fields) != 4 || len(relationships) != 1 {
		t.Fatalf("parsed model options = name:%q fields:%#v relationships:%#v", name, fields, relationships)
	}
	if !fields[0].Required || fields[0].GoType != "string" || !fields[1].Nullable || fields[1].GoType != "*string" ||
		!fields[2].Required || fields[2].GoType != "int64" || !fields[3].Required || fields[3].GoType != "bool" {
		t.Fatalf("model field semantics = %#v", fields)
	}
	if relationships[0].Name != "customer" || relationships[0].ForeignKey != "customer_id" || relationships[0].Target != "Customer" {
		t.Fatalf("model relationship = %#v", relationships[0])
	}

	name, fields, relationships, err = parseMakeModelArguments([]string{"Invoice"})
	if err != nil || name != "Invoice" || len(fields) != 0 || len(relationships) != 0 {
		t.Fatalf("legacy model arguments = name:%q fields:%#v relationships:%#v error:%v", name, fields, relationships, err)
	}

	name, fields, relationships, err = parseMakeModelArguments([]string{
		"Ledger", "--field", "owner:string", "--field", "user_id:integer",
	})
	if err != nil || name != "Ledger" || len(fields) != 2 || len(relationships) != 0 ||
		fields[0].GoName != "Owner" || fields[1].GoName != "UserID" {
		t.Fatalf("ordinary persistence names = name:%q fields:%#v relationships:%#v error:%v", name, fields, relationships, err)
	}
}

func TestParseMakeModelArgumentsRejectsInvalidOptions(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments []string
		want      string
	}{
		{name: "missing name", arguments: nil, want: "usage: forge make:model"},
		{name: "option before name", arguments: []string{"--field", "name:string"}, want: "usage: forge make:model"},
		{name: "missing field", arguments: []string{"Invoice", "--field"}, want: "--field requires"},
		{name: "empty field", arguments: []string{"Invoice", "--field="}, want: "--field requires"},
		{name: "unknown option", arguments: []string{"Invoice", "--searchable"}, want: "unknown model option"},
		{name: "duplicate field", arguments: []string{"Invoice", "--field", "number:string", "--field", "number:string"}, want: "duplicate model field"},
		{name: "invalid nullable", arguments: []string{"Invoice", "--field", "number:string:required:nullable"}, want: "both required and nullable"},
		{name: "self relationship", arguments: []string{"Invoice", "--belongs-to", "parent:Invoice"}, want: "cannot target its own"},
		{name: "member collision", arguments: []string{"Invoice", "--field", "customer_id:integer", "--belongs-to", "customer:Customer"}, want: "conflicts"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := parseMakeModelArguments(test.arguments)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parse error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestMakeModelBelongsToBuiltInUser(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "audit")
	if err := createProject(newOptions{directory: directory, module: "example.com/audit", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)

	var output bytes.Buffer
	if err := Run([]string{
		"make:model", "AuditLog",
		"--field", "action:string",
		"--belongs-to", "user:User",
	}, &output, &output); err != nil {
		t.Fatalf("generate AuditLog: %v\n%s", err, output.String())
	}

	assertFileContainsAll(t, filepath.Join("internal", "models", "audit_log.go"),
		"UserID", `json:"user_id" db:"user_id" forge:"required,index,references=User.ID,on_delete=no_action"`,
		"Action", `json:"action" db:"action" forge:"required,type=varchar(255)"`,
		"User", `json:"-" forge:"belongs_to,target=User,foreign_key=UserID,references=ID"`,
	)
	auditMigration := oneModelMigration(t, "*_create_audit_logs.up.sql")
	assertFileContainsAll(t, auditMigration,
		"user_id BIGINT NOT NULL,",
		"CONSTRAINT audit_logs_user_id_fkey FOREIGN KEY (user_id)",
		"REFERENCES users (id) ON DELETE NO ACTION",
		"CREATE INDEX audit_logs_user_id_idx ON audit_logs (user_id);",
	)
	assertFileContainsAll(t, filepath.FromSlash(generatedORMPath),
		"UserID int64",
		"AuditLogColumns.UserID.Set(input.UserID)",
		"AuditLogColumns.UserID.From(changes.UserID)",
		"func AuditLogUserRelation(options orm.RelationOptions)",
		"func (query AuditLogQuery) LoadUser(",
	)
}

func TestMakeModelIntrinsicMemberCollisionsWriteNothing(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments []string
		want      string
	}{
		{name: "raw field", arguments: []string{"make:model", "AuditLog", "--field", "id:integer"}, want: `model field "id" is reserved`},
		{name: "canonical Go field", arguments: []string{"make:model", "AuditLog", "--field", "i_d:integer"}, want: "reserved Go name ID"},
		{name: "relationship", arguments: []string{"make:model", "AuditLog", "--belongs-to", "created_at:User"}, want: `relationship "created_at" is reserved`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := projectRoot(t)
			directory := filepath.Join(t.TempDir(), "audit")
			if err := createProject(newOptions{directory: directory, module: "example.com/audit", replace: root}); err != nil {
				t.Fatal(err)
			}
			t.Chdir(directory)
			ormBefore, err := os.ReadFile(filepath.FromSlash(generatedORMPath))
			if err != nil {
				t.Fatal(err)
			}
			migrationsBefore, err := filepath.Glob(filepath.Join("database", "migrations", "*"))
			if err != nil {
				t.Fatal(err)
			}

			err = Run(test.arguments, &bytes.Buffer{}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("collision error = %v, want %q", err, test.want)
			}
			if _, err := os.Stat(filepath.Join("internal", "models", "audit_log.go")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("collision wrote model: %v", err)
			}
			ormAfter, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))
			migrationsAfter, _ := filepath.Glob(filepath.Join("database", "migrations", "*"))
			if !bytes.Equal(ormBefore, ormAfter) || len(migrationsBefore) != len(migrationsAfter) {
				t.Fatal("intrinsic member collision changed generated application")
			}
		})
	}
}

func TestMakeModelWithFieldsAndBelongsToGeneratesCompleteSchema(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "billing")
	if err := createProject(newOptions{directory: directory, module: "example.com/billing", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	metadataBefore, err := os.ReadFile(filepath.Join(".forge", "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run([]string{"make:model", "Customer", "--field", "name:string"}, &output, &output); err != nil {
		t.Fatalf("generate Customer: %v\n%s", err, output.String())
	}
	if err := Run([]string{
		"make", "model", "Invoice",
		"--field", "number:string",
		"--field", "total_cents:integer",
		"--field", "paid:boolean",
		"--belongs-to", "customer:Customer",
	}, &output, &output); err != nil {
		t.Fatalf("generate Invoice: %v\n%s", err, output.String())
	}

	assertFileContainsAll(t, filepath.Join("internal", "models", "customer.go"),
		"Name", "string", `json:"name" db:"name" forge:"required,type=varchar(255)"`,
	)
	assertFileContainsAll(t, filepath.Join("internal", "models", "invoice.go"),
		"CustomerID", `json:"customer_id" db:"customer_id" forge:"required,index,references=Customer.ID,on_delete=no_action"`,
		"Number", `json:"number" db:"number" forge:"required,type=varchar(255)"`,
		"TotalCents", `json:"total_cents" db:"total_cents" forge:"required"`,
		"Paid", `json:"paid" db:"paid" forge:"required"`,
		"Customer", `json:"-" forge:"belongs_to,target=Customer,foreign_key=CustomerID,references=ID"`,
	)

	invoiceMigration := oneModelMigration(t, "*_create_invoices.up.sql")
	assertFileContainsAll(t, invoiceMigration,
		"customer_id BIGINT NOT NULL,",
		"number VARCHAR(255) NOT NULL,",
		"total_cents BIGINT NOT NULL,",
		"paid BOOLEAN NOT NULL,",
		"CONSTRAINT invoices_customer_id_fkey FOREIGN KEY (customer_id)",
		"REFERENCES customers (id) ON DELETE NO ACTION",
		"CREATE INDEX invoices_customer_id_idx ON invoices (customer_id);",
	)
	assertFileContainsAll(t, filepath.FromSlash(generatedORMPath),
		"CustomerID int64",
		"Number     string",
		"TotalCents int64",
		"Paid       bool",
		"func InvoiceCustomerRelation(options orm.RelationOptions)",
		"func (query InvoiceQuery) LoadCustomer(",
	)
	metadataAfter, err := os.ReadFile(filepath.Join(".forge", "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(metadataBefore, metadataAfter) {
		t.Fatal("make:model changed HTTP resource metadata")
	}
	var check bytes.Buffer
	if err := Run([]string{"orm:generate", "--check"}, &check, &check); err != nil {
		t.Fatalf("generated ORM is not current: %v\n%s", err, check.String())
	}
	command := exec.Command("go", "test", "./internal/models")
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated model package does not compile: %v\n%s", err, result)
	}
}

func TestConcurrentMakeModelKeepsBothModelsAndCombinedORM(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "concurrent-models")
	if err := createProject(newOptions{directory: directory, module: "example.com/concurrent-models", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type generationResult struct {
		arguments []string
		output    string
		err       error
	}
	start := make(chan struct{})
	results := make(chan generationResult, 2)
	for _, arguments := range [][]string{
		{"make:model", "Customer", "--field", "name:string"},
		{"make", "model", "Invoice", "--field", "number:string"},
	} {
		arguments := append([]string(nil), arguments...)
		go func() {
			<-start
			var output bytes.Buffer
			err := RunContext(ctx, arguments, bytes.NewReader(nil), &output, &output)
			results <- generationResult{arguments: arguments, output: output.String(), err: err}
		}()
	}
	close(start)
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent generation %v: %v\n%s", result.arguments, result.err, result.output)
		}
	}

	for _, model := range []struct {
		name, file, table, field string
	}{
		{name: "Customer", file: "customer", table: "customers", field: "Name"},
		{name: "Invoice", file: "invoice", table: "invoices", field: "Number"},
	} {
		assertFileContainsAll(t, filepath.Join("internal", "models", model.file+".go"),
			"type "+model.name+" struct", model.field,
		)
		migrationFiles, err := filepath.Glob(filepath.Join("database", "migrations", "*_create_"+model.table+".*.sql"))
		if err != nil || len(migrationFiles) != 2 {
			t.Fatalf("%s migration pair = %v, error %v", model.table, migrationFiles, err)
		}
	}
	customerUp := oneModelMigration(t, "*_create_customers.up.sql")
	invoiceUp := oneModelMigration(t, "*_create_invoices.up.sql")
	if strings.Split(filepath.Base(customerUp), "_")[0] == strings.Split(filepath.Base(invoiceUp), "_")[0] {
		t.Fatalf("concurrent models reused one migration version: %s and %s", customerUp, invoiceUp)
	}
	assertFileContainsAll(t, filepath.FromSlash(generatedORMPath),
		"var CustomerMapper", "type CustomerCreateInput struct", "func (store Store) Customers() CustomerQuery",
		"var InvoiceMapper", "type InvoiceCreateInput struct", "func (store Store) Invoices() InvoiceQuery",
	)
	var check bytes.Buffer
	if err := Run([]string{"orm:generate", "--check"}, &check, &check); err != nil {
		t.Fatalf("combined concurrent ORM is not current: %v\n%s", err, check.String())
	}
}

func TestTypedMakeModelRemainsCompatibleWithFrozenFormat6(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "format6-model")
	if err := os.CopyFS(directory, os.DirFS(filepath.Join("testdata", "format6"))); err != nil {
		t.Fatalf("copy frozen format-6 application: %v", err)
	}
	environment := append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	replacement := "-replace=github.com/ShanilKoshitha/goforge=" + root
	if output, err := generatedCommand(directory, environment, "go", "mod", "edit", replacement); err != nil {
		t.Fatalf("point compatibility fixture at current framework: %v\n%s", err, output)
	}
	t.Chdir(directory)
	var output bytes.Buffer
	if err := Run([]string{"views:compile"}, &output, &output); err != nil {
		t.Fatalf("normalize format-6 managed views with current compiler: %v\n%s", err, output.String())
	}
	before := snapshotModelCompatibilityFiles(t, directory)
	output.Reset()
	if err := Run([]string{
		"make:model", "AuditLog", "--field", "action:string", "--belongs-to", "user:User",
	}, &output, &output); err != nil {
		t.Fatalf("typed make:model rejected format 6: %v\n%s", err, output.String())
	}
	assertFileContainsAll(t, filepath.Join("internal", "models", "audit_log.go"),
		"UserID", "Action", "*User", "foreign_key=UserID",
	)
	auditMigration := oneModelMigration(t, "*_create_audit_logs.up.sql")
	assertFileContainsAll(t, auditMigration,
		"user_id BIGINT NOT NULL", "action VARCHAR(255) NOT NULL",
		"REFERENCES users (id)", "CREATE INDEX audit_logs_user_id_idx",
	)
	assertFileContainsAll(t, filepath.FromSlash(generatedORMPath),
		"var IssueMapper", "var UserMapper", "var AuditLogMapper",
		"AuditLogColumns.UserID.Set(input.UserID)", "func (query AuditLogQuery) LoadUser(",
	)
	var check bytes.Buffer
	if err := Run([]string{"orm:generate", "--check"}, &check, &check); err != nil {
		t.Fatalf("format-6 ORM is not current: %v\n%s", err, check.String())
	}
	if result, err := generatedCommand(directory, environment, "go", "test", "./..."); err != nil {
		t.Fatalf("format-6 application with typed model does not compile: %v\n%s", err, result)
	}
	for path, want := range before {
		if filepath.ToSlash(path) == "internal/models/zz_orm_gen.go" {
			continue
		}
		got, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(path)))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("typed make:model altered frozen pre-existing source %s: %v", path, err)
		}
	}
}

func TestMakeModelNullableFieldAndLegacyOutput(t *testing.T) {
	legacy, err := modelFiles(modelSpec{Name: "Invoice", File: "invoice", Table: "invoices", MigrationVersion: "000001"})
	if err != nil {
		t.Fatal(err)
	}
	if legacy[0].content != `package models

import "time"

// Invoice is ordinary application-owned Go. Add fields and forge tags here,
// update the paired migration, then run forge orm:generate.
type Invoice struct {
	ID        int64     `+"`"+`json:"id" forge:"primary,generated,protected,required"`+"`"+`
	CreatedAt time.Time `+"`"+`json:"created_at" db:"created_at" forge:"generated,protected,required"`+"`"+`
	UpdatedAt time.Time `+"`"+`json:"updated_at" db:"updated_at" forge:"generated,protected,required"`+"`"+`
	Version   int64     `+"`"+`json:"version" forge:"protected,required,default=1"`+"`"+`
}
` {
		t.Fatalf("legacy model output changed:\n%s", legacy[0].content)
	}
	if legacy[1].content != "CREATE TABLE invoices (\n    id BIGSERIAL PRIMARY KEY,\n    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),\n    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),\n    version BIGINT NOT NULL DEFAULT 1\n);\n" {
		t.Fatalf("legacy migration output changed:\n%s", legacy[1].content)
	}

	_, fields, _, err := parseMakeModelArguments([]string{"Invoice", "--field", "notes:text:nullable"})
	if err != nil {
		t.Fatal(err)
	}
	spec := modelSpec{Name: "Invoice", File: "invoice", Table: "invoices", MigrationVersion: "000001"}
	for _, field := range fields {
		spec.Fields = append(spec.Fields, projectResourceTemplateField(field))
	}
	files, err := modelFiles(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(files[0].content, `json:"notes" db:"notes" forge:"nullable,type=text"`) ||
		!strings.Contains(files[1].content, "notes TEXT,") {
		t.Fatalf("nullable model output is incomplete:\n%s\n%s", files[0].content, files[1].content)
	}
}

func TestMakeModelBelongsToMissingTargetWritesNothing(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "billing")
	if err := createProject(newOptions{directory: directory, module: "example.com/billing", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	ormBefore, err := os.ReadFile(filepath.FromSlash(generatedORMPath))
	if err != nil {
		t.Fatal(err)
	}
	migrationsBefore, err := filepath.Glob(filepath.Join("database", "migrations", "*"))
	if err != nil {
		t.Fatal(err)
	}
	err = Run([]string{"make:model", "Invoice", "--field", "number:string", "--belongs-to", "customer:Customer"}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not an existing model") {
		t.Fatalf("missing target error = %v", err)
	}
	if _, err := os.Stat(filepath.Join("internal", "models", "invoice.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing target wrote model: %v", err)
	}
	ormAfter, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))
	migrationsAfter, _ := filepath.Glob(filepath.Join("database", "migrations", "*"))
	if !bytes.Equal(ormBefore, ormAfter) || len(migrationsBefore) != len(migrationsAfter) {
		t.Fatal("missing belongs-to target changed generated application")
	}
}

func TestMakeModelOptionsRollBackManagedFailure(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	if err := os.WriteFile("forge.yaml", []byte("version: 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join("internal", "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.FromSlash(generatedORMPath), []byte("// Code generated by GoForge. DO NOT EDIT.\nprevious ORM\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldORM, err := os.ReadFile(filepath.FromSlash(generatedORMPath))
	if err != nil {
		t.Fatal(err)
	}
	_, fields, _, err := parseMakeModelArguments([]string{"Invoice", "--field", "number:string"})
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("injected managed failure")
	managedCalls := 0
	managed := func(path string, content []byte) error {
		managedCalls++
		if managedCalls != 1 {
			return writeManagedFile(path, content)
		}
		if err := writeManagedFile(path, []byte("incomplete ORM\n")); err != nil {
			return err
		}
		return sentinel
	}
	err = makeModelWithOptionsAndWriters("Invoice", fields, nil, &bytes.Buffer{}, writeExclusive, managed)
	if !errors.Is(err, sentinel) {
		t.Fatalf("managed failure = %v", err)
	}
	for _, path := range []string{filepath.Join("internal", "models", "invoice.go"), filepath.Join("database", "migrations")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("option rollback left %s: %v", path, err)
		}
	}
	currentORM, err := os.ReadFile(filepath.FromSlash(generatedORMPath))
	if err != nil || !bytes.Equal(oldORM, currentORM) {
		t.Fatalf("option rollback did not restore ORM: %v %q", err, currentORM)
	}
}

func assertFileContainsAll(t *testing.T, path string, expected ...string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range expected {
		if !strings.Contains(string(contents), value) {
			t.Errorf("%s is missing %q:\n%s", filepath.ToSlash(path), value, contents)
		}
	}
}

func oneModelMigration(t *testing.T, pattern string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("database", "migrations", pattern))
	if err != nil || len(matches) != 1 {
		t.Fatalf("migration %q matches = %v, error %v", pattern, matches, err)
	}
	return matches[0]
}

func snapshotModelCompatibilityFiles(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(relative)] = contents
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot format-6 files: %v", err)
	}
	return files
}
