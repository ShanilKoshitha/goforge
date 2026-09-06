package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type sourceWatchResult struct {
	snapshot sourceSnapshot
	err      error
}

func TestSourceSnapshotIncludesOnlyEligibleApplicationSource(t *testing.T) {
	root := t.TempDir()
	writeWatchFile(t, root, "README.md", "ignored")
	writeWatchFile(t, root, "nested/go.mod", "ignored")
	writeWatchFile(t, root, "nested/.env", "ignored")
	baseline := mustSourceSnapshot(t, root)

	for _, name := range []string{
		"forge.yaml",
		"go.mod",
		"go.sum",
		".env",
		"main.go",
		"internal/app/app.go",
		"resources/views/page.forge.html",
		"database/migrations/one.sql",
	} {
		writeWatchFile(t, root, name, name+" content")
		current := mustSourceSnapshot(t, root)
		if current == baseline {
			t.Fatalf("eligible source %s did not change snapshot", name)
		}
		baseline = current
	}

	for _, name := range []string{"README.md", "nested/go.mod", "nested/.env", "assets/app.js", "styles/app.css"} {
		writeWatchFile(t, root, name, "changed but ineligible")
		if current := mustSourceSnapshot(t, root); current != baseline {
			t.Fatalf("ineligible path %s changed snapshot", name)
		}
	}
}

func TestSourceSnapshotExcludesGeneratedAndIgnoredDirectories(t *testing.T) {
	root := t.TempDir()
	writeWatchFile(t, root, generatedViewSource, "generated one")
	baseline := mustSourceSnapshot(t, root)
	writeWatchFile(t, root, generatedViewSource, "generated two")
	if current := mustSourceSnapshot(t, root); current != baseline {
		t.Fatal("self-produced view artifact changed snapshot")
	}

	for _, directory := range []string{".git", "bin", ".tmp", "tmp", ".cache", "vendor", "node_modules"} {
		name := filepath.Join("parent", directory, "nested", "source.go")
		writeWatchFile(t, root, name, "package ignored")
		if current := mustSourceSnapshot(t, root); current != baseline {
			t.Fatalf("source below excluded directory %s changed snapshot", directory)
		}
	}

	writeWatchFile(t, root, "resources/views/other.go", "package views")
	if current := mustSourceSnapshot(t, root); current == baseline {
		t.Fatal("neighboring Go source was excluded with generated artifact")
	}
}

func TestSourceSnapshotTracksContentCreateDeleteAndRename(t *testing.T) {
	root := t.TempDir()
	writeWatchFile(t, root, "first.go", "package first")
	first := mustSourceSnapshot(t, root)
	if repeated := mustSourceSnapshot(t, root); repeated != first {
		t.Fatal("unchanged source snapshot is not deterministic")
	}

	writeWatchFile(t, root, "first.go", "package changed")
	changed := mustSourceSnapshot(t, root)
	if changed == first {
		t.Fatal("content change did not change snapshot")
	}
	writeWatchFile(t, root, "first.go", "package first")
	if restored := mustSourceSnapshot(t, root); restored != first {
		t.Fatal("restored path and content did not restore deterministic snapshot")
	}

	writeWatchFile(t, root, "created.sql", "SELECT 1;")
	created := mustSourceSnapshot(t, root)
	if created == first {
		t.Fatal("created source did not change snapshot")
	}
	if err := os.Remove(filepath.Join(root, "created.sql")); err != nil {
		t.Fatal(err)
	}
	if deleted := mustSourceSnapshot(t, root); deleted != first {
		t.Fatal("deleting created source did not restore snapshot")
	}

	if err := os.Rename(filepath.Join(root, "first.go"), filepath.Join(root, "renamed.go")); err != nil {
		t.Fatal(err)
	}
	if renamed := mustSourceSnapshot(t, root); renamed == first {
		t.Fatal("rename with identical content did not change path-sensitive snapshot")
	}
}

func TestWaitForChangedSourceSnapshotDebouncesBursts(t *testing.T) {
	root := t.TempDir()
	writeWatchFile(t, root, "main.go", "package main\n")
	baseline := mustSourceSnapshot(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resultChannel := make(chan sourceWatchResult, 1)
	go func() {
		snapshot, err := waitForChangedSourceSnapshot(ctx, root, baseline, 5*time.Millisecond, 100*time.Millisecond)
		resultChannel <- sourceWatchResult{snapshot: snapshot, err: err}
	}()

	writeWatchFile(t, root, "main.go", "package main\n// first\n")
	assertWatchStillWaiting(t, resultChannel, 35*time.Millisecond)
	writeWatchFile(t, root, "nested/new.go", "package nested\n")
	assertWatchStillWaiting(t, resultChannel, 35*time.Millisecond)
	writeWatchFile(t, root, "nested/new.go", "package nested\n// final\n")
	assertWatchStillWaiting(t, resultChannel, 70*time.Millisecond)

	want := mustSourceSnapshot(t, root)
	select {
	case result := <-resultChannel:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.snapshot != want {
			t.Fatal("watch returned a stale snapshot from within the change burst")
		}
	case <-ctx.Done():
		t.Fatal("watch did not return after source became quiet")
	}
}

func TestWaitForChangedSourceSnapshotHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	baseline := mustSourceSnapshot(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := waitForChangedSourceSnapshot(ctx, root, baseline, 5*time.Millisecond, time.Second)
		result <- err
	}()
	writeWatchFile(t, root, "main.go", "package main")
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled watch error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled watch did not return")
	}
}

func TestSourceSnapshotAndWatchReportFilesystemErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := takeSourceSnapshot(missing); err == nil {
		t.Fatal("missing source root did not return an error")
	}

	root := t.TempDir()
	baseline := mustSourceSnapshot(t, root)
	if err := os.Mkdir(filepath.Join(root, ".env"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := waitForChangedSourceSnapshot(ctx, root, baseline, 5*time.Millisecond, 10*time.Millisecond)
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("filesystem error = %v", err)
	}
}

func writeWatchFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustSourceSnapshot(t *testing.T, root string) sourceSnapshot {
	t.Helper()
	snapshot, err := takeSourceSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertWatchStillWaiting(t *testing.T, result <-chan sourceWatchResult, duration time.Duration) {
	t.Helper()
	select {
	case result := <-result:
		t.Fatalf("watch returned before change burst became quiet: %v", result.err)
	case <-time.After(duration):
	}
}
