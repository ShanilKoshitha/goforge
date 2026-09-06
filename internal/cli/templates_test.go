package cli

import (
	"go/format"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	jobpostgres "github.com/ShanilKoshitha/goforge/job/postgres"
)

func TestScaffoldTemplatesProduceFormattedSourceAndDotfiles(t *testing.T) {
	files, err := scaffoldFiles("example.com/app", filepath.Join(t.TempDir(), "framework checkout"), "app: demo")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".env", ".env.example", ".gitignore", ".forge/resources.json", ".forge/jobs.json"} {
		if _, ok := files[name]; !ok {
			t.Errorf("missing embedded dotfile %s", name)
		}
	}
	for name, content := range files {
		if filepath.Ext(name) != ".go" {
			continue
		}
		formatted, err := format.Source([]byte(content))
		if err != nil || string(formatted) != content {
			t.Errorf("unformatted Go source %s: %v", name, err)
		}
	}
	if !strings.Contains(files["forge.yaml"], `name: "app: demo"`) {
		t.Fatal("project name must be quoted YAML")
	}
	if !strings.Contains(files["forge.yaml"], "version: 8") {
		t.Fatal("fresh scaffold must declare format 8")
	}
	if !strings.Contains(files["resources/views/pages/welcome.forge.html"], "{{.Title}}") {
		t.Fatal("HTML template expression was altered")
	}
	if !strings.Contains(files["go.mod"], `=> "`) {
		t.Fatal("local replacement path must be quoted")
	}
	for _, checksum := range []string{
		"github.com/ShanilKoshitha/goforge v0.7.0 h1:FxcLm6Bubmd1tiQifDlgqR41TSTIHZBShc8LfYll8wM=",
		"github.com/ShanilKoshitha/goforge v0.7.0/go.mod h1:Ju5WVBe7csq7eJpSmlq+0x5OpY163cfrxhlFvd7rZrU=",
	} {
		if !strings.Contains(files["go.sum"], checksum) {
			t.Errorf("generated go.sum omits released framework checksum %q", checksum)
		}
	}
	if !strings.Contains(files["compose.yaml"], "postgres-data:/var/lib/postgresql\n") {
		t.Fatal("PostgreSQL 18 volume must contain its versioned data directory")
	}
	for _, setting := range []string{
		"APP_REQUEST_TIMEOUT=15s", "APP_READ_HEADER_TIMEOUT=5s", "APP_READ_TIMEOUT=30s",
		"APP_WRITE_TIMEOUT=30s", "APP_IDLE_TIMEOUT=2m", "APP_MAX_HEADER_BYTES=1048576",
		"APP_ENABLE_HSTS=false", "TRUSTED_PROXIES=",
		"SESSION_IDLE_LIFETIME=2h", "SESSION_ABSOLUTE_LIFETIME=24h",
	} {
		if !strings.Contains(files[".env.example"], setting) {
			t.Errorf("generated .env.example omits %q", setting)
		}
	}
	readme := files["README.md"]
	for _, guidance := range []string{"APP_ENABLE_HSTS", "always uses HTTPS", "TRUSTED_PROXIES", "comma-separated CIDR", "forwarding headers are ignored", "SESSION_IDLE_LIFETIME", "SESSION_ABSOLUTE_LIFETIME", "/settings/security"} {
		if !strings.Contains(readme, guidance) {
			t.Errorf("generated README omits deployment guidance %q", guidance)
		}
	}
	if !strings.Contains(files["internal/models/user.go"], `forge:"primary,generated,protected,required"`) ||
		!strings.Contains(files["internal/models/user.go"], "CredentialVersion") ||
		!strings.Contains(files[generatedORMPath], "var UserColumns") ||
		!strings.Contains(files[generatedORMPath], "CredentialVersion") {
		t.Fatal("scaffold must contain its application-owned User and current typed ORM")
	}
	if strings.TrimSpace(files["database/migrations/000002_create_jobs.up.sql"]) != strings.TrimSpace(jobpostgres.Schema) {
		t.Fatal("scaffold queue migration must match the public PostgreSQL adapter schema")
	}
	worker := files["cmd/worker/main.go"]
	for _, want := range []string{"jobpostgres.New(db)", "jobs.NewRegistry", "job.NewWorker", "worker.Run(ctx)", "job.SlogObserver"} {
		if !strings.Contains(worker, want) {
			t.Errorf("generated worker omits %q", want)
		}
	}
	dispatcher := files["internal/jobs/dispatcher.go"]
	if !strings.Contains(dispatcher, "dispatcher.Using(tx)") || !strings.Contains(dispatcher, "job.NewDispatcher(store, db") {
		t.Fatal("generated dispatcher must teach explicit transaction composition")
	}
	console := files["cmd/console/main.go"]
	for _, want := range []string{"queue:failed", "RetryFailed", "ForgetFailed"} {
		if !strings.Contains(console, want) {
			t.Errorf("generated console omits %q", want)
		}
	}
	if strings.Contains(console, "item.Payload") {
		t.Fatal("generated failed-job listing must not print payloads")
	}
	if !strings.Contains(console, "strconv.QuoteToASCII(item.FailureMessage)") {
		t.Fatal("generated failed-job listing must escape control characters in diagnostics")
	}
	authRepository := files["internal/auth/repository.go"]
	if !strings.Contains(authRepository, "postgres.Classify(err)") || !strings.Contains(authRepository, "errors.Is(err, orm.ErrUnique)") ||
		!strings.Contains(authRepository, "RehashPassword") || !strings.Contains(authRepository, "ChangePassword") ||
		!strings.Contains(authRepository, "RevokeSessions") || strings.Contains(authRepository, "pgconn") {
		t.Fatal("auth repository must classify driver errors without coupling application code to pgx")
	}
	second, err := scaffoldFiles("example.com/app", "", "app")
	if err != nil {
		t.Fatal(err)
	}
	if files[".env"] == second[".env"] {
		t.Fatal("new applications must have different session secrets")
	}
}

func TestCreatedProjectProtectsEnvironmentSecrets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose generated Unix permission bits")
	}
	directory := filepath.Join(t.TempDir(), "app")
	if err := createProject(newOptions{directory: directory, module: "example.com/app"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(directory, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf(".env permissions = %04o, want 0600", got)
	}
	example, err := os.Stat(filepath.Join(directory, ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	if got := example.Mode().Perm(); got != 0o644 {
		t.Fatalf(".env.example permissions = %04o, want 0644", got)
	}
}

func TestTemplateRejectsMissingData(t *testing.T) {
	if _, err := renderTemplate("templates/scaffold/forge.yaml.tmpl", "forge.yaml", map[string]string{}); err == nil {
		t.Fatal("missing template substitutions must fail before publication")
	}
}
