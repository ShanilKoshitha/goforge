package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	forgeconfig "github.com/ShanilKoshitha/goforge/config"
)

const (
	servePollInterval   = 100 * time.Millisecond
	serveDebounce       = 250 * time.Millisecond
	serveStartupTimeout = 10 * time.Second
	serveStopTimeout    = 10 * time.Second
)

type developmentProcess interface {
	Done() <-chan struct{}
	Wait() error
	Stop() error
}

type developmentProxy interface {
	Address() string
	SwapTarget(*url.URL) error
	Done() <-chan error
	Close(context.Context) error
}

type developmentCandidate struct {
	process developmentProcess
	target  *url.URL
	binary  string
}

type developmentBuildResult struct {
	candidate *developmentCandidate
	snapshot  sourceSnapshot
	err       error
}

type developmentServeDependencies struct {
	snapshot      func() (sourceSnapshot, error)
	waitForChange func(context.Context, sourceSnapshot) (sourceSnapshot, error)
	build         func(context.Context) (string, error)
	removeBinary  func(string) error
	start         func(context.Context, string) (*developmentCandidate, error)
	startProxy    func(string, *url.URL) (developmentProxy, error)
	publicAddress string
	stdout        io.Writer
	stderr        io.Writer
}

func runProjectServe(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	processes processRunner,
) (err error) {
	if err := requireProjectRoot(); err != nil {
		return err
	}
	if err := requireProjectFormatRange(minimumWorkflowFormat, maximumWorkflowFormat); err != nil {
		return err
	}
	publicAddress, err := developmentPublicAddress()
	if err != nil {
		return err
	}
	temporaryDirectory, err := os.MkdirTemp("", "goforge-serve-")
	if err != nil {
		return fmt.Errorf("create development build directory: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(temporaryDirectory); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("remove development build directory: %w", removeErr))
		}
	}()

	var generation atomic.Uint64
	dependencies := developmentServeDependencies{
		snapshot: func() (sourceSnapshot, error) {
			return takeSourceSnapshot(".")
		},
		waitForChange: func(waitContext context.Context, previous sourceSnapshot) (sourceSnapshot, error) {
			return waitForChangedSourceSnapshot(waitContext, ".", previous, servePollInterval, serveDebounce)
		},
		build: func(buildContext context.Context) (string, error) {
			if address, addressErr := developmentPublicAddress(); addressErr != nil {
				return "", addressErr
			} else if address != publicAddress {
				return "", fmt.Errorf("APP_ADDRESS changed from %q to %q; restart forge serve to move the public listener", publicAddress, address)
			}
			name := fmt.Sprintf("server-%06d", generation.Add(1))
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			path := filepath.Join(temporaryDirectory, name)
			if buildErr := buildDevelopmentServer(buildContext, stdin, stdout, stderr, processes, path); buildErr != nil {
				_ = os.Remove(path)
				return "", buildErr
			}
			return path, nil
		},
		removeBinary: func(path string) error {
			if path == "" {
				return nil
			}
			if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return removeErr
			}
			return nil
		},
		start: func(startContext context.Context, binary string) (*developmentCandidate, error) {
			return startDevelopmentCandidate(startContext, binary, stdout, stderr)
		},
		startProxy: func(address string, target *url.URL) (developmentProxy, error) {
			return startServeProxy(address, target)
		},
		publicAddress: publicAddress,
		stdout:        stdout,
		stderr:        stderr,
	}
	err = superviseDevelopmentServer(ctx, dependencies)
	if ctx.Err() != nil && (err == nil || errors.Is(err, ctx.Err())) {
		return nil
	}
	return err
}

