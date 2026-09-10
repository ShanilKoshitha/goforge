package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type developmentProcessCall struct {
	stdin io.Reader
	name  string
	args  []string
}

type developmentProcessStub struct {
	mu    sync.Mutex
	calls []developmentProcessCall
	grace time.Duration
	run   func(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error
}

func (process *developmentProcessStub) withProcessTreeGrace(grace time.Duration) processRunner {
	process.mu.Lock()
	process.grace = grace
	process.mu.Unlock()
	return process
}

func (process *developmentProcessStub) Run(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	name string,
	args ...string,
) error {
	process.mu.Lock()
	process.calls = append(process.calls, developmentProcessCall{
		stdin: stdin,
		name:  name,
		args:  append([]string(nil), args...),
	})
	process.mu.Unlock()
	if process.run == nil {
		return nil
	}
	return process.run(ctx, stdin, stdout, stderr, name, args...)
}

func (process *developmentProcessStub) recordedCalls() []developmentProcessCall {
	process.mu.Lock()
	defer process.mu.Unlock()
	return append([]developmentProcessCall(nil), process.calls...)
}

func (process *developmentProcessStub) configuredGrace() time.Duration {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.grace
}

type developmentTestBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *developmentTestBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(value)
}

func (buffer *developmentTestBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}

func TestDevCommandRejectsArgumentsBeforeStartingProcesses(t *testing.T) {
	process := &developmentProcessStub{}
	err := run(context.Background(), []string{"dev", "extra"}, strings.NewReader("input"), io.Discard, io.Discard, process)
	if err == nil || err.Error() != "usage: forge dev" {
		t.Fatalf("error = %v, want usage", err)
	}
	if calls := process.recordedCalls(); len(calls) != 0 {
		t.Fatalf("invalid command started processes: %+v", calls)
	}
}

func TestDevRequiresExactlyFormatElevenBeforeStartingServices(t *testing.T) {
	for _, version := range []string{"10", "12"} {
		t.Run(version, func(t *testing.T) {
			directory := developmentProjectDirectory(t, version)
			t.Chdir(directory)
			process := &developmentProcessStub{}
			var served atomic.Bool
			err := runProjectDevWith(
				context.Background(), nil, io.Discard, io.Discard, process,
				func(context.Context, io.Reader, io.Writer, io.Writer, processRunner) error {
					served.Store(true)
					return nil
				},
			)
			if err == nil || !strings.Contains(err.Error(), "project format version "+version) {
				t.Fatalf("format %s error = %v", version, err)
			}
			if served.Load() {
				t.Fatal("unsupported project started serve")
			}
			if calls := process.recordedCalls(); len(calls) != 0 {
				t.Fatalf("unsupported project started processes: %+v", calls)
			}
		})
	}
}

