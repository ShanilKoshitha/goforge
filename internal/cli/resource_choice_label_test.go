package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMakeResourceChoiceLabelsAreExplicitAndRelationshipScoped(t *testing.T) {
	_, _, relationships, _, err := parseMakeResourceArguments([]string{
		"Ticket", "--choice-label", "category:name", "--belongs-to", "category:Category",
		"--belongs-to", "project:Project", "--choice-label=project:title",
	})
	if err != nil || len(relationships) != 2 || relationships[0].ChoiceLabelName != "name" || relationships[1].ChoiceLabelName != "title" {
		t.Fatalf("explicit labels = %#v, %v", relationships, err)
	}
	for _, suffix := range [][]string{
		{"--choice-label"}, {"--choice-label="}, {"--choice-label", "category"},
		{"--choice-label", "category:title:other"}, {"--choice-label", "Category:title"},
		{"--choice-label", "category: Title"}, {"--choice-label", "other:title"},
		{"--choice-label", "category:title", "--choice-label", "category:name"},
	} {
		arguments := append([]string{"Ticket", "--belongs-to", "category:Category"}, suffix...)
		if _, _, _, _, err := parseMakeResourceArguments(arguments); err == nil {
			t.Errorf("accepted invalid label declaration: %q", arguments)
		}
	}
}

func TestResolveResourceChoiceLabelRequiresExplicitEditableText(t *testing.T) {
	model := modelDefinition{Name: "Category", Fields: []modelField{
		{Name: "Title", Column: "title", GoType: "string", DBType: "text", typeFamily: "string"},
		{Name: "Notes", Column: "notes", GoType: "*string", DBType: "varchar(255)", typeFamily: "string", Nullable: true},
		{Name: "ExternalID", Column: "external_id", GoType: "string", DBType: "uuid", typeFamily: "string"},
		{Name: "Secret", Column: "secret", GoType: "string", DBType: "text", typeFamily: "string", Protected: true},
		{Name: "Computed", Column: "computed", GoType: "string", DBType: "text", typeFamily: "string", Generated: true},
		{Name: "Count", Column: "count", GoType: "int64", DBType: "bigint", typeFamily: "integer"},
		{Name: "Named", Column: "named", GoType: "Title", DBType: "text", typeFamily: "string"},
		{Name: "NullString", Column: "null_string", GoType: "sql.NullString", DBType: "text", typeFamily: "string"},
	}}
	for _, name := range []string{"", "title", "notes"} {
		field, err := resolveResourceChoiceLabel(model, name)
		if err != nil || name != "" && field.Column != name || name == "" && field.Name != "" {
			t.Errorf("label %q = %#v, %v", name, field, err)
		}
	}
	for _, name := range []string{"missing", "secret", "computed", "count", "named", "null_string", "external_id"} {
		if _, err := resolveResourceChoiceLabel(model, name); err == nil {
			t.Errorf("accepted unsafe/unsupported label field %q", name)
		}
	}
}

