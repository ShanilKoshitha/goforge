package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMakeResourceBelongsToGeneratesInspectableOwnerSafeSlice(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "relationshipboard")
	if err := createProject(newOptions{directory: directory, module: "example.com/relationshipboard", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	var output bytes.Buffer
	if err := Run([]string{"make:resource", "Category", "--field", "title:string"}, &output, &output); err != nil {
		t.Fatalf("generate relationship target: %v\n%s", err, output.String())
	}
	if err := Run([]string{"make:resource", "Project", "--field", "title:string"}, &output, &output); err != nil {
		t.Fatalf("generate second relationship target: %v\n%s", err, output.String())
	}
	if err := Run([]string{
		"make:resource", "Issue",
		"--field", "summary:string",
		"--belongs-to", "category:Category",
		"--belongs-to", "project:Project",
	}, &output, &output); err != nil {
		t.Fatalf("generate relationship child: %v\n%s", err, output.String())
	}

	assertGeneratedFileContains(t, "internal/models/issue.go",
		`CategoryID int64`, `json:"category_id"`, `forge:"protected,required,index,references=Category.ID,on_delete=no_action"`,
		`Category`, `*Category`, `json:"-"`, `forge:"belongs_to,target=Category,foreign_key=CategoryID,references=ID"`,
		`ProjectID`, `json:"project_id"`, `forge:"protected,required,index,references=Project.ID,on_delete=no_action"`,
		`Project`, `*Project`, `forge:"belongs_to,target=Project,foreign_key=ProjectID,references=ID"`)
	assertGeneratedFileContains(t, "internal/resources/issue/model.go",
		"type AssociationIDs struct", "CategoryID int64", "ProjectID")
	assertGeneratedFileContains(t, filepath.FromSlash(generatedORMPath),
		"IssueCategoryRelation", "func (query IssueQuery) LoadCategory",
		"IssueProjectRelation", "func (query IssueQuery) LoadProject")

	categoryMigrations, err := filepath.Glob(filepath.Join("database", "migrations", "*_create_categories.up.sql"))
	if err != nil || len(categoryMigrations) != 1 {
		t.Fatalf("category migrations = %v, %v", categoryMigrations, err)
	}
	assertGeneratedFileContains(t, categoryMigrations[0],
		"CONSTRAINT categories_owner_id_key UNIQUE (user_id, id)")
	issueMigrations, err := filepath.Glob(filepath.Join("database", "migrations", "*_create_issues.up.sql"))
	if err != nil || len(issueMigrations) != 1 {
		t.Fatalf("issue migrations = %v, %v", issueMigrations, err)
	}
	assertGeneratedFileContains(t, issueMigrations[0],
		`"category_id" BIGINT NOT NULL`,
		"CONSTRAINT issues_owner_id_key UNIQUE (user_id, id)",
		`CONSTRAINT issues_category_owner_fkey FOREIGN KEY (user_id, "category_id")`,
		"REFERENCES categories (user_id, id) ON DELETE NO ACTION",
		`CREATE INDEX issues_category_id_idx ON issues ("category_id")`,
		`CONSTRAINT issues_project_owner_fkey FOREIGN KEY (user_id, "project_id")`,
		"REFERENCES projects (user_id, id) ON DELETE NO ACTION",
		`CREATE INDEX issues_project_id_idx ON issues ("project_id")`)

	state, err := os.ReadFile(filepath.Join(".forge", "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"relationships"`, `"category_id"`, `"project_id"`, `"belongs_to"`} {
		if bytes.Contains(state, []byte(forbidden)) {
			t.Fatalf("one-shot relationship schema leaked into persistent metadata as %s:\n%s", forbidden, state)
		}
	}

	command := exec.Command("go", "test", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("relationship application does not compile and test: %v\n%s", err, result)
	}
}

func TestMakeResourceBelongsToRequiresFormatTenWithoutWrites(t *testing.T) {
	for _, version := range []string{"8", "9"} {
		t.Run(version, func(t *testing.T) {
			root := projectRoot(t)
			directory := filepath.Join(t.TempDir(), "format-"+version+"-app")
			if err := createProject(newOptions{directory: directory, module: "example.com/format" + version, replace: root}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: "+version+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Chdir(directory)
			var output bytes.Buffer
			if err := Run([]string{"make:resource", "Category"}, &output, &output); err != nil {
				t.Fatalf("format-%s scalar resource: %v\n%s", version, err, output.String())
			}
			command := exec.Command("go", "test", "./...")
			command.Dir = directory
			command.Env = append(os.Environ(),
				"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
				"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
				"GOWORK=off",
			)
			if result, err := command.CombinedOutput(); err != nil {
				t.Fatalf("format-%s scalar application does not compile and test: %v\n%s", version, err, result)
			}
			before := snapshotRelationshipProject(t, directory)

			err := Run([]string{"make:resource", "Issue", "--belongs-to", "category:Category"}, &output, &output)
			if err == nil || !strings.Contains(err.Error(), "requires project format 10") {
				t.Fatalf("relationship format error = %v", err)
			}
			assertRelationshipProjectUnchanged(t, directory, before)
		})
	}
}

func TestMakeResourceBelongsToRejectsConcurrentTargetMigrationEdit(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "relationship-race")
	if err := createProject(newOptions{directory: directory, module: "example.com/relationshiprace", replace: root}); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	var output bytes.Buffer
	if err := Run([]string{"make:resource", "Category"}, &output, &output); err != nil {
		t.Fatalf("generate relationship target: %v\n%s", err, output.String())
	}
	migrations, err := filepath.Glob(filepath.Join("database", "migrations", "*_create_categories.up.sql"))
	if err != nil || len(migrations) != 1 {
		t.Fatalf("category migrations = %v, %v", migrations, err)
	}
	original, err := os.ReadFile(migrations[0])
	if err != nil {
		t.Fatal(err)
	}
	edited := append(append([]byte(nil), original...), []byte("\n-- retained concurrent editor change\n")...)
	before := snapshotRelationshipProject(t, directory)
	relation, err := parseRequiredBelongsTo("category:Category")
	if err != nil {
		t.Fatal(err)
	}
	runner := resourceProcessRunnerFunc(func(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error {
		return os.WriteFile(migrations[0], edited, 0o644)
	})

	err = makeResourceWithRelationshipDependencies(
		context.Background(), "Issue", defaultResourceFields(), []resourceBelongsTo{relation}, false,
		nil, io.Discard, io.Discard, runner, publishResourceManagedFile,
	)
	if err == nil || !strings.Contains(err.Error(), "belongs-to target migration") || !strings.Contains(err.Error(), "changed during resource generation") {
		t.Fatalf("concurrent target migration error = %v", err)
	}
	expected := make(map[string]relationshipFileSnapshot, len(before))
	for path, state := range before {
		expected[path] = state
	}
	relativeMigration := filepath.ToSlash(migrations[0])
	migrationSnapshot := expected[relativeMigration]
	migrationSnapshot.Data = edited
	expected[relativeMigration] = migrationSnapshot
	after := snapshotRelationshipProject(t, directory)
	if !reflect.DeepEqual(after, expected) {
		t.Fatalf("dependency race did not roll back atomically:\nbefore=%s\nafter=%s", snapshotRelationshipNames(expected), snapshotRelationshipNames(after))
	}
}
