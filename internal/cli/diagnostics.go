package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const minimumDiagnosticFormat = 17
const diagnosticOutputLimit = 64 << 10
const diagnosticCommandTimeout = 2 * time.Minute

type diagnosticCheck struct {
	ID      string   `json:"id"`
	Status  string   `json:"status"`
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Action  string   `json:"action"`
	Fields  []string `json:"fields,omitempty"`
}

type diagnosticReport struct {
	Version int               `json:"version"`
	Checks  []diagnosticCheck `json:"checks"`
}

type diagnosticRoute struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Name   string `json:"name"`
}

type diagnosticRoutes struct {
	Version int               `json:"version"`
	Routes  []diagnosticRoute `json:"routes"`
}

var diagnosticCheckIDs = []string{"config.http", "config.worker", "config.scheduler", "config.database", "application", "database", "migrations"}

func runProjectDoctor(ctx context.Context, stdout io.Writer, processes processRunner) error {
	if err := requireDiagnosticProject(); err != nil {
		return err
	}
	return diagnoseProject(ctx, stdout, processes)
}

func requireDiagnosticProject() error {
	if err := requireProjectRoot(); err != nil {
		return errors.New("diagnostics require a GoForge project root containing forge.yaml")
	}
	format, err := projectFormat()
	if err != nil {
		return errors.New("cannot inspect project format; review forge.yaml without changing application files")
	}
	if format < minimumDiagnosticFormat {
		return errors.New("doctor and routes:list require a format-17 application with application-owned diagnostics; existing source is not automatically upgraded")
	}
	if format > currentProjectFormat {
		return errors.New("project format is newer than this CLI supports; use a compatible released CLI")
	}
	return nil
}

// diagnoseProject is separate from the public format gate so runtime-first
// preparation can qualify an explicit candidate without activating its scaffold.
func diagnoseProject(ctx context.Context, stdout io.Writer, processes processRunner) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	failed := false
	output, processErr := captureDiagnosticProcess(ctx, processes, "./cmd/diagnostics", "doctor")
	var report diagnosticReport
	validReport := decodeDiagnosticJSON(output, &report) == nil && validDiagnosticReport(report)
	if !validReport {
		failed = true
		printDiagnostic(stdout, "application.diagnostics", "fail", "Cannot run application diagnostics; inspect cmd/diagnostics and Go dependencies with ordinary Go commands.")
	} else {
		for _, check := range report.Checks {
			message := safeDiagnosticMessage(check)
			printDiagnostic(stdout, check.ID, check.Status, message)
			failed = failed || check.Status != "pass"
		}
	}
	// A valid unhealthy report deliberately exits nonzero. Only add a separate
	// process failure if its exit contradicts a healthy report or no report exists.
	if processErr != nil && (!validReport || !failed) {
		failed = true
		printDiagnostic(stdout, "application.process", "fail", "Application diagnostic process failed; no raw subprocess output is shown. Inspect cmd/diagnostics explicitly.")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := generateORM(true, io.Discard); err != nil {
		failed = true
		printDiagnostic(stdout, "orm", "fail", "ORM cannot be verified; review internal/models, then explicitly run forge orm:generate.")
	} else {
		printDiagnostic(stdout, "orm", "pass", "Generated ORM is current.")
	}
	for _, check := range []struct {
		id, command, repair string
		args                []string
	}{
		{"views", "./cmd/views", "Review Forge templates and view functions, then explicitly run forge views:compile.", []string{"--check"}},
		{"assets", "./cmd/assets", "Review resources/assets and explicitly run forge assets:check.", nil},
	} {
		if err := ctx.Err(); err != nil {
			return err
		}
		_, err := captureDiagnosticProcess(ctx, processes, check.command, check.args...)
		status, message := "pass", "Application artifact check passed."
		if err != nil {
			failed = true
			status, message = "fail", check.repair
		}
		printDiagnostic(stdout, check.id, status, message)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if failed {
		return errors.New("doctor found failed or skipped checks; no repairs were performed")
	}
	return nil
}

func runProjectRouteList(ctx context.Context, stdout io.Writer, processes processRunner) error {
	if err := requireDiagnosticProject(); err != nil {
		return err
	}
	return inspectProjectRoutes(ctx, stdout, processes)
}

func inspectProjectRoutes(ctx context.Context, stdout io.Writer, processes processRunner) error {
	output, err := captureDiagnosticProcess(ctx, processes, "./cmd/diagnostics", "routes:list")
	if err != nil {
		return errors.New("route inventory could not construct the application; review HTTP configuration, constructors, and cmd/diagnostics explicitly")
	}
	var report diagnosticRoutes
	if err := decodeDiagnosticJSON(output, &report); err != nil || report.Version != 1 || len(report.Routes) == 0 {
		return errors.New("invalid application route-inventory report; review cmd/diagnostics")
	}
	for _, route := range report.Routes {
		if route.Method == "" || route.Path == "" {
			return errors.New("invalid application route metadata; review explicit route registration")
		}
	}
	fmt.Fprintln(stdout, "METHOD\tPATH\tNAME")
	for _, route := range report.Routes {
		// Quoting prevents terminal control sequences or tabs in handwritten
		// metadata from being interpreted as commands or additional columns.
		fmt.Fprintf(stdout, "%q\t%q\t%q\n", route.Method, route.Path, route.Name)
	}
	return nil
}

func captureDiagnosticProcess(ctx context.Context, processes processRunner, command string, args ...string) ([]byte, error) {
	runner, ok := processes.(environmentProcessRunner)
	if !ok {
		return nil, errors.New("diagnostic runner requires explicit environment support")
	}
	environment, err := diagnosticProcessEnvironment()
	if err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, diagnosticCommandTimeout)
	defer cancel()
	output := &limitedDiagnosticOutput{}
	arguments := append([]string{"run", "-mod=readonly", command}, args...)
	err = runner.RunWithEnvironment(bounded, nil, output, io.Discard, environment, "go", arguments...)
	if output.overflow {
		return nil, errors.New("application diagnostic output exceeded its bound")
	}
	return output.buffer.Bytes(), err
}

