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
