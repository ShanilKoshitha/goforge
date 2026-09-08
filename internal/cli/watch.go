package cli

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const generatedViewSource = "resources/views/views_gen.go"

const sourceWatchErrorTolerance = 500 * time.Millisecond

var excludedSourceDirectories = map[string]struct{}{
	".git":         {},
	".forge":       {},
	"bin":          {},
	".tmp":         {},
	"tmp":          {},
	".cache":       {},
	"vendor":       {},
	"node_modules": {},
}

// sourceSnapshot identifies the paths and contents that can affect a
// development server. File metadata is deliberately excluded so touching a
// source file without changing it does not cause a rebuild.
type sourceSnapshot [sha256.Size]byte

type sourceGenerationTracker struct {
	cancel     context.CancelFunc
	done       chan struct{}
	generation atomic.Uint64

	mu        sync.Mutex
	current   sourceSnapshot
	changed   chan struct{}
	failingAt time.Time
	lastError error
	tolerance time.Duration
}

func startSourceGenerationTracker(
	ctx context.Context,
	root string,
	initial sourceSnapshot,
	pollInterval time.Duration,
) *sourceGenerationTracker {
	trackContext, cancel := context.WithCancel(ctx)
	tracker := &sourceGenerationTracker{
		cancel:    cancel,
		done:      make(chan struct{}),
		current:   initial,
		changed:   make(chan struct{}),
		tolerance: sourceWatchErrorTolerance,
	}
	go func() {
		defer close(tracker.done)
		poll := time.NewTicker(pollInterval)
		defer poll.Stop()
		for {
			select {
			case <-trackContext.Done():
				return
			case <-poll.C:
				current, err := takeSourceSnapshot(root)
				if err != nil {
					tracker.recordError(err)
					continue
				}
				tracker.recordSnapshot(current)
			}
		}
	}()
	return tracker
}

func (tracker *sourceGenerationTracker) recordSnapshot(current sourceSnapshot) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	recovered := tracker.lastError != nil
	tracker.failingAt = time.Time{}
	tracker.lastError = nil
	if current == tracker.current && !recovered {
		return
	}
	if current != tracker.current {
		tracker.current = current
		tracker.generation.Add(1)
	}
	close(tracker.changed)
	tracker.changed = make(chan struct{})
}

func (tracker *sourceGenerationTracker) recordError(err error) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.failingAt.IsZero() {
		tracker.failingAt = time.Now()
	}
	tracker.lastError = err
	close(tracker.changed)
	tracker.changed = make(chan struct{})
}

func (tracker *sourceGenerationTracker) Generation() uint64 {
	return tracker.generation.Load()
}

func (tracker *sourceGenerationTracker) Snapshot() (sourceSnapshot, error) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.lastError != nil && time.Since(tracker.failingAt) >= tracker.tolerance {
		return sourceSnapshot{}, fmt.Errorf("application source remained unreadable for %s: %w", tracker.tolerance, tracker.lastError)
	}
	return tracker.current, nil
}

func (tracker *sourceGenerationTracker) Read(ctx context.Context) (sourceSnapshot, error) {
	for {
		tracker.mu.Lock()
		current := tracker.current
		lastError := tracker.lastError
		failingAt := tracker.failingAt
		changed := tracker.changed
		tracker.mu.Unlock()
		if lastError == nil {
			return current, nil
		}
		if time.Since(failingAt) >= tracker.tolerance {
			return sourceSnapshot{}, fmt.Errorf("application source remained unreadable for %s: %w", tracker.tolerance, lastError)
		}
		select {
		case <-ctx.Done():
			return sourceSnapshot{}, ctx.Err()
		case <-changed:
		}
	}
}

func (tracker *sourceGenerationTracker) WaitForChange(
	ctx context.Context,
	previous sourceSnapshot,
	previousGeneration uint64,
	debounce time.Duration,
) (sourceSnapshot, error) {
	for {
		tracker.mu.Lock()
		current := tracker.current
		generation := tracker.generation.Load()
		lastError := tracker.lastError
		failingAt := tracker.failingAt
		changed := tracker.changed
		tracker.mu.Unlock()
		if lastError != nil && time.Since(failingAt) >= tracker.tolerance {
			return sourceSnapshot{}, fmt.Errorf("application source remained unreadable for %s: %w", tracker.tolerance, lastError)
		}
		if lastError == nil && (current != previous || generation != previousGeneration) {
			return tracker.Stabilize(ctx, current, debounce)
		}
		select {
		case <-ctx.Done():
			return sourceSnapshot{}, ctx.Err()
		case <-changed:
		}
	}
}

