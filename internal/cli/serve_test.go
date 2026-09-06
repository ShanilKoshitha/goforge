package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeDevelopmentProcess struct {
	done     chan struct{}
	stopOnce sync.Once
	stopErr  error
	waitErr  error
}

func newFakeDevelopmentProcess() *fakeDevelopmentProcess {
	return &fakeDevelopmentProcess{done: make(chan struct{})}
}

func (process *fakeDevelopmentProcess) Done() <-chan struct{} { return process.done }
func (process *fakeDevelopmentProcess) Wait() error {
	<-process.done
	return process.waitErr
}
func (process *fakeDevelopmentProcess) Stop() error {
	process.stopOnce.Do(func() { close(process.done) })
	return process.stopErr
}

type fakeDevelopmentProxy struct {
	address string
	done    chan error

	mu      sync.Mutex
	targets []string
	closed  bool
}

func newFakeDevelopmentProxy(address string, target *url.URL) *fakeDevelopmentProxy {
	return &fakeDevelopmentProxy{address: address, done: make(chan error, 1), targets: []string{target.String()}}
}

func (proxy *fakeDevelopmentProxy) Address() string    { return proxy.address }
func (proxy *fakeDevelopmentProxy) Done() <-chan error { return proxy.done }
func (proxy *fakeDevelopmentProxy) SwapTarget(target *url.URL) error {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	proxy.targets = append(proxy.targets, target.String())
	return nil
}
func (proxy *fakeDevelopmentProxy) Close(context.Context) error {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	if !proxy.closed {
		proxy.closed = true
		proxy.done <- nil
		close(proxy.done)
	}
	return nil
}

type fakeDevelopmentSource struct {
	mu      sync.Mutex
	current sourceSnapshot
	changes chan sourceSnapshot
}

func (source *fakeDevelopmentSource) snapshot() (sourceSnapshot, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.current, nil
}

func (source *fakeDevelopmentSource) change(snapshot sourceSnapshot) {
	source.mu.Lock()
	source.current = snapshot
	source.mu.Unlock()
	source.changes <- snapshot
}

func (source *fakeDevelopmentSource) set(snapshot sourceSnapshot) {
	source.mu.Lock()
	source.current = snapshot
	source.mu.Unlock()
}

func (source *fakeDevelopmentSource) wait(ctx context.Context, previous sourceSnapshot) (sourceSnapshot, error) {
	current, _ := source.snapshot()
	if current != previous {
		return current, nil
	}
	for {
		select {
		case <-ctx.Done():
			return sourceSnapshot{}, ctx.Err()
		case changed := <-source.changes:
			current, _ := source.snapshot()
			if changed == current && current != previous {
				return current, nil
			}
		}
	}
}

func testSnapshot(value byte) sourceSnapshot {
	var snapshot sourceSnapshot
	snapshot[0] = value
	return snapshot
}

func TestDevelopmentSupervisorKeepsLastGoodOnFailureAndRecovers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &fakeDevelopmentSource{current: testSnapshot(1), changes: make(chan sourceSnapshot, 4)}
	builds := make(chan int, 4)
	var buildCount int
	var buildMu sync.Mutex
	processes := make(map[string]*fakeDevelopmentProcess)
	var processMu sync.Mutex
	removed := make(chan string, 8)
	var stdout, stderr synchronizedBuffer
	proxyReady := make(chan *fakeDevelopmentProxy, 1)

	dependencies := developmentServeDependencies{
		snapshot:      source.snapshot,
		waitForChange: source.wait,
		build: func(context.Context) (string, error) {
			buildMu.Lock()
			buildCount++
			count := buildCount
			buildMu.Unlock()
			builds <- count
			if count == 2 {
				return "", errors.New("broken Go source")
			}
			return string(rune('a' + count - 1)), nil
		},
		removeBinary: func(path string) error { removed <- path; return nil },
		start: func(_ context.Context, binary string) (*developmentCandidate, error) {
			process := newFakeDevelopmentProcess()
			processMu.Lock()
			processes[binary] = process
			processMu.Unlock()
			return &developmentCandidate{
				process: process,
				target:  &url.URL{Scheme: "http", Host: "candidate-" + binary},
				binary:  binary,
			}, nil
		},
		startProxy: func(address string, target *url.URL) (developmentProxy, error) {
			proxy := newFakeDevelopmentProxy(address, target)
			proxyReady <- proxy
			return proxy, nil
		},
		publicAddress: "127.0.0.1:8080",
		stdout:        &stdout,
		stderr:        &stderr,
	}

	result := make(chan error, 1)
	go func() { result <- superviseDevelopmentServer(ctx, dependencies) }()
	proxy := <-proxyReady
	if count := <-builds; count != 1 {
		t.Fatalf("initial build = %d", count)
	}

	source.change(testSnapshot(2))
	if count := <-builds; count != 2 {
		t.Fatalf("failed build = %d", count)
	}
	waitForText(t, &stderr, "reload failed: broken Go source")
	processMu.Lock()
	initial := processes["a"]
	processMu.Unlock()
	select {
	case <-initial.Done():
		t.Fatal("failed rebuild stopped the last-good server")
	default:
	}
	proxy.mu.Lock()
	if len(proxy.targets) != 1 {
		t.Fatalf("failed rebuild promoted a target: %v", proxy.targets)
	}
	proxy.mu.Unlock()

	source.change(testSnapshot(3))
	if count := <-builds; count != 3 {
		t.Fatalf("recovery build = %d", count)
	}
	waitForText(t, &stdout, "reloaded development server")
	select {
	case <-initial.Done():
	case <-time.After(time.Second):
		t.Fatal("replacement did not stop the old server")
	}
	proxy.mu.Lock()
	if got, want := proxy.targets, []string{"http://candidate-a", "http://candidate-c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %v, want %v", got, want)
	}
	proxy.mu.Unlock()

	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("supervisor cancellation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not stop")
	}
	processMu.Lock()
	recovered := processes["c"]
	processMu.Unlock()
	select {
	case <-recovered.Done():
	default:
		t.Fatal("cancellation did not stop the active server")
	}
}

