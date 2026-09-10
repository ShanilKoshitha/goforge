package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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
func (process *fakeDevelopmentProcess) PID() int              { return 42 }
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
	onSwap  func(*url.URL)
}

func newFakeDevelopmentProxy(address string, target *url.URL) *fakeDevelopmentProxy {
	return &fakeDevelopmentProxy{address: address, done: make(chan error, 1), targets: []string{target.String()}}
}

func (proxy *fakeDevelopmentProxy) Address() string    { return proxy.address }
func (proxy *fakeDevelopmentProxy) Done() <-chan error { return proxy.done }
func (proxy *fakeDevelopmentProxy) SwapTarget(target *url.URL) error {
	proxy.mu.Lock()
	proxy.targets = append(proxy.targets, target.String())
	hook := proxy.onSwap
	proxy.mu.Unlock()
	if hook != nil {
		hook(target)
	}
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
	mu         sync.Mutex
	current    sourceSnapshot
	changes    chan sourceSnapshot
	generation atomic.Uint64
}

func (source *fakeDevelopmentSource) snapshot() (sourceSnapshot, error) {
	source.mu.Lock()
	defer source.mu.Unlock()
	return source.current, nil
}

func (source *fakeDevelopmentSource) change(snapshot sourceSnapshot) {
	source.mu.Lock()
	changed := source.current != snapshot
	source.current = snapshot
	source.mu.Unlock()
	if changed {
		source.generation.Add(1)
	}
	source.changes <- snapshot
}

func (source *fakeDevelopmentSource) set(snapshot sourceSnapshot) {
	source.mu.Lock()
	changed := source.current != snapshot
	source.current = snapshot
	source.mu.Unlock()
	if changed {
		source.generation.Add(1)
	}
}

