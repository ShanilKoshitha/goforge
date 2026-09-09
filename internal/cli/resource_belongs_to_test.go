package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
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
	if err := Run([]string{
		"make:resource", "Issue",
		"--field", "summary:string",
		"--belongs-to", "category:Category",
	}, &output, &output); err != nil {
		t.Fatalf("generate relationship child: %v\n%s", err, output.String())
	}

	assertGeneratedFileContains(t, "internal/models/issue.go",
		`CategoryID int64`, `json:"category_id"`, `forge:"protected,required,index,references=Category.ID,on_delete=no_action"`,
		`Category`, `*Category`, `json:"-"`, `forge:"belongs_to,target=Category,foreign_key=CategoryID,references=ID"`)
	assertGeneratedFileContains(t, "internal/resources/issue/model.go",
		"type AssociationIDs struct", "CategoryID int64")
	assertGeneratedFileContains(t, filepath.FromSlash(generatedORMPath),
		"IssueCategoryRelation", "func (query IssueQuery) LoadCategory")

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
		`CREATE INDEX issues_category_id_idx ON issues ("category_id")`)

	state, err := os.ReadFile(filepath.Join(".forge", "resources.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"relationships"`, `"category_id"`, `"belongs_to"`} {
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
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "format-nine-app")
	if err := createProject(newOptions{directory: directory, module: "example.com/formatnine", replace: root}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	var output bytes.Buffer
	if err := Run([]string{"make:resource", "Category"}, &output, &output); err != nil {
		t.Fatalf("format-nine scalar resource: %v\n%s", err, output.String())
	}
	before := snapshotRelationshipProject(t, directory)

	err := Run([]string{"make:resource", "Issue", "--belongs-to", "category:Category"}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "requires project format 10") {
		t.Fatalf("relationship format error = %v", err)
	}
	assertRelationshipProjectUnchanged(t, directory, before)
}
