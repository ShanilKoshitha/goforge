package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestMakeComponentCreatesStrictSourceAndCompilesViews(t *testing.T) {
	directory := componentProject(t, 5)
	t.Chdir(directory)
	process := &recordedProcess{}
	var output bytes.Buffer
	if err := makeComponent(context.Background(), "Status Card", nil, &output, io.Discard, process); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("resources", "views", "components", "status-card.forge.html")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"@props(title, tone=\"neutral\")", "$tone", "@slot(\"default\")", "@slot(\"actions\")"} {
		if !strings.Contains(string(contents), expected) {
			t.Errorf("component omits %q:\n%s", expected, contents)
		}
	}
	if process.name != "go" || strings.Join(process.args, " ") != "run ./cmd/views" {
		t.Fatalf("component compiler command = %s %v", process.name, process.args)
	}
	if !strings.Contains(output.String(), filepath.ToSlash(path)) {
		t.Fatalf("component output = %q", output.String())
	}
	if err := makeComponent(context.Background(), "Status Card", nil, io.Discard, io.Discard, process); err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
		t.Fatalf("duplicate component error = %v", err)
	}
}

func TestMakeComponentRemovesSourceWhenCompilationFails(t *testing.T) {
	directory := componentProject(t, 5)
	t.Chdir(directory)
	want := errors.New("compile failed")
	process := &recordedProcess{err: want}
	err := makeComponent(context.Background(), "Alert", nil, io.Discard, io.Discard, process)
	if !errors.Is(err, want) {
		t.Fatalf("component error = %v", err)
	}
	if _, err := os.Stat(filepath.Join("resources", "views", "components", "alert.forge.html")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed component source remains: %v", err)
	}
}

func TestMakeComponentRequiresFormatFive(t *testing.T) {
	directory := componentProject(t, 4)
	t.Chdir(directory)
	err := makeComponent(context.Background(), "Alert", nil, io.Discard, io.Discard, &recordedProcess{})
	if err == nil || !strings.Contains(err.Error(), "upgrade the project to format 5") {
		t.Fatalf("format error = %v", err)
	}
}

func componentProject(t *testing.T, version int) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: "+strconv.Itoa(version)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return directory
}