func TestChoiceLabelResolutionIsReadOnlyAndRechecksTargetSource(t *testing.T) {
	fixture := newResourceRelationshipFixture(t, "Category", "Ticket")
	relation, err := parseRequiredBelongsTo("category:Category")
	if err != nil {
		t.Fatal(err)
	}
	relation.ChoiceLabelName = "name"
	before := snapshotRelationshipProject(t, fixture.directory)
	resolved, err := resolveResourceRelationships(fixture.child, fixture.state, []resourceBelongsTo{relation})
	if err != nil || len(resolved) != 1 || resolved[0].ChoiceLabelGoName != "Name" {
		t.Fatalf("resolved explicit name = %#v, %v", resolved, err)
	}
	assertRelationshipProjectUnchanged(t, fixture.directory, before)
	if err := recheckResourceRelationshipDependencies(resolved); err != nil {
		t.Fatal(err)
	}
	relation.ChoiceLabelName = "missing"
	if _, err := resolveResourceRelationships(fixture.child, fixture.state, []resourceBelongsTo{relation}); err == nil {
		t.Fatal("accepted missing target field")
	}
	assertRelationshipProjectUnchanged(t, fixture.directory, before)
	source, err := os.ReadFile(fixture.targetModel)
	if err != nil {
		t.Fatal(err)
	}
	// Change the selected database column without changing the Go field name.
	changed := bytes.Replace(source, []byte("db:\"name\""), []byte("db:\"renamed\""), 1)
	if bytes.Equal(changed, source) {
		t.Fatalf("fixture has no expected editable field tag: %s", source)
	}
	if err := os.WriteFile(fixture.targetModel, changed, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := recheckResourceRelationshipDependencies(resolved); err == nil || !strings.Contains(err.Error(), "choice-label target") {
		t.Fatalf("changed target recheck = %v", err)
	}
}

func TestChoiceLabelDeclarationRejectsOldFormatWithoutPublication(t *testing.T) {
	fixture := newResourceRelationshipFixture(t, "Category", "Ticket")
	setChoiceLabelFixtureFormat(t, "15")
	before := snapshotRelationshipProject(t, fixture.directory)
	var output bytes.Buffer
	err := Run([]string{"make:resource", "Ticket", "--belongs-to", "category:Category", "--choice-label", "category:name"}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "format 16") {
		t.Fatalf("old-format label error = %v", err)
	}
	assertRelationshipProjectUnchanged(t, fixture.directory, before)
	if _, err := os.Stat(filepath.Join("internal", "resources", "ticket")); !os.IsNotExist(err) {
		t.Fatalf("old-format resource unexpectedly published: %v", err)
	}
}

func TestFormatSixteenChoiceLabelCLIRejectsInvalidDeclarationsWithoutPublication(t *testing.T) {
	directory := newChoiceLabelCLIFixture(t)
	tests := []struct {
		name   string
		flags  []string
		wanted string
	}{
		{"unknown relationship", []string{"--choice-label", "other:title"}, "undeclared belongs-to relationship"},
		{"unknown target field", []string{"--choice-label", "category:missing"}, "has no field column"},
		{"wrong target field type", []string{"--choice-label", "category:rank"}, "requires an editable string"},
		{"duplicate disclosure", []string{"--choice-label", "category:title", "--choice-label", "category:title"}, "duplicate choice-label"},
		// private_label is real text on Project, never on Category. Resolution
		// must not borrow another relationship's field or presentation grant.
		{"cross-target field", []string{"--choice-label", "category:private_label"}, "has no field column"},
		{"qualified cross-target field", []string{"--choice-label", "category:Project.private_label"}, "lower_snake_case"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := snapshotRelationshipProject(t, directory)
			arguments := []string{"make:resource", "Ticket", "--field", "summary:string", "--belongs-to", "category:Category", "--belongs-to", "project:Project"}
			arguments = append(arguments, test.flags...)
			var output bytes.Buffer
			err := Run(arguments, &output, &output)
			if err == nil || !strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("invalid declaration returned %v, want %q\n%s", err, test.wanted, output.String())
			}
			// This includes every model, SQL file, managed ORM/view/registry
			// artifact, metadata file, module file, and their permissions.
			assertRelationshipProjectUnchanged(t, directory, before)
		})
	}
}

