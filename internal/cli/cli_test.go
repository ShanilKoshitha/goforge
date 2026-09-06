package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionMatchesPatchRelease(t *testing.T) {
	var output bytes.Buffer
	if err := Run([]string{"version"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "forge 0.8.1\n" {
		t.Fatalf("version output = %q", output.String())
	}
}

func TestRunNewCreatesInspectableApplication(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "orders")
	root := projectRoot(t)
	var output bytes.Buffer
	if err := Run([]string{"new", directory, "--module", "example.com/orders", "--replace", root}, &output, &output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"go.mod",
		"forge.yaml",
		"cmd/server/main.go",
		"cmd/worker/main.go",
		"cmd/views/main.go",
		"routes/routes.go",
		"internal/application/application.go",
		"internal/models/user.go",
		"internal/models/zz_orm_gen.go",
		"database/migrations/000001_create_auth.up.sql",
		"database/migrations/000002_create_jobs.up.sql",
		"internal/jobs/dependencies.go",
		"internal/jobs/dispatcher.go",
		"internal/jobs/registry_gen.go",
		".forge/jobs.json",
		"resources/views/layouts/app.forge.html",
		"resources/views/components/card.forge.html",
		"resources/views/functions.go",
		"resources/views/viewfuncs/functions.go",
		"resources/views/pages/welcome.forge.html",
		"resources/views/auth/security.forge.html",
		"resources/views/views_gen.go",
	} {
		if _, err := os.Stat(filepath.Join(directory, filepath.FromSlash(name))); err != nil {
			t.Errorf("expected %s: %v", name, err)
		}
	}
	routes, err := os.ReadFile(filepath.Join(directory, "routes", "routes.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(routes), "example.com/orders/internal/http/controllers") {
		t.Fatalf("module placeholder was not replaced:\n%s", routes)
	}
	manifest, err := os.ReadFile(filepath.Join(directory, "forge.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `name: "orders"`) {
		t.Fatalf("project name was not rendered in forge.yaml:\n%s", manifest)
	}
	if !strings.Contains(string(manifest), "version: 8") {
		t.Fatalf("fresh scaffold is not format 8:\n%s", manifest)
	}
	compiledViews, err := os.ReadFile(filepath.Join(directory, "resources", "views", "views_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compiledViews), `Name: "pages/welcome"`) || strings.Contains(string(compiledViews), "@extends") {
		t.Fatalf("generated views are not compiled into inspectable standard templates:\n%s", compiledViews)
	}
	generatedORM, err := os.ReadFile(filepath.Join(directory, "internal", "models", "zz_orm_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generatedORM), "var UserMapper") || !strings.Contains(string(generatedORM), "func (store Store) Users() UserQuery") ||
		!strings.Contains(string(generatedORM), "CredentialVersion") {
		t.Fatalf("scaffold ORM is not current inspectable generated Go:\n%s", generatedORM)
	}
	moduleFile, err := os.ReadFile(filepath.Join(directory, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(moduleFile), "github.com/ShanilKoshitha/goforge v0.8.0") {
		t.Fatalf("scaffold does not pin GoForge v0.8.0:\n%s", moduleFile)
	}
	requestPath, requestContent, err := requestFile("CreateUser")
	if err != nil {
		t.Fatal(err)
	}
	requestPath = filepath.Join(directory, requestPath)
	if err := os.MkdirAll(filepath.Dir(requestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(requestPath, []byte(requestContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	var checkOutput bytes.Buffer
	if err := Run([]string{"orm:generate", "--check"}, &checkOutput, &checkOutput); err != nil {
		t.Fatalf("fresh scaffold ORM is not current: %v\n%s", err, checkOutput.String())
	}
	if err := Run([]string{"make:component", "Notice"}, &checkOutput, &checkOutput); err != nil {
		t.Fatalf("fresh scaffold cannot generate a component: %v\n%s", err, checkOutput.String())
	}
	if _, err := os.Stat(filepath.Join("resources", "views", "components", "notice.forge.html")); err != nil {
		t.Fatalf("generated component is missing: %v", err)
	}
	if err := Run([]string{"views:compile", "--check"}, &checkOutput, &checkOutput); err != nil {
		t.Fatalf("fresh scaffold views are not current: %v\n%s", err, checkOutput.String())
	}
	if err := Run([]string{"make:job", "SendWelcome"}, &checkOutput, &checkOutput); err != nil {
		t.Fatalf("fresh scaffold cannot generate a job: %v\n%s", err, checkOutput.String())
	}
	if err := Run([]string{"make", "job", "ArchiveAccount"}, &checkOutput, &checkOutput); err != nil {
		t.Fatalf("fresh scaffold cannot generate a job through the spaced alias: %v\n%s", err, checkOutput.String())
	}
	registry, err := os.ReadFile(filepath.Join("internal", "jobs", "registry_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(string(registry), "ArchiveAccountDefinition") > strings.Index(string(registry), "SendWelcomeDefinition") {
		t.Fatalf("job registry is not deterministic by job name:\n%s", registry)
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GOCACHE="+filepath.Join(directory, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generated application does not compile: %v\n%s", err, output)
	}
}

func TestCreateProjectRefusesExistingDirectory(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "mine.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := createProject(newOptions{directory: directory, module: "example.com/app"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected existing destination error, got %v", err)
	}
}

func TestGeneratorsRefuseToOverwrite(t *testing.T) {
	directory := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("forge.yaml", []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run([]string{"make:controller", "Users"}, &output, &output); err != nil {
		t.Fatal(err)
	}
	if err := Run([]string{"make:controller", "Users"}, &output, &output); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("expected overwrite refusal, got %v", err)
	}
}

func TestPrimitiveGeneratorsRefuseFutureFormatBeforeWriting(t *testing.T) {
	for _, command := range []string{"controller", "request", "migration"} {
		t.Run(command, func(t *testing.T) {
			directory := t.TempDir()
			t.Chdir(directory)
			if err := os.WriteFile("forge.yaml", []byte("version: 9\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			err := Run([]string{"make:" + command, "Future"}, &output, &output)
			if err == nil || !strings.Contains(err.Error(), "newer than this CLI supports") {
				t.Fatalf("expected future-format refusal, got %v", err)
			}
			entries, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "forge.yaml" {
				t.Fatalf("future-format refusal wrote files: %+v", entries)
			}
		})
	}
}

func projectRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}