func (tracker *sourceGenerationTracker) Stabilize(
	ctx context.Context,
	initial sourceSnapshot,
	debounce time.Duration,
) (sourceSnapshot, error) {
	candidate := initial
	generation := tracker.Generation()
	timer := time.NewTimer(debounce)
	defer timer.Stop()
	for {
		tracker.mu.Lock()
		changed := tracker.changed
		tracker.mu.Unlock()
		select {
		case <-ctx.Done():
			return sourceSnapshot{}, ctx.Err()
		case <-timer.C:
			current, err := tracker.Read(ctx)
			if err != nil {
				return sourceSnapshot{}, err
			}
			if current == candidate && tracker.Generation() == generation {
				return current, nil
			}
			candidate = current
			generation = tracker.Generation()
			timer.Reset(debounce)
		case <-changed:
			current, err := tracker.Read(ctx)
			if err != nil {
				return sourceSnapshot{}, err
			}
			candidate = current
			generation = tracker.Generation()
			stopAndDrainTimer(timer)
			timer.Reset(debounce)
		}
	}
}

func (tracker *sourceGenerationTracker) Close() {
	tracker.cancel()
	<-tracker.done
}

// takeSourceSnapshot returns a deterministic digest of application source.
// Paths are relative to root and normalized with forward slashes.
func takeSourceSnapshot(root string) (sourceSnapshot, error) {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return sourceSnapshot{}, fmt.Errorf("inspect source root: %w", err)
	}
	if !rootInfo.IsDir() {
		return sourceSnapshot{}, errors.New("source root is not a directory")
	}

	type sourceFile struct {
		path string
		full string
	}
	var files []sourceFile
	err = filepath.WalkDir(root, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// A file or directory disappearing during a scan is an ordinary
			// atomic-save observation. The following poll will see the stable
			// tree; persistent access failures still stop the watcher.
			if errors.Is(walkErr, os.ErrNotExist) && name != root {
				return nil
			}
			return walkErr
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return fmt.Errorf("make source path relative: %w", err)
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)

		if entry.IsDir() {
			if isRootSourceFile(relative) {
				return fmt.Errorf("source path %s is not a regular file", relative)
			}
			if _, excluded := excludedSourceDirectories[entry.Name()]; excluded {
				return filepath.SkipDir
			}
			return nil
		}
		if !isWatchedSourcePath(relative) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect source %s: %w", relative, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("source path %s is not a regular file", relative)
		}
		files = append(files, sourceFile{path: relative, full: name})
		return nil
	})
	if err != nil {
		return sourceSnapshot{}, fmt.Errorf("walk application source: %w", err)
	}
	sort.Slice(files, func(left, right int) bool { return files[left].path < files[right].path })

	digest := sha256.New()
	for _, file := range files {
		content, err := os.ReadFile(file.full)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return sourceSnapshot{}, fmt.Errorf("read source %s: %w", file.path, err)
		}
		writeSnapshotField(digest, []byte(file.path))
		writeSnapshotField(digest, content)
	}
	var snapshot sourceSnapshot
	copy(snapshot[:], digest.Sum(nil))
	return snapshot, nil
}

// waitForChangedSourceSnapshot waits until source differs from previous and
// the latest changed snapshot remains stable for debounce. Polling content
// makes the behavior portable across editor rename strategies and operating
// system filesystem notification differences.
func waitForChangedSourceSnapshot(
	ctx context.Context,
	root string,
	previous sourceSnapshot,
	pollInterval time.Duration,
	debounce time.Duration,
) (sourceSnapshot, error) {
	reader := func() (sourceSnapshot, error) { return takeSourceSnapshot(root) }
	return waitForChangedSourceSnapshotWith(ctx, reader, previous, pollInterval, debounce, sourceWatchErrorTolerance)
}

func waitForChangedSourceSnapshotWith(
	ctx context.Context,
	read func() (sourceSnapshot, error),
	previous sourceSnapshot,
	pollInterval, debounce, errorTolerance time.Duration,
) (sourceSnapshot, error) {
	if err := validateSourceWatchDurations(pollInterval, debounce, errorTolerance); err != nil {
		return sourceSnapshot{}, err
	}
	reader := tolerantSourceSnapshotReader{read: read, tolerance: errorTolerance}
	poll := time.NewTicker(pollInterval)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return sourceSnapshot{}, ctx.Err()
		case <-poll.C:
			current, available, err := reader.next()
			if err != nil {
				return sourceSnapshot{}, err
			}
			if available && current != previous {
				return waitForStableSourceSnapshotWithReader(ctx, &reader, current, pollInterval, debounce)
			}
		}
	}
}

