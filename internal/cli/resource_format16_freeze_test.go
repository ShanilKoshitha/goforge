package cli

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Capture authenticates the complete production generation dependency set
// against the immutable released source before printing digests. It never
// overwrites a golden. These reviewed digests were captured before adoption.
const releasedFormat16Source = "d71a93af85d212956fcc5f08bd17f2796f914a50"

var releasedFormat16ResourceDigests = map[string]string{
	"legacy":                 "652b2aca363f1e5e5a51bb79d8cc41e4a2b82a5d82ee092bbe55feae3506ab89",
	"typed":                  "38db02b9791c0c0e587bcfca7808114460c9ce8b359f395980077ff0f91a7c9d",
	"boolean-only":           "2f4db1c1b817002e28b960cb2c8076e4df2ac4607458a9641254e3ad605f5727",
	"nullable-text-only":     "07aa6e8dc6b9a685d880decf049f923d0a0ae6d4feaa4eba6ad4670211c86e0f",
	"related-id-only":        "10a9a3949bffac66e87aec8f53ee5a5302850308e2c34f1047aeba87bbc164a1",
	"related-required-label": "66d2af6b9a40a64a782b01440992f9c6a58ef427e0ff0780fccef899ae054524",
	"related-nullable-label": "58715ab5d28e407c35c303ddf77e6b401f73247f87acbffeaf3042af047d9335",
}

func TestResourceFormat16RetainsReleasedCompleteOutput(t *testing.T) {
	capture := os.Getenv("GOFORGE_CAPTURE_FORMAT16")
	if capture != "" {
		if capture != releasedFormat16Source {
			t.Fatal("format-16 capture requires the exact immutable released source")
		}
		dependencies := []string{
			"internal/cli/resource_templates.go",
			"internal/cli/resource_fields.go",
			"internal/cli/resource_relationships.go",
			"internal/cli/resource_state.go",
			"internal/cli/resource.go",
			"internal/cli/model_schema.go",
			"internal/cli/names.go",
			"internal/cli/templates.go",
			"internal/cli/templates/resource",
		}
		command := exec.Command("git", append([]string{"diff", "--exit-code", capture, "--"}, dependencies...)...)
		command.Dir = projectRoot(t)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("capture renderer/projector/templates differ from immutable v0.22.1: %v\n%s", err, output)
		}
	}
	for _, profile := range []string{"legacy", "typed", "boolean-only", "nullable-text-only", "related-id-only", "related-required-label", "related-nullable-label"} {
		t.Run(profile, func(t *testing.T) {
			definition := format16FreezeDefinition(t, profile)
			files, err := resourceFilesForFormat("example.com/format16snapshot", definition, 16)
			if err != nil {
				t.Fatal(err)
			}
			resources := []resourceSpec{definition.resourceSpec}
			for _, relationship := range definition.Relationships {
				resources = append(resources, relationship.TargetSpec)
			}
			sort.Slice(resources, func(left, right int) bool { return resources[left].Name < resources[right].Name })
			registry, err := generatedResourceRegistryForFormat("example.com/format16snapshot", resourceState{Resources: resources}, 16)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, plannedFile{path: "routes/resources_gen.go", content: registry})
			byPath := make(map[string][]byte, len(files))
			for _, file := range files {
				path := filepath.ToSlash(file.path)
				if _, exists := byPath[path]; exists {
					t.Fatalf("duplicate generated output %s", path)
				}
				// renderTemplate already formats Go exactly as publication does.
				// Normalize only checkout line endings in the final emitted bytes.
				byPath[path] = bytes.ReplaceAll([]byte(file.content), []byte("\r\n"), []byte("\n"))
			}
			if len(byPath) != 21 {
				t.Fatalf("format-16 output count=%d, want 21 including browse, down SQL, and registry", len(byPath))
			}
			for _, path := range []string{
				"internal/models/ticket.go",
				"internal/resources/ticket/browse.go",
				"internal/resources/ticket/browse_test.go",
				"internal/resources/ticket/authorization.go",
				"internal/resources/ticket/authorization_test.go",
				"database/migrations/20261021000000_create_tickets.up.sql",
				"database/migrations/20261021000000_create_tickets.down.sql",
				"resources/views/pages/tickets/form.forge.html",
				"routes/resources_gen.go",
			} {
				if len(byPath[path]) == 0 {
					t.Fatalf("required frozen artifact %s is missing or empty", path)
				}
			}
			browse := string(byPath["internal/resources/ticket/browse.go"])
			if !strings.Contains(browse, "func ParseListOptions") {
				t.Fatal("fixture did not reach real format-16 browsing")
			}
			switch profile {
			case "related-required-label":
				if !strings.Contains(browse, "orm.ContainsFold(models.CategoryColumns.Title, options.Search)") {
					t.Fatal("required explicit choice-label search branch was not exercised")
				}
			case "related-nullable-label":
				if !strings.Contains(browse, "orm.ContainsFoldNullable(models.CategoryColumns.Title, options.Search)") {
					t.Fatal("nullable explicit choice-label search branch was not exercised")
				}
			case "related-id-only":
				if strings.Contains(browse, "models.CategoryColumns.Title") {
					t.Fatal("ID-only fixture unexpectedly discloses target text")
				}
			}
			paths := make([]string, 0, len(byPath))
			for path := range byPath {
				paths = append(paths, path)
			}
			sort.Strings(paths)
			digest := sha256.New()
			for _, path := range paths {
				fmt.Fprintf(digest, "%s\x00%s\x00", path, byPath[path])
			}
			actual := fmt.Sprintf("%x", digest.Sum(nil))
			if capture != "" {
				t.Logf("%q: %q,", profile, actual)
				return
			}
			wanted, exists := releasedFormat16ResourceDigests[profile]
			if !exists || wanted == "" {
				t.Fatalf("missing reviewed immutable format-16 baseline for %s", profile)
			}
			if actual != wanted {
				t.Errorf("released format-16 output changed for %s: got %s, want %s", profile, actual, wanted)
			}
		})
	}
}

