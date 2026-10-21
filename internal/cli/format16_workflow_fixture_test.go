package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

const format16WorkflowRuntime = "github.com/ShanilKoshitha/goforge"

var format16WorkflowRuntimeSums = []string{
	"github.com/ShanilKoshitha/goforge v0.22.0 h1:NgjmTT++ejfa8dOkFmcvFc5SRT7FTuutR38d8JfnLk0=",
	"github.com/ShanilKoshitha/goforge v0.22.0/go.mod h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=",
}

// This restores a disposable current-generator fixture to explicit released
// boundaries. It is not an application downgrade command or evidence of public
// old-runtime provenance; the separate immutable-CLI CI journey proves that.
// Callers own the fixture and have selected it with t.Chdir before restoring.
func preserveFormat16WorkflowFixture(t *testing.T, directory string) {
	t.Helper()
	removeDiagnosticWorkflowFixtureFiles(t, directory)
	manifestPath := filepath.Join(directory, "forge.yaml")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	first, tail, found := strings.Cut(string(manifest), "\n")
	if !found || !strings.HasPrefix(first, "version: ") {
		t.Fatalf("generated fixture has no leading project version: %q", first)
	}
	writeFormat15WorkflowFile(t, manifestPath, []byte("version: 16\n"+tail))

	modulePath := filepath.Join(directory, "go.mod")
	moduleSource, err := os.ReadFile(modulePath)
	if err != nil {
		t.Fatal(err)
	}
	module, err := modfile.Parse(modulePath, moduleSource, nil)
	if err != nil {
		t.Fatal(err)
	}
	frameworkRequirements := 0
	for _, requirement := range module.Require {
		if requirement.Mod.Path != format16WorkflowRuntime {
			continue
		}
		frameworkRequirements++
		if requirement.Mod.Version == "v0.22.0" {
			continue
		}
		// Preserve unrelated dependency text and local-replacement quoting.
		line := regexp.MustCompile(`(?m)^([\t ]*(?:require[\t ]+)?` + regexp.QuoteMeta(format16WorkflowRuntime) + `[\t ]+)` + regexp.QuoteMeta(requirement.Mod.Version) + `([\t ]*(?://[^\r\n]*)?\r?$)`)
		if len(line.FindAllIndex(moduleSource, -1)) != 1 {
			t.Fatal("generated fixture has no unique framework requirement to restore")
		}
		moduleSource = line.ReplaceAll(moduleSource, []byte("${1}v0.22.0${2}"))
	}
	if frameworkRequirements != 1 {
		t.Fatalf("generated fixture has %d framework requirements, want one", frameworkRequirements)
	}
	writeFormat15WorkflowFile(t, modulePath, moduleSource)

	sumsPath := filepath.Join(directory, "go.sum")
	sums, err := os.ReadFile(sumsPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := make([]string, 0)
	insertedRuntimeSums := false
	for _, line := range strings.Split(strings.TrimSuffix(string(sums), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == format16WorkflowRuntime {
			if !insertedRuntimeSums {
				lines = append(lines, format16WorkflowRuntimeSums...)
				insertedRuntimeSums = true
			}
			continue
		}
		lines = append(lines, line)
	}
	if !insertedRuntimeSums {
		lines = append(append([]string(nil), format16WorkflowRuntimeSums...), lines...)
	}
	writeFormat15WorkflowFile(t, sumsPath, []byte(strings.Join(lines, "\n")+"\n"))

	textareaPath := "resources/views/components/textarea.forge.html"
	textarea, err := renderTemplate("templates/scaffold/"+textareaPath+".tmpl", textareaPath, scaffoldData{ScalableResources: true})
	if err != nil {
		t.Fatal(err)
	}
	writeFormat15WorkflowFile(t, filepath.Join(directory, filepath.FromSlash(textareaPath)), []byte(textarea))
	views, err := compileViewsWithPlanned(nil)
	if err != nil {
		t.Fatal(err)
	}
	writeFormat15WorkflowFile(t, filepath.Join(directory, filepath.FromSlash(generatedViewsPath)), []byte(views))
}

var diagnosticWorkflowFixtureDirectories = []string{"cmd/diagnostics", "internal/diagnostics"}

// Only the four known new scaffold files may be removed, and only from test-
// owned historical fixtures. Reject unexpected files or links before deleting
// anything; never recursively remove a directory that could contain user code.
func removeDiagnosticWorkflowFixtureFiles(t *testing.T, directory string) {
	t.Helper()
	if err := removeKnownDiagnosticWorkflowFixtureFiles(directory); err != nil {
		t.Fatal(err)
	}
}

func removeKnownDiagnosticWorkflowFixtureFiles(directory string) error {
	var files, directories []string
	for _, relative := range diagnosticWorkflowFixtureDirectories {
		path := filepath.Join(directory, filepath.FromSlash(relative))
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("historical fixture diagnostic path is not an ordinary directory: %s", path)
		}
		allowed := map[string]bool{"main.go": true, "main_test.go": true}
		if relative == "internal/diagnostics" {
			allowed = map[string]bool{"diagnostics.go": true, "diagnostics_test.go": true}
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !allowed[entry.Name()] || !info.Mode().IsRegular() {
				return fmt.Errorf("refusing to remove unexpected historical fixture diagnostic entry: %s", filepath.Join(path, entry.Name()))
			}
			files = append(files, filepath.Join(path, entry.Name()))
		}
		directories = append(directories, path)
	}
	for _, path := range append(files, directories...) {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	return nil
}

func assertNoDiagnosticWorkflowFixtureFiles(t *testing.T, directory string) {
	t.Helper()
	for _, relative := range diagnosticWorkflowFixtureDirectories {
		if _, err := os.Lstat(filepath.Join(directory, filepath.FromSlash(relative))); !os.IsNotExist(err) {
			t.Fatalf("historical fixture gained diagnostic source %s: %v", relative, err)
		}
	}
}

func assertFormat16WorkflowBoundaries(t *testing.T, directory, runtimeRoot string) {
	t.Helper()
	assertNoDiagnosticWorkflowFixtureFiles(t, directory)
	manifest, err := os.ReadFile(filepath.Join(directory, "forge.yaml"))
	if err != nil || !bytes.HasPrefix(manifest, []byte("version: 16\n")) {
		t.Fatalf("fixture must explicitly retain format 16: %v\n%s", err, manifest)
	}
	modulePath := filepath.Join(directory, "go.mod")
	moduleSource, err := os.ReadFile(modulePath)
	if err != nil {
		t.Fatal(err)
	}
	module, err := modfile.Parse(modulePath, moduleSource, nil)
	if err != nil {
		t.Fatal(err)
	}
	requirements := 0
	for _, requirement := range module.Require {
		if requirement.Mod.Path == format16WorkflowRuntime {
			requirements++
			if requirement.Mod.Version != "v0.22.0" || requirement.Indirect {
				t.Fatalf("fixture must directly require public v0.22.0: %+v", requirement)
			}
		}
	}
	if requirements != 1 {
		t.Fatalf("fixture must have exactly one framework requirement, got %d", requirements)
	}
	replaced := false
	for _, replacement := range module.Replace {
		if replacement.Old.Path == format16WorkflowRuntime && replacement.Old.Version == "" &&
			replacement.New.Version == "" && filepath.Clean(replacement.New.Path) == filepath.Clean(runtimeRoot) {
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("fixture did not preserve its exact local runtime replacement")
	}
	sums, err := os.ReadFile(filepath.Join(directory, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	var actual []string
	for _, line := range strings.Split(strings.TrimSuffix(string(sums), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == format16WorkflowRuntime {
			actual = append(actual, line)
		}
	}
	if len(actual) != 2 || strings.Join(actual, "\n") != strings.Join(format16WorkflowRuntimeSums, "\n") {
		t.Fatalf("fixture must retain only the two exact public v0.22.0 sums: %q", actual)
	}
	textarea, err := os.ReadFile(filepath.Join(directory, "resources", "views", "components", "textarea.forge.html"))
	textarea = bytes.ReplaceAll(textarea, []byte("\r\n"), []byte("\n"))
	if err != nil || !bytes.Contains(textarea, []byte("@endif>\n{{$form.Old $name $fallback}}</textarea>")) {
		t.Fatalf("fixture lost the format-16 synthetic leading textarea newline: %v\n%s", err, textarea)
	}
}

func TestFormat16WorkflowFixtureRestoresExplicitFormatBoundaries(t *testing.T) {
	root := projectRoot(t)
	scratch := t.TempDir()
	binary := filepath.Join(scratch, "forge")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	environment := append(os.Environ(), "GOWORK=off")
	if output, err := generatedCommand(root, environment, "go", "build", "-o", binary, "./cmd/forge"); err != nil {
		t.Fatalf("build current fixture CLI: %v\n%s", err, output)
	}
	directory := filepath.Join(scratch, "format16")
	const module = "example.com/format16proof"
	if err := createProject(newOptions{directory: directory, module: module, replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	original := snapshotRelationshipProject(t, directory)
	preserveFormat16WorkflowFixture(t, directory)
	assertFormat16WorkflowBoundaries(t, directory, root)
	if currentProjectFormat == 16 {
		assertRelationshipProjectUnchanged(t, directory, original)
	} else {
		after := snapshotRelationshipProject(t, directory)
		allowed := map[string]bool{"forge.yaml": true, "go.mod": true, "go.sum": true}
		for _, path := range []string{"cmd/diagnostics/main.go", "cmd/diagnostics/main_test.go", "internal/diagnostics/diagnostics.go", "internal/diagnostics/diagnostics_test.go"} {
			allowed[path] = true
		}
		for path, before := range original {
			if allowed[path] {
				continue
			}
			current, exists := after[path]
			if !exists || !bytes.Equal(current.Data, before.Data) || current.Mode != before.Mode {
				t.Errorf("historical restoration changed unrelated source %s", path)
			}
		}
		for path := range after {
			if _, exists := original[path]; !exists {
				t.Errorf("historical restoration created unexpected source %s", path)
			}
		}
	}
	if output, err := generatedCommand(directory, environment, binary, "make:resource", "Note", "--field", "body:text"); err != nil {
		t.Fatalf("generate format-16 textarea consumer: %v\n%s", err, output)
	}
	baseline := snapshotRelationshipProject(t, directory)
	preserveFormat16WorkflowFixture(t, directory)
	assertRelationshipProjectUnchanged(t, directory, baseline)

	manifest := bytes.Replace(baseline["forge.yaml"].Data, []byte("version: 16\n"), []byte("version: 17\n"), 1)
	writeFormat15WorkflowFile(t, "forge.yaml", manifest)
	futureModule := bytes.Replace(baseline["go.mod"].Data, []byte(format16WorkflowRuntime+" v0.22.0"), []byte(format16WorkflowRuntime+" v0.99.0"), 1)
	if bytes.Equal(futureModule, baseline["go.mod"].Data) {
		t.Fatal("future-runtime perturbation did not change the old pin")
	}
	writeFormat15WorkflowFile(t, "go.mod", futureModule)
	writeFormat15WorkflowFile(t, "go.sum", append(append([]byte(nil), baseline["go.sum"].Data...), []byte(format16WorkflowRuntime+" v0.99.0 h1:"+strings.Repeat("A", 43)+"=\n")...))
	diagnostics, err := diagnosticScaffoldFiles(module)
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range diagnostics {
		path := filepath.FromSlash(name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFormat15WorkflowFile(t, path, []byte(source))
	}
	// Change both authoring and compiled textarea behavior, not just its marker.
	textareaPath := "resources/views/components/textarea.forge.html"
	legacyTextarea, err := renderTemplate("templates/scaffold/"+textareaPath+".tmpl", textareaPath, scaffoldData{ScalableResources: false})
	if err != nil {
		t.Fatal(err)
	}
	if legacyTextarea == string(baseline[textareaPath].Data) {
		t.Fatal("textarea perturbation did not remove the synthetic newline")
	}
	writeFormat15WorkflowFile(t, filepath.FromSlash(textareaPath), []byte(legacyTextarea))
	legacyViews, err := compileViewsWithPlanned(nil)
	if err != nil {
		t.Fatal(err)
	}
	if legacyViews == string(baseline[generatedViewsPath].Data) {
		t.Fatal("textarea perturbation did not change the compiled consumer")
	}
	writeFormat15WorkflowFile(t, filepath.FromSlash(generatedViewsPath), []byte(legacyViews))
	preserveFormat16WorkflowFixture(t, directory)
	assertFormat16WorkflowBoundaries(t, directory, root)
	assertRelationshipProjectUnchanged(t, directory, baseline)

	for _, command := range [][]string{{"views:compile", "--check"}, {"orm:generate", "--check"}, {"assets:check"}} {
		if output, err := generatedCommand(directory, environment, binary, command...); err != nil {
			t.Fatalf("restored format-16 fixture %s: %v\n%s", strings.Join(command, " "), err, output)
		}
	}
	for _, command := range []string{"doctor", "routes:list"} {
		var output bytes.Buffer
		if err := Run([]string{command}, &output, &output); err == nil {
			t.Fatalf("historical format-16 fixture silently accepted %s", command)
		}
	}
	assertRelationshipProjectUnchanged(t, directory, baseline)
	if output, err := generatedCommand(directory, environment, "go", "test", "./...", "-count=1"); err != nil {
		t.Fatalf("restored format-16 ordinary application tests: %v\n%s", err, output)
	}
}

func TestHistoricalWorkflowDiagnosticRemovalRefusesUnknownSource(t *testing.T) {
	directory := t.TempDir()
	for _, relative := range diagnosticWorkflowFixtureDirectories {
		if err := os.MkdirAll(filepath.Join(directory, filepath.FromSlash(relative)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFormat15WorkflowFile(t, filepath.Join(directory, "cmd", "diagnostics", "main.go"), []byte("package main\n"))
	writeFormat15WorkflowFile(t, filepath.Join(directory, "internal", "diagnostics", "custom.go"), []byte("package diagnostics\n"))
	before := snapshotRelationshipProject(t, directory)
	if err := removeKnownDiagnosticWorkflowFixtureFiles(directory); err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("unexpected custom fixture source was not protected: %v", err)
	}
	assertRelationshipProjectUnchanged(t, directory, before)
}
