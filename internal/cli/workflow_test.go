package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

type workflowProcessCall struct {
	ctx    context.Context
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	name   string
	args   []string
}

type workflowProcess struct {
	calls        []workflowProcessCall
	errors       map[int]error
	beforeRun    map[int]func(workflowProcessCall)
	afterRun     map[int]func()
	buildContent []byte
}

type blockingWorkflowProcess struct{}

func (blockingWorkflowProcess) Run(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	name string,
	args ...string,
) error {
	if name == "go" && reflect.DeepEqual(args, []string{"run", "./cmd/views", "--check"}) {
		return nil
	}
	if name == "go" && reflect.DeepEqual(args, []string{"run", "./cmd/assets"}) {
		return nil
	}
	if name == "go" && len(args) >= 4 && args[0] == "-C" && args[2] == "run" {
		return nil
	}
	if name != "go" || len(args) != 7 || args[0] != "-C" || args[2] != "build" || args[4] != "-o" {
		return errors.New("unexpected blocking workflow command")
	}
	if err := os.WriteFile(args[5], []byte("completed but unpublished build"), 0o755); err != nil {
		return err
	}
	return execProcessRunner{}.Run(
		ctx, stdin, stdout, stderr,
		os.Args[0], "-test.run=^TestForgeBuildCancellationHelper$",
	)
}

func (process *workflowProcess) Run(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	name string,
	args ...string,
) error {
	call := workflowProcessCall{
		ctx: ctx, stdin: stdin, stdout: stdout, stderr: stderr,
		name: name, args: append([]string(nil), args...),
	}
	process.calls = append(process.calls, call)
	index := len(process.calls) - 1
	if callback := process.beforeRun[index]; callback != nil {
		callback(call)
	}
	if output := workflowBuildOutput(args); output != "" && process.buildContent != nil {
		if err := os.WriteFile(output, process.buildContent, 0o755); err != nil {
			return err
		}
	}
	if callback := process.afterRun[index]; callback != nil {
		callback()
	}
	return process.errors[index]
}

func workflowBuildOutput(args []string) string {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == "-o" {
			return args[index+1]
		}
	}
	return ""
}

func TestForgeTestChecksArtifactsThenRunsConventionalGoTest(t *testing.T) {
	directory := workflowProject(t)
	t.Chdir(directory)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdin := strings.NewReader("input")
	var stdout, stderr bytes.Buffer
	process := &workflowProcess{}

	if err := run(ctx, []string{"test"}, stdin, &stdout, &stderr, process); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"run", "./cmd/views", "--check"}, {"run", "./cmd/assets"}, {"test", "./..."}}
	if len(process.calls) != len(want) {
		t.Fatalf("process calls = %d, want %d: %+v", len(process.calls), len(want), process.calls)
	}
	for index, call := range process.calls {
		if call.ctx != ctx || call.stdin != stdin || call.stdout != &stdout || call.stderr != &stderr {
			t.Fatalf("call %d did not preserve context and streams", index)
		}
		if call.name != "go" || !reflect.DeepEqual(call.args, want[index]) {
			t.Fatalf("call %d = %s %v, want go %v", index, call.name, call.args, want[index])
		}
	}
	if !strings.Contains(stdout.String(), generatedORMPath+" is current") {
		t.Fatalf("ORM freshness result was not visible:\n%s", stdout.String())
	}
}

func TestAssetCheckDelegatesToApplicationOwnedValidator(t *testing.T) {
	directory := workflowProject(t)
	t.Chdir(directory)
	ctx := context.Background()
	stdin := strings.NewReader("input")
	var stdout, stderr bytes.Buffer
	process := &workflowProcess{}

	if err := run(ctx, []string{"assets:check"}, stdin, &stdout, &stderr, process); err != nil {
		t.Fatal(err)
	}
	if len(process.calls) != 1 || process.calls[0].name != "go" || !reflect.DeepEqual(process.calls[0].args, []string{"run", "./cmd/assets"}) {
		t.Fatalf("asset check calls = %+v", process.calls)
	}
	if process.calls[0].ctx != ctx || process.calls[0].stdin != stdin || process.calls[0].stdout != &stdout || process.calls[0].stderr != &stderr {
		t.Fatal("asset check did not preserve context and streams")
	}
}

