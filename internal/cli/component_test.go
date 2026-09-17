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

const legacyGeneratedComponentSource = `@props(title, tone="neutral")
<article class="card card--{{$tone}}">
  <header><h2>{{$title}}</h2></header>
  <div>@slot("default")@endslot</div>
  <footer>@slot("actions")@endslot</footer>
</article>
`

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
	if string(contents) != legacyGeneratedComponentSource {
		t.Fatalf("format-5 component source changed:\n%s", contents)
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

func TestMakeComponentFormatFourteenRetainsLegacySource(t *testing.T) {
	directory := componentProject(t, 14)
	t.Chdir(directory)
	if err := makeComponent(context.Background(), "Status Card", nil, io.Discard, io.Discard, &recordedProcess{}); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(filepath.Join("resources", "views", "components", "status-card.forge.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != legacyGeneratedComponentSource {
		t.Fatalf("format-14 component source changed:\n%s", contents)
	}
}

func TestMakeComponentFormatFifteenCreatesAttributeSink(t *testing.T) {
	directory := componentProject(t, 15)
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
	source := string(contents)
	if strings.Count(source, "@attributes(") != 1 ||
		!strings.Contains(source, `@attributes(class="card", class=(printf "card--%s" $tone))`) ||
		strings.Contains(source, `<article class="card`) {
		t.Fatalf("format-15 component lacks one compile-time attribute sink:\n%s", source)
	}
	if process.name != "go" || strings.Join(process.args, " ") != "run ./cmd/views" {
		t.Fatalf("component compiler command = %s %v", process.name, process.args)
	}
	if !strings.Contains(output.String(), filepath.ToSlash(path)) {
		t.Fatalf("component output = %q", output.String())
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