func superviseDevelopmentServer(ctx context.Context, dependencies developmentServeDependencies) (err error) {
	input, err := dependencies.snapshot()
	if err != nil {
		return fmt.Errorf("snapshot application source: %w", err)
	}
	initial := buildStableDevelopmentCandidate(ctx, input, dependencies)
	if initial.err != nil {
		return initial.err
	}
	active := initial.candidate
	defer func() {
		if stopErr := active.process.Stop(); stopErr != nil {
			err = errors.Join(err, fmt.Errorf("stop development server: %w", stopErr))
		}
		if removeErr := dependencies.removeBinary(active.binary); removeErr != nil {
			err = errors.Join(err, fmt.Errorf("remove development server binary: %w", removeErr))
		}
	}()

	proxy, err := dependencies.startProxy(dependencies.publicAddress, active.target)
	if err != nil {
		return err
	}
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), serveStopTimeout)
		defer cancel()
		if closeErr := proxy.Close(shutdownContext); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("stop development proxy: %w", closeErr))
		}
	}()

	fmt.Fprintf(dependencies.stdout, "serving http://%s (watching for changes)\n", displayDevelopmentAddress(proxy.Address()))
	baseline := initial.snapshot
	for {
		next, waitErr := waitForDevelopmentChange(ctx, baseline, active.process, proxy, dependencies.waitForChange)
		if waitErr != nil {
			return waitErr
		}
		result := buildStableDevelopmentCandidate(ctx, next, dependencies)
		if result.err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Fprintf(dependencies.stderr, "reload failed: %v\n", result.err)
			baseline = result.snapshot
			continue
		}

		select {
		case <-active.process.Done():
			_ = result.candidate.process.Stop()
			_ = dependencies.removeBinary(result.candidate.binary)
			return developmentProcessExitError("development server exited", active.process.Wait())
		default:
		}
		select {
		case proxyErr := <-proxy.Done():
			_ = result.candidate.process.Stop()
			_ = dependencies.removeBinary(result.candidate.binary)
			if proxyErr == nil {
				return errors.New("development proxy stopped unexpectedly")
			}
			return fmt.Errorf("development proxy stopped: %w", proxyErr)
		default:
		}
		if swapErr := proxy.SwapTarget(result.candidate.target); swapErr != nil {
			_ = result.candidate.process.Stop()
			_ = dependencies.removeBinary(result.candidate.binary)
			return fmt.Errorf("promote development server: %w", swapErr)
		}
		previous := active
		active = result.candidate
		baseline = result.snapshot
		if stopErr := previous.process.Stop(); stopErr != nil {
			return fmt.Errorf("stop replaced development server: %w", stopErr)
		}
		if removeErr := dependencies.removeBinary(previous.binary); removeErr != nil {
			return fmt.Errorf("remove replaced development server binary: %w", removeErr)
		}
		fmt.Fprintln(dependencies.stdout, "reloaded development server")
	}
}

func buildStableDevelopmentCandidate(
	ctx context.Context,
	input sourceSnapshot,
	dependencies developmentServeDependencies,
) developmentBuildResult {
	for {
		binary, buildErr := dependencies.build(ctx)
		if ctx.Err() != nil {
			_ = dependencies.removeBinary(binary)
			return developmentBuildResult{snapshot: input, err: ctx.Err()}
		}
		current, snapshotErr := dependencies.snapshot()
		if snapshotErr != nil {
			_ = dependencies.removeBinary(binary)
			return developmentBuildResult{snapshot: input, err: fmt.Errorf("snapshot application source: %w", snapshotErr)}
		}
		if current != input {
			_ = dependencies.removeBinary(binary)
			stable, waitErr := dependencies.waitForChange(ctx, input)
			if waitErr != nil {
				return developmentBuildResult{snapshot: current, err: waitErr}
			}
			input = stable
			continue
		}
		if buildErr != nil {
			return developmentBuildResult{snapshot: current, err: buildErr}
		}

		candidate, startErr := dependencies.start(ctx, binary)
		if startErr != nil {
			_ = dependencies.removeBinary(binary)
			if ctx.Err() != nil {
				return developmentBuildResult{snapshot: current, err: ctx.Err()}
			}
			latest, latestErr := dependencies.snapshot()
			if latestErr != nil {
				return developmentBuildResult{snapshot: current, err: fmt.Errorf("snapshot application source: %w", latestErr)}
			}
			if latest != input {
				stable, waitErr := dependencies.waitForChange(ctx, input)
				if waitErr != nil {
					return developmentBuildResult{snapshot: latest, err: waitErr}
				}
				input = stable
				continue
			}
			return developmentBuildResult{snapshot: latest, err: startErr}
		}
		latest, latestErr := dependencies.snapshot()
		if latestErr != nil {
			_ = candidate.process.Stop()
			_ = dependencies.removeBinary(binary)
			return developmentBuildResult{snapshot: current, err: fmt.Errorf("snapshot application source: %w", latestErr)}
		}
		if latest != input {
			_ = candidate.process.Stop()
			_ = dependencies.removeBinary(binary)
			stable, waitErr := dependencies.waitForChange(ctx, input)
			if waitErr != nil {
				return developmentBuildResult{snapshot: latest, err: waitErr}
			}
			input = stable
			continue
		}
		return developmentBuildResult{candidate: candidate, snapshot: latest}
	}
}

