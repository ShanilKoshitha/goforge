package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
	"sync"
	"sync/atomic"
	"time"

	forgeconfig "github.com/ShanilKoshitha/goforge/config"
	"github.com/ShanilKoshitha/goforge/view"
)

const (
	servePollInterval   = 100 * time.Millisecond
	serveDebounce       = 250 * time.Millisecond
	serveStartupTimeout = 10 * time.Second
	serveStopTimeout    = 10 * time.Second
	serveStartAttempts  = 5
)

type developmentProcess interface {
	Done() <-chan struct{}
	PID() int
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
	build   *developmentBuild
}

type developmentBuild struct {
	binary   string
	publish  func() error
	rollback func() error
	finalize func() error
}

type developmentBuildResult struct {
	candidate  *developmentCandidate
	snapshot   sourceSnapshot
	generation uint64
	err        error
}

type developmentServeDependencies struct {
	snapshot      func() (sourceSnapshot, error)
	readSnapshot  func(context.Context) (sourceSnapshot, error)
	waitForChange func(context.Context, sourceSnapshot, uint64) (sourceSnapshot, error)
	stabilize     func(context.Context, sourceSnapshot) (sourceSnapshot, error)
	generation    func() uint64
	build         func(context.Context) (*developmentBuild, error)
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
	if err := requireProjectFormatRange(minimumWorkflowFormat, currentProjectFormat); err != nil {
		return err
	}
	publicAddress, err := developmentPublicAddress()
	if err != nil {
		return err
	}
	trackedSource, err := takeSourceSnapshot(".")
	if err != nil {
		return fmt.Errorf("snapshot application source: %w", err)
	}
	tracker := startSourceGenerationTracker(ctx, ".", trackedSource, servePollInterval)
	defer tracker.Close()
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
		snapshot:     tracker.Snapshot,
		readSnapshot: tracker.Read,
		waitForChange: func(waitContext context.Context, previous sourceSnapshot, previousGeneration uint64) (sourceSnapshot, error) {
			return tracker.WaitForChange(waitContext, previous, previousGeneration, serveDebounce)
		},
		stabilize: func(waitContext context.Context, initial sourceSnapshot) (sourceSnapshot, error) {
			return tracker.Stabilize(waitContext, initial, serveDebounce)
		},
		generation: tracker.Generation,
		build: func(buildContext context.Context) (*developmentBuild, error) {
			if address, addressErr := developmentPublicAddress(); addressErr != nil {
				return nil, addressErr
			} else if address != publicAddress {
				return nil, fmt.Errorf("APP_ADDRESS changed from %q to %q; restart forge serve to move the public listener", publicAddress, address)
			}
			name := fmt.Sprintf("server-%06d", generation.Add(1))
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			path := filepath.Join(temporaryDirectory, name)
			build, buildErr := buildDevelopmentServer(buildContext, stdin, stdout, stderr, processes, path)
			if buildErr != nil {
				removeErr := os.Remove(path)
				if errors.Is(removeErr, os.ErrNotExist) {
					removeErr = nil
				}
				if removeErr != nil {
					removeErr = fmt.Errorf("remove failed development binary: %w", removeErr)
				}
				return nil, errors.Join(buildErr, removeErr)
			}
			return build, nil
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
	if ctx.Err() != nil && (err == nil || err == ctx.Err()) {
		return nil
	}
	return err
}

func superviseDevelopmentServer(ctx context.Context, dependencies developmentServeDependencies) (err error) {
	input, err := readDevelopmentSnapshot(ctx, dependencies)
	if err != nil {
		return fmt.Errorf("snapshot application source: %w", err)
	}
	var initial developmentBuildResult
	var active *developmentCandidate
	var proxy developmentProxy
	var latest sourceSnapshot
	var snapshotErr error
	for {
		initial = buildStableDevelopmentCandidate(ctx, input, dependencies)
		if initial.err != nil {
			return initial.err
		}
		latest, snapshotErr = readDevelopmentSnapshot(ctx, dependencies)
		if snapshotErr != nil {
			return joinDevelopmentError(snapshotErr, cleanupDevelopmentCandidate(initial.candidate, dependencies.removeBinary))
		}
		if latest != initial.snapshot || developmentGeneration(dependencies) != initial.generation {
			if cleanupErr := cleanupDevelopmentCandidate(initial.candidate, dependencies.removeBinary); cleanupErr != nil {
				return cleanupErr
			}
			input, err = dependencies.stabilize(ctx, latest)
			if err != nil {
				return err
			}
			continue
		}

		active = initial.candidate
		select {
		case <-active.process.Done():
			exitErr := developmentProcessExitError("development server exited before proxy startup", active.process.Wait())
			return joinDevelopmentError(exitErr, cleanupDevelopmentCandidate(active, dependencies.removeBinary))
		default:
		}
		if publishErr := publishDevelopmentBuild(active.build); publishErr != nil {
			cleanupErr := cleanupDevelopmentCandidate(active, dependencies.removeBinary)
			if cleanupErr != nil {
				return errors.Join(publishErr, cleanupErr)
			}
			latest, snapshotErr = readDevelopmentSnapshot(ctx, dependencies)
			if snapshotErr != nil {
				return snapshotErr
			}
			input, err = dependencies.stabilize(ctx, latest)
			if err != nil {
				return err
			}
			continue
		}
		select {
		case <-active.process.Done():
			return joinDevelopmentError(
				developmentProcessExitError("development server exited before proxy startup", active.process.Wait()),
				cleanupDevelopmentCandidate(active, dependencies.removeBinary),
			)
		default:
		}
		proxy, err = dependencies.startProxy(dependencies.publicAddress, active.target)
		if err != nil {
			return joinDevelopmentError(err, cleanupDevelopmentCandidate(active, dependencies.removeBinary))
		}
		latest, snapshotErr = readDevelopmentSnapshot(ctx, dependencies)
		stale := snapshotErr == nil && (latest != initial.snapshot || developmentGeneration(dependencies) != initial.generation)
		select {
		case <-active.process.Done():
			shutdownContext, cancel := context.WithTimeout(context.Background(), serveStopTimeout)
			closeErr := proxy.Close(shutdownContext)
			cancel()
			return errors.Join(
				developmentProcessExitError("development server exited during proxy startup", active.process.Wait()),
				closeErr,
				cleanupDevelopmentCandidate(active, dependencies.removeBinary),
			)
		default:
		}
		if snapshotErr == nil && !stale {
			break
		}
		shutdownContext, cancel := context.WithTimeout(context.Background(), serveStopTimeout)
		closeErr := proxy.Close(shutdownContext)
		cancel()
		cleanupErr := cleanupDevelopmentCandidate(active, dependencies.removeBinary)
		if snapshotErr != nil || closeErr != nil || cleanupErr != nil {
			return errors.Join(snapshotErr, closeErr, cleanupErr)
		}
		input, err = dependencies.stabilize(ctx, latest)
		if err != nil {
			return err
		}
	}
	defer func() {
		if cleanupErr := cleanupDevelopmentCandidate(active, dependencies.removeBinary); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), serveStopTimeout)
		defer cancel()
		if closeErr := proxy.Close(shutdownContext); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("stop development proxy: %w", closeErr))
		}
	}()
	if commitErr := commitDevelopmentBuild(active.build); commitErr != nil {
		return commitErr
	}

	fmt.Fprintf(dependencies.stdout, "serving http://%s (watching for changes)\n", displayDevelopmentAddress(proxy.Address()))
	baseline := initial.snapshot
	baselineGeneration := initial.generation

