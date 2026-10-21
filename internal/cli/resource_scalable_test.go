package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise future-format source independently of the default scaffold format.
// This lets runtime-first implementation qualify without publishing a scaffold
// that depends on an unavailable runtime module.
func TestFormatSixteenResourceTemplatesCompileAndTest(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "scalableboard")
	root := projectRoot(t)
	const module = "example.com/scalableboard"
	if err := createProject(newOptions{directory: directory, module: module, replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	textareaPath := "resources/views/components/textarea.forge.html"
	textarea, err := renderTemplate("templates/scaffold/"+textareaPath+".tmpl", textareaPath, scaffoldData{ScalableResources: true})
	if err != nil {
		t.Fatal(err)
	}
	writeScalableFixture(t, textareaPath, textarea)
	state := resourceState{}
	for _, arguments := range [][]string{
		{"Category", "--field", "title:string:nullable"},
		{"Project", "--field", "active:boolean"},
		{"Ticket", "--field", "title:string", "--field", "notes:text:nullable", "--field", "priority:integer",
			"--field", "estimate:integer:nullable", "--field", "active:boolean", "--field", "featured:boolean:nullable",
			"--belongs-to", "category:Category", "--choice-label", "category:title", "--belongs-to", "project:Project"},
		{"LegacyIssue"},
	} {
		name, fields, requested, schemaDriven, err := parseMakeResourceArguments(arguments)
		if err != nil {
			t.Fatal(err)
		}
		spec, err := newResourceSpec(name, state)
		if err != nil {
			t.Fatal(err)
		}
		relationships, err := resolveResourceRelationships(spec, state, requested)
		if err != nil {
			t.Fatal(err)
		}
		files, err := resourceFilesForFormat(module, resourceDefinition{
			resourceSpec: spec, Fields: fields, Relationships: relationships, SchemaDriven: schemaDriven,
		}, 16)
		if err != nil {
			t.Fatalf("format16 %s generation: %v", name, err)
		}
		for _, file := range files {
			writeScalableFixture(t, file.path, file.content)
		}
		state.Resources = append(state.Resources, spec)
	}
	registry, err := generatedResourceRegistryForFormat(module, state, 16)
	if err != nil {
		t.Fatal(err)
	}
	writeScalableFixture(t, "routes/resources_gen.go", registry)
	sources, err := currentModelSourcesWithPlanned(nil)
	if err != nil {
		t.Fatal(err)
	}
	ormSource, err := renderORMArtifactForModelSources(sources)
	if err != nil {
		t.Fatal(err)
	}
	writeScalableFixture(t, generatedORMPath, ormSource)
	views, err := compileViewsWithPlanned(nil)
	if err != nil {
		t.Fatal(err)
	}
	writeScalableFixture(t, generatedViewsPath, views)
	for _, fragment := range []string{"type ListOptions struct", "func ParseListOptions", "func (repository *PostgresRepository) Browse", "ContainsFoldNullable"} {
		found := false
		for _, name := range []string{"browse.go", "repository.go"} {
			contents, err := os.ReadFile(filepath.Join("internal", "resources", "ticket", name))
			if err != nil {
				t.Fatal(err)
			}
			found = found || strings.Contains(string(contents), fragment)
		}
		if !found {
			t.Errorf("explicit format16 source omits %q", fragment)
		}
	}
	if output, err := generatedCommand(directory, append(os.Environ(), "GOWORK=off"), "go", "test", "./...", "-count=1"); err != nil {
		t.Fatalf("explicit format16 application tests: %v\n%s", err, output)
	}
	proof := strings.ReplaceAll(scalableResourcesAcceptanceProgram, "example.com/scalable/", module+"/")
	writeScalableFixture(t, ".forge/scalable_repository_proof.go", proof)
	if output, err := generatedCommand(directory, append(os.Environ(), "GOWORK=off"), "go", "build", "-o", ".forge/scalable-repository-proof", "./.forge/scalable_repository_proof.go"); err != nil {
		t.Fatalf("real-PostgreSQL acceptance proof does not compile: %v\n%s", err, output)
	}
}

func writeScalableFixture(t *testing.T, name, contents string) {
	t.Helper()
	name = filepath.FromSlash(name)
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFormatSixteenRejectsNewDeclarationAndFormControlCollisions(t *testing.T) {
	for name := range format16ResourceDeclarations {
		definition := resourceDefinition{resourceSpec: resourceSpec{Name: name, Package: "resource", Plural: "resources"}, Fields: defaultResourceFields()}
		if _, err := resourceFilesForFormat("example.com/collisions", definition, 16); err == nil || !strings.Contains(err.Error(), "format-16 browsing declaration") {
			t.Errorf("new declaration %s: %v", name, err)
		}
		if _, err := resourceFilesForFormat("example.com/collisions", definition, 15); err != nil {
			t.Errorf("old format newly rejected %s: %v", name, err)
		}
	}
	for _, name := range []string{"intent", "validation_attempted"} {
		fields, err := parseResourceFields([]string{name + ":string"})
		if err != nil {
			t.Fatal(err)
		}
		definition := resourceDefinition{resourceSpec: resourceSpec{Name: "Ticket", Package: "ticket", Plural: "tickets"}, Fields: fields, SchemaDriven: true}
		if _, err := resourceFilesForFormat("example.com/collisions", definition, 16); err == nil || !strings.Contains(err.Error(), "format-16 form control") {
			t.Errorf("new form control %s: %v", name, err)
		}
		if _, err := resourceFilesForFormat("example.com/collisions", definition, 15); err != nil {
			t.Errorf("old format newly rejected field %s: %v", name, err)
		}
	}
}
