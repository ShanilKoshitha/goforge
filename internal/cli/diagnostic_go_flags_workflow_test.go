package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Go's own linker can write into the app through GOFLAGS even when modules
// are readonly. Exercise real process and persisted flags, not a fake runner.
func TestDiagnosticGoFlagsCannotWriteApplicationProfiles(t *testing.T) {
	directory := adoptionDiagnosticProject(t)
	for key, value := range map[string]string{
		"APP_ENV": "local", "APP_URL": "http://localhost:8080",
		"DATABASE_URL": "postgres://diagnostic:private-dsn-secret@127.0.0.1:1/diagnostic?sslmode=disable",
		"GOPRIVATE":    "",
	} {
		t.Setenv(key, value)
	}
	const privateModules = "example.test/private/*"
	external := t.TempDir()
	for _, command := range []string{"doctor", "routes:list"} {
		for _, source := range []string{"process", "persisted"} {
			t.Run(command+"/"+source, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				name := strings.ReplaceAll(command, ":", "-") + "-" + source
				// Distinct linker inputs avoid a warmed link cache concealing a
				// regression after the ordinary-Go anti-vacuity control runs.
				directProfile := filepath.Join(directory, "direct linker "+name+".cpu")
				guardedProfile := filepath.Join(directory, "guarded linker "+name+".cpu")
				directFlags := diagnosticLinkerProfileFlags(t, directProfile)
				guardedFlags := diagnosticLinkerProfileFlags(t, guardedProfile)
				writeEnvironment := func(label, flags string) string {
					t.Helper()
					path := filepath.Join(external, name+"-"+label+".env")
					content := "GOFLAGS=" + flags + "\nGOPRIVATE=" + privateModules + "\n"
					if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
					return path
				}
				directEnvironment := writeEnvironment("direct", directFlags)
				guardedEnvironment := writeEnvironment("guarded", guardedFlags)
				t.Setenv("GOENV", directEnvironment)
				processFlags := ""
				if source == "process" {
					processFlags = directFlags
				}
				t.Setenv("GOFLAGS", processFlags)
				environment, err := diagnosticProcessEnvironment()
				if err != nil {
					t.Fatal(err)
				}
				// Deliberately bypass only the diagnostic GOFLAGS normalization
				// for this ordinary-Go control, retaining the shared external caches.
				ordinary := overrideProcessEnvironment(environment, []string{"GOFLAGS=" + processFlags})
				var output bytes.Buffer
				binary := filepath.Join(external, name+filepath.Ext(workflowBuildDestination()))
				if err := (execProcessRunner{}).RunWithEnvironment(ctx, nil, &output, &output, ordinary,
					"go", "build", "-mod=readonly", "-o", binary, "./cmd/diagnostics"); err != nil {
					t.Fatalf("ordinary Go profiling control failed: %v\n%s", err, &output)
				}
				info, err := os.Stat(directProfile)
				if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
					t.Fatalf("ordinary Go did not create a nonempty linker profile: info=%v error=%v", info, err)
				}
				if err := os.Remove(directProfile); err != nil {
					t.Fatalf("remove only the intentional control profile: %v", err)
				}
				if _, err := os.Stat(guardedProfile); !os.IsNotExist(err) {
					t.Fatalf("guarded profile exists before the diagnostic command: %v", err)
				}

				t.Setenv("GOENV", guardedEnvironment)
				if source == "process" {
					t.Setenv("GOFLAGS", guardedFlags)
				}
				before := diagnosticSnapshot(t, directory)
				environment, err = diagnosticProcessEnvironment()
				if err != nil {
					t.Fatal(err)
				}
				output.Reset()
				if err := (execProcessRunner{}).RunWithEnvironment(ctx, nil, &output, &output, environment,
					"go", "env", "-json", "GOFLAGS", "GOPRIVATE"); err != nil {
					t.Fatalf("inspect actual diagnostic child Go configuration: %v\n%s", err, &output)
				}
				var effective map[string]string
				if err := json.Unmarshal(output.Bytes(), &effective); err != nil {
					t.Fatalf("decode child Go configuration: %v\n%s", err, &output)
				}
				if effective["GOFLAGS"] != " " || effective["GOPRIVATE"] != privateModules {
					t.Fatalf("diagnostic child must suppress flags and preserve unrelated persisted settings: %v", effective)
				}
				output.Reset()
				err = RunContext(ctx, []string{command}, nil, &output, &output)
				if command == "doctor" {
					if err == nil || !strings.Contains(output.String(), "fail\tdatabase\t") ||
						!strings.Contains(output.String(), "skipped\tmigrations\t") {
						t.Fatalf("offline diagnostic result = %v\n%s", err, &output)
					}
					for _, id := range []string{"config.http", "application", "orm", "views", "assets"} {
						if !strings.Contains(output.String(), "pass\t"+id+"\t") {
							t.Errorf("independent check %s did not pass: %s", id, &output)
						}
					}
				} else if err != nil || !strings.Contains(output.String(), `"/diagnostic_issues"`) {
					t.Fatalf("actual route inventory = %v\n%s", err, &output)
				}
				if _, err := os.Stat(guardedProfile); !os.IsNotExist(err) {
					t.Errorf("diagnostic Go flags created an application profile: %v", err)
				}
				if !reflect.DeepEqual(before, diagnosticSnapshot(t, directory)) {
					t.Fatal("diagnostic commands mutated application files or directories through Go flags")
				}
				for path, flags := range map[string]string{directEnvironment: directFlags, guardedEnvironment: guardedFlags} {
					content, err := os.ReadFile(path)
					if err != nil || string(content) != "GOFLAGS="+flags+"\nGOPRIVATE="+privateModules+"\n" {
						t.Fatalf("persisted Go configuration changed: %v", err)
					}
				}
			})
		}
	}
}

func diagnosticLinkerProfileFlags(t *testing.T, path string) string {
	t.Helper()
	// GOFLAGS and -ldflags each parse one quoting layer without unescaping.
	// The explicit package pattern permits a quoted linker argument after '='.
	// Keep the fixture meaningful even when the application path has spaces.
	path = filepath.ToSlash(path)
	if !strings.Contains(path, "'") {
		return `"-ldflags=all='-cpuprofile=` + path + `'"`
	}
	if !strings.Contains(path, `"`) {
		return `'-ldflags=all="-cpuprofile=` + path + `"'`
	}
	t.Fatal("profile fixture path contains both unsupported Go flag quote characters")
	return ""
}