func TestAssetCheckRejectsArgumentsAndOtherFormatsBeforeSpawning(t *testing.T) {
	t.Run("arguments", func(t *testing.T) {
		process := &workflowProcess{}
		err := run(context.Background(), []string{"assets:check", "extra"}, nil, io.Discard, io.Discard, process)
		if err == nil || err.Error() != "usage: forge assets:check" || len(process.calls) != 0 {
			t.Fatalf("argument refusal = %v calls=%+v", err, process.calls)
		}
	})
	for _, version := range []string{"11", "14"} {
		t.Run("format "+version, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: "+version+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Chdir(directory)
			process := &workflowProcess{}
			err := run(context.Background(), []string{"assets:check"}, nil, io.Discard, io.Discard, process)
			if err == nil || !strings.Contains(err.Error(), "format") || len(process.calls) != 0 {
				t.Fatalf("format %s refusal = %v calls=%+v", version, err, process.calls)
			}
		})
	}
}

func TestForgeBuildStagesAndRepeatedlyReplacesCanonicalArtifact(t *testing.T) {
	directory := workflowProject(t)
	t.Chdir(directory)
	var output bytes.Buffer
	process := &workflowProcess{buildContent: []byte("first build")}

	if err := run(context.Background(), []string{"build"}, nil, &output, &output, process); err != nil {
		t.Fatal(err)
	}
	destination := workflowBuildDestination()
	assertFileContent(t, destination, "first build")
	assertBuildCall(t, process.calls, 4)
	assertNoTemporaryBuilds(t)
	if !strings.Contains(output.String(), "built "+filepath.ToSlash(destination)) {
		t.Fatalf("build output omits canonical artifact:\n%s", output.String())
	}

	process.buildContent = []byte("second build")
	if err := run(context.Background(), []string{"build"}, nil, &output, &output, process); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, destination, "second build")
	assertBuildCall(t, process.calls, 9)
	assertNoTemporaryBuilds(t)
}

func TestForgeBuildFailurePreservesLastGoodArtifact(t *testing.T) {
	directory := workflowProject(t)
	t.Chdir(directory)
	destination := workflowBuildDestination()
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("last good"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := errors.New("compiler failed")
	process := &workflowProcess{
		errors:       map[int]error{4: want},
		buildContent: []byte("incomplete replacement"),
	}

	err := run(context.Background(), []string{"build"}, nil, io.Discard, io.Discard, process)
	if !errors.Is(err, want) {
		t.Fatalf("build error = %v, want compiler error", err)
	}
	assertFileContent(t, destination, "last good")
	assertBuildCall(t, process.calls, 4)
	assertNoTemporaryBuilds(t)
}

func TestForgeBuildCompilesIsolatedValidatedSourceDuringABAEdit(t *testing.T) {
	directory := workflowProject(t)
	t.Chdir(directory)
	process := &workflowProcess{
		beforeRun: map[int]func(workflowProcessCall){4: func(call workflowProcessCall) {
			unsafe := filepath.FromSlash("resources/assets/files/unsafe.svg")
			if err := os.WriteFile(unsafe, []byte("<svg/>"), 0o644); err != nil {
				t.Error(err)
			}
			defer os.Remove(unsafe)
			if _, err := os.Stat(filepath.Join(call.args[1], unsafe)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("isolated build source observed ABA edit: %v", err)
			}
		}},
		buildContent: []byte("validated isolated generation"),
	}
	if err := run(context.Background(), []string{"build"}, nil, io.Discard, io.Discard, process); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, workflowBuildDestination(), "validated isolated generation")
	assertBuildCall(t, process.calls, 4)
}

func TestForgeBuildSuccessWithoutArtifactPreservesLastGoodArtifact(t *testing.T) {
	directory := workflowProject(t)
	t.Chdir(directory)
	destination := workflowBuildDestination()
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("last good"), 0o755); err != nil {
		t.Fatal(err)
	}
	process := &workflowProcess{}

	err := run(context.Background(), []string{"build"}, nil, io.Discard, io.Discard, process)
	if err == nil || !strings.Contains(err.Error(), "inspect temporary build artifact") {
		t.Fatalf("missing artifact error = %v", err)
	}
	assertFileContent(t, destination, "last good")
	assertBuildCall(t, process.calls, 4)
	assertNoTemporaryBuilds(t)
}

func TestForgeBuildCancellationAfterCompilationPreservesLastGoodArtifact(t *testing.T) {
	directory := workflowProject(t)
	t.Chdir(directory)
	destination := workflowBuildDestination()
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("last good"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	process := &workflowProcess{
		afterRun:     map[int]func(){4: cancel},
		buildContent: []byte("cancelled replacement"),
	}

	err := run(ctx, []string{"build"}, nil, io.Discard, io.Discard, process)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled build error = %v", err)
	}
	assertFileContent(t, destination, "last good")
	assertBuildCall(t, process.calls, 4)
	assertNoTemporaryBuilds(t)
}