func TestFormatSixteenChoiceLabelCLIPreservesOneShotSourceAcrossRegeneration(t *testing.T) {
	directory := newChoiceLabelCLIFixture(t)
	runChoiceLabelCLI(t, "make:resource", "Ticket", "--field", "summary:string", "--belongs-to", "category:Category", "--belongs-to", "project:Project", "--choice-label", "category:title")
	before := snapshotRelationshipProject(t, directory)
	browse, exists := before["internal/resources/ticket/browse.go"]
	if !exists || !bytes.Contains(browse.Data, []byte("models.CategoryColumns.Title")) ||
		!bytes.Contains(browse.Data, []byte("ContainsFoldNullable")) || bytes.Contains(browse.Data, []byte("PrivateLabel")) {
		t.Fatalf("one-shot declaration was not emitted as explicit, isolated source:\n%s", browse.Data)
	}
	metadata := before[".forge/resources.json"].Data
	for _, forbidden := range []string{"choice_label", "ChoiceLabel", "category:title", "private_label", "relationships"} {
		if bytes.Contains(metadata, []byte(forbidden)) {
			t.Fatalf("one-shot choice schema was persisted in resource metadata as %q: %s", forbidden, metadata)
		}
	}

	runChoiceLabelCLI(t, "orm:generate")
	runChoiceLabelCLI(t, "orm:generate", "--check")
	runChoiceLabelCLI(t, "views:compile")
	runChoiceLabelCLI(t, "views:compile", "--check")
	// Registry generation has no standalone CLI command. Exercise its real
	// source-only renderer explicitly, then exercise publication through the
	// next actual resource command below.
	state, err := loadResourceState()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := generatedResourceRegistryForFormat("example.com/choiceproof", state, 16)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeManagedFile(filepath.Join("routes", "resources_gen.go"), []byte(registry)); err != nil {
		t.Fatal(err)
	}
	assertRelationshipProjectUnchanged(t, directory, before)

	runChoiceLabelCLI(t, "make:resource", "AuditEntry", "--field", "message:text")
	after := snapshotRelationshipProject(t, directory)
	managed := map[string]bool{
		".forge/resources.json":   true,
		"routes/resources_gen.go": true,
		generatedORMPath:          true,
		generatedViewsPath:        true,
	}
	for name, previous := range before {
		if managed[name] {
			continue
		}
		current, exists := after[name]
		if !exists || !bytes.Equal(current.Data, previous.Data) || current.Mode != previous.Mode {
			t.Errorf("later resource generation rewrote existing application-owned file %s", name)
		}
	}
	if bytes.Equal(after["routes/resources_gen.go"].Data, before["routes/resources_gen.go"].Data) ||
		!bytes.Contains(after["routes/resources_gen.go"].Data, []byte("auditentryController")) {
		t.Fatal("later resource did not actually regenerate and publish the registry")
	}
	runChoiceLabelCLI(t, "orm:generate", "--check")
	runChoiceLabelCLI(t, "views:compile", "--check")
	if current, err := os.ReadFile(filepath.Join(directory, "internal", "resources", "ticket", "browse.go")); err != nil || !bytes.Equal(current, browse.Data) {
		t.Fatalf("choice-safe application-owned source changed after final checks: %v", err)
	}
}

func newChoiceLabelCLIFixture(t *testing.T) string {
	t.Helper()
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "choice-label-app")
	if err := createProject(newOptions{directory: directory, module: "example.com/choiceproof", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	preserveFormat16WorkflowFixture(t, directory)
	assertFormat16WorkflowBoundaries(t, directory, root)
	runChoiceLabelCLI(t, "make:resource", "Category", "--field", "title:string:nullable", "--field", "rank:integer")
	runChoiceLabelCLI(t, "make:resource", "Project", "--field", "private_label:string")
	return directory
}

func setChoiceLabelFixtureFormat(t *testing.T, version string) {
	t.Helper()
	manifest, err := os.ReadFile("forge.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, tail, exists := strings.Cut(string(manifest), "\n")
	if !exists {
		t.Fatal("generated manifest has no version line")
	}
	if err := os.WriteFile("forge.yaml", []byte("version: "+version+"\n"+tail), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runChoiceLabelCLI(t *testing.T, arguments ...string) {
	t.Helper()
	var output bytes.Buffer
	if err := Run(arguments, &output, &output); err != nil {
		t.Fatalf("forge %s: %v\n%s", strings.Join(arguments, " "), err, output.String())
	}
}