developmentLoop:
	for {
		var next sourceSnapshot
		if developmentGeneration(dependencies) != baselineGeneration {
			latest, readErr := readDevelopmentSnapshot(ctx, dependencies)
			if readErr != nil {
				return readErr
			}
			next, err = dependencies.stabilize(ctx, latest)
			if err != nil {
				return err
			}
		} else {
			var waitErr error
			next, waitErr = waitForDevelopmentChange(ctx, baseline, baselineGeneration, active.process, proxy, dependencies.waitForChange)
			if waitErr != nil {
				return waitErr
			}
		}
		for {
			result := buildStableDevelopmentCandidate(ctx, next, dependencies)
			if result.err != nil {
				if ctx.Err() != nil {
					return result.err
				}
				fmt.Fprintf(dependencies.stderr, "reload failed: %v\n", result.err)
				baseline = result.snapshot
				baselineGeneration = developmentGeneration(dependencies)
				continue developmentLoop
			}
			select {
			case <-result.candidate.process.Done():
				candidateErr := developmentProcessExitError("candidate server exited before promotion", result.candidate.process.Wait())
				if cleanupErr := cleanupDevelopmentCandidate(result.candidate, dependencies.removeBinary); cleanupErr != nil {
					return errors.Join(candidateErr, cleanupErr)
				}
				fmt.Fprintf(dependencies.stderr, "reload failed: %v\n", candidateErr)
				baseline = result.snapshot
				baselineGeneration = developmentGeneration(dependencies)
				continue developmentLoop
			default:
			}

			select {
			case <-active.process.Done():
				return joinDevelopmentError(
					developmentProcessExitError("development server exited", active.process.Wait()),
					cleanupDevelopmentCandidate(result.candidate, dependencies.removeBinary),
				)
			default:
			}
			select {
			case proxyErr := <-proxy.Done():
				cleanupErr := cleanupDevelopmentCandidate(result.candidate, dependencies.removeBinary)
				if proxyErr == nil {
					return joinDevelopmentError(errors.New("development proxy stopped unexpectedly"), cleanupErr)
				}
				return joinDevelopmentError(fmt.Errorf("development proxy stopped: %w", proxyErr), cleanupErr)
			default:
			}
			if developmentGeneration(dependencies) != result.generation {
				latest, snapshotErr := readDevelopmentSnapshot(ctx, dependencies)
				cleanupErr := cleanupDevelopmentCandidate(result.candidate, dependencies.removeBinary)
				if snapshotErr != nil || cleanupErr != nil {
					return errors.Join(snapshotErr, cleanupErr)
				}
				stable, stableErr := dependencies.stabilize(ctx, latest)
				if stableErr != nil {
					return stableErr
				}
				next = stable
				continue
			}
			if publishErr := publishDevelopmentBuild(result.candidate.build); publishErr != nil {
				cleanupErr := cleanupDevelopmentCandidate(result.candidate, dependencies.removeBinary)
				if cleanupErr != nil {
					return errors.Join(publishErr, cleanupErr)
				}
				latest, snapshotErr := readDevelopmentSnapshot(ctx, dependencies)
				if snapshotErr != nil {
					return snapshotErr
				}
				next, err = dependencies.stabilize(ctx, latest)
				if err != nil {
					return err
				}
				continue
			}
			select {
			case <-result.candidate.process.Done():
				candidateErr := developmentProcessExitError("candidate server exited before promotion", result.candidate.process.Wait())
				if cleanupErr := cleanupDevelopmentCandidate(result.candidate, dependencies.removeBinary); cleanupErr != nil {
					return errors.Join(candidateErr, cleanupErr)
				}
				fmt.Fprintf(dependencies.stderr, "reload failed: %v\n", candidateErr)
				baseline = result.snapshot
				baselineGeneration = developmentGeneration(dependencies)
				continue developmentLoop
			default:
			}
			select {
			case <-active.process.Done():
				return joinDevelopmentError(
					developmentProcessExitError("development server exited", active.process.Wait()),
					cleanupDevelopmentCandidate(result.candidate, dependencies.removeBinary),
				)
			default:
			}
			select {
			case proxyErr := <-proxy.Done():
				cleanupErr := cleanupDevelopmentCandidate(result.candidate, dependencies.removeBinary)
				if proxyErr == nil {
					return joinDevelopmentError(errors.New("development proxy stopped unexpectedly"), cleanupErr)
				}
				return joinDevelopmentError(fmt.Errorf("development proxy stopped: %w", proxyErr), cleanupErr)
			default:
			}
			if swapErr := proxy.SwapTarget(result.candidate.target); swapErr != nil {
				return joinDevelopmentError(
					fmt.Errorf("promote development server: %w", swapErr),
					cleanupDevelopmentCandidate(result.candidate, dependencies.removeBinary),
				)
			}
			latest, snapshotErr := readDevelopmentSnapshot(ctx, dependencies)
			stale := snapshotErr == nil && (latest != result.snapshot || developmentGeneration(dependencies) != result.generation)
			select {
			case <-result.candidate.process.Done():
				revertErr := proxy.SwapTarget(active.target)
				candidateErr := developmentProcessExitError("candidate server exited during promotion", result.candidate.process.Wait())
				cleanupErr := cleanupDevelopmentCandidate(result.candidate, dependencies.removeBinary)
				if revertErr != nil || cleanupErr != nil {
					return errors.Join(candidateErr, revertErr, cleanupErr)
				}
				fmt.Fprintf(dependencies.stderr, "reload failed: %v\n", candidateErr)
				baseline = result.snapshot
				baselineGeneration = developmentGeneration(dependencies)
				continue developmentLoop
			default:
			}
			if snapshotErr != nil || stale {
				revertErr := proxy.SwapTarget(active.target)
				cleanupErr := cleanupDevelopmentCandidate(result.candidate, dependencies.removeBinary)
				if snapshotErr != nil || revertErr != nil || cleanupErr != nil {
					return errors.Join(snapshotErr, revertErr, cleanupErr)
				}
				stable, stableErr := dependencies.stabilize(ctx, latest)
				if stableErr != nil {
					return stableErr
				}
				next = stable
				continue
			}
			previous := active
			active = result.candidate
			baseline = result.snapshot
			baselineGeneration = result.generation
			if cleanupErr := cleanupDevelopmentCandidate(previous, dependencies.removeBinary); cleanupErr != nil {
				return cleanupErr
			}
			if commitErr := commitDevelopmentBuild(active.build); commitErr != nil {
				return commitErr
			}
			fmt.Fprintln(dependencies.stdout, "reloaded development server")
			continue developmentLoop
		}
	}
}

