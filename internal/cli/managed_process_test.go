package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestManagedProcessCachesExitResult(t *testing.T) {
	requireManagedProcesses(t)

	process := startManagedProcessHelper(t, "exit", "23", managedProcessSpec{})
	select {
	case <-process.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("managed process did not exit")
	}

	first := process.Wait()
	second := process.Wait()
	var exitError *exec.ExitError
	if !errors.As(first, &exitError) || exitError.ExitCode() != 23 {
		t.Fatalf("exit error = %v, want status 23", first)
	}
	if first != second {
		t.Fatalf("cached errors differ: first=%p second=%p", first, second)
	}
	if err := process.Stop(); err != nil {
		t.Fatalf("stop exited process: %v", err)
	}
}

func TestManagedProcessUsesExplicitDirectoryEnvironmentAndStreams(t *testing.T) {
	requireManagedProcesses(t)

	directory := t.TempDir()
	var stdout, stderr bytes.Buffer
	spec := managedProcessSpec{
		Directory: directory,
		Environment: append(os.Environ(),
			"GOFORGE_MANAGED_VALUE=explicit-value",
		),
		Stdin:  strings.NewReader("explicit-input"),
		Stdout: &stdout,
		Stderr: &stderr,
	}
	process := startManagedProcessHelper(t, "inspect", "", spec)
	if err := process.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}

	wantDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		wantDirectory = directory
	}
	got, _, _ := strings.Cut(stdout.String(), "\n")
	parts := strings.Split(got, "|")
	if len(parts) != 3 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	gotDirectory, err := filepath.EvalSymlinks(parts[0])
	if err != nil {
		gotDirectory = parts[0]
	}
	if !samePath(gotDirectory, wantDirectory) {
		t.Fatalf("directory = %q, want %q", parts[0], directory)
	}
	if parts[1] != "explicit-value" || parts[2] != "explicit-input" {
		t.Fatalf("environment/input = %q|%q", parts[1], parts[2])
	}
	if stderr.String() != "explicit-stderr" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestManagedProcessStopIsBoundedAndIdempotent(t *testing.T) {
	requireManagedProcesses(t)

	marker := filepath.Join(t.TempDir(), "started")
	process := startManagedProcessHelper(t, "block", marker, managedProcessSpec{})
	waitForManagedProcessMarker(t, marker)

	started := time.Now()
	results := make(chan error, 2)
	var callers sync.WaitGroup
	callers.Add(2)
	for range 2 {
		go func() {
			defer callers.Done()
			results <- process.Stop()
		}()
	}
	callers.Wait()
	close(results)
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Fatalf("Stop took %s", elapsed)
	}
	for err := range results {
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}
	if err := process.Stop(); err != nil {
		t.Fatalf("repeated Stop: %v", err)
	}
	select {
	case <-process.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("stopped process did not finish")
	}
	if process.Wait() == nil {
		t.Fatal("stopped process unexpectedly reported a successful exit")
	}
}

func TestManagedProcessReportsStartFailure(t *testing.T) {
	requireManagedProcesses(t)

	missing := filepath.Join(t.TempDir(), "missing", "working-directory")
	_, err := startManagedProcess(managedProcessSpec{
		Name:      managedProcessExecutable(t),
		Arguments: []string{"-test.run=^TestManagedProcessHelper$"},
		Directory: missing,
	})
	if err == nil || !strings.Contains(err.Error(), "start managed process") {
		t.Fatalf("start error = %v", err)
	}
	if _, err := startManagedProcess(managedProcessSpec{}); err == nil {
		t.Fatal("empty command unexpectedly started")
	}
}

func startManagedProcessHelper(t *testing.T, mode, value string, base managedProcessSpec) *managedProcess {
	t.Helper()
	base.Name = managedProcessExecutable(t)
	base.Arguments = []string{"-test.run=^TestManagedProcessHelper$"}
	base.Environment = append(base.Environment,
		"GOFORGE_MANAGED_HELPER="+mode,
		"GOFORGE_MANAGED_HELPER_VALUE="+value,
	)
	process, err := startManagedProcess(base)
	if err != nil {
		t.Fatalf("start managed process: %v", err)
	}
	t.Cleanup(func() {
		_ = process.Stop()
	})
	return process
}

func managedProcessExecutable(t *testing.T) string {
	t.Helper()
	executable, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	return executable
}

func requireManagedProcesses(t *testing.T) {
	t.Helper()
	if !processTreeControlSupported {
		t.Skip("managed process-tree control is unsupported on this platform")
	}
}

func waitForManagedProcessMarker(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("managed process helper did not start")
}

func samePath(left, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if os.PathSeparator == '\\' {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func TestManagedProcessHelper(t *testing.T) {
	mode := os.Getenv("GOFORGE_MANAGED_HELPER")
	if mode == "" {
		return
	}
	value := os.Getenv("GOFORGE_MANAGED_HELPER_VALUE")
	switch mode {
	case "exit":
		code, err := strconv.Atoi(value)
		if err != nil {
			os.Exit(254)
		}
		os.Exit(code)
	case "inspect":
		directory, err := os.Getwd()
		if err != nil {
			os.Exit(253)
		}
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(252)
		}
		fmt.Fprintf(os.Stdout, "%s|%s|%s\n", directory, os.Getenv("GOFORGE_MANAGED_VALUE"), input)
		fmt.Fprint(os.Stderr, "explicit-stderr")
	case "block":
		if err := os.WriteFile(value, []byte("ready"), 0o644); err != nil {
			os.Exit(251)
		}
		select {}
	default:
		os.Exit(250)
	}
}
