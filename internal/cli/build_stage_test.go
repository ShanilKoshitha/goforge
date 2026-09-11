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
	writeStageFile(t, root, ".git", "gitdir: ../worktrees/app\n")

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
	for _, name := range []string{"node_modules/package/index.js", "bin/app", ".git"} {
		if _, err := os.Stat(filepath.Join(staged, filepath.FromSlash(name))); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("excluded staged path %s: %v", name, err)
		}
	}
}

func TestStagedSourceExclusionsApplyToDirectoriesFilesAndLinks(t *testing.T) {
	for _, test := range []struct {
		relative string
		name     string
		want     bool
	}{
		{relative: ".git", name: ".git", want: true},
		{relative: "node_modules", name: "node_modules", want: true},
		{relative: "web/node_modules", name: "node_modules", want: true},
		{relative: "bin", name: "bin", want: true},
		{relative: "internal/bin", name: "bin", want: false},
		{relative: "public/dist", name: "dist", want: false},
	} {
		if got := excludedStagedSourceEntry(test.relative, test.name); got != test.want {
			t.Errorf("excludedStagedSourceEntry(%q, %q) = %v, want %v", test.relative, test.name, got, test.want)
		}
	}
}

func TestStageProjectSourceDoesNotFollowUnrelatedSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(target, []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "notes-link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	staged, err := stageProjectSource(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(staged)
	if _, err := os.Stat(filepath.Join(staged, "notes-link.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staged symlink = %v", err)
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
