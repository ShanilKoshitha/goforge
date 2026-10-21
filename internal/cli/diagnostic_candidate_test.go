package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// This candidate is deliberately separate from normal generation until the
// added runtime APIs have a public immutable tag and verified module sums.
func diagnosticCandidateProject(t *testing.T) string {
	t.Helper()
	directory := workflowProject(t)
	t.Chdir(directory)
	if err := RunContext(context.Background(), []string{"make:resource", "DiagnosticIssue", "--field", "title:string"}, nil, io.Discard, io.Discard); err != nil {
		t.Fatalf("generate inspectable resource before candidate overlay: %v", err)
	}
	files, err := diagnosticScaffoldFiles("example.com/application")
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range files {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: 17\nname: candidate\ndatabase: postgres\nlayout: standard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return directory
}

func TestDiagnosticCandidateCompilesAndRunsOrdinaryGoChecks(t *testing.T) {
	directory := diagnosticCandidateProject(t)
	t.Chdir(directory)
	for key, value := range map[string]string{
		"GOWORK": "off", "APP_ENV": "local", "APP_URL": "http://localhost:8080",
		"DATABASE_URL": "postgres://diagnostic:private-dsn-secret@127.0.0.1:1/diagnostic?sslmode=disable",
	} {
		t.Setenv(key, value)
	}
	if output, err := generatedCommand(directory, os.Environ(), "go", "test", "-race", "-mod=readonly", "./...", "-count=1"); err != nil {
		t.Fatalf("ordinary application-owned diagnostic tests failed: %v\n%s", err, output)
	}
	// Local cache settings must not cause a read-only workflow to create files
	// inside the app. The CLI deliberately uses conventional shared user caches.
	for _, key := range []string{"GOCACHE", "GOMODCACHE", "GOTMPDIR"} {
		t.Setenv(key, filepath.Join(directory, "absent-diagnostic-cache", key))
	}
	before := diagnosticSnapshot(t, directory)
	var output bytes.Buffer
	err := diagnoseProject(context.Background(), &output, execProcessRunner{})
	if err == nil || !strings.Contains(output.String(), "fail\tdatabase") || !strings.Contains(output.String(), "skipped\tmigrations") {
		t.Fatalf("offline candidate diagnosis = %v\n%s", err, &output)
	}
	for _, id := range []string{"config.http", "config.worker", "config.scheduler", "config.database", "application", "orm", "views", "assets"} {
		if !strings.Contains(output.String(), "pass\t"+id+"\t") {
			t.Errorf("offline independent check %s did not pass: %s", id, &output)
		}
	}
	assertDiagnosticPrivacy(t, output.String()+err.Error())
	output.Reset()
	if err := inspectProjectRoutes(context.Background(), &output, execProcessRunner{}); err != nil {
		t.Fatalf("offline actual route inventory failed: %v", err)
	}
	for _, expected := range []string{`"/health"`, `"/ready"`, `"health.ready"`, `"/auth/tokens"`, `"/diagnostic_issues"`, `"/app/diagnostic_issues"`} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("actual route inventory missing %s: %s", expected, &output)
		}
	}
	if !reflect.DeepEqual(before, diagnosticSnapshot(t, directory)) {
		t.Fatal("application diagnostic tests/commands mutated application files or directories")
	}
	// A parsed JSON report is available directly through application Go, without
	// depending on the framework CLI's presentation or subprocess protocol.
	raw, _ := captureDiagnosticProcess(context.Background(), execProcessRunner{}, "./cmd/diagnostics", "doctor")
	var report diagnosticReport
	if err := json.Unmarshal(raw, &report); err != nil || !validDiagnosticReport(report) {
		t.Fatalf("application-owned report is invalid: %v", err)
	}
	assertDiagnosticPrivacy(t, string(raw))

	// Inventory follows handwritten production registration changes instead of
	// reading the managed resource registry or parsing a route-source manifest.
	routesPath := filepath.Join(directory, "routes", "routes.go")
	routes, err := os.ReadFile(routesPath)
	if err != nil {
		t.Fatal(err)
	}
	needle := `router.Named("health.ready", "GET", "/ready", health.Ready)`
	if strings.Count(string(routes), needle) != 1 {
		t.Fatal("production route registration seam is ambiguous")
	}
	for _, path := range []string{"/handwritten", "/edited-handwritten"} {
		edited := strings.Replace(string(routes), needle, needle+"\n\trouter.Named(\"handwritten.show\", \"GET\", \""+path+"\", health.Show)", 1)
		if err := os.WriteFile(routesPath, []byte(edited), 0o644); err != nil {
			t.Fatal(err)
		}
		before := diagnosticSnapshot(t, directory)
		output.Reset()
		if err := inspectProjectRoutes(context.Background(), &output, execProcessRunner{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), `"`+path+`"`) || !strings.Contains(output.String(), `"handwritten.show"`) {
			t.Fatalf("inventory missed edited production registration: %s", &output)
		}
		if !reflect.DeepEqual(before, diagnosticSnapshot(t, directory)) {
			t.Fatal("route inventory mutated application")
		}
	}

	t.Run("malformed secrets across processes", func(t *testing.T) {
		for key, value := range map[string]string{
			"APP_REQUEST_TIMEOUT": "private-http-duration-secret",
			"JOB_CONCURRENCY":     "private-worker-integer-secret",
			"SCHEDULE_OP_TIMEOUT": "private-scheduler-duration-secret",
		} {
			t.Setenv(key, value)
		}
		before := diagnosticSnapshot(t, directory)
		output.Reset()
		if err := diagnoseProject(context.Background(), &output, execProcessRunner{}); err == nil {
			t.Fatal("malformed configuration passed diagnosis")
		}
		for _, key := range []string{"APP_REQUEST_TIMEOUT", "JOB_CONCURRENCY", "SCHEDULE_OP_TIMEOUT"} {
			if !strings.Contains(output.String(), key) {
				t.Errorf("safe diagnosis omitted actionable key %s: %s", key, &output)
			}
		}
		assertDiagnosticPrivacy(t, output.String())
		if !reflect.DeepEqual(before, diagnosticSnapshot(t, directory)) {
			t.Fatal("invalid-configuration diagnosis changed application")
		}
	})

	paths := []string{generatedORMPath, "resources/views/pages/welcome.forge.html", "resources/assets/files/app.js"}
	originals := make(map[string][]byte)
	for _, path := range paths {
		content, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Fatal(err)
		}
		originals[path] = content
	}
	for _, indices := range [][]int{{0}, {1}, {2}, {0, 1, 2}} {
		t.Run("artifact failures "+strings.Join(diagnosticArtifactIDs(indices), "+"), func(t *testing.T) {
			for _, path := range paths {
				if err := os.WriteFile(filepath.FromSlash(path), originals[path], 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for _, index := range indices {
				path := filepath.FromSlash(paths[index])
				if index == 2 {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(path, append(append([]byte(nil), originals[paths[index]]...), []byte("\n// private-stale-source-secret\n")...), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			before := diagnosticSnapshot(t, directory)
			output.Reset()
			if err := diagnoseProject(context.Background(), &output, execProcessRunner{}); err == nil {
				t.Fatal("dirty artifacts passed diagnosis")
			}
			for _, id := range diagnosticArtifactIDs(indices) {
				if !strings.Contains(output.String(), "fail\t"+id+"\t") {
					t.Errorf("independent failure %s not reported: %s", id, &output)
				}
			}
			assertDiagnosticPrivacy(t, output.String())
			if !reflect.DeepEqual(before, diagnosticSnapshot(t, directory)) {
				t.Fatal("artifact diagnosis repaired or mutated the application")
			}
		})
	}
}

func diagnosticArtifactIDs(indices []int) []string {
	var result []string
	for _, index := range indices {
		result = append(result, []string{"orm", "views", "assets"}[index])
	}
	return result
}

func TestDiagnosticRuntimePreparationPreservesFormatSixteenScaffold(t *testing.T) {
	if currentProjectFormat >= minimumDiagnosticFormat {
		t.Skip("runtime preparation gate was superseded by explicit format-17 adoption")
	}
	directory := workflowProject(t)
	for _, name := range []string{"cmd/diagnostics", "internal/diagnostics"} {
		if _, err := os.Stat(filepath.Join(directory, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Fatalf("runtime-first preparation injected %s: %v", name, err)
		}
	}
	t.Chdir(directory)
	if format, err := projectFormat(); err != nil || format != 16 {
		t.Fatalf("runtime-first preparation changed current format: %d %v", format, err)
	}
	if err := runProjectDoctor(context.Background(), io.Discard, execProcessRunner{}); err == nil {
		t.Fatal("old application silently gained diagnostic support")
	}
}
