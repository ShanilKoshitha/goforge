package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

const format15WorkflowRuntime = "github.com/ShanilKoshitha/goforge"

var format15WorkflowRuntimeSums = []string{
	"github.com/ShanilKoshitha/goforge v0.21.0 h1:i2oJUQaIbA3nbJJJUV6ewMm2Mds8kLYgmIC+50dBNU0=",
	"github.com/ShanilKoshitha/goforge v0.21.0/go.mod h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=",
}

// Preserve the concrete format-15 boundaries used by historical request and
// relationship journeys when the current scaffold advances. This is a current-
// generator compatibility fixture, not provenance from an immutable old CLI.
// Keep ordinary local replacements and unrelated module dependencies intact.
func preserveFormat15WorkflowFixture(t *testing.T, directory, forgeBinary string, environment []string) {
	t.Helper()
	manifestPath := filepath.Join(directory, "forge.yaml")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	first, tail, found := strings.Cut(string(manifest), "\n")
	if !found || !strings.HasPrefix(first, "version: ") {
		t.Fatalf("generated fixture has no leading project version: %q", first)
	}
	writeFormat15WorkflowFile(t, manifestPath, []byte("version: 15\n"+tail))

	textareaPath := "resources/views/components/textarea.forge.html"
	textarea, err := renderTemplate("templates/scaffold/"+textareaPath+".tmpl", textareaPath, scaffoldData{ScalableResources: false})
	if err != nil {
		t.Fatal(err)
	}
	writeFormat15WorkflowFile(t, filepath.Join(directory, filepath.FromSlash(textareaPath)), []byte(textarea))

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
		if requirement.Mod.Path != format15WorkflowRuntime {
			continue
		}
		frameworkRequirements++
		if requirement.Mod.Version == "v0.21.0" {
			continue
		}
		// Patch only the generated version token. Formatting the whole module
		// file would gratuitously change its replacement quoting/whitespace.
		line := regexp.MustCompile(`(?m)^([\t ]*(?:require[\t ]+)?` + regexp.QuoteMeta(format15WorkflowRuntime) + `[\t ]+)` + regexp.QuoteMeta(requirement.Mod.Version) + `([\t ]*(?://[^\r\n]*)?\r?$)`)
		if len(line.FindAllIndex(moduleSource, -1)) != 1 {
			t.Fatal("generated fixture has no unique framework requirement to restore")
		}
		moduleSource = line.ReplaceAll(moduleSource, []byte("${1}v0.21.0${2}"))
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
		if len(fields) > 0 && fields[0] == format15WorkflowRuntime {
			if !insertedRuntimeSums {
				lines = append(lines, format15WorkflowRuntimeSums...)
				insertedRuntimeSums = true
			}
			continue
		}
		lines = append(lines, line)
	}
	if !insertedRuntimeSums {
		lines = append(append([]string(nil), format15WorkflowRuntimeSums...), lines...)
	}
	writeFormat15WorkflowFile(t, sumsPath, []byte(strings.Join(lines, "\n")+"\n"))
	if output, err := generatedCommand(directory, environment, forgeBinary, "views:compile"); err != nil {
		t.Fatalf("compile restored format-15 fixture views: %v\n%s", err, output)
	}
}

