package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"testing"
)

func TestRunCLIPropagatesChildExitCodeWithoutDiagnostic(t *testing.T) {
	exitError := processExitError(t, 37)
	var stderr bytes.Buffer

	code := runCLI(context.Background(), []string{"test"}, nil, io.Discard, &stderr,
		func(context.Context, []string, io.Reader, io.Writer, io.Writer) error {
			return exitError
		})

	if code != 37 {
		t.Fatalf("exit code = %d, want 37", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("redundant child diagnostic = %q", stderr.String())
	}
}

func TestRunCLIRecognizesWrappedChildExitError(t *testing.T) {
	exitError := processExitError(t, 23)
	var stderr bytes.Buffer

	code := runCLI(context.Background(), nil, nil, io.Discard, &stderr,
		func(context.Context, []string, io.Reader, io.Writer, io.Writer) error {
			return fmt.Errorf("run project command: %w", exitError)
		})

	if code != 23 {
		t.Fatalf("exit code = %d, want 23", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("redundant child diagnostic = %q", stderr.String())
	}
}

func TestRunCLIPrintsFrameworkErrorOnce(t *testing.T) {
	var stderr bytes.Buffer

	code := runCLI(context.Background(), nil, nil, io.Discard, &stderr,
		func(context.Context, []string, io.Reader, io.Writer, io.Writer) error {
			return errors.New("forge.yaml not found")
		})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if got, want := stderr.String(), "error: forge.yaml not found\n"; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestRunCLIHandlesCallerCancellationQuietly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer
	code := runCLI(ctx, []string{"serve"}, nil, io.Discard, &stderr,
		func(context.Context, []string, io.Reader, io.Writer, io.Writer) error {
			return fmt.Errorf("stop development server: %w", context.Canceled)
		})
	if code != 0 {
		t.Fatalf("exit code = %d, want graceful cancellation", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("cancellation diagnostic = %q", stderr.String())
	}
}

func TestRunCLIForwardsContextArgumentsAndStreams(t *testing.T) {
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("key"), "value")
	args := []string{"test", "--", "-run", "TestSelected"}
	stdin := bytes.NewBufferString("stdin")
	var stdout, stderr bytes.Buffer
	called := false

	code := runCLI(ctx, args, stdin, &stdout, &stderr,
		func(gotContext context.Context, gotArgs []string, gotStdin io.Reader, gotStdout, gotStderr io.Writer) error {
			called = true
			if gotContext != ctx || gotStdin != stdin || gotStdout != &stdout || gotStderr != &stderr {
				t.Fatal("runCLI did not preserve its context and streams")
			}
			if !reflect.DeepEqual(gotArgs, args) {
				t.Fatalf("arguments = %#v, want %#v", gotArgs, args)
			}
			return nil
		})

	if code != 0 || !called {
		t.Fatalf("exit code = %d, called = %t", code, called)
	}
}

func processExitError(t *testing.T, code int) *exec.ExitError {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestRunCLIExitHelper$")
	command.Env = append(os.Environ(), "GOFORGE_MAIN_EXIT_HELPER="+strconv.Itoa(code))
	err := command.Run()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("helper error = %v, want *exec.ExitError", err)
	}
	return exitError
}

func TestRunCLIExitHelper(t *testing.T) {
	raw := os.Getenv("GOFORGE_MAIN_EXIT_HELPER")
	if raw == "" {
		return
	}
	code, err := strconv.Atoi(raw)
	if err != nil {
		os.Exit(255)
	}
	os.Exit(code)
}