// Feed the real production definition projection, including all scalar
// families, explicit nullable text labels, a second independently ID-only
// relationship, owner constraints, source models, and registry generation.
func format16FreezeDefinition(t *testing.T, profile string) resourceDefinition {
	t.Helper()
	fields := defaultResourceFields()
	schemaDriven := profile != "legacy"
	if schemaDriven {
		specifications := []string{"title:string", "notes:text:nullable", "priority:integer", "estimate:integer:nullable", "active:boolean", "featured:boolean:nullable"}
		if profile == "boolean-only" {
			specifications = []string{"active:boolean"}
		} else if profile == "nullable-text-only" {
			specifications = []string{"title:string:nullable"}
		}
		var err error
		fields, err = parseResourceFields(specifications)
		if err != nil {
			t.Fatal(err)
		}
	}
	definition := resourceDefinition{
		resourceSpec: resourceSpec{Name: "Ticket", Package: "ticket", Plural: "tickets", MigrationVersion: "20261021000000"},
		Fields:       fields, SchemaDriven: schemaDriven,
	}
	if strings.HasPrefix(profile, "related-") {
		definition.Relationships = []resourceRelationship{
			{
				resourceBelongsTo:   resourceBelongsTo{Name: "category", GoName: "Category", Label: "Category", ForeignKey: "category_id", ForeignKeyGoName: "CategoryID", Target: "Category", Required: true},
				TargetSpec:          resourceSpec{Name: "Category", Package: "category", Plural: "categories", MigrationVersion: "20261020000000"},
				TargetQueryAccessor: "Categories", IndexName: "tickets_category_id_idx", ConstraintName: "tickets_category_owner_fkey",
			},
			{
				resourceBelongsTo:   resourceBelongsTo{Name: "project", GoName: "Project", Label: "Project", ForeignKey: "project_id", ForeignKeyGoName: "ProjectID", Target: "Project", Required: true},
				TargetSpec:          resourceSpec{Name: "Project", Package: "project", Plural: "projects", MigrationVersion: "20261020000001"},
				TargetQueryAccessor: "Projects", IndexName: "tickets_project_id_idx", ConstraintName: "tickets_project_owner_fkey",
			},
		}
		if profile == "related-required-label" || profile == "related-nullable-label" {
			definition.Relationships[0].ChoiceLabelName = "title"
			definition.Relationships[0].ChoiceLabelGoName = "Title"
			definition.Relationships[0].ChoiceLabelNullable = profile == "related-nullable-label"
		}
	}
	return definition
}