func buildStableDevelopmentCandidate(
	ctx context.Context,
	input sourceSnapshot,
	dependencies developmentServeDependencies,
) developmentBuildResult {
	for {
		startedGeneration := developmentGeneration(dependencies)
		build, buildErr := dependencies.build(ctx)
		if ctx.Err() != nil {
			cleanupErr := discardDevelopmentBuild(build, dependencies.removeBinary)
			return developmentBuildResult{snapshot: input, err: joinDevelopmentError(ctx.Err(), cleanupErr)}
		}
		current, snapshotErr := readDevelopmentSnapshot(ctx, dependencies)
		if snapshotErr != nil {
			cleanupErr := discardDevelopmentBuild(build, dependencies.removeBinary)
			return developmentBuildResult{snapshot: input, err: joinDevelopmentError(fmt.Errorf("snapshot application source: %w", snapshotErr), cleanupErr)}
		}
		if current != input || developmentGeneration(dependencies) != startedGeneration {
			if discardErr := discardDevelopmentBuild(build, dependencies.removeBinary); discardErr != nil {
				return developmentBuildResult{snapshot: current, err: fmt.Errorf("discard stale development build: %w", discardErr)}
			}
			stable, waitErr := dependencies.stabilize(ctx, current)
			if waitErr != nil {
				return developmentBuildResult{snapshot: current, err: waitErr}
			}
			input = stable
			continue
		}
		if buildErr != nil {
			return developmentBuildResult{
				snapshot: current,
				err:      joinDevelopmentError(buildErr, discardDevelopmentBuild(build, dependencies.removeBinary)),
			}
		}

		candidate, startErr := dependencies.start(ctx, build.binary)
		if startErr != nil {
			discardErr := discardDevelopmentBuild(build, dependencies.removeBinary)
			if ctx.Err() != nil {
				return developmentBuildResult{snapshot: current, err: joinDevelopmentError(ctx.Err(), discardErr)}
			}
			startErr = joinDevelopmentError(startErr, discardErr)
			latest, latestErr := readDevelopmentSnapshot(ctx, dependencies)
			if latestErr != nil {
				return developmentBuildResult{snapshot: current, err: fmt.Errorf("snapshot application source: %w", latestErr)}
			}
			if latest != input || developmentGeneration(dependencies) != startedGeneration {
				stable, waitErr := dependencies.stabilize(ctx, latest)
				if waitErr != nil {
					return developmentBuildResult{snapshot: latest, err: waitErr}
				}
				input = stable
				continue
			}
			return developmentBuildResult{snapshot: latest, err: startErr}
		}
		latest, latestErr := readDevelopmentSnapshot(ctx, dependencies)
		if latestErr != nil {
			candidate.build = build
			cleanupErr := cleanupDevelopmentCandidate(candidate, dependencies.removeBinary)
			return developmentBuildResult{snapshot: current, err: joinDevelopmentError(fmt.Errorf("snapshot application source: %w", latestErr), cleanupErr)}
		}
		if latest != input || developmentGeneration(dependencies) != startedGeneration {
			candidate.build = build
			if discardErr := cleanupDevelopmentCandidate(candidate, dependencies.removeBinary); discardErr != nil {
				return developmentBuildResult{snapshot: latest, err: fmt.Errorf("discard stale development build: %w", discardErr)}
			}
			stable, waitErr := dependencies.stabilize(ctx, latest)
			if waitErr != nil {
				return developmentBuildResult{snapshot: latest, err: waitErr}
			}
			input = stable
			continue
		}
		candidate.build = build
		return developmentBuildResult{
			candidate:  candidate,
			snapshot:   latest,
			generation: developmentGeneration(dependencies),
		}
	}
}

