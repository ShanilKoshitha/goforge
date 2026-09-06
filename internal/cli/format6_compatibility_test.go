package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormat6ApplicationRemainsCompatible(t *testing.T) {
	root := projectRoot(t)
	directory := filepath.Join(t.TempDir(), "format6")
	if err := os.CopyFS(directory, os.DirFS(filepath.Join("testdata", "format6"))); err != nil {
		t.Fatalf("copy frozen format-6 application: %v", err)
	}
	moduleFile, err := os.ReadFile(filepath.Join(directory, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(moduleFile, []byte("github.com/ShanilKoshitha/goforge v0.6.0")) || bytes.Contains(moduleFile, []byte("replace github.com/ShanilKoshitha/goforge")) {
		t.Fatalf("format-6 fixture must retain its historical v0.6.0 requirement before the compatibility replacement:\n%s", moduleFile)
	}

	environment := append(os.Environ(),
		"GOCACHE="+filepath.Join(root, ".cache", "go-build"),
		"GOMODCACHE="+filepath.Join(root, ".cache", "go-mod"),
		"GOWORK=off",
	)
	replacement := "-replace=github.com/ShanilKoshitha/goforge=" + root
	if output, err := generatedCommand(directory, environment, "go", "mod", "edit", replacement); err != nil {
		t.Fatalf("point compatibility fixture at current framework: %v\n%s", err, output)
	}

	t.Chdir(directory)
	var output bytes.Buffer
	for _, args := range [][]string{
		{"make:component", "CompatibilityNotice"},
		{"make:job", "CompatibilityProbe"},
		{"views:compile", "--check"},
		{"orm:generate", "--check"},
	} {
		output.Reset()
		if err := Run(args, &output, &output); err != nil {
			t.Fatalf("current CLI rejected format-6 command %v: %v\n%s", args, err, output.String())
		}
	}
	output.Reset()
	err = Run([]string{"make:resource", "MustUpgrade"}, &output, &output)
	if err == nil || !strings.Contains(err.Error(), "upgrade the project to format 8") {
		t.Fatalf("format-8 resource generator did not require an explicit format upgrade: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join("internal", "resources", "must_upgrade")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("refused resource generator changed the format-6 application: %v", statErr)
	}

	for _, gate := range [][]string{{"test", "./..."}, {"vet", "./..."}, {"build", "./cmd/..."}} {
		if result, gateErr := generatedCommand(directory, environment, "go", gate...); gateErr != nil {
			t.Fatalf("frozen format-6 application go %s: %v\n%s", strings.Join(gate, " "), gateErr, result)
		}
	}

	ormBefore, err := os.ReadFile(filepath.FromSlash(generatedORMPath))
	if err != nil {
		t.Fatal(err)
	}
	viewsBefore, err := os.ReadFile(filepath.FromSlash(generatedViewsPath))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOCACHE", filepath.Join(root, ".cache", "go-build"))
	t.Setenv("GOMODCACHE", filepath.Join(root, ".cache", "go-mod"))
	t.Setenv("GOWORK", "off")
	for _, command := range []string{"test", "build"} {
		output.Reset()
		if err := RunContext(context.Background(), []string{command}, bytes.NewReader(nil), &output, &output); err != nil {
			t.Fatalf("current CLI forge %s rejected format-6 application: %v\n%s", command, err, output.String())
		}
	}
	ormAfter, err := os.ReadFile(filepath.FromSlash(generatedORMPath))
	if err != nil {
		t.Fatal(err)
	}
	viewsAfter, err := os.ReadFile(filepath.FromSlash(generatedViewsPath))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ormBefore, ormAfter) || !bytes.Equal(viewsBefore, viewsAfter) {
		t.Fatal("forge test/build rewrote frozen format-6 generated artifacts")
	}
	if _, err := os.Stat(workflowBuildDestination()); err != nil {
		t.Fatalf("forge build did not publish format-6 server: %v", err)
	}
}

func assertArtifactBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	current, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, want) {
		t.Fatalf("workflow command rewrote frozen artifact %s", path)
	}
}
