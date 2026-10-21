package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type diagnosticProcess struct {
	report      diagnosticReport
	routes      diagnosticRoutes
	raw         []byte
	failures    map[string]error
	calls       [][]string
	environment [][]string
}

func (process *diagnosticProcess) Run(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error {
	return errors.New("diagnostic subprocess must use explicit environment")
}

func (process *diagnosticProcess) RunWithEnvironment(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, environment []string, name string, args ...string) error {
	process.calls = append(process.calls, append([]string{name}, args...))
	process.environment = append(process.environment, append([]string(nil), environment...))
	if stdin != nil {
		return errors.New("diagnostic subprocess must not prompt")
	}
	if _, ok := ctx.Deadline(); !ok {
		return errors.New("diagnostic subprocess must have a deadline")
	}
	// Deliberately hostile output must never be forwarded to the terminal.
	fmt.Fprint(stderr, "smtp-password-secret postgres://user:dsn-secret@host/db SELECT password FROM users panic: stack-secret")
	key := args[2]
	if key == "./cmd/diagnostics" {
		if process.raw != nil {
			_, _ = stdout.Write(process.raw)
		} else if len(args) == 4 && args[3] == "routes:list" {
			_ = json.NewEncoder(stdout).Encode(process.routes)
		} else {
			_ = json.NewEncoder(stdout).Encode(process.report)
		}
	} else {
		fmt.Fprint(stdout, "private-template-secret raw-compiler-secret")
	}
	return process.failures[key]
}

func passingDiagnosticReport() diagnosticReport {
	report := diagnosticReport{Version: 1}
	for _, id := range diagnosticCheckIDs {
		report.Checks = append(report.Checks, diagnosticCheck{ID: id, Status: "pass", Code: "ok"})
	}
	return report
}

func TestDoctorRunsIndependentChecksAndNeverPrintsRawErrors(t *testing.T) {
	directory := workflowProject(t)
	t.Chdir(directory)
	before := diagnosticSnapshot(t, directory)
	process := &diagnosticProcess{report: passingDiagnosticReport(), failures: map[string]error{
		"./cmd/diagnostics": errors.New("private-driver-secret"),
		"./cmd/views":       errors.New("private-compiler-secret"),
		"./cmd/assets":      errors.New("private-asset-secret"),
	}}
	process.report.Checks[0] = diagnosticCheck{ID: "config.http", Status: "fail", Code: "invalid_config", Fields: []string{"APP_REQUEST_TIMEOUT", "private-field-secret"}, Message: "private-message-secret", Action: "private-action-secret"}
	process.report.Checks[5] = diagnosticCheck{ID: "database", Status: "fail", Code: "unavailable"}
	process.report.Checks[6] = diagnosticCheck{ID: "migrations", Status: "skipped", Code: "dependency"}
	// A stale ORM must not prevent either remaining artifact subprocess.
	name := filepath.FromSlash(generatedORMPath)
	if err := os.WriteFile(name, []byte("// stale generated ORM\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before = diagnosticSnapshot(t, directory)
	var output bytes.Buffer
	err := diagnoseProject(context.Background(), &output, process)
	if err == nil {
		t.Fatal("doctor succeeded despite independent failures")
	}
	for _, expected := range []string{"config.http", "APP_REQUEST_TIMEOUT", "database", "migrations", "skipped", "orm", "views", "assets", "forge orm:generate", "forge views:compile", "forge assets:check"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("missing %q in safe report: %s", expected, &output)
		}
	}
	assertDiagnosticPrivacy(t, output.String()+err.Error())
	assertDiagnosticCalls(t, process, 3)
	if !reflect.DeepEqual(before, diagnosticSnapshot(t, directory)) {
		t.Fatal("doctor changed application files or directories")
	}
}

func TestDoctorHealthyReportDoesNotMutateProject(t *testing.T) {
	directory := workflowProject(t)
	t.Chdir(directory)
	before := diagnosticSnapshot(t, directory)
	process := &diagnosticProcess{report: passingDiagnosticReport()}
	var output bytes.Buffer
	if err := diagnoseProject(context.Background(), &output, process); err != nil {
		t.Fatal(err)
	}
	if strings.Count(output.String(), "pass\t") != 10 {
		t.Fatalf("not all ten independent checks passed: %s", &output)
	}
	assertDiagnosticCalls(t, process, 3)
	if !reflect.DeepEqual(before, diagnosticSnapshot(t, directory)) {
		t.Fatal("doctor changed healthy application")
	}
}

func TestDoctorRejectsInvalidReportsWithoutLeakingOutput(t *testing.T) {
	directory := workflowProject(t)
	t.Chdir(directory)
	for name, raw := range map[string][]byte{
		"compiler":  []byte("private-compiler-secret"),
		"trailing":  []byte(`{"version":1,"checks":[]} private-trailing-secret`),
		"unknown":   []byte(`{"version":1,"checks":[],"private-field-secret":"private-value-secret"}`),
		"oversized": bytes.Repeat([]byte("private-output-secret"), diagnosticOutputLimit),
	} {
		t.Run(name, func(t *testing.T) {
			process := &diagnosticProcess{raw: raw}
			var output bytes.Buffer
			if err := diagnoseProject(context.Background(), &output, process); err == nil {
				t.Fatal("invalid diagnostic report passed")
			}
			assertDiagnosticPrivacy(t, output.String())
			assertDiagnosticCalls(t, process, 3)
		})
	}
	for name, mutate := range map[string]func(*diagnosticReport){
		"missing":    func(report *diagnosticReport) { report.Checks = report.Checks[:6] },
		"duplicate":  func(report *diagnosticReport) { report.Checks[1].ID = report.Checks[0].ID },
		"version":    func(report *diagnosticReport) { report.Version = 2 },
		"status":     func(report *diagnosticReport) { report.Checks[0].Status = "private-status-secret" },
		"false-pass": func(report *diagnosticReport) { report.Checks[0].Code = "invalid_config" },
	} {
		t.Run(name, func(t *testing.T) {
			report := passingDiagnosticReport()
			mutate(&report)
			if validDiagnosticReport(report) {
				t.Fatal("accepted malformed report")
			}
		})
	}
}

func TestDiagnosticCLIRejectsOldFutureAndInvalidUsageWithoutMutation(t *testing.T) {
	directory := workflowProject(t)
	t.Chdir(directory)
	for _, version := range []string{"16", "999", "private-format-secret"} {
		if err := os.WriteFile("forge.yaml", []byte("version: "+version+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		before := diagnosticSnapshot(t, directory)
		for _, args := range [][]string{{"doctor"}, {"routes:list"}, {"doctor", "--fix"}, {"routes:list", "--private"}} {
			process := &diagnosticProcess{}
			var output bytes.Buffer
			err := run(context.Background(), args, nil, &output, &output, process)
			if err == nil || len(process.calls) != 0 {
				t.Fatalf("unsafe invocation %v ran: %v", args, err)
			}
			assertDiagnosticPrivacy(t, err.Error()+output.String())
		}
		if !reflect.DeepEqual(before, diagnosticSnapshot(t, directory)) {
			t.Fatal("format/usage refusal mutated project")
		}
	}
}

func TestRouteInventoryUsesReportedActualMetadataAndQuotesControls(t *testing.T) {
	process := &diagnosticProcess{routes: diagnosticRoutes{Version: 1, Routes: []diagnosticRoute{
		{Method: "GET", Path: "/ready", Name: "health.ready"},
		{Method: "POST", Path: "/custom", Name: "handwritten\x1b[2J\tcontrol"},
	}}}
	var output bytes.Buffer
	if err := inspectProjectRoutes(context.Background(), &output, process); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"/custom"`) || !strings.Contains(output.String(), `\x1b`) || strings.ContainsRune(output.String(), '\x1b') {
		t.Fatalf("actual metadata missing or unsafe: %q", output.String())
	}
	assertDiagnosticPrivacy(t, output.String())
	assertDiagnosticCalls(t, process, 1)
	process.failures = map[string]error{"./cmd/diagnostics": errors.New("private-route-secret")}
	output.Reset()
	if err := inspectProjectRoutes(context.Background(), &output, process); err == nil || output.Len() != 0 {
		t.Fatal("failed construction produced partial inventory")
	}
}

func TestDoctorCancellationStopsBeforeStartingChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	process := &diagnosticProcess{}
	err := diagnoseProject(ctx, io.Discard, process)
	if !errors.Is(err, context.Canceled) || len(process.calls) != 0 {
		t.Fatalf("cancelled doctor ran: %v calls=%v", err, process.calls)
	}
}

func TestOverrideProcessEnvironmentPreservesParentAndRemovesDuplicates(t *testing.T) {
	base := []string{"PATH=original", "GOWORK=parent.work", "GOWORK=duplicate", "UNRELATED=retained"}
	original := append([]string(nil), base...)
	got := overrideProcessEnvironment(base, []string{"GOWORK=off"})
	want := []string{"PATH=original", "UNRELATED=retained", "GOWORK=off"}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(base, original) {
		t.Fatalf("environment=%v parent=%v", got, base)
	}
	if runtime.GOOS == "windows" {
		got = overrideProcessEnvironment([]string{"GoWork=parent.work"}, []string{"GOWORK=off"})
		if !reflect.DeepEqual(got, []string{"GOWORK=off"}) {
			t.Fatalf("Windows duplicate case key retained: %v", got)
		}
	}
}

func TestDiagnosticCacheInsideApplicationIsRefusedBeforeSubprocess(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	switch runtime.GOOS {
	case "windows":
		t.Setenv("LOCALAPPDATA", filepath.Join(directory, "absent-cache"))
	case "darwin":
		// macOS's conventional cache directory is derived from the user home.
		t.Setenv("HOME", directory)
	default:
		t.Setenv("XDG_CACHE_HOME", filepath.Join(directory, "absent-cache"))
	}
	before := diagnosticSnapshot(t, directory)
	process := &diagnosticProcess{report: passingDiagnosticReport()}
	if _, err := captureDiagnosticProcess(context.Background(), process, "./cmd/diagnostics", "doctor"); err == nil || len(process.calls) != 0 {
		t.Fatalf("application-contained shared cache was used: %v calls=%v", err, process.calls)
	}
	if !reflect.DeepEqual(before, diagnosticSnapshot(t, directory)) {
		t.Fatal("cache refusal mutated application directories")
	}
}

func TestResolvedDiagnosticCachePathResolvesExistingSymlinkAncestors(t *testing.T) {
	directory := t.TempDir()
	actual := filepath.Join(directory, "actual")
	if err := os.Mkdir(actual, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(actual, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skip("creating symlinks requires Windows developer mode or the symlink privilege")
		}
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(actual)
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolvedDiagnosticPath(filepath.Join(link, "missing", "go-build"))
	if err != nil || got != filepath.Join(want, "missing", "go-build") {
		t.Fatalf("cache ancestor was not resolved: %q %v", got, err)
	}
}

func TestDiagnosticPreparesMissingExternalTemporaryCacheParent(t *testing.T) {
	base := t.TempDir()
	application := filepath.Join(base, "application")
	if err := os.Mkdir(application, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(application)
	external := filepath.Join(base, "external", "first-use")
	switch runtime.GOOS {
	case "windows":
		t.Setenv("LOCALAPPDATA", external)
	case "darwin":
		t.Setenv("HOME", external)
	default:
		t.Setenv("XDG_CACHE_HOME", external)
	}
	before := diagnosticSnapshot(t, application)
	environment, err := diagnosticProcessEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range environment {
		if strings.HasPrefix(value, "GOTMPDIR=") {
			info, err := os.Stat(strings.TrimPrefix(value, "GOTMPDIR="))
			if err != nil || !info.IsDir() {
				t.Fatalf("first-use temporary parent unavailable: %v", err)
			}
		}
	}
	if !reflect.DeepEqual(before, diagnosticSnapshot(t, application)) {
		t.Fatal("external cache preparation mutated app")
	}
}

func TestDiagnosticOutputBoundAppliesToIOCopy(t *testing.T) {
	output := &limitedDiagnosticOutput{}
	// Do not embed bytes.Buffer: its promoted ReadFrom would bypass Write when
	// os/exec drains child stdout through io.Copy.
	if _, bypasses := any(output).(io.ReaderFrom); bypasses {
		t.Fatal("output bound can be bypassed through ReaderFrom")
	}
	if _, err := io.Copy(output, io.LimitReader(strings.NewReader(strings.Repeat("x", diagnosticOutputLimit+1)), diagnosticOutputLimit+1)); err != nil {
		t.Fatal(err)
	}
	if !output.overflow || output.buffer.Len() != diagnosticOutputLimit {
		t.Fatal("subprocess output exceeded capture bound")
	}
}

func assertDiagnosticPrivacy(t *testing.T, output string) {
	t.Helper()
	for _, secret := range []string{"private-", "smtp-password-secret", "dsn-secret", "stack-secret", "SELECT password"} {
		if strings.Contains(output, secret) {
			t.Fatalf("diagnostic output disclosed %q: %s", secret, output)
		}
	}
}

func assertDiagnosticCalls(t *testing.T, process *diagnosticProcess, count int) {
	t.Helper()
	if len(process.calls) != count {
		t.Fatalf("subprocess count=%d want=%d calls=%v", len(process.calls), count, process.calls)
	}
	for index, args := range process.calls {
		wantEnvironment, err := diagnosticProcessEnvironment()
		if err != nil {
			t.Fatal(err)
		}
		if len(args) < 4 || !reflect.DeepEqual(args[:3], []string{"go", "run", "-mod=readonly"}) || !reflect.DeepEqual(process.environment[index], wantEnvironment) {
			t.Fatalf("unsafe subprocess %v environment=%v", args, process.environment[index])
		}
	}
}

func diagnosticSnapshot(t *testing.T, directory string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			result[name] = "directory"
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result[name] = fmt.Sprintf("%x", sha256.Sum256(content))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
