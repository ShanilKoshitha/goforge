package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGeneratorLockWaitHonorsContextCancellation(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: 8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)

	first, err := acquireGeneratorLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := first.Close(); err != nil {
			t.Errorf("release first lock: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	second, err := acquireGeneratorLock(ctx)
	if second != nil {
		_ = second.Close()
		t.Fatal("overlapping generator lock unexpectedly succeeded")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock wait error = %v, want context deadline", err)
	}
}

func TestMakeCommandWaitsForProjectGeneratorLock(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: 8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)

	held, err := acquireGeneratorLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := held.Close(); err != nil {
			t.Errorf("release held lock: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	var output bytes.Buffer
	err = RunContext(ctx, []string{"make:controller", "Blocked"}, bytes.NewReader(nil), &output, &output)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("make command error = %v, want context deadline", err)
	}
	if _, err := os.Stat(filepath.Join("internal", "http", "controllers", "blocked_controller.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("blocked generator wrote source: %v", err)
	}
}