func TestForgeBuildRealProcessCancellationPreservesLastGoodArtifact(t *testing.T) {
	directory := workflowProject(t)
	t.Chdir(directory)
	destination := workflowBuildDestination()
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("last good"), 0o755); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	t.Setenv("GOFORGE_WORKFLOW_BUILD_HELPER_READY", ready)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- run(ctx, []string{"build"}, nil, io.Discard, io.Discard, blockingWorkflowProcess{})
	}()
	waitForWorkflowHelperOrResult(t, ready, result)
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancelled real build process returned success")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled real build process did not stop")
	}
	assertFileContent(t, destination, "last good")
	assertNoTemporaryBuilds(t)
}

func waitForWorkflowHelperOrResult(t *testing.T, ready string, result <-chan error) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case err := <-result:
			t.Fatalf("build ended before cancellation helper started: %v", err)
		case <-deadline.C:
			t.Fatal("build cancellation helper did not start")
		case <-poll.C:
			if _, err := os.Stat(ready); err == nil {
				return
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
		}
	}
}

func TestForgeBuildCancellationHelper(t *testing.T) {
	ready := os.Getenv("GOFORGE_WORKFLOW_BUILD_HELPER_READY")
	if ready == "" {
		return
	}
	if err := os.WriteFile(ready, []byte("ready"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {}
}

func TestForgeWorkflowsStopAtNonMutatingArtifactPreflights(t *testing.T) {
	t.Run("stale ORM", func(t *testing.T) {
		directory := workflowProject(t)
		t.Chdir(directory)
		stale := []byte("// Code generated by GoForge. DO NOT EDIT.\n\npackage models\n")
		if err := os.WriteFile(filepath.FromSlash(generatedORMPath), stale, 0o644); err != nil {
			t.Fatal(err)
		}
		process := &workflowProcess{}
		err := run(context.Background(), []string{"test"}, nil, io.Discard, io.Discard, process)
		if err == nil || !strings.Contains(err.Error(), "generated ORM is stale") {
			t.Fatalf("stale ORM error = %v", err)
		}
		if len(process.calls) != 0 {
			t.Fatalf("stale ORM spawned processes: %+v", process.calls)
		}
		current, readErr := os.ReadFile(filepath.FromSlash(generatedORMPath))
		if readErr != nil || !bytes.Equal(current, stale) {
			t.Fatalf("ORM preflight mutated stale artifact: %v %q", readErr, current)
		}
		assertNoBuildDirectory(t)
	})

	t.Run("stale views", func(t *testing.T) {
		directory := workflowProject(t)
		t.Chdir(directory)
		want := errors.New("views are stale")
		process := &workflowProcess{errors: map[int]error{0: want}}
		err := run(context.Background(), []string{"build"}, nil, io.Discard, io.Discard, process)
		if !errors.Is(err, want) {
			t.Fatalf("view preflight error = %v", err)
		}
		if len(process.calls) != 1 || !reflect.DeepEqual(process.calls[0].args, []string{"run", "./cmd/views", "--check"}) {
			t.Fatalf("view failure calls = %+v", process.calls)
		}
		assertNoBuildDirectory(t)
	})

	t.Run("invalid assets", func(t *testing.T) {
		directory := workflowProject(t)
		t.Chdir(directory)
		want := errors.New("assets are invalid")
		process := &workflowProcess{errors: map[int]error{1: want}}
		err := run(context.Background(), []string{"build"}, nil, io.Discard, io.Discard, process)
		if !errors.Is(err, want) {
			t.Fatalf("asset preflight error = %v", err)
		}
		if len(process.calls) != 2 || !reflect.DeepEqual(process.calls[1].args, []string{"run", "./cmd/assets"}) {
			t.Fatalf("asset failure calls = %+v", process.calls)
		}
		assertNoBuildDirectory(t)
	})
}

func TestForgeWorkflowsRejectArgumentsRootsAndUnsupportedFormatsBeforeWork(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "test arguments", args: []string{"test", "-race"}, want: "usage: forge test"},
		{name: "build arguments", args: []string{"build", "./cmd/worker"}, want: "usage: forge build"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			process := &workflowProcess{}
			err := run(context.Background(), test.args, nil, io.Discard, io.Discard, process)
			if err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if len(process.calls) != 0 {
				t.Fatalf("invalid arguments spawned processes: %+v", process.calls)
			}
		})
	}

	t.Run("outside root", func(t *testing.T) {
		t.Chdir(t.TempDir())
		process := &workflowProcess{}
		err := run(context.Background(), []string{"test"}, nil, io.Discard, io.Discard, process)
		if err == nil || !strings.Contains(err.Error(), "forge.yaml not found") {
			t.Fatalf("root error = %v", err)
		}
		if len(process.calls) != 0 {
			t.Fatalf("missing root spawned processes: %+v", process.calls)
		}
	})

	for _, test := range []struct {
		version string
		want    string
	}{
		{version: "3", want: "format"},
		{version: "14", want: "format"},
		{version: "nope", want: "invalid version"},
	} {
		t.Run("format "+test.version, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: "+test.version+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Chdir(directory)
			process := &workflowProcess{}
			err := run(context.Background(), []string{"build"}, nil, io.Discard, io.Discard, process)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("format %s error = %v", test.version, err)
			}
			if len(process.calls) != 0 {
				t.Fatalf("format refusal spawned processes: %+v", process.calls)
			}
			assertNoBuildDirectory(t)
		})
	}
}