func developmentGeneration(dependencies developmentServeDependencies) uint64 {
	if dependencies.generation == nil {
		return 0
	}
	return dependencies.generation()
}

func readDevelopmentSnapshot(ctx context.Context, dependencies developmentServeDependencies) (sourceSnapshot, error) {
	if dependencies.readSnapshot != nil {
		return dependencies.readSnapshot(ctx)
	}
	return dependencies.snapshot()
}

func discardDevelopmentBuild(build *developmentBuild, removeBinary func(string) error) error {
	if build == nil {
		return nil
	}
	return errors.Join(retryDevelopmentBuildRollback(build), removeBinary(build.binary))
}

func cleanupDevelopmentCandidate(candidate *developmentCandidate, removeBinary func(string) error) error {
	if candidate == nil {
		return nil
	}
	stopErr := candidate.process.Stop()
	build := candidate.build
	if build == nil {
		build = &developmentBuild{binary: candidate.binary}
	}
	cleanupErr := discardDevelopmentBuild(build, removeBinary)
	if stopErr != nil {
		stopErr = fmt.Errorf("stop development server: %w", stopErr)
	}
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("discard development build: %w", cleanupErr)
	}
	return errors.Join(stopErr, cleanupErr)
}

func joinDevelopmentError(primary, cleanup error) error {
	if cleanup == nil {
		return primary
	}
	if primary == nil {
		return cleanup
	}
	return errors.Join(primary, cleanup)
}

