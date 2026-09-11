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
	if !strings.Contains(files["forge.yaml"], "version: 14") {
		t.Fatal("fresh scaffold must declare format 14")
	}
	if !strings.Contains(files["resources/views/pages/welcome.forge.html"], "{{.Title}}") {
		t.Fatal("HTML template expression was altered")
	}
	if !strings.Contains(files["go.mod"], `=> "`) {
		t.Fatal("local replacement path must be quoted")
	}
	if strings.Contains(files["go.sum"], "github.com/ShanilKoshitha/goforge v0.17.0") {
		t.Fatal("fresh scaffold retains a stale framework checksum")
	}
	if !strings.Contains(files["compose.yaml"], "postgres-data:/var/lib/postgresql\n") {
		t.Fatal("PostgreSQL 18 volume must contain its versioned data directory")
	}
	for _, want := range []string{"axllent/mailpit:v1.31.1", `"127.0.0.1:1025:1025"`, `"127.0.0.1:8025:8025"`} {
		if !strings.Contains(files["compose.yaml"], want) {
			t.Errorf("generated Compose file omits safe mail catcher setting %q", want)
		}
	}
	for _, setting := range []string{
		"APP_REQUEST_TIMEOUT=15s", "APP_READ_HEADER_TIMEOUT=5s", "APP_READ_TIMEOUT=30s",
		"APP_WRITE_TIMEOUT=30s", "APP_IDLE_TIMEOUT=2m", "APP_MAX_HEADER_BYTES=1048576",
		"APP_ENABLE_HSTS=false", "TRUSTED_PROXIES=",
		"SESSION_IDLE_LIFETIME=2h", "SESSION_ABSOLUTE_LIFETIME=24h",
		"APP_URL=https://example.com", "AUTH_PASSWORD_RESET_TTL=30m",
		"MAIL_FROM=GoForge <no-reply@example.test>", "MAIL_SMTP_TLS=implicit",
		"MAIL_OUTBOX_KEY=replace-with-32-random-bytes-as-unpadded-base64url",
	} {
		if !strings.Contains(files[".env.example"], setting) {
			t.Errorf("generated .env.example omits %q", setting)
		}
	}
	readme := files["README.md"]
	for _, guidance := range []string{"APP_ENABLE_HSTS", "always uses HTTPS", "TRUSTED_PROXIES", "comma-separated CIDR", "forwarding headers are ignored", "SESSION_IDLE_LIFETIME", "SESSION_ABSOLUTE_LIFETIME", "/settings/security", "Authorization: Bearer", "personal_tokens.go", "forge dev", "FORGE_DEV_SHUTDOWN_TIMEOUT", "worker", "scheduler"} {
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
	recoveryMigration := files["database/migrations/000004_create_account_recovery.up.sql"]
	for _, want := range []string{"selector TEXT PRIMARY KEY", "secret_digest BYTEA", "credential_version BIGINT", "mail_outbox", "nonce BYTEA", "ciphertext BYTEA"} {
		if !strings.Contains(recoveryMigration, want) {
			t.Errorf("recovery migration omits %q", want)
		}
	}
	for _, forbidden := range []string{"presented_token", "raw_token", "recipient", "subject", "body", "email TEXT"} {
		if strings.Contains(recoveryMigration, forbidden) {
			t.Errorf("recovery migration persists forbidden plaintext field %q", forbidden)
		}
	}
	personalTokenMigration := files["database/migrations/000006_create_personal_access_tokens.up.sql"]
	for _, want := range []string{
		"personal_access_tokens", "OCTET_LENGTH(selector) = 22",
		"selector ~ '^[A-Za-z0-9_-]{22}$'", "OCTET_LENGTH(secret_digest) = 32",
		"UNIQUE (user_id, name)", "REFERENCES users (id) ON DELETE CASCADE",
	} {
		if !strings.Contains(personalTokenMigration, want) {
			t.Errorf("personal-token migration omits %q", want)
		}
	}
	for _, forbidden := range []string{"raw_token", "presented_token", "secret TEXT", "password"} {
		if strings.Contains(personalTokenMigration, forbidden) {
			t.Errorf("personal-token migration persists forbidden field %q", forbidden)
		}
	}
	personalTokens := files["internal/auth/personal_tokens.go"]
	for _, want := range []string{"securitytoken.Issue()", "securitytoken.MatchDigest", "FOR UPDATE", "maximumLivePersonalTokens", "PersonalTokenAuthenticator"} {
		if !strings.Contains(personalTokens, want) {
			t.Errorf("generated personal-token repository omits %q", want)
		}
	}
	apiMiddleware := files["internal/auth/api_middleware.go"]
	for _, want := range []string{"Header.Values(\"Authorization\")", "bearerUnauthorized", "TokenIDFrom", "sessionHandler"} {
		if !strings.Contains(apiMiddleware, want) {
			t.Errorf("generated API middleware omits %q", want)
		}
	}
	mailbox := files["internal/mailbox/store.go"]
	for _, want := range []string{"aes.NewCipher", "cipher.NewGCM", "additionalData(id, version)", "frameworkmail.NewMessage", "type Executor interface"} {
		if !strings.Contains(mailbox, want) {
			t.Errorf("generated encrypted mailbox omits %q", want)
		}
	}
	worker := files["cmd/worker/main.go"]
	for _, want := range []string{"jobpostgres.New(db)", "jobs.NewRegistry", "job.NewWorker", "worker.Run(ctx)", "job.SlogObserver", "mailsmtp.New", "mailbox.New(settings.MailOutboxKey)"} {
		if !strings.Contains(worker, want) {
			t.Errorf("generated worker omits %q", want)
		}
	}
	routes := files["routes/routes.go"]
	for _, want := range []string{"/forgot-password", "/reset-password", "/auth/password/forgot", "/auth/password/reset", "defaultPasswordRecovery"} {
		if !strings.Contains(routes, want) {
			t.Errorf("generated routes omit recovery contract %q", want)
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
		t.Fatal("new applications must have different generated secrets")
	}
	firstKey := environmentValue(files[".env"], "MAIL_OUTBOX_KEY")
	secondKey := environmentValue(second[".env"], "MAIL_OUTBOX_KEY")
	if firstKey == "" || secondKey == "" || firstKey == secondKey || len(firstKey) != 43 || len(secondKey) != 43 {
		t.Fatal("new applications must have independent 256-bit mail outbox keys")
	}
}

func environmentValue(contents, name string) string {
	prefix := name + "="
	for _, line := range strings.Split(contents, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
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
