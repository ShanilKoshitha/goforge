package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteManagedFilePreservesDestinationMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated.go")
	if err := os.WriteFile(path, []byte("old\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := writeManagedFile(path, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "new\n" {
		t.Fatalf("managed contents = %q", contents)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o640 {
			t.Fatalf("managed mode = %o, want 640", got)
		}
	}
}

func TestWriteExclusiveUsesInspectableSourceMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "generated", "model.go")
	if err := writeExclusive(path, "package generated\n"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "package generated\n" {
		t.Fatalf("exclusive source: read=%v contents=%q", err, contents)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o644 {
			t.Fatalf("exclusive source mode = %o, want 644", got)
		}
	}
}