func rollbackDevelopmentBuild(build *developmentBuild) error {
	if build == nil || build.rollback == nil {
		return nil
	}
	rollback := build.rollback
	if err := rollback(); err != nil {
		return err
	}
	build.rollback = nil
	return nil
}

func retryDevelopmentBuildRollback(build *developmentBuild) error {
	var rollbackErr error
	for attempt := 0; attempt < 3; attempt++ {
		rollbackErr = rollbackDevelopmentBuild(build)
		if rollbackErr == nil {
			return nil
		}
		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * 25 * time.Millisecond)
		}
	}
	return rollbackErr
}

func publishDevelopmentBuild(build *developmentBuild) error {
	if build == nil || build.publish == nil {
		return nil
	}
	if err := build.publish(); err != nil {
		return err
	}
	build.publish = nil
	return nil
}

func commitDevelopmentBuild(build *developmentBuild) error {
	if build == nil {
		return nil
	}
	if build.finalize == nil {
		build.rollback = nil
		return nil
	}
	finalize := build.finalize
	if err := finalize(); err != nil {
		return err
	}
	build.finalize = nil
	build.rollback = nil
	return nil
}

func waitForDevelopmentChange(
	ctx context.Context,
	baseline sourceSnapshot,
	baselineGeneration uint64,
	active developmentProcess,
	proxy developmentProxy,
	waitForChange func(context.Context, sourceSnapshot, uint64) (sourceSnapshot, error),
) (sourceSnapshot, error) {
	watchContext, cancel := context.WithCancel(ctx)
	defer cancel()
	type watchResult struct {
		snapshot sourceSnapshot
		err      error
	}
	result := make(chan watchResult, 1)
	go func() {
		snapshot, err := waitForChange(watchContext, baseline, baselineGeneration)
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
) (_ *developmentBuild, err error) {
	lock, err := acquireGeneratorLock(ctx)
	if err != nil {
		return nil, err
	}
	if formatErr := requireProjectFormatRange(minimumWorkflowFormat, currentProjectFormat); formatErr != nil {
		return nil, errors.Join(formatErr, lock.Close())
	}
	projectFormat, err := projectFormat()
	if err != nil {
		return nil, errors.Join(err, lock.Close())
	}
	previousViews, err := captureDevelopmentFile(generatedViewsPath)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("capture last-good views: %w", err), lock.Close())
	}
	if ormErr := generateORM(true, stdout); ormErr != nil {
		return nil, errors.Join(ormErr, lock.Close())
	}
	publishedViews, viewCleanup, compileErr := stageDevelopmentViews(ctx, stdin, stdout, stderr, processes, output, projectFormat, previousViews)
	if compileErr != nil {
		return nil, errors.Join(compileErr, viewCleanup(), lock.Close())
	}
	if closeErr := lock.Close(); closeErr != nil {
		return nil, errors.Join(closeErr, viewCleanup())
	}

	candidateViewsPath := output + ".views.go"
	overlayPath := output + ".overlay.json"
	cleanupStaging := func() error {
		cleanupErr := viewCleanup()
		for _, path := range []string{candidateViewsPath, overlayPath} {
			if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove development staging file %s: %w", path, removeErr))
			}
		}
		return cleanupErr
	}
	if err := writeManagedFile(candidateViewsPath, publishedViews.data); err != nil {
		return nil, errors.Join(fmt.Errorf("stage candidate views: %w", err), cleanupStaging())
	}
	generatedAbsolute, err := filepath.Abs(generatedViewsPath)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("resolve generated views path: %w", err), cleanupStaging())
	}
	candidateAbsolute, err := filepath.Abs(candidateViewsPath)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("resolve candidate views path: %w", err), cleanupStaging())
	}
	overlay, err := json.Marshal(struct {
		Replace map[string]string
	}{Replace: map[string]string{generatedAbsolute: candidateAbsolute}})
	if err != nil {
		return nil, errors.Join(fmt.Errorf("encode development build overlay: %w", err), cleanupStaging())
	}
	if err := writeManagedFile(overlayPath, overlay); err != nil {
		return nil, errors.Join(fmt.Errorf("stage development build overlay: %w", err), cleanupStaging())
	}
	if err := processes.Run(ctx, stdin, stdout, stderr, "go", "build", "-trimpath", "-overlay", overlayPath, "-o", output, "./cmd/server"); err != nil {
		return nil, errors.Join(err, cleanupStaging())
	}
	info, err := os.Stat(output)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("inspect development server binary: %w", err), cleanupStaging())
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, errors.Join(errors.New("development server build produced no executable"), cleanupStaging())
	}
	if err := cleanupStaging(); err != nil {
		return nil, fmt.Errorf("clean development build staging: %w", err)
	}
	var transactionMu sync.Mutex
	publishedByBuild := false
	var promotionLock *generatorLock
	publish := func() error {
		transactionMu.Lock()
		defer transactionMu.Unlock()
		if publishedByBuild {
			return nil
		}
		lock, err := acquireDevelopmentGeneratorLock()
		if err != nil {
			return err
		}
		published, publishErr := compareAndSwapDevelopmentViewsLocked(previousViews, publishedViews, true)
		if published {
			publishedByBuild = true
			promotionLock = lock
			return publishErr
		}
		return errors.Join(publishErr, lock.Close())
	}
	rollback := func() error {
		transactionMu.Lock()
		defer transactionMu.Unlock()
		var rollbackErr error
		if publishedByBuild {
			var restored bool
			restored, rollbackErr = compareAndSwapDevelopmentViewsLocked(publishedViews, previousViews, false)
			if restored || rollbackErr == nil {
				publishedByBuild = false
				rollbackErr = errors.Join(rollbackErr, promotionLock.Close())
				promotionLock = nil
			}
		}
		return rollbackErr
	}
	finalize := func() error {
		transactionMu.Lock()
		defer transactionMu.Unlock()
		if !publishedByBuild {
			return nil
		}
		publishedByBuild = false
		lock := promotionLock
		promotionLock = nil
		return lock.Close()
	}
	return &developmentBuild{
		binary:   output,
		publish:  publish,
		rollback: rollback,
		finalize: finalize,
	}, nil
}

