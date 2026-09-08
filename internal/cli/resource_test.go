package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestMakeResourceGeneratesCompleteSafeVerticalSlices(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "issueboard")
	if err := createProject(newOptions{directory: directory, module: "example.com/issueboard", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	var output bytes.Buffer
	if err := Run([]string{"make:resource", "Issue"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"internal/models/issue.go",
		"internal/resources/issue/model.go",
		"internal/resources/issue/repository.go",
		"internal/resources/issue/repository_test.go",
		"internal/resources/issue/request.go",
		"internal/resources/issue/controller.go",
		"internal/resources/issue/controller_test.go",
		"internal/resources/issue/web_controller.go",
		"internal/resources/issue/web_controller_test.go",
		"resources/views/pages/issues/index.forge.html",
		"resources/views/pages/issues/form.forge.html",
		"resources/views/pages/issues/new.forge.html",
		"resources/views/pages/issues/edit.forge.html",
		"resources/views/pages/issues/show.forge.html",
		"routes/resources_gen.go",
	} {
		if _, err := os.Stat(filepath.FromSlash(path)); err != nil {
			t.Errorf("expected %s: %v", path, err)
		}
	}
	legacyMigrations, err := filepath.Glob(filepath.Join("database", "migrations", "*_create_issues.up.sql"))
	if err != nil || len(legacyMigrations) != 1 {
		t.Fatalf("legacy resource migration = %v, %v", legacyMigrations, err)
	}
	assertGeneratedFileContains(t, legacyMigrations[0], `"name" TEXT NOT NULL`)
	assertGeneratedFileContains(t, "internal/resources/issue/request.go",
		"request.validate(false)", "validation.StringLength(2, 200)", "decodeExactJSONObject")
	assertGeneratedFileContains(t, "resources/views/pages/issues/form.forge.html", `minlength="2" maxlength="200"`)
	assertGeneratedFileContains(t, "resources/views/pages/issues/index.forge.html", `>{{.Name}}</a>`)
	assertGeneratedFileContains(t, "resources/views/pages/issues/show.forge.html", `title=.Item.Name`)
	registryBefore, _ := os.ReadFile(filepath.Join("routes", "resources_gen.go"))
	stateBefore, _ := os.ReadFile(filepath.Join(".forge", "resources.json"))
	ormBefore, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))
	if !strings.Contains(string(registryBefore), `"/issues/{id}"`) || !strings.Contains(string(registryBefore), `"/app/issues/{id}/edit"`) || !strings.Contains(string(registryBefore), "requireAuth") {
		t.Fatalf("resource routes are not explicit and authenticated:\n%s", registryBefore)
	}
	compiledViews, err := os.ReadFile(filepath.Join("resources", "views", "views_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	generatedORM, err := os.ReadFile(filepath.FromSlash(generatedORMPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generatedORM), "var IssueMapper") || !strings.Contains(string(generatedORM), "IssueOwnerRelation") || !strings.Contains(string(generatedORM), "Version:   orm.MustColumn") {
		t.Fatalf("resource ORM is missing typed model/version/owner metadata:\n%s", generatedORM)
	}
	repository, err := os.ReadFile(filepath.Join("internal", "resources", "issue", "repository.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(repository), "Executor orm.Executor") || !strings.Contains(string(repository), "IssueColumns.UserID.Eq(userID)") {
		t.Fatalf("resource repository is not ORM-backed and owner-scoped:\n%s", repository)
	}
	if !strings.Contains(string(compiledViews), `Name: "pages/issues/index"`) || strings.Contains(string(compiledViews), "@extends") {
		t.Fatalf("resource views were not compiled into the managed artifact:\n%s", compiledViews)
	}
	if err := Run([]string{"make:resource", "Issue"}, &output, &output); err == nil {
		t.Fatal("expected duplicate resource generation to fail")
	}
	registryAfter, _ := os.ReadFile(filepath.Join("routes", "resources_gen.go"))
	stateAfter, _ := os.ReadFile(filepath.Join(".forge", "resources.json"))
	ormAfter, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))
	if !bytes.Equal(registryBefore, registryAfter) || !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(ormBefore, ormAfter) {
		t.Fatal("failed duplicate generation changed managed files")
	}

	if err := Run([]string{"make", "resource", "Category"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	registryAfter, _ = os.ReadFile(filepath.Join("routes", "resources_gen.go"))
	if !strings.Contains(string(registryAfter), `"/categories"`) {
		t.Fatalf("successive resource missing from registry:\n%s", registryAfter)
	}
	for _, name := range []string{"APIClient", "SQL", "Auth", "Context"} {
		if err := makeResource(name, &output); err != nil {
			t.Fatalf("generate %s: %v", name, err)
		}
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("resource application does not compile and test: %v\n%s", err, output)
	}
}

func TestMakeResourceGeneratesOneTypedFieldContractAcrossEveryLayer(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "issueboard")
	if err := createProject(newOptions{directory: directory, module: "example.com/issueboard", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)

	var output bytes.Buffer
	if err := Run([]string{
		"make:resource", "Issue",
		"--field", "title:string",
		"--field=notes:text:nullable",
		"--field", "priority:integer:required",
		"--field=active:boolean",
	}, &output, &output); err != nil {
		t.Fatalf("generate typed resource: %v\n%s", err, output.String())
	}

	assertGeneratedFileContains(t, "internal/models/issue.go",
		"Title     string", "Notes     *string", "Priority  int64", "Active    bool",
		`json:"notes" db:"notes" forge:"nullable,type=text"`)
	migrations, err := filepath.Glob(filepath.Join("database", "migrations", "*_create_issues.up.sql"))
	if err != nil || len(migrations) != 1 {
		t.Fatalf("typed resource migration = %v, %v", migrations, err)
	}
	assertGeneratedFileContains(t, migrations[0],
		`"title" VARCHAR(255) NOT NULL`, `"notes" TEXT`, `"priority" BIGINT NOT NULL`, `"active" BOOLEAN NOT NULL`)
	assertGeneratedFileContains(t, "internal/resources/issue/model.go",
		"type Attributes struct", "Title    string", "Notes    *string", "Priority int64", "Active   bool")
	assertGeneratedFileContains(t, "internal/resources/issue/request.go",
		`body["title"]`, `body["notes"]`, `body["priority"]`, `body["active"]`,
		"decodeExactJSONObject", "http.MaxBytesReader",
		"request.validate(true)", `decodeJSONInt64("priority"`, `decodeJSONBoolean("active"`)
	assertGeneratedFileContains(t, "internal/resources/issue/repository.go",
		"Create(ctx context.Context, userID int64, attributes Attributes)",
		"Title:    attributes.Title", "Notes:    nullableField(attributes.Notes)",
		"Priority: orm.Value(attributes.Priority)", "Active:   orm.Value(attributes.Active)",
		"IssueColumns.UserID.Eq(userID)")
	assertGeneratedFileContains(t, "resources/views/pages/issues/form.forge.html",
		`name="title"`, `name="notes"`, `name="priority"`, `name="active"`,
		`type="text"`, "<textarea", `type="number"`, "<select")

	state, err := os.ReadFile(filepath.Join(".forge", "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(state), "title") || strings.Contains(string(state), "priority") || strings.Contains(string(state), "fields") {
		t.Fatalf("one-shot field schema leaked into persistent metadata:\n%s", state)
	}
	for _, arguments := range [][]string{
		{"make:resource", "Note", "--field", "body:text:nullable"},
		{"make:resource", "Setting", "--field", "retries:integer:nullable", "--field", "enabled:boolean:nullable"},
		{"make:resource", "Label", "--field", "name:string"},
	} {
		if err := Run(arguments, &output, &output); err != nil {
			t.Fatalf("generate typed edge schema %v: %v\n%s", arguments, err, output.String())
		}
	}
	assertGeneratedFileContains(t, "internal/resources/label/request.go", "request.validate(true)")
	assertGeneratedFileContains(t, "internal/resources/label/request.go", "validation.StringLength(1, 255)")
	assertGeneratedFileContains(t, "internal/resources/issue/request.go", "validation.StringLength(1, 255)")

	command := exec.Command("go", "test", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("typed resource application does not compile and test: %v\n%s", err, output)
	}
}

type resourceGeneratorProcess struct {
	name    string
	fields  []string
	command *exec.Cmd
	output  *bytes.Buffer
}

func TestConcurrentMakeResourceProcessesPreserveEveryTypedSlice(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "issueboard")
	if err := createProject(newOptions{directory: directory, module: "example.com/issueboard", replace: root}); err != nil {
		t.Fatal(err)
	}

	generators := make([]resourceGeneratorProcess, 12)
	for index := range generators {
		ordinal := strconv.Itoa(index + 1)
		fields := []string{"label_" + ordinal + ":string"}
		if index%2 == 0 {
			fields = append(fields, "count_"+ordinal+":integer")
		}
		if index%3 == 0 {
			fields = append(fields, "enabled_"+ordinal+":boolean:nullable")
		}
		if index%4 == 0 {
			fields = append(fields, "notes_"+ordinal+":text:nullable")
		}
		name := "ConcurrentResource" + ordinal
		generators[index] = startResourceGeneratorProcess(t, directory, name, fields)
	}
	for _, generator := range generators {
		if err := generator.command.Wait(); err != nil {
			t.Errorf("%s failed: %v\n%s", generator.name, err, generator.output.String())
		}
	}
	if t.Failed() {
		return
	}

	metadata, err := os.ReadFile(filepath.Join(directory, ".forge", "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state resourceState
	if err := json.Unmarshal(metadata, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Resources) != len(generators) {
		t.Fatalf("resource metadata retained %d of %d concurrent resources: %#v", len(state.Resources), len(generators), state.Resources)
	}
	registry, err := os.ReadFile(filepath.Join(directory, "routes", "resources_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	orm, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(generatedORMPath)))
	if err != nil {
		t.Fatal(err)
	}
	views, err := os.ReadFile(filepath.Join(directory, filepath.FromSlash(generatedViewsPath)))
	if err != nil {
		t.Fatal(err)
	}
	versions := make(map[string]struct{}, len(state.Resources))
	for _, resource := range state.Resources {
		versions[resource.MigrationVersion] = struct{}{}
	}
	if len(versions) != len(generators) {
		t.Fatalf("concurrent resources did not receive unique migrations: %#v", state.Resources)
	}
	for _, generator := range generators {
		packageName := strings.ToLower(generator.name)
		tableName, err := snake(generator.name)
		if err != nil {
			t.Fatal(err)
		}
		plural := pluralize(tableName)
		if !bytes.Contains(registry, []byte(packageName+"resource.NewPostgresRepository")) {
			t.Errorf("registry omitted %s", generator.name)
		}
		if !bytes.Contains(orm, []byte(generator.name+"Mapper")) {
			t.Errorf("ORM omitted %s", generator.name)
		}
		if !bytes.Contains(views, []byte(`Name: "pages/`+plural+`/index"`)) {
			t.Errorf("compiled views omitted %s", generator.name)
		}
		if _, err := os.Stat(filepath.Join(directory, "internal", "resources", packageName, "controller.go")); err != nil {
			t.Errorf("%s source missing: %v", generator.name, err)
		}
	}

	first := startResourceGeneratorProcess(t, directory, "ContendedResource", []string{"title:string", "active:boolean"})
	second := startResourceGeneratorProcess(t, directory, "ContendedResource", []string{"title:string", "active:boolean"})
	results := []error{first.command.Wait(), second.command.Wait()}
	successes := 0
	for _, result := range results {
		if result == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("same-name contention successes = %d, want 1\nfirst: %s\nsecond: %s", successes, first.output, second.output)
	}
	metadata, err = os.ReadFile(filepath.Join(directory, ".forge", "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(metadata, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Resources) != len(generators)+1 {
		t.Fatalf("same-name contention left metadata count %d, want %d", len(state.Resources), len(generators)+1)
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("concurrently generated application does not compile and test: %v\n%s", err, output)
	}
}

func startResourceGeneratorProcess(t *testing.T, directory, name string, fields []string) resourceGeneratorProcess {
	t.Helper()
	output := &bytes.Buffer{}
	command := exec.Command(os.Args[0], "-test.run=^TestMakeResourceHelperProcess$")
	command.Env = append(os.Environ(),
		"GOFORGE_MAKE_RESOURCE_HELPER=1",
		"GOFORGE_MAKE_RESOURCE_DIRECTORY="+directory,
		"GOFORGE_MAKE_RESOURCE_NAME="+name,
		"GOFORGE_MAKE_RESOURCE_FIELDS="+strings.Join(fields, ","),
	)
	command.Stdout = output
	command.Stderr = output
	if err := command.Start(); err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	return resourceGeneratorProcess{name: name, fields: fields, command: command, output: output}
}

func TestMakeResourceHelperProcess(t *testing.T) {
	if os.Getenv("GOFORGE_MAKE_RESOURCE_HELPER") != "1" {
		return
	}
	if err := os.Chdir(os.Getenv("GOFORGE_MAKE_RESOURCE_DIRECTORY")); err != nil {
		t.Fatal(err)
	}
	arguments := []string{"make:resource", os.Getenv("GOFORGE_MAKE_RESOURCE_NAME")}
	for _, field := range strings.Split(os.Getenv("GOFORGE_MAKE_RESOURCE_FIELDS"), ",") {
		arguments = append(arguments, "--field", field)
	}
	var output bytes.Buffer
	if err := Run(arguments, &output, &output); err != nil {
		t.Fatalf("make resource: %v\n%s", err, output.String())
	}
}

func assertGeneratedFileContains(t *testing.T, path string, fragments ...string) {
	t.Helper()
	contents, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range fragments {
		if !strings.Contains(string(contents), fragment) {
			t.Errorf("%s does not contain %q:\n%s", path, fragment, contents)
		}
	}
}

func TestMakeResourceKeepsProjectUnchangedWhenViewsDoNotCompile(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "issueboard")
	if err := createProject(newOptions{directory: directory, module: "example.com/issueboard", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	layout := filepath.Join("resources", "views", "layouts", "app.forge.html")
	if err := os.WriteFile(layout, []byte(`@include("missing")`), 0o644); err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join("routes", "resources_gen.go")
	statePath := filepath.Join(".forge", "resources.json")
	viewsPath := filepath.Join("resources", "views", "views_gen.go")
	registryBefore, _ := os.ReadFile(registryPath)
	stateBefore, _ := os.ReadFile(statePath)
	viewsBefore, _ := os.ReadFile(viewsPath)
	ormBefore, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))

	err := makeResource("Issue", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unknown include") {
		t.Fatalf("expected compile error, got %v", err)
	}
	for _, path := range []string{filepath.Join("internal", "resources", "issue"), filepath.Join("resources", "views", "pages", "issues")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed generation left %s behind: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join("internal", "models", "issue.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed generation left application model behind: %v", err)
	}
	registryAfter, _ := os.ReadFile(registryPath)
	stateAfter, _ := os.ReadFile(statePath)
	viewsAfter, _ := os.ReadFile(viewsPath)
	ormAfter, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))
	if !bytes.Equal(registryBefore, registryAfter) || !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(viewsBefore, viewsAfter) || !bytes.Equal(ormBefore, ormAfter) {
		t.Fatal("failed template compilation changed managed project state")
	}
}

func TestMakeResourceRollsBackWhenApplicationFunctionValidationFails(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "issueboard")
	if err := createProject(newOptions{directory: directory, module: "example.com/issueboard", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	pagePath := filepath.Join("resources", "views", "pages", "welcome.forge.html")
	page, err := os.ReadFile(pagePath)
	if err != nil {
		t.Fatal(err)
	}
	page = []byte(strings.Replace(string(page), "{{.Title}}", "{{unregistered .Title}}", 1))
	if err := os.WriteFile(pagePath, page, 0o644); err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join("routes", "resources_gen.go")
	statePath := filepath.Join(".forge", "resources.json")
	viewsPath := filepath.FromSlash(generatedViewsPath)
	registryBefore, _ := os.ReadFile(registryPath)
	stateBefore, _ := os.ReadFile(statePath)
	viewsBefore, _ := os.ReadFile(viewsPath)
	ormBefore, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))

	var output bytes.Buffer
	err = makeResource("Issue", &output)
	if err == nil || !strings.Contains(output.String(), "unregistered") {
		t.Fatalf("function validation error = %v:\n%s", err, output.String())
	}
	for _, path := range []string{
		filepath.Join("internal", "models", "issue.go"),
		filepath.Join("internal", "resources", "issue"),
		filepath.Join("resources", "views", "pages", "issues"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("failed generation left %s behind: %v", path, err)
		}
	}
	registryAfter, _ := os.ReadFile(registryPath)
	stateAfter, _ := os.ReadFile(statePath)
	viewsAfter, _ := os.ReadFile(viewsPath)
	ormAfter, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))
	if !bytes.Equal(registryBefore, registryAfter) || !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(viewsBefore, viewsAfter) || !bytes.Equal(ormBefore, ormAfter) {
		t.Fatal("application-function validation failure changed managed project state")
	}
}

func TestMakeResourceRollsBackEveryManagedWriteFailure(t *testing.T) {
	for _, failAt := range []int{1, 2, 3, 4} {
		t.Run(strconv.Itoa(failAt), func(t *testing.T) {
			root := projectRoot(t)
			directory := filepath.Join(t.TempDir(), "issueboard")
			if err := createProject(newOptions{directory: directory, module: "example.com/issueboard", replace: root}); err != nil {
				t.Fatal(err)
			}
			t.Chdir(directory)
			registryPath := filepath.Join("routes", "resources_gen.go")
			statePath := filepath.Join(".forge", "resources.json")
			viewsPath := filepath.Join("resources", "views", "views_gen.go")
			registryBefore, _ := os.ReadFile(registryPath)
			stateBefore, _ := os.ReadFile(statePath)
			viewsBefore, _ := os.ReadFile(viewsPath)
			ormBefore, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))
			sentinel := errors.New("injected managed write failure")
			calls := 0
			writer := func(path string, contents []byte) error {
				calls++
				if calls == failAt {
					return sentinel
				}
				return writeManagedFile(path, contents)
			}

			err := makeResourceWithWriter("Issue", &bytes.Buffer{}, writer)
			if !errors.Is(err, sentinel) {
				t.Fatalf("expected injected failure, got %v", err)
			}
			for _, path := range []string{filepath.Join("internal", "resources", "issue"), filepath.Join("resources", "views", "pages", "issues")} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("rollback left generated directory %s: %v", path, err)
				}
			}
			if _, err := os.Stat(filepath.Join("internal", "models", "issue.go")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rollback left application model behind: %v", err)
			}
			registryAfter, _ := os.ReadFile(registryPath)
			stateAfter, _ := os.ReadFile(statePath)
			viewsAfter, _ := os.ReadFile(viewsPath)
			ormAfter, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))
			if !bytes.Equal(registryBefore, registryAfter) || !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(viewsBefore, viewsAfter) || !bytes.Equal(ormBefore, ormAfter) {
				t.Fatal("managed write failure changed project state")
			}
		})
	}
}

func TestMakeResourceRollbackRestoresManagedModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits precisely")
	}
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "issueboard")
	if err := createProject(newOptions{directory: directory, module: "example.com/issueboard", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	paths := []string{
		filepath.Join("routes", "resources_gen.go"),
		filepath.FromSlash(generatedViewsPath),
		filepath.FromSlash(generatedORMPath),
		filepath.Join(".forge", "resources.json"),
	}
	modes := []os.FileMode{0o600, 0o640, 0o604, 0o644}
	for index, path := range paths {
		if err := os.Chmod(path, modes[index]); err != nil {
			t.Fatal(err)
		}
	}
	sentinel := errors.New("fail final publication")
	calls := 0
	writer := func(path string, contents []byte) error {
		calls++
		if calls == len(paths) {
			return sentinel
		}
		return writeManagedFile(path, contents)
	}
	if err := makeResourceWithWriter("Issue", io.Discard, writer); !errors.Is(err, sentinel) {
		t.Fatalf("rollback error = %v", err)
	}
	for index, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != modes[index] {
			t.Errorf("restored mode for %s = %o, want %o", path, got, modes[index])
		}
	}
}

type resourceProcessRunnerFunc func(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error

func (run resourceProcessRunnerFunc) Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name string, arguments ...string) error {
	return run(ctx, stdin, stdout, stderr, name, arguments...)
}