func TestDevRequiresProjectRootBeforeStartingServices(t *testing.T) {
	t.Chdir(t.TempDir())
	process := &developmentProcessStub{}
	var served atomic.Bool
	err := runProjectDevWith(
		context.Background(), nil, io.Discard, io.Discard, process,
		func(context.Context, io.Reader, io.Writer, io.Writer, processRunner) error {
			served.Store(true)
			return nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "forge.yaml not found") {
		t.Fatalf("error = %v, want project-root refusal", err)
	}
	if served.Load() || len(process.recordedCalls()) != 0 {
		t.Fatal("project-root refusal started a service")
	}
}

func TestDevStartsExactServicesAndWaitsForReadinessBarrier(t *testing.T) {
	t.Chdir(developmentProjectDirectory(t, "11"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stdout := &developmentTestBuffer{}
	stderr := &developmentTestBuffer{}
	stdin := strings.NewReader("serve input")
	serveReady := make(chan struct{})
	workerReady := make(chan struct{})
	schedulerReady := make(chan struct{})
	process := &developmentProcessStub{}
	process.run = func(ctx context.Context, childStdin io.Reader, _ io.Writer, childStderr io.Writer, name string, args ...string) error {
		if childStdin != nil {
			t.Errorf("%s %v received stdin", name, args)
		}
		switch strings.Join(args, " ") {
		case "run ./cmd/worker":
			<-workerReady
			_, _ = io.WriteString(childStderr, "2026/01/01 00:00:00 INFO job worker_")
			_, _ = io.WriteString(childStderr, "started job_id=\"\"\n")
		case "run ./cmd/scheduler":
			<-schedulerReady
			_, _ = io.WriteString(childStderr, "2026/01/01 00:00:00 INFO scheduler started event=scheduler_started mode=work\n")
		default:
			t.Errorf("unexpected process: %s %v", name, args)
		}
		<-ctx.Done()
		return ctx.Err()
	}
	serve := func(ctx context.Context, childStdin io.Reader, childStdout, _ io.Writer, _ processRunner) error {
		if childStdin != stdin {
			t.Error("serve did not receive command stdin")
		}
		<-serveReady
		_, _ = io.WriteString(childStdout, "serving http://127.0.0.1:8080 ")
		_, _ = io.WriteString(childStdout, "(watching for changes)\n")
		<-ctx.Done()
		return nil
	}

	result := make(chan error, 1)
	go func() {
		result <- runProjectDevWith(ctx, stdin, stdout, stderr, process, serve)
	}()
	waitForDevelopmentProcessCalls(t, process, 2)
	close(serveReady)
	waitForDevTestOutput(t, stdout, "[server] serving http://")
	if strings.Contains(stdout.String(), developmentReadyMessage) {
		t.Fatal("development reported ready before worker and scheduler")
	}
	close(workerReady)
	waitForDevTestOutput(t, stderr, "INFO job worker_started")
	if strings.Contains(stdout.String(), developmentReadyMessage) {
		t.Fatal("development reported ready before scheduler")
	}
	close(schedulerReady)
	waitForDevTestOutput(t, stdout, developmentReadyMessage)
	if count := strings.Count(stdout.String(), developmentReadyMessage); count != 1 {
		t.Fatalf("ready message count = %d, output:\n%s", count, stdout.String())
	}
	cancel()
	if err := waitForDevelopmentResult(t, result); err != nil {
		t.Fatalf("parent cancellation error = %v", err)
	}

	calls := process.recordedCalls()
	sort.Slice(calls, func(left, right int) bool {
		return strings.Join(calls[left].args, " ") < strings.Join(calls[right].args, " ")
	})
	want := []developmentProcessCall{
		{name: "go", args: []string{"run", "./cmd/scheduler"}},
		{name: "go", args: []string{"run", "./cmd/worker"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("process calls = %#v, want %#v", calls, want)
	}
	if grace := process.configuredGrace(); grace != developmentServiceProcessTreeGraceDefault {
		t.Fatalf("process-tree grace = %s, want %s", grace, developmentServiceProcessTreeGraceDefault)
	}
	if !strings.Contains(stderr.String(), "[worker] 2026/01/01") || !strings.Contains(stderr.String(), "[scheduler] 2026/01/01") {
		t.Fatalf("service output was not prefixed:\n%s", stderr.String())
	}
}

func TestDevelopmentLineWritersPrefixConcurrentFragmentsAtomically(t *testing.T) {
	destination := &developmentTestBuffer{}
	output := &synchronizedDevelopmentOutput{}
	readiness := newDevelopmentReadiness(output, io.Discard)
	serve := newDevelopmentLineWriter("server", nil, output, destination, readiness)
	worker := newDevelopmentLineWriter("worker", nil, output, destination, readiness)

	if _, err := serve.Write([]byte("serve ")); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.Write([]byte("worker ")); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var writers sync.WaitGroup
	writers.Add(2)
	go func() {
		defer writers.Done()
		<-start
		_, _ = serve.Write([]byte("line\n"))
	}()
	go func() {
		defer writers.Done()
		<-start
		_, _ = worker.Write([]byte("line\n"))
	}()
	close(start)
	writers.Wait()

	lines := strings.Split(strings.TrimSpace(destination.String()), "\n")
	sort.Strings(lines)
	want := []string{"[server] serve line", "[worker] worker line"}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines = %#v, want %#v", lines, want)
	}
}

func TestDevelopmentReadinessRequiresCanonicalLineOnIntendedStream(t *testing.T) {
	stdout := &developmentTestBuffer{}
	stderr := &developmentTestBuffer{}
	output := &synchronizedDevelopmentOutput{}
	readiness := newDevelopmentReadiness(output, stdout)
	serverOut := newDevelopmentLineWriter("server", developmentServerReadyLine, output, stdout, readiness)
	serverErr := newDevelopmentLineWriter("server", nil, output, stderr, readiness)
	workerOut := newDevelopmentLineWriter("worker", nil, output, stdout, readiness)
	workerErr := newDevelopmentLineWriter("worker", developmentWorkerReadyLine, output, stderr, readiness)
	schedulerOut := newDevelopmentLineWriter("scheduler", nil, output, stdout, readiness)
	schedulerErr := newDevelopmentLineWriter("scheduler", developmentSchedulerReadyLine, output, stderr, readiness)

	_, _ = io.WriteString(serverErr, "serving http://127.0.0.1:8080 (watching for changes)\n")
	_, _ = io.WriteString(serverOut, "error: serving http://127.0.0.1:8080 (watching for changes)\n")
	_, _ = io.WriteString(workerOut, "2026/01/01 00:00:00 INFO job worker_started job_id=\"\"\n")
	_, _ = io.WriteString(workerErr, "2026/01/01 00:00:00 INFO job worker_started_later job_id=\"\"\n")
	_, _ = io.WriteString(schedulerOut, "2026/01/01 00:00:00 INFO scheduler started event=scheduler_started mode=work\n")
	_, _ = io.WriteString(schedulerErr, "2026/01/01 00:00:00 INFO scheduler started event=scheduler_started mode=once\n")
	if strings.Contains(stdout.String(), developmentReadyMessage) {
		t.Fatalf("spoofed readiness line crossed the barrier:\n%s", stdout.String())
	}

	_, _ = io.WriteString(serverOut, "serving http://127.0.0.1:8080 (watching for changes)\r\n")
	_, _ = io.WriteString(workerErr, "2026/01/01 00:00:00 INFO job worker_started job_id=\"\"\n")
	_, _ = io.WriteString(schedulerErr, "2026/01/01 00:00:00 INFO scheduler started event=scheduler_started mode=work schedules=1\n")
	if count := strings.Count(stdout.String(), developmentReadyMessage); count != 1 {
		t.Fatalf("canonical readiness count = %d, want 1:\n%s", count, stdout.String())
	}
}

func TestDevelopmentLineWriterTruncatesOneLogicalLineWithoutInterleaving(t *testing.T) {
	destination := &developmentTestBuffer{}
	output := &synchronizedDevelopmentOutput{}
	readiness := newDevelopmentReadiness(output, io.Discard)
	server := newDevelopmentLineWriter("server", nil, output, destination, readiness)
	worker := newDevelopmentLineWriter("worker", nil, output, destination, readiness)

	oversized := strings.Repeat("x", developmentLineBufferMax+1024)
	if _, err := io.WriteString(server, oversized[:developmentLineBufferMax]); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(worker, "other service\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(server, oversized[developmentLineBufferMax:]+" discarded\nrecovered\n"); err != nil {
		t.Fatal(err)
	}

	value := destination.String()
	if count := strings.Count(value, "[server] "); count != 2 {
		t.Fatalf("server line count = %d, want truncated and recovered lines", count)
	}
	if !strings.Contains(value, developmentLineTruncation) || strings.Contains(value, "discarded") {
		t.Fatalf("oversized line was not bounded correctly")
	}
	if !strings.Contains(value, "[worker] other service\n") || !strings.HasSuffix(value, "[server] recovered\n") {
		t.Fatalf("line recovery or atomic prefixing failed")
	}
}

func TestDevelopmentProcessTreeGraceCoversGeneratedShutdownBudgets(t *testing.T) {
	if developmentServiceProcessTreeGraceDefault <= 15*time.Second+100*time.Millisecond {
		t.Fatalf("development process-tree grace = %s, must exceed generated 15s shutdown budget", developmentServiceProcessTreeGraceDefault)
	}
}

func TestDevelopmentProcessTreeGraceIsExplicitlyConfigurable(t *testing.T) {
	t.Setenv(developmentServiceProcessTreeGraceEnv, "31s")
	grace, err := developmentProcessTreeGrace()
	if err != nil || grace != 31*time.Second {
		t.Fatalf("configured grace = %s, %v", grace, err)
	}
	t.Setenv(developmentServiceProcessTreeGraceEnv, "invalid")
	if _, err := developmentProcessTreeGrace(); err == nil || !strings.Contains(err.Error(), developmentServiceProcessTreeGraceEnv) {
		t.Fatalf("invalid grace error = %v", err)
	}
}

func TestDevStartupFailureCancelsAndWaitsForSiblings(t *testing.T) {
	want := errors.New("serve startup failed")
	var cancelled atomic.Int32
	process := &developmentProcessStub{run: func(ctx context.Context, _ io.Reader, _, _ io.Writer, _ string, _ ...string) error {
		<-ctx.Done()
		cancelled.Add(1)
		return ctx.Err()
	}}
	err := superviseDevelopmentServices(
		context.Background(), nil, io.Discard, io.Discard, process,
		func(context.Context, io.Reader, io.Writer, io.Writer, processRunner) error { return want },
	)
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "server service exited before all services were ready") {
		t.Fatalf("error = %v", err)
	}
	if cancelled.Load() != 2 {
		t.Fatalf("cancelled siblings = %d, want 2", cancelled.Load())
	}
}

func TestDevDoesNotReportReadyAfterMarkedServiceAlreadyExited(t *testing.T) {
	process := &developmentProcessStub{}
	process.run = func(ctx context.Context, _ io.Reader, _ io.Writer, stderr io.Writer, _ string, args ...string) error {
		switch strings.Join(args, " ") {
		case "run ./cmd/worker":
			_, _ = io.WriteString(stderr, `time=now level=INFO msg="job worker_started"`+"\n")
			return errors.New("worker exited after its startup marker")
		case "run ./cmd/scheduler":
			<-ctx.Done()
			_, _ = io.WriteString(stderr, `msg="scheduler started" event=scheduler_started mode=work`+"\n")
			return ctx.Err()
		default:
			return errors.New("unexpected process")
		}
	}
	stdout := &developmentTestBuffer{}
	serve := func(ctx context.Context, _ io.Reader, serviceStdout, _ io.Writer, _ processRunner) error {
		<-ctx.Done()
		_, _ = io.WriteString(serviceStdout, "serving http://127.0.0.1:8080 (watching for changes)\n")
		return nil
	}
	result := make(chan error, 1)
	go func() {
		result <- superviseDevelopmentServices(context.Background(), nil, stdout, io.Discard, process, serve)
	}()
	err := waitForDevelopmentResult(t, result)
	if err == nil || !strings.Contains(err.Error(), "before all services were ready") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(stdout.String(), developmentReadyMessage) {
		t.Fatalf("reported ready after a service exited:\n%s", stdout.String())
	}
}

func TestDevSteadyStateExitCancelsSiblingsAndPreservesCause(t *testing.T) {
	want := errors.New("worker stopped")
	releaseWorker := make(chan struct{})
	var siblingsCancelled atomic.Int32
	process := &developmentProcessStub{}
	process.run = func(ctx context.Context, _ io.Reader, _ io.Writer, stderr io.Writer, _ string, args ...string) error {
		switch strings.Join(args, " ") {
		case "run ./cmd/worker":
			_, _ = io.WriteString(stderr, `time=now level=INFO msg="job worker_started"`+"\n")
			<-releaseWorker
			return want
		case "run ./cmd/scheduler":
			_, _ = io.WriteString(stderr, `msg="scheduler started" event=scheduler_started mode=work`+"\n")
			<-ctx.Done()
			siblingsCancelled.Add(1)
			return ctx.Err()
		default:
			return errors.New("unexpected process")
		}
	}
	stdout := &developmentTestBuffer{}
	serve := func(ctx context.Context, _ io.Reader, serviceStdout, _ io.Writer, _ processRunner) error {
		_, _ = io.WriteString(serviceStdout, "serving http://127.0.0.1:8080 (watching for changes)\n")
		<-ctx.Done()
		siblingsCancelled.Add(1)
		return nil
	}
	result := make(chan error, 1)
	go func() {
		result <- superviseDevelopmentServices(context.Background(), nil, stdout, io.Discard, process, serve)
	}()
	waitForDevTestOutput(t, stdout, developmentReadyMessage)
	close(releaseWorker)
	err := waitForDevelopmentResult(t, result)
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "worker service exited after all services were ready") {
		t.Fatalf("error = %v", err)
	}
	if siblingsCancelled.Load() != 2 {
		t.Fatalf("cancelled siblings = %d, want 2", siblingsCancelled.Load())
	}
}

func TestDevParentCancellationWaitsForEveryServiceAndReturnsNil(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 3)
	var completed atomic.Int32
	process := &developmentProcessStub{run: func(ctx context.Context, _ io.Reader, _, _ io.Writer, _ string, _ ...string) error {
		started <- struct{}{}
		<-ctx.Done()
		completed.Add(1)
		return ctx.Err()
	}}
	serve := func(ctx context.Context, _ io.Reader, _, _ io.Writer, _ processRunner) error {
		started <- struct{}{}
		<-ctx.Done()
		completed.Add(1)
		return nil
	}
	result := make(chan error, 1)
	go func() {
		result <- superviseDevelopmentServices(ctx, nil, io.Discard, io.Discard, process, serve)
	}()
	for range 3 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("service did not start")
		}
	}
	cancel()
	if err := waitForDevelopmentResult(t, result); err != nil {
		t.Fatalf("parent cancellation error = %v", err)
	}
	if completed.Load() != 3 {
		t.Fatalf("completed services = %d, want 3", completed.Load())
	}
}

