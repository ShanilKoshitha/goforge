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
	"time"
)

const generatedViewSource = "resources/views/views_gen.go"

var excludedSourceDirectories = map[string]struct{}{
	".git":         {},
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
	if pollInterval <= 0 {
		return sourceSnapshot{}, errors.New("source watch poll interval must be positive")
	}
	if debounce < 0 {
		return sourceSnapshot{}, errors.New("source watch debounce must not be negative")
	}

	poll := time.NewTicker(pollInterval)
	defer poll.Stop()
	var quiet *time.Timer
	var quietChannel <-chan time.Time
	defer func() {
		if quiet != nil {
			quiet.Stop()
		}
	}()

	var candidate sourceSnapshot
	changed := false
	observe := func() error {
		current, err := takeSourceSnapshot(root)
		if err != nil {
			return err
		}
		if current == previous {
			changed = false
			quietChannel = nil
			if quiet != nil {
				stopAndDrainTimer(quiet)
			}
			return nil
		}
		if changed && current == candidate {
			return nil
		}
		candidate = current
		changed = true
		if debounce == 0 {
			return nil
		}
		if quiet == nil {
			quiet = time.NewTimer(debounce)
		} else {
			stopAndDrainTimer(quiet)
			quiet.Reset(debounce)
		}
		quietChannel = quiet.C
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return sourceSnapshot{}, ctx.Err()
		case <-poll.C:
			if err := observe(); err != nil {
				return sourceSnapshot{}, err
			}
			if debounce == 0 && changed {
				return candidate, nil
			}
		case <-quietChannel:
			// Verify once more at the publication boundary. An edit may have
			// landed after the last poll but before the quiet timer fired.
			current, err := takeSourceSnapshot(root)
			if err != nil {
				return sourceSnapshot{}, err
			}
			if current == candidate && current != previous {
				return current, nil
			}
			quietChannel = nil
			if current == previous {
				changed = false
				continue
			}
			candidate = current
			changed = true
			quiet.Reset(debounce)
			quietChannel = quiet.C
		}
	}
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