func (source *fakeDevelopmentSource) wait(ctx context.Context, previous sourceSnapshot, previousGeneration uint64) (sourceSnapshot, error) {
	current, _ := source.snapshot()
	if current != previous || source.generation.Load() != previousGeneration {
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

func (source *fakeDevelopmentSource) stabilize(context.Context, sourceSnapshot) (sourceSnapshot, error) {
	return source.snapshot()
}

func testSnapshot(value byte) sourceSnapshot {
	var snapshot sourceSnapshot
	snapshot[0] = value
	return snapshot
}

func testDevelopmentBuild(binary string) *developmentBuild {
	return &developmentBuild{binary: binary}
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
		stabilize:     source.stabilize,
		generation:    source.generation.Load,
		build: func(context.Context) (*developmentBuild, error) {
			buildMu.Lock()
			buildCount++
			count := buildCount
			buildMu.Unlock()
			builds <- count
			if count == 2 {
				return nil, errors.New("broken Go source")
			}
			return testDevelopmentBuild(string(rune('a' + count - 1))), nil
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
	waitForText(t, &stdout, "serving http://127.0.0.1:8080 (watching for changes)")

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
		stabilize:     source.stabilize,
		generation:    source.generation.Load,
		build: func(context.Context) (*developmentBuild, error) {
			buildMu.Lock()
			buildCount++
			count := buildCount
			buildMu.Unlock()
			if count == 2 {
				close(secondStarted)
				<-releaseSecond
			}
			return testDevelopmentBuild(string(rune('0' + count))), nil
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
	source.set(testSnapshot(2))
	close(releaseSecond)
	deadline := time.Now().Add(3 * time.Second)
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

func TestDevelopmentSupervisorRebuildsEditDuringInitialProxyStartup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &fakeDevelopmentSource{current: testSnapshot(1), changes: make(chan sourceSnapshot, 4)}
	var buildCount int
	var proxies []*fakeDevelopmentProxy
	proxyReady := make(chan *fakeDevelopmentProxy, 2)
	dependencies := developmentServeDependencies{
		snapshot:      source.snapshot,
		waitForChange: source.wait,
		stabilize:     source.stabilize,
		generation:    source.generation.Load,
		build: func(context.Context) (*developmentBuild, error) {
			buildCount++
			return testDevelopmentBuild(fmt.Sprintf("server-%d", buildCount)), nil
		},
		removeBinary: func(string) error { return nil },
		start: func(_ context.Context, binary string) (*developmentCandidate, error) {
			return &developmentCandidate{
				process: newFakeDevelopmentProcess(),
				target:  &url.URL{Scheme: "http", Host: binary},
				binary:  binary,
			}, nil
		},
		startProxy: func(address string, target *url.URL) (developmentProxy, error) {
			proxy := newFakeDevelopmentProxy(address, target)
			proxies = append(proxies, proxy)
			if len(proxies) == 1 {
				source.set(testSnapshot(2))
			}
			proxyReady <- proxy
			return proxy, nil
		},
		publicAddress: "127.0.0.1:8080",
		stdout:        io.Discard,
		stderr:        io.Discard,
	}

	result := make(chan error, 1)
	go func() { result <- superviseDevelopmentServer(ctx, dependencies) }()
	first := <-proxyReady
	second := <-proxyReady
	first.mu.Lock()
	firstClosed := first.closed
	first.mu.Unlock()
	if !firstClosed {
		t.Fatal("stale initial proxy was not closed")
	}
	second.mu.Lock()
	gotTargets := append([]string(nil), second.targets...)
	second.mu.Unlock()
	if want := []string{"http://server-2"}; !reflect.DeepEqual(gotTargets, want) {
		t.Fatalf("replacement proxy targets = %v, want %v", gotTargets, want)
	}
	if buildCount != 2 {
		t.Fatalf("build count = %d, want stale plus current", buildCount)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("supervisor cancellation = %v", err)
	}
}

func TestDevelopmentSupervisorPropagatesUnexpectedServerExit(t *testing.T) {
	source := &fakeDevelopmentSource{current: testSnapshot(1), changes: make(chan sourceSnapshot)}
	process := newFakeDevelopmentProcess()
	process.waitErr = errors.New("application crashed")
	proxyReady := make(chan struct{})
	dependencies := developmentServeDependencies{
		snapshot:      source.snapshot,
		waitForChange: source.wait,
		stabilize:     source.stabilize,
		generation:    source.generation.Load,
		build:         func(context.Context) (*developmentBuild, error) { return testDevelopmentBuild("server"), nil },
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

func TestDevelopmentSupervisorDoesNotPromoteCandidateThatAlreadyExited(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &fakeDevelopmentSource{current: testSnapshot(1), changes: make(chan sourceSnapshot, 2)}
	initial := newFakeDevelopmentProcess()
	var starts int
	var stdout, stderr synchronizedBuffer
	proxyReady := make(chan *fakeDevelopmentProxy, 1)
	dependencies := developmentServeDependencies{
		snapshot:      source.snapshot,
		waitForChange: source.wait,
		stabilize:     source.stabilize,
		generation:    source.generation.Load,
		build:         func(context.Context) (*developmentBuild, error) { return testDevelopmentBuild("server"), nil },
		removeBinary:  func(string) error { return nil },
		start: func(context.Context, string) (*developmentCandidate, error) {
			starts++
			process := developmentProcess(initial)
			if starts == 2 {
				failed := newFakeDevelopmentProcess()
				failed.waitErr = errors.New("candidate crashed")
				_ = failed.Stop()
				process = failed
			}
			return &developmentCandidate{process: process, target: &url.URL{Scheme: "http", Host: "candidate"}, binary: "server"}, nil
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
	waitForText(t, &stdout, "serving http://127.0.0.1:8080 (watching for changes)")
	source.change(testSnapshot(2))
	waitForText(t, &stderr, "candidate crashed")
	select {
	case <-initial.Done():
		t.Fatal("failed candidate stopped the last-good server")
	default:
	}
	proxy.mu.Lock()
	if len(proxy.targets) != 1 {
		t.Fatalf("failed candidate was promoted: %v", proxy.targets)
	}
	proxy.mu.Unlock()
	cancel()
	<-result
}

func TestDevelopmentSupervisorRejectsInitialProcessExitDuringProxyStartup(t *testing.T) {
	source := &fakeDevelopmentSource{current: testSnapshot(1), changes: make(chan sourceSnapshot)}
	process := newFakeDevelopmentProcess()
	process.waitErr = errors.New("startup crash")
	var proxy *fakeDevelopmentProxy
	dependencies := developmentServeDependencies{
		snapshot:      source.snapshot,
		waitForChange: source.wait,
		stabilize:     source.stabilize,
		generation:    source.generation.Load,
		build:         func(context.Context) (*developmentBuild, error) { return testDevelopmentBuild("server"), nil },
		removeBinary:  func(string) error { return nil },
		start: func(context.Context, string) (*developmentCandidate, error) {
			return &developmentCandidate{process: process, target: &url.URL{Scheme: "http", Host: "candidate"}, binary: "server"}, nil
		},
		startProxy: func(address string, target *url.URL) (developmentProxy, error) {
			proxy = newFakeDevelopmentProxy(address, target)
			_ = process.Stop()
			return proxy, nil
		},
		publicAddress: "127.0.0.1:8080",
		stdout:        io.Discard,
		stderr:        io.Discard,
	}
	err := superviseDevelopmentServer(context.Background(), dependencies)
	if err == nil || !strings.Contains(err.Error(), "startup crash") {
		t.Fatalf("startup exit error = %v", err)
	}
	proxy.mu.Lock()
	closed := proxy.closed
	proxy.mu.Unlock()
	if !closed {
		t.Fatal("proxy was not closed after initial process exited")
	}
}

func TestDevelopmentSupervisorClosesProxyWhenInitialCommitFails(t *testing.T) {
	source := &fakeDevelopmentSource{current: testSnapshot(1), changes: make(chan sourceSnapshot)}
	process := newFakeDevelopmentProcess()
	commitErr := errors.New("remove staging failed")
	var proxy *fakeDevelopmentProxy
	dependencies := developmentServeDependencies{
		snapshot:      source.snapshot,
		waitForChange: source.wait,
		stabilize:     source.stabilize,
		generation:    source.generation.Load,
		build: func(context.Context) (*developmentBuild, error) {
			return &developmentBuild{binary: "server", finalize: func() error { return commitErr }}, nil
		},
		removeBinary: func(string) error { return nil },
		start: func(context.Context, string) (*developmentCandidate, error) {
			return &developmentCandidate{process: process, target: &url.URL{Scheme: "http", Host: "candidate"}, binary: "server"}, nil
		},
		startProxy: func(address string, target *url.URL) (developmentProxy, error) {
			proxy = newFakeDevelopmentProxy(address, target)
			return proxy, nil
		},
		publicAddress: "127.0.0.1:8080",
		stdout:        io.Discard,
		stderr:        io.Discard,
	}
	if err := superviseDevelopmentServer(context.Background(), dependencies); !errors.Is(err, commitErr) {
		t.Fatalf("commit error = %v, want %v", err, commitErr)
	}
	proxy.mu.Lock()
	closed := proxy.closed
	proxy.mu.Unlock()
	if !closed {
		t.Fatal("proxy was not closed after initial commit failure")
	}
	select {
	case <-process.Done():
	default:
		t.Fatal("server was not stopped after initial commit failure")
	}
}

func TestDevelopmentSupervisorRevertsCandidateExitDuringPromotion(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &fakeDevelopmentSource{current: testSnapshot(1), changes: make(chan sourceSnapshot, 2)}
	initial := newFakeDevelopmentProcess()
	candidate := newFakeDevelopmentProcess()
	candidate.waitErr = errors.New("promotion crash")
	var starts int
	var stdout, stderr synchronizedBuffer
	proxyReady := make(chan *fakeDevelopmentProxy, 1)
	dependencies := developmentServeDependencies{
		snapshot:      source.snapshot,
		waitForChange: source.wait,
		stabilize:     source.stabilize,
		generation:    source.generation.Load,
		build: func(context.Context) (*developmentBuild, error) {
			starts++
			return testDevelopmentBuild(fmt.Sprintf("server-%d", starts)), nil
		},
		removeBinary: func(string) error { return nil },
		start: func(context.Context, string) (*developmentCandidate, error) {
			process := developmentProcess(initial)
			if starts == 2 {
				process = candidate
			}
			return &developmentCandidate{process: process, target: &url.URL{Scheme: "http", Host: fmt.Sprintf("candidate-%d", starts)}, binary: fmt.Sprintf("server-%d", starts)}, nil
		},
		startProxy: func(address string, target *url.URL) (developmentProxy, error) {
			proxy := newFakeDevelopmentProxy(address, target)
			proxy.onSwap = func(target *url.URL) {
				if target.Host == "candidate-2" {
					_ = candidate.Stop()
				}
			}
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
	waitForText(t, &stdout, "serving http://127.0.0.1:8080 (watching for changes)")
	source.change(testSnapshot(2))
	waitForText(t, &stderr, "promotion crash")
	select {
	case <-initial.Done():
		t.Fatal("promotion crash stopped last-good server")
	default:
	}
	proxy.mu.Lock()
	targets := append([]string(nil), proxy.targets...)
	proxy.mu.Unlock()
	want := []string{"http://candidate-1", "http://candidate-2", "http://candidate-1"}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("promotion targets = %v, want %v", targets, want)
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("supervisor cancellation = %v", err)
	}
}

func TestServeRejectsUnsupportedFormatsBeforeSpawning(t *testing.T) {
	for _, manifest := range []string{"version: 3\n", "version: 12\n", "version: nope\n"} {
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

func TestDevelopmentCandidateRetriesPortClaimThatPassesUnrelatedHealthProbe(t *testing.T) {
	target := &url.URL{Scheme: "http", Host: "127.0.0.1:43210"}
	var starts int
	var startedSpec managedProcessSpec
	candidate, err := startDevelopmentCandidateWith(
		context.Background(),
		"server",
		io.Discard,
		io.Discard,
		developmentCandidateStartDependencies{
			reserve: func() (string, *url.URL, error) {
				return target.Host, target, nil
			},
			environment: func(string) ([]string, error) { return nil, nil },
			start: func(spec managedProcessSpec) (developmentProcess, error) {
				starts++
				startedSpec = spec
				return newFakeDevelopmentProcess(), nil
			},
			health: func(context.Context, developmentProcess, *url.URL, time.Duration) error {
				// The occupied port may belong to another healthy application.
				return nil
			},
			ownsAddress: func(developmentProcess, string) (bool, error) {
				return starts == 2, nil
			},
			attempts:         2,
			processTreeGrace: 27 * time.Second,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer candidate.process.Stop()
	if starts != 2 {
		t.Fatalf("candidate starts = %d, want collision retry", starts)
	}
	if startedSpec.ProcessTreeGrace != 27*time.Second {
		t.Fatalf("candidate process-tree grace = %s", startedSpec.ProcessTreeGrace)
	}
}

func TestEnvironmentWithOverrideIsCaseInsensitive(t *testing.T) {
	got := environmentWithOverride([]string{"A=1", "app_address=:9", "B=2"}, "APP_ADDRESS", "127.0.0.1:10")
	want := []string{"A=1", "B=2", "APP_ADDRESS=127.0.0.1:10"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("environment = %v, want %v", got, want)
	}
}

func TestDevelopmentCandidateEnvironmentUsesPrivateAddressAndTrustedProxy(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	if err := os.WriteFile(".env", []byte("TRUSTED_PROXIES=10.0.0.0/8, 127.0.0.1/32\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	environment, err := developmentCandidateEnvironment("127.0.0.1:43210")
	if err != nil {
		t.Fatal(err)
	}
	values := make(map[string]string)
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[strings.ToUpper(key)] = value
		}
	}
	if got := values["APP_ADDRESS"]; got != "127.0.0.1:43210" {
		t.Fatalf("candidate APP_ADDRESS = %q", got)
	}
	if got := values["TRUSTED_PROXIES"]; got != "127.0.0.1/32,10.0.0.0/8" {
		t.Fatalf("candidate TRUSTED_PROXIES = %q", got)
	}
}

func TestDevelopmentSupervisorReportsCleanupFailureOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &fakeDevelopmentSource{current: testSnapshot(1), changes: make(chan sourceSnapshot)}
	process := newFakeDevelopmentProcess()
	cleanupErr := errors.New("process tree remained alive")
	process.stopErr = cleanupErr
	proxyReady := make(chan struct{})
	dependencies := developmentServeDependencies{
		snapshot:      source.snapshot,
		waitForChange: source.wait,
		stabilize:     source.stabilize,
		generation:    source.generation.Load,
		build:         func(context.Context) (*developmentBuild, error) { return testDevelopmentBuild("server"), nil },
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
	go func() { result <- superviseDevelopmentServer(ctx, dependencies) }()
	<-proxyReady
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, cleanupErr) {
			t.Fatalf("joined cancellation cleanup error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not finish cancellation cleanup")
	}
}

func waitForText(t *testing.T, buffer interface{ String() string }, expected string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(buffer.String(), expected) {
		if time.Now().After(deadline) {
			t.Fatalf("output never contained %q: %q", expected, buffer.String())
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBuildDevelopmentServerCompilesViewsThenStagesOrdinaryBinary(t *testing.T) {
	setupDevelopmentBuildProject(t)
	output := filepath.Join(t.TempDir(), "server")
	runner := &developmentBuildRecorder{output: output}
	stdin := strings.NewReader("input")
	var stdout, stderr bytes.Buffer
	ctx := context.Background()
	build, err := buildDevelopmentServer(ctx, stdin, &stdout, &stderr, runner, output)
	if err != nil {
		t.Fatal(err)
	}
	if build.binary != output {
		t.Fatalf("staged binary = %q, want %q", build.binary, output)
	}
	if err := publishDevelopmentBuild(build); err != nil {
		t.Fatal(err)
	}
	if err := commitDevelopmentBuild(build); err != nil {
		t.Fatal(err)
	}
	compiler := output + ".views-compiler"
	if runtime.GOOS == "windows" {
		compiler += ".exe"
	}
	absoluteCompiler, err := filepath.Abs(compiler)
	if err != nil {
		t.Fatal(err)
	}
	want := []processCall{
		{name: "go", args: []string{"build", "-trimpath", "-o", compiler, "./cmd/views"}},
		{name: absoluteCompiler, args: []string{"-C", output + ".views-root"}},
		{name: "go", args: []string{"build", "-trimpath", "-overlay", output + ".overlay.json", "-o", output, "./cmd/server"}},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
	if runner.ctx != ctx || runner.stdin != stdin || runner.stdout != &stdout || runner.stderr != &stderr {
		t.Fatal("development build did not preserve process context and streams")
	}
}

func TestBuildDevelopmentServerRestoresLastGoodViewsOnGoFailure(t *testing.T) {
	setupDevelopmentBuildProject(t)
	if err := os.MkdirAll(filepath.Dir(generatedViewsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	lastGood := []byte("package views\n// last good\n")
	if err := os.WriteFile(generatedViewsPath, lastGood, 0o640); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "server")
	runner := &developmentBuildRecorder{
		output:     output,
		viewOutput: []byte("package views\n// candidate\n"),
		buildErr:   errors.New("broken Go source"),
		onViewRun: func() {
			assertDevelopmentArtifact(t, generatedViewsPath, lastGood, "isolated view compilation")
		},
		onBuild: func() {
			current, readErr := os.ReadFile(generatedViewsPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(current, lastGood) {
				t.Fatalf("candidate views became observable during Go build: %q", current)
			}
		},
	}
	build, err := buildDevelopmentServer(context.Background(), nil, io.Discard, io.Discard, runner, output)
	if err == nil || !strings.Contains(err.Error(), "broken Go source") || build != nil {
		t.Fatalf("build result = %#v, %v", build, err)
	}
	got, readErr := os.ReadFile(generatedViewsPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, lastGood) {
		t.Fatalf("views after failed Go build = %q, want %q", got, lastGood)
	}
	info, statErr := os.Stat(generatedViewsPath)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if gotMode := info.Mode().Perm(); runtime.GOOS != "windows" && gotMode != 0o640 {
		t.Fatalf("restored view mode = %o, want 640", gotMode)
	}
}

func TestFailedDevelopmentBuildDoesNotRollbackIdenticalExternalPublication(t *testing.T) {
	setupDevelopmentBuildProject(t)
	lastGood := []byte("package views\n// last good\n")
	candidate := []byte("package views\n// shared candidate\n")
	if err := os.WriteFile(generatedViewsPath, lastGood, 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "server")
	runner := &developmentBuildRecorder{
		output:     output,
		viewOutput: candidate,
		buildErr:   errors.New("broken Go source"),
		onBuild: func() {
			if err := os.WriteFile(generatedViewsPath, candidate, 0o644); err != nil {
				t.Fatal(err)
			}
		},
	}
	if build, err := buildDevelopmentServer(context.Background(), nil, io.Discard, io.Discard, runner, output); err == nil || build != nil {
		t.Fatalf("build result = %#v, %v", build, err)
	}
	assertDevelopmentArtifact(t, generatedViewsPath, candidate, "external identical publication")
}

func TestPublishedDevelopmentBuildSerializesIdenticalGeneratorPublication(t *testing.T) {
	setupDevelopmentBuildProject(t)
	lastGood := []byte("package views\n// last good\n")
	candidate := []byte("package views\n// shared candidate\n")
	if err := os.WriteFile(generatedViewsPath, lastGood, 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "server")
	build, err := buildDevelopmentServer(
		context.Background(), nil, io.Discard, io.Discard,
		&developmentBuildRecorder{output: output, viewOutput: candidate}, output,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := publishDevelopmentBuild(build); err != nil {
		t.Fatal(err)
	}

	publication := make(chan error, 1)
	go func() {
		lock, lockErr := acquireGeneratorLock(context.Background())
		if lockErr != nil {
			publication <- lockErr
			return
		}
		publication <- errors.Join(writeManagedFile(generatedViewsPath, candidate), lock.Close())
	}()
	select {
	case err := <-publication:
		t.Fatalf("generator bypassed promotion lock: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	if err := rollbackDevelopmentBuild(build); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-publication:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("generator did not resume after development rollback")
	}
	assertDevelopmentArtifact(t, generatedViewsPath, candidate, "serialized identical publication")
}

func TestBuildDevelopmentServerPublishesViewsOnlyAtPromotion(t *testing.T) {
	setupDevelopmentBuildProject(t)
	if err := os.MkdirAll(filepath.Dir(generatedViewsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	lastGood := []byte("package views\n// last good\n")
	candidate := []byte("package views\n// promoted candidate\n")
	if err := os.WriteFile(generatedViewsPath, lastGood, 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "server")
	runner := &developmentBuildRecorder{output: output, viewOutput: candidate}
	build, err := buildDevelopmentServer(context.Background(), nil, io.Discard, io.Discard, runner, output)
	if err != nil {
		t.Fatal(err)
	}
	assertDevelopmentArtifact(t, generatedViewsPath, lastGood, "successful unpromoted build")
	if err := publishDevelopmentBuild(build); err != nil {
		t.Fatal(err)
	}
	assertDevelopmentArtifact(t, generatedViewsPath, candidate, "candidate promotion")
	if err := rollbackDevelopmentBuild(build); err != nil {
		t.Fatal(err)
	}
	assertDevelopmentArtifact(t, generatedViewsPath, lastGood, "candidate rollback")
	for _, path := range []string{output + ".views.go", output + ".overlay.json"} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("staging file %s remained after rollback: %v", path, statErr)
		}
	}
}

func TestBuildDevelopmentServerStagesAnEmptyViewSourceRoot(t *testing.T) {
	setupDevelopmentBuildProject(t)
	if err := os.Remove(filepath.Join("resources", "views", "index.forge.html")); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "server")
	runner := &developmentBuildRecorder{output: output, onViewRun: func() {
		info, err := os.Stat(filepath.Join(output+".views-root", "resources", "views"))
		if err != nil || !info.IsDir() {
			t.Fatalf("empty staged view root = %v, %v", info, err)
		}
	}}
	build, err := buildDevelopmentServer(context.Background(), nil, io.Discard, io.Discard, runner, output)
	if err != nil {
		t.Fatal(err)
	}
	if err := rollbackDevelopmentBuild(build); err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentViewRollbackDoesNotOverwriteNewerGeneratorPublication(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	if err := os.WriteFile("forge.yaml", []byte("version: 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(generatedViewsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := developmentFileState{exists: true, mode: 0o644, data: []byte("previous")}
	published := developmentFileState{exists: true, mode: 0o644, data: []byte("candidate")}
	newer := []byte("newer generator publication")
	if err := os.WriteFile(generatedViewsPath, newer, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rollbackDevelopmentViews(previous, published); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(generatedViewsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newer) {
		t.Fatalf("rollback overwrote newer publication: %q", got)
	}
}

func TestDevelopmentBuildRollbackCanRetryAfterFailure(t *testing.T) {
	want := errors.New("temporary rollback failure")
	attempts := 0
	build := &developmentBuild{rollback: func() error {
		attempts++
		if attempts == 1 {
			return want
		}
		return nil
	}}
	if err := rollbackDevelopmentBuild(build); !errors.Is(err, want) {
		t.Fatalf("first rollback error = %v, want %v", err, want)
	}
	if build.rollback == nil {
		t.Fatal("failed rollback was not retained for retry")
	}
	if err := rollbackDevelopmentBuild(build); err != nil {
		t.Fatalf("retry rollback: %v", err)
	}
	if attempts != 2 || build.rollback != nil {
		t.Fatalf("rollback attempts = %d, retained = %t", attempts, build.rollback != nil)
	}
}

func TestDiscardDevelopmentBuildRetriesTransientRollback(t *testing.T) {
	want := errors.New("temporary rollback failure")
	attempts := 0
	removed := false
	build := &developmentBuild{binary: "server", rollback: func() error {
		attempts++
		if attempts == 1 {
			return want
		}
		return nil
	}}
	if err := discardDevelopmentBuild(build, func(path string) error {
		removed = path == "server"
		return nil
	}); err != nil {
		t.Fatalf("discard after transient rollback: %v", err)
	}
	if attempts != 2 || !removed || build.rollback != nil {
		t.Fatalf("attempts = %d, removed = %t, retained = %t", attempts, removed, build.rollback != nil)
	}
}

func TestBuildDevelopmentServerRejectsStaleORMBeforeViewCompilation(t *testing.T) {
	setupDevelopmentBuildProject(t)
	modelPath := filepath.Join("internal", "models", "item.go")
	if err := os.WriteFile(modelPath, []byte("package models\n\ntype Item struct {\n\tID int64 `forge:\"primary,generated,protected,required\"`\n\tName string `forge:\"required\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &developmentBuildRecorder{output: filepath.Join(t.TempDir(), "server")}
	_, err := buildDevelopmentServer(context.Background(), nil, io.Discard, io.Discard, runner, runner.output)
	if err == nil || !strings.Contains(err.Error(), "generated ORM is stale") {
		t.Fatalf("stale ORM error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("stale ORM invoked view/build processes: %#v", runner.calls)
	}
}

func TestBuildDevelopmentServerRevalidatesProjectFormat(t *testing.T) {
	setupDevelopmentBuildProject(t)
	if err := os.WriteFile("forge.yaml", []byte("version: 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &developmentBuildRecorder{output: filepath.Join(t.TempDir(), "server")}
	_, err := buildDevelopmentServer(context.Background(), nil, io.Discard, io.Discard, runner, runner.output)
	if err == nil || !strings.Contains(err.Error(), "version 3") {
		t.Fatalf("runtime format transition error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unsupported format invoked processes: %#v", runner.calls)
	}
}

func setupDevelopmentBuildProject(t *testing.T) {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "forge.yaml"), []byte("version: 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	if err := os.MkdirAll(filepath.Join("internal", "models"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join("resources", "views"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("resources", "views", "index.forge.html"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("internal", "models", "item.go"), []byte("package models\n\ntype Item struct {\n\tID int64 `forge:\"primary,generated,protected,required\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := generateORM(false, io.Discard); err != nil {
		t.Fatal(err)
	}
}

type developmentBuildRecorder struct {
	calls      []processCall
	output     string
	viewOutput []byte
	buildErr   error
	onViewRun  func()
	onBuild    func()
	ctx        context.Context
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
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
	if len(arguments) > 0 && arguments[0] == "build" && arguments[len(arguments)-1] == "./cmd/server" {
		if runner.onBuild != nil {
			runner.onBuild()
		}
		if runner.buildErr != nil {
			return runner.buildErr
		}
		return os.WriteFile(runner.output, []byte("executable"), 0o755)
	}
	if len(arguments) > 0 && arguments[0] == "build" && arguments[len(arguments)-1] == "./cmd/views" {
		for index := range arguments {
			if arguments[index] == "-o" && index+1 < len(arguments) {
				return os.WriteFile(arguments[index+1], []byte("view compiler"), 0o755)
			}
		}
	}
	return nil
}

func (runner *developmentBuildRecorder) RunInDirectory(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	directory, name string,
	arguments ...string,
) error {
	runner.calls = append(runner.calls, processCall{name: name, args: append([]string{"-C", directory}, arguments...)})
	runner.ctx, runner.stdin, runner.stdout, runner.stderr = ctx, stdin, stdout, stderr
	if runner.onViewRun != nil {
		runner.onViewRun()
	}
	if runner.viewOutput == nil {
		runner.viewOutput = []byte("package views\n")
	}
	path := filepath.Join(directory, filepath.FromSlash(generatedViewsPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, runner.viewOutput, 0o644)
}