func writeFormat15WorkflowFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFormat15WorkflowFixtureRestoresExplicitFormatBoundaries(t *testing.T) {
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
	directory := filepath.Join(scratch, "format15")
	if err := createProject(newOptions{directory: directory, module: "example.com/format15proof", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	paths := []string{"forge.yaml", "go.mod", "go.sum", "resources/views/components/textarea.forge.html", generatedViewsPath}
	original := make(map[string][]byte)
	for _, path := range paths {
		contents, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Fatal(err)
		}
		original[path] = contents
	}
	baseline := make(map[string][]byte)
	// Establish an explicit old-format baseline even after the default advances.
	preserveFormat15WorkflowFixture(t, directory, binary, environment)
	if currentProjectFormat == 15 {
		for _, path := range paths {
			assertGeneratedArtifactUnchanged(t, directory, path, original[path])
		}
	}
	// The textarea must actually be referenced so its future source difference
	// also reaches the compiled view artifact rather than remaining unused.
	if output, err := generatedCommand(directory, environment, binary, "make:resource", "Note", "--field", "body:text"); err != nil {
		t.Fatalf("generate format-15 textarea consumer: %v\n%s", err, output)
	}
	for _, path := range paths {
		contents, err := os.ReadFile(filepath.FromSlash(path))
		if err != nil {
			t.Fatal(err)
		}
		baseline[path] = contents
	}
	assertFormat15WorkflowBoundaries(t, directory, root)
	// A second restoration is neutral, including the managed compiled artifact.
	preserveFormat15WorkflowFixture(t, directory, binary, environment)
	for _, path := range paths {
		assertGeneratedArtifactUnchanged(t, directory, path, baseline[path])
	}

	manifest := bytes.Replace(baseline["forge.yaml"], []byte("version: 15\n"), []byte("version: 16\n"), 1)
	writeFormat15WorkflowFile(t, "forge.yaml", manifest)
	textareaPath := "resources/views/components/textarea.forge.html"
	futureTextarea, err := renderTemplate("templates/scaffold/"+textareaPath+".tmpl", textareaPath, scaffoldData{ScalableResources: true})
	if err != nil {
		t.Fatal(err)
	}
	if futureTextarea == string(baseline[textareaPath]) {
		t.Fatal("future textarea fixture does not exercise its synthetic newline difference")
	}
	writeFormat15WorkflowFile(t, filepath.FromSlash(textareaPath), []byte(futureTextarea))
	futureViews, err := compileViewsWithPlanned(nil)
	if err != nil {
		t.Fatal(err)
	}
	if futureViews == string(baseline[generatedViewsPath]) {
		t.Fatal("future textarea did not change the managed view artifact")
	}
	writeFormat15WorkflowFile(t, filepath.FromSlash(generatedViewsPath), []byte(futureViews))
	futureModule := bytes.Replace(baseline["go.mod"], []byte(format15WorkflowRuntime+" v0.21.0"), []byte(format15WorkflowRuntime+" v0.99.0"), 1)
	if bytes.Equal(futureModule, baseline["go.mod"]) {
		t.Fatal("future runtime pin fixture did not replace the explicit v0.21.0 requirement")
	}
	writeFormat15WorkflowFile(t, "go.mod", futureModule)
	writeFormat15WorkflowFile(t, "go.sum", append(append([]byte(nil), baseline["go.sum"]...), []byte(format15WorkflowRuntime+" v0.99.0 h1:"+strings.Repeat("A", 43)+"=\n")...))

	preserveFormat15WorkflowFixture(t, directory, binary, environment)
	assertFormat15WorkflowBoundaries(t, directory, root)
	for _, path := range paths {
		assertGeneratedArtifactUnchanged(t, directory, path, baseline[path])
	}
	if !bytes.Contains(baseline["go.mod"], []byte("replace "+format15WorkflowRuntime+" => ")) {
		t.Fatal("restoration lost the local runtime replacement")
	}
	for _, command := range [][]string{{"views:compile", "--check"}, {"orm:generate", "--check"}, {"assets:check"}} {
		if output, err := generatedCommand(directory, environment, binary, command...); err != nil {
			t.Fatalf("restored format-15 fixture %s: %v\n%s", strings.Join(command, " "), err, output)
		}
	}
}

func assertFormat15WorkflowBoundaries(t *testing.T, directory, runtimeRoot string) {
	t.Helper()
	manifest, err := os.ReadFile(filepath.Join(directory, "forge.yaml"))
	if err != nil || !bytes.HasPrefix(manifest, []byte("version: 15\n")) {
		t.Fatalf("fixture must explicitly retain format 15: %v\n%s", err, manifest)
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
		if requirement.Mod.Path == "github.com/ShanilKoshitha/goforge" {
			requirements++
			if requirement.Mod.Version != "v0.21.0" || requirement.Indirect {
				t.Fatalf("fixture must directly require public v0.21.0: %+v", requirement)
			}
		}
	}
	if requirements != 1 {
		t.Fatalf("fixture must have exactly one framework requirement, got %d", requirements)
	}
	replaced := false
	for _, replacement := range module.Replace {
		if replacement.Old.Path == "github.com/ShanilKoshitha/goforge" && replacement.Old.Version == "" &&
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
		if len(fields) > 0 && fields[0] == "github.com/ShanilKoshitha/goforge" {
			actual = append(actual, line)
		}
	}
	const expected = "github.com/ShanilKoshitha/goforge v0.21.0 h1:i2oJUQaIbA3nbJJJUV6ewMm2Mds8kLYgmIC+50dBNU0=\n" +
		"github.com/ShanilKoshitha/goforge v0.21.0/go.mod h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM="
	if len(actual) != 2 || strings.Join(actual, "\n") != expected {
		t.Fatalf("fixture must retain only the two exact public v0.21.0 sums: %q", actual)
	}
	textarea, err := os.ReadFile(filepath.Join(directory, "resources", "views", "components", "textarea.forge.html"))
	if err != nil || !bytes.Contains(textarea, []byte(`@endif>{{$form.Old $name $fallback}}</textarea>`)) {
		t.Fatalf("fixture must retain the legacy textarea boundary without a synthetic LF: %v\n%s", err, textarea)
	}
}
