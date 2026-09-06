package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type recordedProcess struct {
	called bool
	ctx    context.Context
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	name   string
	args   []string
	err    error
}

type processCall struct {
	name string
	args []string
}

type processSequence struct {
	calls []processCall
}

func (sequence *processSequence) Run(_ context.Context, _ io.Reader, _, _ io.Writer, name string, args ...string) error {
	sequence.calls = append(sequence.calls, processCall{name: name, args: append([]string(nil), args...)})
	return nil
}

func (process *recordedProcess) Run(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	name string,
	args ...string,
) error {
	process.called = true
	process.ctx = ctx
	process.stdin = stdin
	process.stdout = stdout
	process.stderr = stderr
	process.name = name
	process.args = append([]string(nil), args...)
	return process.err
}

func TestServeRunsGeneratedServerWithProcessStreams(t *testing.T) {
	project := projectDirectory(t)
	t.Chdir(project)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdin := strings.NewReader("input")
	var stdout, stderr bytes.Buffer
	process := &recordedProcess{}

	if err := run(ctx, []string{"serve"}, stdin, &stdout, &stderr, process); err != nil {
		t.Fatal(err)
	}
	if !process.called {
		t.Fatal("expected server process to run")
	}
	if process.ctx != ctx || process.stdin != stdin || process.stdout != &stdout || process.stderr != &stderr {
		t.Fatal("project command did not preserve its context and streams")
	}
	if process.name != "go" || !reflect.DeepEqual(process.args, []string{"run", "./cmd/server"}) {
		t.Fatalf("unexpected process: %s %v", process.name, process.args)
	}
}

func TestMigrateRunsGeneratedConsole(t *testing.T) {
	t.Chdir(projectDirectory(t))
	process := &recordedProcess{}

	if err := run(context.Background(), []string{"migrate"}, strings.NewReader(""), io.Discard, io.Discard, process); err != nil {
		t.Fatal(err)
	}
	if process.name != "go" || !reflect.DeepEqual(process.args, []string{"run", "./cmd/console", "migrate"}) {
		t.Fatalf("unexpected process: %s %v", process.name, process.args)
	}
}

func TestQueueCommandsDelegateForSupportedFormats(t *testing.T) {
	for _, version := range []string{"6", "7", "8"} {
		t.Run(version, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: "+version+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Chdir(directory)
			sequence := &processSequence{}
			for _, args := range [][]string{
				{"queue:work"},
				{"queue:failed"},
				{"queue:retry", "job-id"},
				{"queue:retry", "--all"},
				{"queue:forget", "job-id"},
			} {
				if err := run(context.Background(), args, nil, io.Discard, io.Discard, sequence); err != nil {
					t.Fatalf("run(%v): %v", args, err)
				}
			}
			want := []processCall{
				{name: "go", args: []string{"run", "./cmd/worker"}},
				{name: "go", args: []string{"run", "./cmd/console", "queue:failed"}},
				{name: "go", args: []string{"run", "./cmd/console", "queue:retry", "job-id"}},
				{name: "go", args: []string{"run", "./cmd/console", "queue:retry", "--all"}},
				{name: "go", args: []string{"run", "./cmd/console", "queue:forget", "job-id"}},
			}
			if !reflect.DeepEqual(sequence.calls, want) {
				t.Fatalf("queue process calls = %#v, want %#v", sequence.calls, want)
			}
			if err := run(context.Background(), []string{"queue:forget", "--all"}, nil, io.Discard, io.Discard, sequence); err == nil || err.Error() != "usage: forge queue:forget <id>" {
				t.Fatalf("queue:forget --all error = %v", err)
			}
			if len(sequence.calls) != len(want) {
				t.Fatal("invalid queue command spawned a process")
			}
		})
	}
}

func TestQueueCommandsRefuseOtherProjectFormatsBeforeSpawning(t *testing.T) {
	for _, version := range []string{"5", "9"} {
		t.Run(version, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: "+version+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Chdir(directory)
			process := &recordedProcess{}
			err := run(context.Background(), []string{"queue:failed"}, nil, io.Discard, io.Discard, process)
			if err == nil || !strings.Contains(err.Error(), "format") {
				t.Fatalf("format %s error = %v", version, err)
			}
			if process.called {
				t.Fatal("format refusal spawned a process")
			}
		})
	}
}

func TestFormatFiveViewCommandsDelegateToApplicationCompiler(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(directory+string(os.PathSeparator)+"forge.yaml", []byte("version: 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	sequence := &processSequence{}
	if err := run(context.Background(), []string{"views:compile", "--check"}, nil, io.Discard, io.Discard, sequence); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"serve"}, nil, io.Discard, io.Discard, sequence); err != nil {
		t.Fatal(err)
	}
	want := []processCall{
		{name: "go", args: []string{"run", "./cmd/views", "--check"}},
		{name: "go", args: []string{"run", "./cmd/views"}},
		{name: "go", args: []string{"run", "./cmd/server"}},
	}
	if !reflect.DeepEqual(sequence.calls, want) {
		t.Fatalf("process calls = %#v, want %#v", sequence.calls, want)
	}
}