func stageDevelopmentViews(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	processes processRunner,
	output string,
	format int,
	previous developmentFileState,
) (developmentFileState, func() error, error) {
	cleanup := func() error { return nil }
	if format < 5 {
		found, err := hasDevelopmentViewSources()
		if err != nil {
			return developmentFileState{}, cleanup, err
		}
		if !found {
			return previous, cleanup, nil
		}
		compiled, err := compileLegacyForge(os.DirFS("resources/views"))
		if err != nil {
			return developmentFileState{}, cleanup, fmt.Errorf("compile project views: %w", err)
		}
		if _, err := view.ParseCompiled(compiled, nil); err != nil {
			return developmentFileState{}, cleanup, fmt.Errorf("validate project views: %w", err)
		}
		artifact, err := renderLegacyCompiledViews(compiled)
		if err != nil {
			return developmentFileState{}, cleanup, err
		}
		return developmentFileState{exists: true, mode: 0o644, data: []byte(artifact)}, cleanup, nil
	}

	directoryRunner, ok := processes.(directoryProcessRunner)
	if !ok {
		return developmentFileState{}, cleanup, errors.New("development process runner cannot execute the project view compiler in a staging directory")
	}
	compilerPath := output + ".views-compiler"
	if runtime.GOOS == "windows" {
		compilerPath += ".exe"
	}
	stagingRoot := output + ".views-root"
	cleanup = func() error {
		var cleanupErr error
		if err := os.Remove(compilerPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove staged view compiler: %w", err))
		}
		if err := os.RemoveAll(stagingRoot); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove staged view source: %w", err))
		}
		return cleanupErr
	}
	if err := processes.Run(ctx, stdin, stdout, stderr, "go", "build", "-trimpath", "-o", compilerPath, "./cmd/views"); err != nil {
		return developmentFileState{}, cleanup, err
	}
	if err := copyDevelopmentViewSources(stagingRoot); err != nil {
		return developmentFileState{}, cleanup, err
	}
	absoluteCompiler, err := filepath.Abs(compilerPath)
	if err != nil {
		return developmentFileState{}, cleanup, fmt.Errorf("resolve staged view compiler: %w", err)
	}
	if err := directoryRunner.RunInDirectory(ctx, stdin, stdout, stderr, stagingRoot, absoluteCompiler); err != nil {
		return developmentFileState{}, cleanup, err
	}
	candidate, err := captureDevelopmentFile(filepath.Join(stagingRoot, filepath.FromSlash(generatedViewsPath)))
	if err != nil {
		return developmentFileState{}, cleanup, fmt.Errorf("capture candidate views: %w", err)
	}
	if !candidate.exists {
		return developmentFileState{}, cleanup, errors.New("project view compiler produced no generated views")
	}
	return candidate, cleanup, nil
}