func waitForDevelopmentChange(
	ctx context.Context,
	baseline sourceSnapshot,
	active developmentProcess,
	proxy developmentProxy,
	waitForChange func(context.Context, sourceSnapshot) (sourceSnapshot, error),
) (sourceSnapshot, error) {
	watchContext, cancel := context.WithCancel(ctx)
	defer cancel()
	type watchResult struct {
		snapshot sourceSnapshot
		err      error
	}
	result := make(chan watchResult, 1)
	go func() {
		snapshot, err := waitForChange(watchContext, baseline)
		result <- watchResult{snapshot: snapshot, err: err}
	}()
	select {
	case <-ctx.Done():
		return sourceSnapshot{}, ctx.Err()
	case <-active.Done():
		return sourceSnapshot{}, developmentProcessExitError("development server exited", active.Wait())
	case proxyErr := <-proxy.Done():
		if proxyErr == nil {
			return sourceSnapshot{}, errors.New("development proxy stopped unexpectedly")
		}
		return sourceSnapshot{}, fmt.Errorf("development proxy stopped: %w", proxyErr)
	case changed := <-result:
		if changed.err != nil {
			return sourceSnapshot{}, fmt.Errorf("watch application source: %w", changed.err)
		}
		return changed.snapshot, nil
	}
}

func buildDevelopmentServer(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	processes processRunner,
	output string,
) (err error) {
	lock, err := acquireGeneratorLock(ctx)
	if err != nil {
		return err
	}
	if compileErr := prepareProjectViews(ctx, stdin, stdout, stderr, processes); compileErr != nil {
		err = compileErr
	}
	if closeErr := lock.Close(); closeErr != nil {
		err = errors.Join(err, closeErr)
	}
	if err != nil {
		return err
	}
	if err := processes.Run(ctx, stdin, stdout, stderr, "go", "build", "-trimpath", "-o", output, "./cmd/server"); err != nil {
		return err
	}
	info, err := os.Stat(output)
	if err != nil {
		return fmt.Errorf("inspect development server binary: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("development server build produced no executable")
	}
	return nil
}

func startDevelopmentCandidate(
	ctx context.Context,
	binary string,
	stdout, stderr io.Writer,
) (*developmentCandidate, error) {
	address, target, err := reserveDevelopmentAddress()
	if err != nil {
		return nil, err
	}
	process, err := startManagedProcess(managedProcessSpec{
		Name:        binary,
		Environment: environmentWithOverride(os.Environ(), "APP_ADDRESS", address),
		Stdout:      stdout,
		Stderr:      stderr,
	})
	if err != nil {
		return nil, err
	}
	if err := waitForCandidateHealth(ctx, process, target, serveStartupTimeout); err != nil {
		stopErr := process.Stop()
		return nil, errors.Join(err, stopErr)
	}
	return &developmentCandidate{process: process, target: target, binary: binary}, nil
}

func reserveDevelopmentAddress() (string, *url.URL, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("reserve development server address: %w", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", nil, fmt.Errorf("release development server address: %w", err)
	}
	target := &url.URL{Scheme: "http", Host: address}
	return address, target, nil
}

func waitForCandidateHealth(
	ctx context.Context,
	process developmentProcess,
	target *url.URL,
	timeout time.Duration,
) error {
	probeContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: 300 * time.Millisecond}
	probeURL := *target
	probeURL.Path = "/health"
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		request, err := http.NewRequestWithContext(probeContext, http.MethodGet, probeURL.String(), nil)
		if err != nil {
			return fmt.Errorf("create development health probe: %w", err)
		}
		response, requestErr := client.Do(request)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-process.Done():
			return developmentProcessExitError("development server exited before becoming healthy", process.Wait())
		case <-probeContext.Done():
			return fmt.Errorf("development server did not become healthy within %s: %w", timeout, probeContext.Err())
		case <-ticker.C:
		}
	}
}

func developmentProcessExitError(message string, err error) error {
	if err == nil {
		return errors.New(message)
	}
	return fmt.Errorf("%s: %w", message, err)
}

func developmentPublicAddress() (string, error) {
	reader := forgeconfig.FromEnvironment()
	if err := reader.LoadFile(".env"); err != nil {
		return "", err
	}
	address := strings.TrimSpace(reader.String("APP_ADDRESS", ":8080"))
	if address == "" {
		return "", errors.New("APP_ADDRESS must not be empty")
	}
	return address, nil
}

func environmentWithOverride(environment []string, key, value string) []string {
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if found && strings.EqualFold(name, key) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, key+"="+value)
}

func displayDevelopmentAddress(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
}