func TestFormatFourViewCompileRetainsLegacyPath(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module example.com/legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	views := filepath.Join(directory, "resources", "views")
	if err := os.MkdirAll(views, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(views, "page.forge.html"), []byte("Hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	process := &recordedProcess{}
	if err := run(context.Background(), []string{"views:compile"}, nil, io.Discard, io.Discard, process); err != nil {
		t.Fatal(err)
	}
	if process.called {
		t.Fatal("format-4 view compilation unexpectedly delegated to a process")
	}
	artifactPath := filepath.Join(views, "views_gen.go")
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("legacy compiler did not publish artifact: %v", err)
	}
	artifact, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(artifact), "Mappings:") || strings.Contains(string(artifact), "SourceMapping") {
		t.Fatalf("format-4 artifact requires v0.5-only runtime declarations:\n%s", artifact)
	}
}

func TestProjectCommandsRequireProjectRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	process := &recordedProcess{}

	err := run(context.Background(), []string{"serve"}, strings.NewReader(""), io.Discard, io.Discard, process)
	if err == nil || !strings.Contains(err.Error(), "forge.yaml not found") {
		t.Fatalf("expected project root error, got %v", err)
	}
	if process.called {
		t.Fatal("process ran outside a GoForge project")
	}
}

func TestProjectCommandsRejectArguments(t *testing.T) {
	process := &recordedProcess{}
	tests := []struct {
		args  []string
		usage string
	}{
		{args: []string{"serve", "--watch"}, usage: "usage: forge serve"},
		{args: []string{"migrate", "--force"}, usage: "usage: forge migrate"},
		{args: []string{"views:compile", "--write"}, usage: "usage: forge views:compile [--check]"},
	}
	for _, test := range tests {
		err := run(context.Background(), test.args, strings.NewReader(""), io.Discard, io.Discard, process)
		if err == nil || err.Error() != test.usage {
			t.Errorf("run(%v): expected %q, got %v", test.args, test.usage, err)
		}
	}
	if process.called {
		t.Fatal("invalid project command ran a process")
	}
}

func TestProjectCommandReturnsProcessError(t *testing.T) {
	t.Chdir(projectDirectory(t))
	want := errors.New("process failed")
	process := &recordedProcess{err: want}

	err := run(context.Background(), []string{"migrate"}, strings.NewReader(""), io.Discard, io.Discard, process)
	if !errors.Is(err, want) {
		t.Fatalf("expected process error, got %v", err)
	}
}

func TestExecProcessRunnerStopsDescendantTreeOnCancellation(t *testing.T) {
	if !processTreeControlSupported {
		t.Skip("process-tree control is unsupported on this platform")
	}
	marker := filepath.Join(t.TempDir(), "descendant-heartbeat")
	t.Setenv("GOFORGE_PROCESS_TREE_HELPER", "parent")
	t.Setenv("GOFORGE_PROCESS_TREE_MARKER", marker)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- (execProcessRunner{}).Run(ctx, nil, io.Discard, io.Discard, os.Args[0], "-test.run=^TestProcessTreeHelper$")
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("descendant helper did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runner error = %v, want context cancellation", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("cancelled process tree did not stop")
	}
	time.Sleep(200 * time.Millisecond)
	first, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	second, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("descendant remained alive after runner cancellation")
	}
}

func TestProcessTreeHelper(t *testing.T) {
	role := os.Getenv("GOFORGE_PROCESS_TREE_HELPER")
	if role == "" {
		return
	}
	if role == "parent" {
		command := exec.Command(os.Args[0], "-test.run=^TestProcessTreeHelper$")
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "GOFORGE_PROCESS_TREE_HELPER=") {
				command.Env = append(command.Env, entry)
			}
		}
		command.Env = append(command.Env, "GOFORGE_PROCESS_TREE_HELPER=child")
		if err := command.Run(); err != nil {
			os.Exit(2)
		}
		return
	}
	marker := os.Getenv("GOFORGE_PROCESS_TREE_MARKER")
	ignoreProcessTreeGracefulSignal()
	for counter := 0; ; counter++ {
		if err := os.WriteFile(marker, []byte(fmt.Sprintf("%d", counter)), 0o600); err != nil {
			os.Exit(3)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestHelpListsProjectCommands(t *testing.T) {
	var output bytes.Buffer
	if err := run(context.Background(), []string{"help"}, strings.NewReader(""), &output, io.Discard, &recordedProcess{}); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"forge serve", "forge migrate", "forge views:compile [--check]", "forge make:component", "forge make:job", "forge queue:work", "forge queue:failed", "forge queue:retry", "forge queue:forget <id>"} {
		if !strings.Contains(output.String(), command) {
			t.Errorf("help does not list %q", command)
		}
	}
}

func projectDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(directory+string(os.PathSeparator)+"forge.yaml", []byte("version: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return directory
}