func copyDevelopmentViewSources(stagingRoot string) error {
	root := filepath.FromSlash("resources/views")
	if err := os.MkdirAll(filepath.Join(stagingRoot, root), 0o755); err != nil {
		return fmt.Errorf("create staged view directory: %w", err)
	}
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("inspect project views: %w", err)
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".forge.html") {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("project view %s is not a regular file", filepath.ToSlash(path))
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read project view %s: %w", filepath.ToSlash(path), err)
		}
		destination := filepath.Join(stagingRoot, path)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return fmt.Errorf("create staged view directory: %w", err)
		}
		if err := writeManagedFile(destination, content); err != nil {
			return fmt.Errorf("stage project view %s: %w", filepath.ToSlash(path), err)
		}
		return nil
	})
}

func hasDevelopmentViewSources() (bool, error) {
	found := false
	err := filepath.WalkDir(filepath.FromSlash("resources/views"), func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".forge.html") {
			found = true
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect project views: %w", err)
	}
	return found, nil
}

func rollbackDevelopmentViews(previous, published developmentFileState) error {
	_, err := compareAndSwapDevelopmentViews(published, previous, false)
	return err
}

func compareAndSwapDevelopmentViews(expected, replacement developmentFileState, mismatchIsError bool) (bool, error) {
	lock, err := acquireDevelopmentGeneratorLock()
	if err != nil {
		return false, err
	}
	swapped, swapErr := compareAndSwapDevelopmentViewsLocked(expected, replacement, mismatchIsError)
	return swapped, errors.Join(swapErr, lock.Close())
}

func acquireDevelopmentGeneratorLock() (*generatorLock, error) {
	rollbackContext, cancel := context.WithTimeout(context.Background(), serveStopTimeout)
	defer cancel()
	return acquireGeneratorLock(rollbackContext)
}

func compareAndSwapDevelopmentViewsLocked(expected, replacement developmentFileState, mismatchIsError bool) (bool, error) {
	current, captureErr := captureDevelopmentFile(generatedViewsPath)
	if captureErr != nil {
		return false, captureErr
	}
	if !current.equal(expected) {
		if mismatchIsError {
			return false, errors.New("compiled views changed while staging a development candidate")
		}
		return false, nil
	}
	restoreErr := replacement.restore(generatedViewsPath)
	installed := restoreErr == nil
	var inspectErr error
	if restoreErr != nil {
		var after developmentFileState
		after, inspectErr = captureDevelopmentFile(generatedViewsPath)
		installed = inspectErr == nil && after.equal(replacement)
	}
	return installed, errors.Join(restoreErr, inspectErr)
}

type developmentFileState struct {
	exists bool
	mode   os.FileMode
	data   []byte
}