func TestDevParentCancellationReportsChildTreeCleanupFailure(t *testing.T) {
	want := errors.New("child process tree remained alive")
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 3)
	process := &developmentProcessStub{run: func(ctx context.Context, _ io.Reader, _, _ io.Writer, _ string, _ ...string) error {
		started <- struct{}{}
		<-ctx.Done()
		return errors.Join(ctx.Err(), want)
	}}
	serve := func(ctx context.Context, _ io.Reader, _, _ io.Writer, _ processRunner) error {
		started <- struct{}{}
		<-ctx.Done()
		return nil
	}
	result := make(chan error, 1)
	go func() {
		result <- superviseDevelopmentServices(ctx, nil, io.Discard, io.Discard, process, serve)
	}()
	for range 3 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("service did not start")
		}
	}
	cancel()
	err := waitForDevelopmentResult(t, result)
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "service shutdown") {
		t.Fatalf("cleanup error = %v", err)
	}
}

func TestHelpListsDevCommand(t *testing.T) {
	var output bytes.Buffer
	printHelp(&output)
	if !strings.Contains(output.String(), "forge dev") {
		t.Fatalf("help does not list forge dev:\n%s", output.String())
	}
}

func developmentProjectDirectory(t *testing.T, version string) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: "+version+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return directory
}

func waitForDevelopmentProcessCalls(t *testing.T, process *developmentProcessStub, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(process.recordedCalls()) < count {
		if time.Now().After(deadline) {
			t.Fatalf("process calls = %d, want %d", len(process.recordedCalls()), count)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForDevTestOutput(t *testing.T, output *developmentTestBuffer, value string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(output.String(), value) {
		if time.Now().After(deadline) {
			t.Fatalf("output did not contain %q:\n%s", value, output.String())
		}
		time.Sleep(time.Millisecond)
	}
}

func waitForDevelopmentResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("development supervisor did not finish")
		return nil
	}
}
