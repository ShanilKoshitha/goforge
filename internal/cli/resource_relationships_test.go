package cli

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestResolveResourceRelationshipsResolvesExistingGeneratedTarget(t *testing.T) {
	fixture := newResourceRelationshipFixture(t, "APIClient", "AuditEntry")
	relation, err := parseRequiredBelongsTo("reviewer:api-client")
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := resolveResourceRelationships(fixture.child, fixture.state, []resourceBelongsTo{relation})
	if err != nil {
		t.Fatal(err)
	}
	want := []resourceRelationship{{
		resourceBelongsTo:   relation,
		TargetSpec:          fixture.target,
		TargetQueryAccessor: "APIClients",
		IndexName:           "audit_entries_reviewer_id_idx",
		ConstraintName:      "audit_entries_reviewer_owner_fkey",
	}}
	if !reflect.DeepEqual(resolved, want) {
		t.Fatalf("resolved relationships = %#v, want %#v", resolved, want)
	}
}

func TestResolveResourceRelationshipsRejectsMissingTargetWithoutWrites(t *testing.T) {
	fixture := newResourceRelationshipFixture(t, "Project", "Issue")
	relation, err := parseRequiredBelongsTo("category:Category")
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotRelationshipProject(t, fixture.directory)

	_, err = resolveResourceRelationships(fixture.child, fixture.state, []resourceBelongsTo{relation})
	if err == nil || !strings.Contains(err.Error(), `targets Category, which is not an existing generated resource`) {
		t.Fatalf("error = %v", err)
	}
	assertRelationshipProjectUnchanged(t, fixture.directory, before)
}

func TestResolveResourceRelationshipsRejectsIncompatibleEditedTargetWithoutWrites(t *testing.T) {
	tests := []struct {
		name   string
		edit   func(string) string
		wanted string
	}{
		{
			name: "ID",
			edit: replaceRelationshipSource(
				`forge:"primary,generated,protected,required"`,
				`forge:"primary,protected,required"`,
			),
			wanted: "model must retain its generated required int64 ID",
		},
		{
			name: "UserID",
			edit: replaceRelationshipSource(
				`forge:"protected,required,index,references=User.ID,on_delete=cascade"`,
				`forge:"required,index,references=User.ID,on_delete=cascade"`,
			),
			wanted: "model must retain its protected required int64 UserID reference",
		},
		{
			name: "Owner",
			edit: replaceRelationshipSource(
				`forge:"belongs_to,target=User,foreign_key=UserID,references=ID"`,
				`forge:"ignore"`,
			),
			wanted: "model must retain its Owner belongs-to relationship",
		},
		{
			name: "table",
			edit: replaceRelationshipSource(
				"type Project struct {\n",
				"type Project struct {\n\t_ struct{} `forge:\"table=renamed_projects\"`\n",
			),
			wanted: `model table is "renamed_projects", want "projects"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newResourceRelationshipFixture(t, "Project", "Issue")
			source, err := os.ReadFile(fixture.targetModel)
			if err != nil {
				t.Fatal(err)
			}
			edited := test.edit(string(source))
			if edited == string(source) {
				t.Fatal("test edit did not change the generated target model")
			}
			if err := os.WriteFile(fixture.targetModel, []byte(edited), 0o644); err != nil {
				t.Fatal(err)
			}
			before := snapshotRelationshipProject(t, fixture.directory)
			relation, err := parseRequiredBelongsTo("project:Project")
			if err != nil {
				t.Fatal(err)
			}

			_, err = resolveResourceRelationships(fixture.child, fixture.state, []resourceBelongsTo{relation})
			if err == nil || !strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("error = %v, want text %q", err, test.wanted)
			}
			assertRelationshipProjectUnchanged(t, fixture.directory, before)
		})
	}
}

func TestResolveResourceRelationshipsRejectsPostgresIdentifierOverflowWithoutWrites(t *testing.T) {
	fixture := newResourceRelationshipFixture(t, "Project", "Issue")
	fixture.child.Plural = strings.Repeat("a", maximumResourceIdentifier)
	relation, err := parseRequiredBelongsTo("project:Project")
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotRelationshipProject(t, fixture.directory)

	_, err = resolveResourceRelationships(fixture.child, fixture.state, []resourceBelongsTo{relation})
	if err == nil || !strings.Contains(err.Error(), "longer than PostgreSQL's 63-byte identifier limit") {
		t.Fatalf("error = %v", err)
	}
	assertRelationshipProjectUnchanged(t, fixture.directory, before)
}

type resourceRelationshipFixture struct {
	directory   string
	state       resourceState
	target      resourceSpec
	child       resourceSpec
	targetModel string
}

func newResourceRelationshipFixture(t *testing.T, targetName, childName string) resourceRelationshipFixture {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "relationship-app")
	if err := createProject(newOptions{
		directory: directory,
		module:    "example.com/relationships",
		replace:   projectRoot(t),
	}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)

	state, err := loadResourceState()
	if err != nil {
		t.Fatal(err)
	}
	target, err := newResourceSpec(targetName, state)
	if err != nil {
		t.Fatal(err)
	}
	files, err := resourceFiles("example.com/relationships", resourceDefinition{
		resourceSpec: target,
		Fields:       defaultResourceFields(),
	})
	if err != nil {
		t.Fatal(err)
	}
	targetModel := filepath.Join("internal", "models", target.Package+".go")
	wroteModel := false
	for _, file := range files {
		if filepath.Clean(file.path) != targetModel {
			continue
		}
		if err := os.WriteFile(targetModel, []byte(file.content), 0o644); err != nil {
			t.Fatal(err)
		}
		wroteModel = true
		break
	}
	if !wroteModel {
		t.Fatalf("resource files did not contain generated target model %s", targetModel)
	}

	state.Resources = append(state.Resources, target)
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(filepath.Join(".forge", "resources.json"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	state, err = loadResourceState()
	if err != nil {
		t.Fatal(err)
	}
	child, err := newResourceSpec(childName, state)
	if err != nil {
		t.Fatal(err)
	}
	return resourceRelationshipFixture{
		directory: directory, state: state, target: target, child: child,
		targetModel: filepath.Join(directory, targetModel),
	}
}

func replaceRelationshipSource(old, replacement string) func(string) string {
	return func(source string) string {
		return strings.Replace(source, old, replacement, 1)
	}
}

type relationshipFileSnapshot struct {
	Data []byte
	Mode fs.FileMode
}

func snapshotRelationshipProject(t *testing.T, directory string) map[string]relationshipFileSnapshot {
	t.Helper()
	result := make(map[string]relationshipFileSnapshot)
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		result[filepath.ToSlash(relative)] = relationshipFileSnapshot{Data: contents, Mode: info.Mode()}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot relationship fixture: %v", err)
	}
	return result
}

func assertRelationshipProjectUnchanged(t *testing.T, directory string, before map[string]relationshipFileSnapshot) {
	t.Helper()
	after := snapshotRelationshipProject(t, directory)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("relationship resolution failure changed project files:\nbefore=%s\nafter=%s", snapshotRelationshipNames(before), snapshotRelationshipNames(after))
	}
}

func snapshotRelationshipNames(snapshot map[string]relationshipFileSnapshot) string {
	names := make([]string, 0, len(snapshot))
	for name := range snapshot {
		names = append(names, name)
	}
	return fmt.Sprintf("%q", names)
}