func TestDevelopmentSupervisorDoesNotLoseEditDuringBuild(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &fakeDevelopmentSource{current: testSnapshot(1), changes: make(chan sourceSnapshot, 4)}
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	promoted := make(chan struct{}, 2)
	removed := make(chan string, 8)
	var buildCount int
	var buildMu sync.Mutex
	proxyReady := make(chan *fakeDevelopmentProxy, 1)
	dependencies := developmentServeDependencies{
		snapshot:      source.snapshot,
		waitForChange: source.wait,
		build: func(context.Context) (string, error) {
			buildMu.Lock()
			buildCount++
			count := buildCount
			buildMu.Unlock()
			if count == 2 {
				close(secondStarted)
				<-releaseSecond
			}
			return string(rune('0' + count)), nil
		},
		removeBinary: func(path string) error { removed <- path; return nil },
		start: func(_ context.Context, binary string) (*developmentCandidate, error) {
			return &developmentCandidate{
				process: newFakeDevelopmentProcess(),
				target:  &url.URL{Scheme: "http", Host: "candidate-" + binary},
				binary:  binary,
			}, nil
		},
		startProxy: func(address string, target *url.URL) (developmentProxy, error) {
			proxy := newFakeDevelopmentProxy(address, target)
			proxyReady <- proxy
			return proxy, nil
		},
		publicAddress: "127.0.0.1:8080",
		stdout:        io.Discard,
		stderr:        io.Discard,
	}
	result := make(chan error, 1)
	go func() { result <- superviseDevelopmentServer(ctx, dependencies) }()
	proxy := <-proxyReady

	source.change(testSnapshot(2))
	<-secondStarted
	source.set(testSnapshot(3))
	close(releaseSecond)
	deadline := time.Now().Add(time.Second)
	for {
		proxy.mu.Lock()
		count := len(proxy.targets)
		proxy.mu.Unlock()
		if count == 2 {
			close(promoted)
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("newest source was not promoted")
		}
		time.Sleep(time.Millisecond)
	}
	<-promoted
	buildMu.Lock()
	if buildCount != 3 {
		t.Fatalf("build count = %d, want initial + stale + newest", buildCount)
	}
	buildMu.Unlock()
	proxy.mu.Lock()
	if got, want := proxy.targets, []string{"http://candidate-1", "http://candidate-3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %v, want %v", got, want)
	}
	proxy.mu.Unlock()
	select {
	case path := <-removed:
		if path != "2" {
			t.Fatalf("first removed binary = %q, want stale candidate 2", path)
		}
	case <-time.After(time.Second):
		t.Fatal("stale candidate binary was not removed")
	}
	cancel()
	<-result
}