func (state developmentFileState) equal(other developmentFileState) bool {
	return state.exists == other.exists && bytes.Equal(state.data, other.data)
}

func captureDevelopmentFile(path string) (developmentFileState, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return developmentFileState{}, nil
	}
	if err != nil {
		return developmentFileState{}, err
	}
	if !info.Mode().IsRegular() {
		return developmentFileState{}, fmt.Errorf("%s is not a regular file", filepath.ToSlash(path))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return developmentFileState{}, err
	}
	return developmentFileState{exists: true, mode: info.Mode().Perm(), data: data}, nil
}

func (state developmentFileState) restore(path string) error {
	if !state.exists {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := writeManagedFile(path, state.data); err != nil {
		return err
	}
	return os.Chmod(path, state.mode)
}

func startDevelopmentCandidate(
	ctx context.Context,
	binary string,
	stdout, stderr io.Writer,
) (*developmentCandidate, error) {
	return startDevelopmentCandidateWith(ctx, binary, stdout, stderr, developmentCandidateStartDependencies{
		reserve:     reserveDevelopmentAddress,
		environment: developmentCandidateEnvironment,
		start: func(spec managedProcessSpec) (developmentProcess, error) {
			return startManagedProcess(spec)
		},
		health: waitForCandidateHealth,
		ownsAddress: func(process developmentProcess, address string) (bool, error) {
			return processOwnsDevelopmentAddress(process.PID(), address)
		},
		attempts: serveStartAttempts,
	})
}

type developmentCandidateStartDependencies struct {
	reserve     func() (string, *url.URL, error)
	environment func(string) ([]string, error)
	start       func(managedProcessSpec) (developmentProcess, error)
	health      func(context.Context, developmentProcess, *url.URL, time.Duration) error
	ownsAddress func(developmentProcess, string) (bool, error)
	attempts    int
}

func startDevelopmentCandidateWith(
	ctx context.Context,
	binary string,
	stdout, stderr io.Writer,
	dependencies developmentCandidateStartDependencies,
) (*developmentCandidate, error) {
	var lastErr error
	startupContext, cancel := context.WithTimeout(ctx, serveStartupTimeout)
	defer cancel()
	for attempt := 1; attempt <= dependencies.attempts; attempt++ {
		address, target, err := dependencies.reserve()
		if err != nil {
			lastErr = err
			continue
		}
		environment, err := dependencies.environment(address)
		if err != nil {
			return nil, err
		}
		process, err := dependencies.start(managedProcessSpec{
			Name:        binary,
			Environment: environment,
			Stdout:      stdout,
			Stderr:      stderr,
		})
		if err == nil {
			err = dependencies.health(startupContext, process, target, serveStartupTimeout)
		}
		if err == nil {
			owned, ownerErr := dependencies.ownsAddress(process, address)
			if ownerErr != nil {
				err = fmt.Errorf("verify development server listener ownership: %w", ownerErr)
			} else if !owned {
				err = fmt.Errorf("development server process %d does not own %s", process.PID(), address)
			}
		}
		if err == nil {
			return &developmentCandidate{process: process, target: target, binary: binary}, nil
		}
		lastErr = err
		if process != nil {
			if stopErr := process.Stop(); stopErr != nil {
				return nil, errors.Join(lastErr, fmt.Errorf("stop failed development candidate: %w", stopErr))
			}
		}
		if startupContext.Err() != nil {
			return nil, fmt.Errorf("start development server within %s: %w", serveStartupTimeout, startupContext.Err())
		}
	}
	return nil, fmt.Errorf("start development server after %d attempts: %w", dependencies.attempts, lastErr)
}

func developmentCandidateEnvironment(address string) ([]string, error) {
	reader := forgeconfig.FromEnvironment()
	if err := reader.LoadFile(".env"); err != nil {
		return nil, err
	}
	trusted := developmentTrustedProxies(reader.String("TRUSTED_PROXIES", ""))
	environment := environmentWithOverride(os.Environ(), "APP_ADDRESS", address)
	environment = environmentWithOverride(environment, "TRUSTED_PROXIES", trusted)
	return environment, nil
}

func developmentTrustedProxies(configured string) string {
	values := []string{"127.0.0.1/32"}
	seen := map[string]struct{}{"127.0.0.1/32": {}}
	for _, value := range strings.Split(configured, ",") {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		values = append(values, value)
	}
	return strings.Join(values, ",")
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
			closeErr := response.Body.Close()
			if response.StatusCode == http.StatusOK && closeErr == nil {
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