func TestForgeWorkflowHonorsCancellationBeforePreflight(t *testing.T) {
	directory := workflowProject(t)
	t.Chdir(directory)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	process := &workflowProcess{}
	err := run(ctx, []string{"build"}, nil, io.Discard, io.Discard, process)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled build error = %v", err)
	}
	if len(process.calls) != 0 {
		t.Fatalf("cancelled build spawned processes: %+v", process.calls)
	}
	assertNoBuildDirectory(t)
}

func TestForgeTestSupportsFrozenLegacyViewCompilerAtFormatFour(t *testing.T) {
	directory := workflowProject(t)
	if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	views := filepath.Join(directory, "resources", "views")
	if err := os.RemoveAll(views); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(views, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(views, "page.forge.html"), []byte("Hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	if err := compileProjectViews(io.Discard); err != nil {
		t.Fatal(err)
	}
	process := &workflowProcess{}
	if err := run(context.Background(), []string{"test"}, nil, io.Discard, io.Discard, process); err != nil {
		t.Fatal(err)
	}
	if len(process.calls) != 1 || process.calls[0].name != "go" || !reflect.DeepEqual(process.calls[0].args, []string{"test", "./..."}) {
		t.Fatalf("format-4 calls = %+v", process.calls)
	}
}

func workflowProject(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "application")
	if err := createProject(newOptions{
		directory: directory,
		module:    "example.com/application",
		replace:   projectRoot(t),
	}); err != nil {
		t.Fatal(err)
	}
	return directory
}

func workflowBuildDestination() string {
	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	return filepath.Join("bin", "app"+extension)
}

func assertBuildCall(t *testing.T, calls []workflowProcessCall, index int) {
	t.Helper()
	if len(calls) <= index {
		t.Fatalf("missing build call %d: %+v", index, calls)
	}
	call := calls[index]
	if call.name != "go" || len(call.args) != 7 || call.args[0] != "-C" || call.args[2] != "build" ||
		!reflect.DeepEqual(call.args[3:5], []string{"-trimpath", "-o"}) || call.args[6] != "./cmd/server" {
		t.Fatalf("build call = %s %v", call.name, call.args)
	}
	temporary := filepath.Clean(call.args[5])
	absoluteBin, err := filepath.Abs("bin")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(temporary) != absoluteBin || !strings.HasPrefix(filepath.Base(temporary), ".goforge-build-") {
		t.Fatalf("build temporary path = %q", call.args[5])
	}
	if runtime.GOOS == "windows" && filepath.Ext(temporary) != ".exe" {
		t.Fatalf("Windows build temporary lacks .exe: %q", temporary)
	}
}

func assertFileContent(t *testing.T, name, want string) {
	t.Helper()
	content, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != want {
		t.Fatalf("%s = %q, want %q", name, content, want)
	}
}

func assertNoTemporaryBuilds(t *testing.T) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("bin", ".goforge-build-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary build artifacts remain: %v", matches)
	}
}

func assertNoBuildDirectory(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("bin"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preflight created build directory: %v", err)
	}
}

func waitForWorkflowHelper(t *testing.T, ready string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); err == nil {
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("build cancellation helper did not start")
}