func TestMakeResourceRollbackPreservesConcurrentEditorBytes(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "issueboard")
	if err := createProject(newOptions{directory: directory, module: "example.com/issueboard", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)

	registryPath := filepath.Join("routes", "resources_gen.go")
	statePath := filepath.Join(".forge", "resources.json")
	viewsPath := filepath.FromSlash(generatedViewsPath)
	ormPath := filepath.FromSlash(generatedORMPath)
	stateBefore, _ := os.ReadFile(statePath)
	viewsBefore, _ := os.ReadFile(viewsPath)
	ormBefore, _ := os.ReadFile(ormPath)
	editedModel := []byte("package models\n\n// KeptByEditor proves rollback is compare-and-swap safe.\nconst KeptByEditor = true\n")
	editedRegistry := []byte("package routes\n\n// KeptByEditor was published while the generator validated views.\nconst KeptByEditor = true\n")
	sentinel := errors.New("injected compiler failure after editor save")
	runner := resourceProcessRunnerFunc(func(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error {
		if err := os.WriteFile(filepath.Join("internal", "models", "issue.go"), editedModel, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(registryPath, editedRegistry, 0o644); err != nil {
			return err
		}
		return sentinel
	})

	err := makeResourceWithDependencies(context.Background(), "Issue", defaultResourceFields(), false, nil, io.Discard, io.Discard, runner, publishResourceManagedFile)
	if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "newer bytes were preserved") {
		t.Fatalf("rollback error = %v", err)
	}
	modelAfter, readErr := os.ReadFile(filepath.Join("internal", "models", "issue.go"))
	if readErr != nil || !bytes.Equal(modelAfter, editedModel) {
		t.Fatalf("rollback overwrote editor model: read=%v contents=%q", readErr, modelAfter)
	}
	registryAfter, readErr := os.ReadFile(registryPath)
	if readErr != nil || !bytes.Equal(registryAfter, editedRegistry) {
		t.Fatalf("rollback overwrote editor registry: read=%v contents=%q", readErr, registryAfter)
	}
	stateAfter, _ := os.ReadFile(statePath)
	viewsAfter, _ := os.ReadFile(viewsPath)
	ormAfter, _ := os.ReadFile(ormPath)
	if !bytes.Equal(stateAfter, stateBefore) || !bytes.Equal(viewsAfter, viewsBefore) || !bytes.Equal(ormAfter, ormBefore) {
		t.Fatal("compare-and-swap rollback failed to restore untouched managed publications")
	}
}

func TestMakeResourceRefusesEditorChangesBeforeEveryManagedPublication(t *testing.T) {
	managedPaths := []string{
		filepath.Join("routes", "resources_gen.go"),
		filepath.FromSlash(generatedViewsPath),
		filepath.FromSlash(generatedORMPath),
		filepath.Join(".forge", "resources.json"),
	}
	for target := range managedPaths {
		t.Run(strconv.Itoa(target+1), func(t *testing.T) {
			root := projectRoot(t)
			directory := filepath.Join(t.TempDir(), "issueboard")
			if err := createProject(newOptions{directory: directory, module: "example.com/issueboard", replace: root}); err != nil {
				t.Fatal(err)
			}
			t.Chdir(directory)
			before := make(map[string][]byte, len(managedPaths))
			for _, path := range managedPaths {
				before[path], _ = os.ReadFile(path)
			}
			editorBytes := []byte(fmt.Sprintf("editor publication %d\n", target+1))
			calls := 0
			publisher := func(path string, expected developmentFileState, contents []byte) error {
				if calls == target {
					if err := os.WriteFile(path, editorBytes, expected.mode); err != nil {
						return err
					}
				}
				calls++
				return publishResourceManagedFile(path, expected, contents)
			}
			runner := resourceProcessRunnerFunc(func(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error {
				return nil
			})
			err := makeResourceWithDependencies(context.Background(), "Issue", defaultResourceFields(), false, nil, io.Discard, io.Discard, runner, publisher)
			if err == nil || !strings.Contains(err.Error(), "refusing to overwrite changed") || !strings.Contains(err.Error(), "newer bytes were preserved") {
				t.Fatalf("managed publication conflict error = %v", err)
			}
			for index, path := range managedPaths {
				after, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				want := before[path]
				if index == target {
					want = editorBytes
				}
				if !bytes.Equal(after, want) {
					t.Errorf("managed path %s = %q, want %q", path, after, want)
				}
			}
			if _, statErr := os.Stat(filepath.Join("internal", "models", "issue.go")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("publication conflict left generated model: %v", statErr)
			}
		})
	}
}

func TestMakeResourceCancellationRollsBackEveryManagedPublication(t *testing.T) {
	for target := range 5 {
		t.Run(strconv.Itoa(target+1), func(t *testing.T) {
			root := projectRoot(t)
			directory := filepath.Join(t.TempDir(), "issueboard")
			if err := createProject(newOptions{directory: directory, module: "example.com/issueboard", replace: root}); err != nil {
				t.Fatal(err)
			}
			t.Chdir(directory)
			managedPaths := []string{
				filepath.Join("routes", "resources_gen.go"),
				filepath.FromSlash(generatedViewsPath),
				filepath.FromSlash(generatedORMPath),
				filepath.Join(".forge", "resources.json"),
			}
			before := make(map[string][]byte, len(managedPaths))
			for _, path := range managedPaths {
				before[path], _ = os.ReadFile(path)
			}
			ctx, cancel := context.WithCancel(context.Background())
			calls := 0
			publisher := func(path string, expected developmentFileState, contents []byte) error {
				err := publishResourceManagedFile(path, expected, contents)
				if calls == target {
					cancel()
				}
				calls++
				return err
			}
			runner := resourceProcessRunnerFunc(func(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error {
				if target == len(managedPaths) {
					cancel()
				}
				return nil
			})
			err := makeResourceWithDependencies(ctx, "Issue", defaultResourceFields(), false, nil, io.Discard, io.Discard, runner, publisher)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation at boundary %d = %v", target+1, err)
			}
			for _, path := range managedPaths {
				after, readErr := os.ReadFile(path)
				if readErr != nil || !bytes.Equal(after, before[path]) {
					t.Errorf("cancellation changed %s: read=%v", path, readErr)
				}
			}
			if _, statErr := os.Stat(filepath.Join("internal", "models", "issue.go")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("cancellation left generated model: %v", statErr)
			}
		})
	}
}

func TestMakeResourceRejectsStaleORMWhenApplicationModelChanges(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "issueboard")
	if err := createProject(newOptions{directory: directory, module: "example.com/issueboard", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	userPath := filepath.Join("internal", "models", "user.go")
	userSource, err := os.ReadFile(userPath)
	if err != nil {
		t.Fatal(err)
	}
	editedUser := bytes.Replace(userSource, []byte("\tCreatedAt"), []byte("\tEditorNote *string `json:\"editor_note\" db:\"editor_note\" forge:\"nullable,type=text\"`\n\tCreatedAt"), 1)
	if bytes.Equal(editedUser, userSource) {
		t.Fatal("failed to construct concurrent model edit")
	}
	managedPaths := []string{
		filepath.Join("routes", "resources_gen.go"),
		filepath.FromSlash(generatedViewsPath),
		filepath.FromSlash(generatedORMPath),
		filepath.Join(".forge", "resources.json"),
	}
	before := make(map[string][]byte, len(managedPaths))
	for _, path := range managedPaths {
		before[path], _ = os.ReadFile(path)
	}
	runner := resourceProcessRunnerFunc(func(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error {
		return os.WriteFile(userPath, editedUser, 0o644)
	})
	err = makeResourceWithDependencies(context.Background(), "Issue", defaultResourceFields(), false, nil, io.Discard, io.Discard, runner, publishResourceManagedFile)
	if err == nil || !strings.Contains(err.Error(), "models changed during resource generation") {
		t.Fatalf("stale ORM generation error = %v", err)
	}
	afterUser, readErr := os.ReadFile(userPath)
	if readErr != nil || !bytes.Equal(afterUser, editedUser) {
		t.Fatalf("rollback overwrote application model edit: read=%v", readErr)
	}
	for _, path := range managedPaths {
		after, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(after, before[path]) {
			t.Errorf("stale ORM rollback changed %s: read=%v", path, readErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join("internal", "models", "issue.go")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stale ORM rollback left generated model: %v", statErr)
	}
}

func TestMakeResourceInvalidORMLeavesPreviousApplicationIntact(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "issueboard")
	if err := createProject(newOptions{directory: directory, module: "example.com/issueboard", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	badModel := filepath.Join("internal", "models", "broken.go")
	if err := os.WriteFile(badModel, []byte("package models\ntype Broken struct { Name string `forge:\"required\"` }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registryBefore, _ := os.ReadFile(filepath.Join("routes", "resources_gen.go"))
	stateBefore, _ := os.ReadFile(filepath.Join(".forge", "resources.json"))
	viewsBefore, _ := os.ReadFile(filepath.FromSlash(generatedViewsPath))
	ormBefore, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))

	err := makeResource("Issue", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "must declare exactly one primary field") {
		t.Fatalf("invalid ORM error = %v", err)
	}
	for _, path := range []string{
		filepath.Join("internal", "models", "issue.go"),
		filepath.Join("internal", "resources", "issue"),
		filepath.Join("resources", "views", "pages", "issues"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid ORM generation left %s behind: %v", path, err)
		}
	}
	registryAfter, _ := os.ReadFile(filepath.Join("routes", "resources_gen.go"))
	stateAfter, _ := os.ReadFile(filepath.Join(".forge", "resources.json"))
	viewsAfter, _ := os.ReadFile(filepath.FromSlash(generatedViewsPath))
	ormAfter, _ := os.ReadFile(filepath.FromSlash(generatedORMPath))
	if !bytes.Equal(registryBefore, registryAfter) || !bytes.Equal(stateBefore, stateAfter) || !bytes.Equal(viewsBefore, viewsAfter) || !bytes.Equal(ormBefore, ormAfter) {
		t.Fatal("invalid ORM generation changed managed application state")
	}
}

func TestMakeResourceRefusesOlderProjectFormatBeforeWriting(t *testing.T) {
	for _, format := range []string{"4", "5", "6", "7"} {
		t.Run(format, func(t *testing.T) {
			directory := t.TempDir()
			t.Chdir(directory)
			if err := os.WriteFile("forge.yaml", []byte("version: "+format+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			err := makeResource("Issue", &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "upgrade the project to format 8") {
				t.Fatalf("expected explicit project upgrade error, got %v", err)
			}
			if _, err := os.Stat(filepath.Join("internal", "resources", "issue")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("old-format refusal wrote files: %v", err)
			}
		})
	}
}

func TestMakeResourceRefusesNewerProjectFormatBeforeWriting(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	if err := os.WriteFile("forge.yaml", []byte("version: 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := makeResource("Issue", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "newer than this CLI supports") {
		t.Fatalf("expected explicit CLI upgrade error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join("internal", "resources", "issue")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("future-format refusal wrote files: %v", err)
	}
}

func TestResourceRejectsReservedNamesBeforeWriting(t *testing.T) {
	for _, name := range []string{"User", "Session", "SchemaMigration", "Job", "FailedJob", "GoforgeJob", "GoforgeFailedJob", "Controller", "Repository", "PostgresRepository", "WriteRequest", "NewController", "NewPostgresRepository"} {
		t.Run(name, func(t *testing.T) {
			if _, err := newResourceSpec(name, resourceState{}); err == nil {
				t.Fatalf("expected %s to conflict with existing generated code or tables", name)
			}
		})
	}
}

func TestMakeResourcePreflightsAllFiles(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "issueboard")
	if err := createProject(newOptions{directory: directory, module: "example.com/issueboard", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	path := filepath.Join("internal", "resources", "issue", "controller.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := makeResource("Issue", &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("expected collision refusal, got %v", err)
	}
	if _, err := os.Stat(filepath.Join("internal", "resources", "issue", "model.go")); !os.IsNotExist(err) {
		t.Fatal("generator wrote files before completing collision preflight")
	}
	contents, _ := os.ReadFile(path)
	if string(contents) != "mine" {
		t.Fatal("generator changed a human-owned file")
	}
}

func TestMigrationVersionAllocationIncludesFilesAndResourceState(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "issueboard")
	if err := createProject(newOptions{directory: directory, module: "example.com/issueboard", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)

	const existingVersion = "29991231235959"
	for _, direction := range []string{"up", "down"} {
		path := filepath.Join("database", "migrations", existingVersion+"_manual."+direction+".sql")
		if err := os.WriteFile(path, []byte("-- manual\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := makeResource("Issue", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	state, err := loadResourceState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Resources) != 1 || state.Resources[0].MigrationVersion != "29991231235960" {
		t.Fatalf("resource did not allocate after existing migration files: %+v", state.Resources)
	}

	resourceMigration := state.Resources[0].MigrationVersion + "_create_issues"
	for _, direction := range []string{"up", "down"} {
		if err := os.Remove(filepath.Join("database", "migrations", resourceMigration+"."+direction+".sql")); err != nil {
			t.Fatal(err)
		}
	}
	if err := makeMigration("after_resource", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, direction := range []string{"up", "down"} {
		path := filepath.Join("database", "migrations", "29991231235961_after_resource."+direction+".sql")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("migration did not allocate after resource metadata: %s: %v", path, err)
		}
	}
}
