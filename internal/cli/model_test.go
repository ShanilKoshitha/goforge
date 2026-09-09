package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMakeModelCreatesConventionalSourceMigrationAndCurrentORM(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "ledger")
	if err := createProject(newOptions{directory: directory, module: "example.com/ledger", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	var output bytes.Buffer
	if err := Run([]string{"make:model", "Invoice"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"make", "model", "LedgerEntry"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	var help bytes.Buffer
	printHelp(&help)
	if !strings.Contains(help.String(), "forge make model <name>") || !strings.Contains(help.String(), "forge make:model <name>") {
		t.Fatalf("CLI help omits make:model forms:\n%s", help.String())
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "forge make:model Invoice") || !strings.Contains(string(readme), "forge orm:generate") {
		t.Fatalf("scaffold README omits editable model workflow:\n%s", readme)
	}
	for _, path := range []string{"internal/models/invoice.go", "internal/models/ledger_entry.go", generatedORMPath} {
		if _, err := os.Stat(filepath.FromSlash(path)); err != nil {
			t.Errorf("expected %s: %v", path, err)
		}
	}
	matches, err := filepath.Glob(filepath.Join("database", "migrations", "*_create_invoices.*.sql"))
	if err != nil || len(matches) != 2 {
		t.Fatalf("invoice migrations = %v, error %v", matches, err)
	}
	model, err := os.ReadFile(filepath.Join("internal", "models", "invoice.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`forge:"primary,generated,protected,required"`,
		`db:"created_at" forge:"generated,protected,required"`,
		`db:"updated_at" forge:"generated,protected,required"`,
		`forge:"protected,required,default=1"`,
	} {
		if !strings.Contains(string(model), expected) {
			t.Errorf("model source is missing %q:\n%s", expected, model)
		}
	}
	generated, err := os.ReadFile(filepath.FromSlash(generatedORMPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "var InvoiceMapper") || !strings.Contains(string(generated), "func (store Store) Invoices() InvoiceQuery") {
		t.Fatalf("generated ORM omits Invoice:\n%s", generated)
	}
	var check bytes.Buffer
	if err := Run([]string{"orm:generate", "--check"}, &check, &check); err != nil {
		t.Fatalf("generated ORM is not current: %v\n%s", err, check.String())
	}
	for _, command := range [][]string{{"test", "./..."}, {"vet", "./..."}, {"build", "./cmd/server"}} {
		process := exec.Command("go", command...)
		process.Dir = directory
		process.Env = append(os.Environ(),
			"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
			"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
			"GOWORK=off",
		)
		if result, err := process.CombinedOutput(); err != nil {
			t.Fatalf("generated application go %s failed: %v\n%s", strings.Join(command, " "), err, result)
		}
	}
}

func TestMakeModelRefusesInvalidDuplicateAndOverwriteBeforeWriting(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "ledger")
	if err := createProject(newOptions{directory: directory, module: "example.com/ledger", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	var output bytes.Buffer
	for _, name := range []string{"---", "123"} {
		if err := makeModel(name, &output); err == nil {
			t.Fatalf("invalid model %q was accepted", name)
		}
	}
	if err := makeModel("Invoice", &output); err != nil {
		t.Fatal(err)
	}
	ormBefore, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))
	migrationsBefore, _ := filepath.Glob(filepath.Join("database", "migrations", "*"))
	if err := makeModel("Invoice", &output); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("duplicate model error = %v", err)
	}
	ormAfter, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))
	migrationsAfter, _ := filepath.Glob(filepath.Join("database", "migrations", "*"))
	if !bytes.Equal(ormBefore, ormAfter) || len(migrationsBefore) != len(migrationsAfter) {
		t.Fatal("duplicate model generation changed the application")
	}
}

func TestNewModelSpecRejectsBuiltInAndResourceTables(t *testing.T) {
	state := resourceState{Resources: []resourceSpec{{Name: "Invoice", Plural: "invoices"}}}
	for _, name := range []string{"User", "Session", "SchemaMigration", "Job", "FailedJob", "GoforgeJob", "GoforgeFailedJob", "Invoice"} {
		if _, err := newModelSpec(name, state); err == nil {
			t.Fatalf("model %s should conflict with a built-in or generated resource table", name)
		}
	}
}

func TestMakeModelRollsBackExclusiveAndManagedFailures(t *testing.T) {
	for _, failExclusiveAt := range []int{1, 2, 3} {
		t.Run("exclusive_"+string(rune('0'+failExclusiveAt)), func(t *testing.T) {
			assertMakeModelRollback(t, failExclusiveAt, false, false)
		})
	}
	t.Run("managed_after_replace", func(t *testing.T) {
		assertMakeModelRollback(t, 0, true, false)
	})
	t.Run("managed_with_initially_missing_orm", func(t *testing.T) {
		assertMakeModelRollback(t, 0, true, true)
	})
}

func TestMakeModelInvalidExistingSchemaWritesNothing(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "ledger")
	if err := createProject(newOptions{directory: directory, module: "example.com/ledger", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	if err := os.WriteFile(filepath.Join("internal", "models", "broken.go"), []byte("package models\ntype Broken struct { Name string `forge:\"required\"` }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ormBefore, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))
	migrationsBefore, _ := filepath.Glob(filepath.Join("database", "migrations", "*"))
	err := makeModel("Invoice", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "must declare exactly one primary field") {
		t.Fatalf("invalid schema error = %v", err)
	}
	ormAfter, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))
	migrationsAfter, _ := filepath.Glob(filepath.Join("database", "migrations", "*"))
	if !bytes.Equal(ormBefore, ormAfter) || len(migrationsBefore) != len(migrationsAfter) {
		t.Fatal("invalid existing model schema changed managed application files")
	}
	if _, err := os.Stat(filepath.Join("internal", "models", "invoice.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid schema generation wrote Invoice: %v", err)
	}
}

func assertMakeModelRollback(t *testing.T, failExclusiveAt int, failManaged, removeORM bool) {
	t.Helper()
	directory := t.TempDir()
	t.Chdir(directory)
	if err := os.WriteFile("forge.yaml", []byte("version: 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !removeORM {
		if err := os.MkdirAll(filepath.Join("internal", "models"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.FromSlash(generatedORMPath), []byte("// Code generated by GoForge. DO NOT EDIT.\nprevious ORM\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldORM, readErr := os.ReadFile(filepath.FromSlash(generatedORMPath))
	sentinel := errors.New("injected model generation failure")
	exclusiveCalls := 0
	exclusive := func(path, content string) error {
		exclusiveCalls++
		if exclusiveCalls == failExclusiveAt {
			return sentinel
		}
		return writeExclusive(path, content)
	}
	managedCalls := 0
	managed := func(path string, content []byte) error {
		managedCalls++
		if failManaged && managedCalls == 1 {
			if err := writeManagedFile(path, []byte("incomplete ORM\n")); err != nil {
				return err
			}
			return sentinel
		}
		return writeManagedFile(path, content)
	}
	err := makeModelWithWriters("Invoice", &bytes.Buffer{}, exclusive, managed)
	if !errors.Is(err, sentinel) {
		t.Fatalf("rollback error = %v", err)
	}
	for _, path := range []string{filepath.Join("internal", "models", "invoice.go"), filepath.Join("database", "migrations")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rollback left %s behind: %v", path, err)
		}
	}
	currentORM, err := os.ReadFile(filepath.FromSlash(generatedORMPath))
	if removeORM {
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rollback created initially missing ORM: %v", err)
		}
	} else if err != nil || readErr != nil || !bytes.Equal(oldORM, currentORM) {
		t.Fatalf("rollback did not restore ORM: read=%v current=%q", err, currentORM)
	}
}

func TestMakeModelRequiresSupportedProjectFormat(t *testing.T) {
	for _, test := range []struct {
		version string
		want    string
	}{
		{version: "3", want: "upgrade the project to format 4"},
		{version: "11", want: "newer than this CLI supports"},
	} {
		t.Run(test.version, func(t *testing.T) {
			directory := t.TempDir()
			t.Chdir(directory)
			if err := os.WriteFile("forge.yaml", []byte("version: "+test.version+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			err := makeModel("Invoice", &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("format %s error = %v", test.version, err)
			}
			if _, err := os.Stat(filepath.Join("internal", "models", "invoice.go")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("format refusal wrote a model: %v", err)
			}
		})
	}
}