// Reuse the user's conventional shared Go caches, not application-local
// GOCACHE/GOMODCACHE/GOTMPDIR settings (including persisted go env settings).
// This creates no per-command or per-review cache copies and never changes
// persistent environment configuration. Cache paths are never printed.
func diagnosticProcessEnvironment() ([]string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, errors.New("cannot locate an external shared Go cache for read-only diagnostics")
	}
	homeDirectory, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.New("cannot locate an external shared Go module cache for read-only diagnostics")
	}
	root, err := os.Getwd()
	if err != nil {
		return nil, errors.New("cannot inspect project location for read-only diagnostics")
	}
	root, err = resolvedDiagnosticPath(root)
	if err != nil {
		return nil, errors.New("cannot resolve project location for read-only diagnostics")
	}
	paths := []struct{ key, value string }{
		{"GOCACHE", filepath.Join(cache, "go-build")},
		{"GOMODCACHE", filepath.Join(homeDirectory, "go", "pkg", "mod")},
		{"GOTMPDIR", filepath.Join(cache, "goforge", "tmp")},
	}
	environment := []string{"GOWORK=off"}
	for _, path := range paths {
		resolved, err := resolvedDiagnosticPath(path.value)
		if err != nil {
			return nil, errors.New("cannot safely resolve external Go cache directories for read-only diagnostics")
		}
		if strings.EqualFold(filepath.VolumeName(root), filepath.VolumeName(resolved)) {
			relative, err := filepath.Rel(root, resolved)
			if err != nil || relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return nil, errors.New("shared Go cache would be inside the application; use an external user cache location before diagnosis")
			}
		}
		environment = append(environment, path.key+"="+resolved)
	}
	// Go creates its temporary work directory before initializing its build
	// cache. A first-use external user cache therefore needs this parent first.
	temporaryParent := strings.TrimPrefix(environment[len(environment)-1], "GOTMPDIR=")
	if err := os.MkdirAll(temporaryParent, 0o755); err != nil {
		return nil, errors.New("cannot prepare external temporary Go build directory for read-only diagnostics")
	}
	return environment, nil
}

// Resolve existing ancestors so a cache symlink cannot redirect incidental Go
// writes into the application. Missing cache leaves are normal on first use.
func resolvedDiagnosticPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(absolute)
		if err == nil {
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(absolute)
		if parent == absolute {
			return "", err
		}
		missing = append(missing, filepath.Base(absolute))
		absolute = parent
	}
}

type limitedDiagnosticOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (output *limitedDiagnosticOutput) Write(content []byte) (int, error) {
	length := len(content)
	remaining := diagnosticOutputLimit - output.buffer.Len()
	if length > remaining {
		output.overflow = true
		content = content[:remaining]
	}
	_, _ = output.buffer.Write(content)
	return length, nil
}

func decodeDiagnosticJSON(output []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("unexpected trailing diagnostic output")
	}
	return nil
}

func validDiagnosticReport(report diagnosticReport) bool {
	if report.Version != 1 || len(report.Checks) != len(diagnosticCheckIDs) {
		return false
	}
	for index, check := range report.Checks {
		if check.ID != diagnosticCheckIDs[index] || check.Status != "pass" && check.Status != "fail" && check.Status != "skipped" {
			return false
		}
		if check.Status == "pass" && check.Code != "ok" || check.Status != "pass" && check.Code == "ok" {
			return false
		}
	}
	return true
}

func printDiagnostic(stdout io.Writer, id, status, message string) {
	fmt.Fprintf(stdout, "%s\t%s\t%s\n", status, id, message)
}

func safeDiagnosticMessage(check diagnosticCheck) string {
	if check.Status == "pass" {
		return "Check passed."
	}
	if check.Status == "skipped" {
		return "Not checked because a dependency failed; correct earlier failures and rerun forge doctor."
	}
	if strings.HasPrefix(check.ID, "config.") {
		loader := map[string]string{
			"config.http":      "config.Load (HTTP, sessions, and recovery)",
			"config.worker":    "config.LoadWorker (JOB_* and MAIL_SMTP_* settings)",
			"config.scheduler": "config.LoadScheduler (SCHEDULE_* settings)",
			"config.database":  "config.LoadDatabase (DATABASE_URL)",
		}[check.ID]
		return "Invalid configuration" + safeDiagnosticFields(check.Fields) + "; review .env and " + loader + " in internal/config. Values are never printed."
	}
	switch check.ID {
	case "database":
		return "PostgreSQL unavailable or timed out; review DATABASE_URL, credentials, network, and database availability."
	case "migrations":
		switch check.Code {
		case "pending":
			return "Migrations are pending; review paired SQL, back up production data, then deliberately run forge migrate."
		case "drift":
			return "Applied migration integrity differs from source; restore reviewed originals and investigate. Do not rebaseline checksums."
		case "ledger_upgrade":
			return "Migration ledger is uninitialized or legacy; inspect and back up the database before deliberate operational migration. No bookkeeping was changed."
		case "invalid_source":
			return "Migration source has an incomplete, empty, or inconsistently named SQL pair; review original paired files before any operational migration."
		default:
			return "Migration integrity could not be inspected; review database permissions and paired migration files explicitly."
		}
	default:
		return "Application construction failed; review HTTP settings, dependencies, assets, views, and explicit constructors. No server was started."
	}
}

func safeDiagnosticFields(fields []string) string {
	known := strings.Fields(`APP_ENV APP_ADDRESS APP_SHUTDOWN_TIMEOUT APP_REQUEST_TIMEOUT APP_READ_HEADER_TIMEOUT APP_READ_TIMEOUT APP_WRITE_TIMEOUT APP_IDLE_TIMEOUT APP_MAX_HEADER_BYTES APP_ENABLE_HSTS TRUSTED_PROXIES DATABASE_URL SESSION_SECRET SESSION_IDLE_LIFETIME SESSION_ABSOLUTE_LIFETIME APP_URL AUTH_PASSWORD_RESET_TTL MAIL_FROM MAIL_OUTBOX_KEY SCHEDULE_POLL_INTERVAL SCHEDULE_OP_TIMEOUT JOB_QUEUES JOB_CONCURRENCY JOB_POLL_INTERVAL JOB_LEASE_DURATION JOB_HEARTBEAT_INTERVAL JOB_OPERATION_TIMEOUT JOB_CANCELLATION_TIMEOUT JOB_SHUTDOWN_TIMEOUT MAIL_SMTP_ADDRESS MAIL_SMTP_USERNAME MAIL_SMTP_PASSWORD MAIL_SMTP_TLS MAIL_SMTP_SERVER_NAME MAIL_SMTP_TIMEOUT`)
	var safe []string
	for _, key := range known {
		for _, supplied := range fields {
			if supplied == key {
				safe = append(safe, key)
				break
			}
		}
	}
	if len(safe) == 0 {
		return ""
	}
	return " (" + strings.Join(safe, ", ") + ")"
}
