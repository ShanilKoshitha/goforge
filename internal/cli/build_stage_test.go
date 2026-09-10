package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStageProjectSourceCopiesInputsAndExcludesToolOutputs(t *testing.T) {
	root := t.TempDir()
	writeStageFile(t, root, "go.mod", "module example.com/app\n")
	writeStageFile(t, root, "vendor/example/data.txt", "vendored")
	writeStageFile(t, root, "public/dist/app.js", "compiled")
	writeStageFile(t, root, "node_modules/package/index.js", "dependency")
	writeStageFile(t, root, "bin/app", "old build")

	staged, err := stageProjectSource(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(staged)
	for _, name := range []string{"go.mod", "vendor/example/data.txt", "public/dist/app.js"} {
		if _, err := os.Stat(filepath.Join(staged, filepath.FromSlash(name))); err != nil {
			t.Errorf("staged input %s: %v", name, err)
		}
	}
	for _, name := range []string{"node_modules/package/index.js", "bin/app"} {
		if _, err := os.Stat(filepath.Join(staged, filepath.FromSlash(name))); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("excluded staged path %s: %v", name, err)
		}
	}
}

func TestStageProjectSourceHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	writeStageFile(t, root, "main.go", "package main\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := stageProjectSource(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled staging error = %v", err)
	}
}

func writeStageFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
