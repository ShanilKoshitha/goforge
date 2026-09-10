package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
		"resources/assets/files/app.css",
		"resources/assets/files/app.js",
		"resources/assets/files/images/mark.bin",
		"resources/assets/files/node_modules/local.css",
		"resources/assets/files/.private/theme.css",
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

	for _, name := range []string{"README.md", "nested/go.mod", "nested/.env", "assets/app.js", "styles/app.css", "resources/assets/README.md"} {
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

	for _, directory := range []string{".git", ".forge", "bin", ".tmp", "tmp", ".cache", "vendor", "node_modules"} {
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

func TestSourceSnapshotStreamsLargeAssetInputs(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, filepath.FromSlash("resources/assets/files/oversized.css"))
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(9 << 20); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := takeSourceSnapshot(root)
	if err != nil {
		t.Fatalf("snapshot large application-owned asset: %v", err)
	}
	file, err = os.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte("x"), (9<<20)-1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := takeSourceSnapshot(root)
	if err != nil {
		t.Fatalf("snapshot changed large application-owned asset: %v", err)
	}
	if after == before {
		t.Fatal("large asset content change was not detected")
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

func TestReadableSourceSnapshotRecoversFromTransientError(t *testing.T) {
	want := testSnapshot(7)
	var reads atomic.Int32
	got, err := waitForReadableSourceSnapshotWith(
		context.Background(),
		func() (sourceSnapshot, error) {
			if reads.Add(1) == 1 {
				return sourceSnapshot{}, errors.New("sharing violation")
			}
			return want, nil
		},
		time.Millisecond,
		100*time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("snapshot = %v, want %v", got, want)
	}
}

func TestReadableSourceSnapshotReportsPersistentError(t *testing.T) {
	started := time.Now()
	_, err := waitForReadableSourceSnapshotWith(
		context.Background(),
		func() (sourceSnapshot, error) { return sourceSnapshot{}, errors.New("sharing violation") },
		time.Millisecond,
		10*time.Millisecond,
	)
	if err == nil || !strings.Contains(err.Error(), "remained unreadable") {
		t.Fatalf("persistent read error = %v", err)
	}
	if time.Since(started) < 10*time.Millisecond {
		t.Fatal("persistent error was reported before its tolerance elapsed")
	}
}

func TestStableSourceSnapshotObservesReturnToOriginalContent(t *testing.T) {
	original := testSnapshot(1)
	changed := testSnapshot(2)
	var reads atomic.Int32
	reader := tolerantSourceSnapshotReader{
		read: func() (sourceSnapshot, error) {
			switch reads.Add(1) {
			case 1:
				return changed, nil
			default:
				return original, nil
			}
		},
		tolerance: time.Second,
	}
	got, err := waitForStableSourceSnapshotWithReader(
		context.Background(), &reader, original, time.Millisecond, 10*time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != original {
		t.Fatalf("stable snapshot = %v, want returned original %v", got, original)
	}
}

func TestSourceGenerationTrackerRecordsContentRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeWatchFile(t, root, "main.go", "package main\n")
	initial := mustSourceSnapshot(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracker := startSourceGenerationTracker(ctx, root, initial, 2*time.Millisecond)
	defer tracker.Close()

	writeWatchFile(t, root, "main.go", "package main\n// changed\n")
	waitForGeneration(t, tracker, 1)
	writeWatchFile(t, root, "main.go", "package main\n")
	waitForGeneration(t, tracker, 2)
	if got := mustSourceSnapshot(t, root); got != initial {
		t.Fatal("content round trip did not restore the original digest")
	}
}

func TestSourceGenerationTrackerWakesWhenTransientReadRecoversUnchanged(t *testing.T) {
	want := testSnapshot(4)
	tracker := &sourceGenerationTracker{
		current:   want,
		changed:   make(chan struct{}),
		tolerance: time.Second,
	}
	tracker.recordError(errors.New("sharing violation"))
	result := make(chan sourceWatchResult, 1)
	go func() {
		snapshot, err := tracker.Read(context.Background())
		result <- sourceWatchResult{snapshot: snapshot, err: err}
	}()
	tracker.recordSnapshot(want)
	select {
	case got := <-result:
		if got.err != nil || got.snapshot != want {
			t.Fatalf("recovered read = %v, %v", got.snapshot, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("read did not wake after unchanged source became readable")
	}
}

func TestSourceGenerationTrackerDoesNotLoseRoundTripBeforeWaitStarts(t *testing.T) {
	original := testSnapshot(5)
	tracker := &sourceGenerationTracker{
		current:   original,
		changed:   make(chan struct{}),
		tolerance: time.Second,
	}
	tracker.generation.Store(2)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, err := tracker.WaitForChange(ctx, original, 0, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got != original {
		t.Fatalf("round-trip snapshot = %v, want %v", got, original)
	}
}

func waitForGeneration(t *testing.T, tracker *sourceGenerationTracker, want uint64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for tracker.Generation() < want {
		if time.Now().After(deadline) {
			t.Fatalf("generation = %d, want at least %d", tracker.Generation(), want)
		}
		time.Sleep(time.Millisecond)
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