func waitForStableSourceSnapshot(
	ctx context.Context,
	root string,
	initial sourceSnapshot,
	pollInterval time.Duration,
	debounce time.Duration,
) (sourceSnapshot, error) {
	reader := tolerantSourceSnapshotReader{
		read:      func() (sourceSnapshot, error) { return takeSourceSnapshot(root) },
		tolerance: sourceWatchErrorTolerance,
	}
	if err := validateSourceWatchDurations(pollInterval, debounce, reader.tolerance); err != nil {
		return sourceSnapshot{}, err
	}
	return waitForStableSourceSnapshotWithReader(ctx, &reader, initial, pollInterval, debounce)
}

func waitForReadableSourceSnapshotWith(
	ctx context.Context,
	read func() (sourceSnapshot, error),
	pollInterval, errorTolerance time.Duration,
) (sourceSnapshot, error) {
	if err := validateSourceWatchDurations(pollInterval, 0, errorTolerance); err != nil {
		return sourceSnapshot{}, err
	}
	reader := tolerantSourceSnapshotReader{read: read, tolerance: errorTolerance}
	for {
		current, available, err := reader.next()
		if err != nil {
			return sourceSnapshot{}, err
		}
		if available {
			return current, nil
		}
		poll := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			stopAndDrainTimer(poll)
			return sourceSnapshot{}, ctx.Err()
		case <-poll.C:
		}
	}
}

func waitForStableSourceSnapshotWithReader(
	ctx context.Context,
	reader *tolerantSourceSnapshotReader,
	initial sourceSnapshot,
	pollInterval, debounce time.Duration,
) (sourceSnapshot, error) {
	if debounce == 0 {
		return initial, nil
	}
	if pollInterval <= 0 {
		return sourceSnapshot{}, errors.New("source watch poll interval must be positive")
	}
	poll := time.NewTicker(pollInterval)
	defer poll.Stop()
	quiet := time.NewTimer(debounce)
	defer quiet.Stop()
	quietChannel := quiet.C
	candidate := initial
	for {
		select {
		case <-ctx.Done():
			return sourceSnapshot{}, ctx.Err()
		case <-poll.C:
			current, available, err := reader.next()
			if err != nil {
				return sourceSnapshot{}, err
			}
			if !available {
				stopAndDrainTimer(quiet)
				quietChannel = nil
				continue
			}
			if current != candidate {
				candidate = current
				stopAndDrainTimer(quiet)
				quiet.Reset(debounce)
				quietChannel = quiet.C
			} else if quietChannel == nil {
				quiet.Reset(debounce)
				quietChannel = quiet.C
			}
		case <-quietChannel:
			current, available, err := reader.next()
			if err != nil {
				return sourceSnapshot{}, err
			}
			quietChannel = nil
			if !available {
				continue
			}
			if current == candidate {
				return current, nil
			}
			candidate = current
			quiet.Reset(debounce)
			quietChannel = quiet.C
		}
	}
}

type tolerantSourceSnapshotReader struct {
	read      func() (sourceSnapshot, error)
	tolerance time.Duration
	failingAt time.Time
	lastError error
}

func (reader *tolerantSourceSnapshotReader) next() (sourceSnapshot, bool, error) {
	current, err := reader.read()
	if err == nil {
		reader.failingAt = time.Time{}
		reader.lastError = nil
		return current, true, nil
	}
	reader.lastError = err
	if reader.failingAt.IsZero() {
		reader.failingAt = time.Now()
		return sourceSnapshot{}, false, nil
	}
	if time.Since(reader.failingAt) < reader.tolerance {
		return sourceSnapshot{}, false, nil
	}
	return sourceSnapshot{}, false, fmt.Errorf("application source remained unreadable for %s: %w", reader.tolerance, reader.lastError)
}

func validateSourceWatchDurations(pollInterval, debounce, errorTolerance time.Duration) error {
	if pollInterval <= 0 {
		return errors.New("source watch poll interval must be positive")
	}
	if debounce < 0 {
		return errors.New("source watch debounce must not be negative")
	}
	if errorTolerance < 0 {
		return errors.New("source watch error tolerance must not be negative")
	}
	return nil
}

func isWatchedSourcePath(relative string) bool {
	if relative == generatedViewSource {
		return false
	}
	if isRootSourceFile(relative) {
		return true
	}
	return strings.HasSuffix(relative, ".go") ||
		strings.HasSuffix(relative, ".forge.html") ||
		strings.HasSuffix(relative, ".sql")
}

func isRootSourceFile(relative string) bool {
	switch relative {
	case "forge.yaml", "go.mod", "go.sum", ".env":
		return true
	default:
		return false
	}
}

func writeSnapshotField(digest interface{ Write([]byte) (int, error) }, value []byte) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = digest.Write(size[:])
	_, _ = digest.Write(value)
}

func stopAndDrainTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}
