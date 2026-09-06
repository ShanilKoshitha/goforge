package cli

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