func TestDevelopmentSupervisorPropagatesUnexpectedServerExit(t *testing.T) {
	source := &fakeDevelopmentSource{current: testSnapshot(1), changes: make(chan sourceSnapshot)}
	process := newFakeDevelopmentProcess()
	process.waitErr = errors.New("application crashed")
	proxyReady := make(chan struct{})
	dependencies := developmentServeDependencies{
		snapshot:      source.snapshot,
		waitForChange: source.wait,
		build:         func(context.Context) (string, error) { return "server", nil },
		removeBinary:  func(string) error { return nil },
		start: func(context.Context, string) (*developmentCandidate, error) {
			return &developmentCandidate{process: process, target: &url.URL{Scheme: "http", Host: "candidate"}, binary: "server"}, nil
		},
		startProxy: func(address string, target *url.URL) (developmentProxy, error) {
			close(proxyReady)
			return newFakeDevelopmentProxy(address, target), nil
		},
		publicAddress: "127.0.0.1:8080",
		stdout:        io.Discard,
		stderr:        io.Discard,
	}
	result := make(chan error, 1)
	go func() { result <- superviseDevelopmentServer(context.Background(), dependencies) }()
	<-proxyReady
	_ = process.Stop()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "application crashed") {
			t.Fatalf("unexpected exit error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("unexpected server exit did not stop the supervisor")
	}
}

func TestServeRejectsUnsupportedFormatsBeforeSpawning(t *testing.T) {
	for _, manifest := range []string{"version: 3\n", "version: 9\n", "version: nope\n"} {
		t.Run(strings.TrimSpace(manifest), func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte(manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Chdir(directory)
			process := &recordedProcess{}
			err := run(context.Background(), []string{"serve"}, nil, io.Discard, io.Discard, process)
			if err == nil || !strings.Contains(err.Error(), "version") {
				t.Fatalf("manifest %q error = %v", manifest, err)
			}
			if process.called {
				t.Fatal("unsupported project spawned a process")
			}
		})
	}
}

func TestWaitForCandidateHealthObservesHealthyServerAndEarlyExit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/health" {
			t.Fatalf("probe path = %q", request.URL.Path)
		}
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	healthy := newFakeDevelopmentProcess()
	if err := waitForCandidateHealth(context.Background(), healthy, target, time.Second); err != nil {
		t.Fatal(err)
	}

	exited := newFakeDevelopmentProcess()
	exited.waitErr = errors.New("startup failed")
	_ = exited.Stop()
	unreachable := &url.URL{Scheme: "http", Host: "127.0.0.1:1"}
	err = waitForCandidateHealth(context.Background(), exited, unreachable, time.Second)
	if err == nil || !strings.Contains(err.Error(), "startup failed") {
		t.Fatalf("early exit error = %v", err)
	}
}

func TestEnvironmentWithOverrideIsCaseInsensitive(t *testing.T) {
	got := environmentWithOverride([]string{"A=1", "app_address=:9", "B=2"}, "APP_ADDRESS", "127.0.0.1:10")
	want := []string{"A=1", "B=2", "APP_ADDRESS=127.0.0.1:10"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %v, want %v", got, want)
	}
}

func waitForText(t *testing.T, buffer interface{ String() string }, expected string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(buffer.String(), expected) {
		if time.Now().After(deadline) {
			t.Fatalf("output never contained %q: %q", expected, buffer.String())
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBuildDevelopmentServerCompilesViewsThenStagesOrdinaryBinary(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	output := filepath.Join(t.TempDir(), "server")
	runner := &developmentBuildRecorder{output: output}
	stdin := strings.NewReader("input")
	var stdout, stderr bytes.Buffer
	ctx := context.Background()
	if err := buildDevelopmentServer(ctx, stdin, &stdout, &stderr, runner, output); err != nil {
		t.Fatal(err)
	}
	want := []processCall{
		{name: "go", args: []string{"run", "./cmd/views"}},
		{name: "go", args: []string{"build", "-trimpath", "-o", output, "./cmd/server"}},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
	if runner.ctx != ctx || runner.stdin != stdin || runner.stdout != &stdout || runner.stderr != &stderr {
		t.Fatal("development build did not preserve process context and streams")
	}
}

type developmentBuildRecorder struct {
	calls  []processCall
	output string
	ctx    context.Context
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func (runner *developmentBuildRecorder) Run(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	name string,
	arguments ...string,
) error {
	runner.calls = append(runner.calls, processCall{name: name, args: append([]string(nil), arguments...)})
	runner.ctx, runner.stdin, runner.stdout, runner.stderr = ctx, stdin, stdout, stderr
	if len(arguments) > 0 && arguments[0] == "build" {
		return os.WriteFile(runner.output, []byte("executable"), 0o755)
	}
	return nil
}
