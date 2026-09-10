package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	developmentReadyMessage                   = "development services ready\n"
	developmentLineBufferMax                  = 64 << 10
	developmentLineTruncation                 = " [output truncated]\n"
	developmentServiceProcessTreeGraceDefault = 20 * time.Second
	developmentServiceProcessTreeGraceEnv     = "FORGE_DEV_SHUTDOWN_TIMEOUT"
)

var errDevelopmentServiceExited = errors.New("service exited unexpectedly")

type projectServeRunner func(context.Context, io.Reader, io.Writer, io.Writer, processRunner) error

type developmentServiceResult struct {
	name string
	err  error
}

type synchronizedDevelopmentOutput struct {
	mu sync.Mutex
}

func (output *synchronizedDevelopmentOutput) write(destination io.Writer, value []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	return destination.Write(value)
}

type developmentReadiness struct {
	mu       sync.Mutex
	ready    map[string]bool
	stopped  map[string]bool
	output   *synchronizedDevelopmentOutput
	stdout   io.Writer
	complete bool
}

func newDevelopmentReadiness(output *synchronizedDevelopmentOutput, stdout io.Writer) *developmentReadiness {
	return &developmentReadiness{
		ready:   make(map[string]bool, 3),
		stopped: make(map[string]bool, 3),
		output:  output,
		stdout:  stdout,
	}
}

func (readiness *developmentReadiness) mark(name string) {
	readiness.mu.Lock()
	defer readiness.mu.Unlock()
	if readiness.ready[name] || len(readiness.stopped) != 0 || readiness.complete {
		return
	}
	readiness.ready[name] = true
	if len(readiness.ready) != 3 {
		return
	}
	readiness.complete = true
	_, _ = readiness.output.write(readiness.stdout, []byte(developmentReadyMessage))
}

func (readiness *developmentReadiness) stop(name string) {
	readiness.mu.Lock()
	defer readiness.mu.Unlock()
	readiness.stopped[name] = true
}

func (readiness *developmentReadiness) isComplete() bool {
	readiness.mu.Lock()
	defer readiness.mu.Unlock()
	return readiness.complete
}

type developmentLineWriter struct {
	mu         sync.Mutex
	buffer     []byte
	discarding bool
	prefix     []byte
	readyLine  func([]byte) bool
	service    string
	output     *synchronizedDevelopmentOutput
	dest       io.Writer
	readiness  *developmentReadiness
}

func newDevelopmentLineWriter(
	service string,
	readyLine func([]byte) bool,
	output *synchronizedDevelopmentOutput,
	destination io.Writer,
	readiness *developmentReadiness,
) *developmentLineWriter {
	return &developmentLineWriter{
		prefix:    []byte("[" + service + "] "),
		readyLine: readyLine,
		service:   service,
		output:    output,
		dest:      destination,
		readiness: readiness,
	}
}

func (writer *developmentLineWriter) Write(value []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	consumed := len(value)
	for len(value) != 0 {
		if writer.discarding {
			newline := bytes.IndexByte(value, '\n')
			if newline < 0 {
				return consumed, nil
			}
			writer.discarding = false
			value = value[newline+1:]
			continue
		}

		capacity := developmentLineBufferMax - len(writer.buffer)
		newline := bytes.IndexByte(value, '\n')
		if newline >= 0 && newline <= capacity {
			writer.buffer = append(writer.buffer, value[:newline+1]...)
			value = value[newline+1:]
			line := append([]byte(nil), writer.buffer...)
			writer.buffer = writer.buffer[:0]
			if err := writer.writeLine(line, true); err != nil {
				return consumed, err
			}
			continue
		}

		if newline < 0 && len(value) <= capacity {
			writer.buffer = append(writer.buffer, value...)
			break
		}
		writer.buffer = append(writer.buffer, value[:capacity]...)
		value = value[capacity:]
		line := make([]byte, 0, len(writer.buffer)+len(developmentLineTruncation))
		line = append(line, writer.buffer...)
		line = append(line, developmentLineTruncation...)
		writer.buffer = writer.buffer[:0]
		writer.discarding = true
		if err := writer.writeLine(line, false); err != nil {
			return consumed, err
		}
	}
	return consumed, nil
}

func (writer *developmentLineWriter) Flush() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.buffer) == 0 || writer.discarding {
		writer.buffer = writer.buffer[:0]
		writer.discarding = false
		return nil
	}
	line := append([]byte(nil), writer.buffer...)
	writer.buffer = writer.buffer[:0]
	return writer.writeLine(line, true)
}

func (writer *developmentLineWriter) writeLine(line []byte, detectReady bool) error {
	value := make([]byte, 0, len(writer.prefix)+len(line))
	value = append(value, writer.prefix...)
	value = append(value, line...)
	written, err := writer.output.write(writer.dest, value)
	if err == nil && written != len(value) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return err
	}
	if detectReady && writer.readyLine != nil && writer.readyLine(line) {
		writer.readiness.mark(writer.service)
	}
	return nil
}

func runProjectDev(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	processes processRunner,
) error {
	return runProjectDevWith(ctx, stdin, stdout, stderr, processes, runProjectServe)
}

func runProjectDevWith(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	processes processRunner,
	serve projectServeRunner,
) error {
	if err := requireProjectRoot(); err != nil {
		return err
	}
	if err := requireProjectFormatRange(11, 11); err != nil {
		return err
	}
	processTreeGrace, err := developmentProcessTreeGrace()
	if err != nil {
		return err
	}
	processes = processRunnerWithGrace(processes, processTreeGrace)
	return superviseDevelopmentServices(ctx, stdin, stdout, stderr, processes, serve)
}

func superviseDevelopmentServices(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	processes processRunner,
	serve projectServeRunner,
) error {
	childContext, cancel := context.WithCancel(ctx)
	defer cancel()

	output := &synchronizedDevelopmentOutput{}
	readiness := newDevelopmentReadiness(output, stdout)
	results := make(chan developmentServiceResult, 3)

	type serviceDefinition struct {
		name        string
		stdoutReady func([]byte) bool
		stderrReady func([]byte) bool
		run         func(context.Context, io.Writer, io.Writer) error
	}
	services := []serviceDefinition{
		{
			name:        "server",
			stdoutReady: developmentServerReadyLine,
			run: func(serviceContext context.Context, serviceStdout, serviceStderr io.Writer) error {
				return serve(serviceContext, stdin, serviceStdout, serviceStderr, processes)
			},
		},
		{
			name:        "worker",
			stderrReady: developmentWorkerReadyLine,
			run: func(serviceContext context.Context, serviceStdout, serviceStderr io.Writer) error {
				return processes.Run(serviceContext, nil, serviceStdout, serviceStderr, "go", "run", "./cmd/worker")
			},
		},
		{
			name:        "scheduler",
			stderrReady: developmentSchedulerReadyLine,
			run: func(serviceContext context.Context, serviceStdout, serviceStderr io.Writer) error {
				return processes.Run(serviceContext, nil, serviceStdout, serviceStderr, "go", "run", "./cmd/scheduler")
			},
		},
	}

	for _, service := range services {
		service := service
		go func() {
			serviceStdout := newDevelopmentLineWriter(service.name, service.stdoutReady, output, stdout, readiness)
			serviceStderr := newDevelopmentLineWriter(service.name, service.stderrReady, output, stderr, readiness)
			err := service.run(childContext, serviceStdout, serviceStderr)
			readiness.stop(service.name)
			err = errors.Join(err, serviceStdout.Flush(), serviceStderr.Flush())
			results <- developmentServiceResult{name: service.name, err: err}
		}()
	}

	var root *developmentServiceResult
	var shutdownErr error
	parentCancelled := false
	completed := 0
	for completed < len(services) {
		if root == nil && !parentCancelled {
			select {
			case <-ctx.Done():
				parentCancelled = true
				cancel()
				continue
			case result := <-results:
				completed++
				if ctx.Err() != nil {
					parentCancelled = true
					cancel()
					if unexpected := unexpectedDevelopmentShutdownError(result.err); unexpected != nil {
						shutdownErr = errors.Join(shutdownErr, fmt.Errorf("%s service shutdown: %w", result.name, unexpected))
					}
					continue
				}
				root = &result
				cancel()
				continue
			}
		}
		result := <-results
		completed++
		unexpected := unexpectedDevelopmentShutdownError(result.err)
		if parentCancelled && unexpected != nil {
			shutdownErr = errors.Join(shutdownErr, fmt.Errorf("%s service shutdown: %w", result.name, unexpected))
		}
		if root != nil && result.name != root.name && unexpected != nil {
			root.err = errors.Join(root.err, fmt.Errorf("%s service shutdown: %w", result.name, unexpected))
		}
	}

	if parentCancelled {
		return shutdownErr
	}
	if root == nil {
		return nil
	}
	cause := root.err
	if cause == nil {
		cause = errDevelopmentServiceExited
	}
	phase := "before all services were ready"
	if readiness.isComplete() {
		phase = "after all services were ready"
	}
	return &developmentServiceError{service: root.name, phase: phase, cause: cause}
}

type developmentServiceError struct {
	service string
	phase   string
	cause   error
}

func (err *developmentServiceError) Error() string {
	return fmt.Sprintf("forge dev: %s service exited %s: %v", err.service, err.phase, err.cause)
}

func (err *developmentServiceError) Unwrap() error { return err.cause }

func (err *developmentServiceError) ReportCLIError() bool { return true }

func developmentServerReadyLine(line []byte) bool {
	value := developmentOutputLine(line)
	return strings.HasPrefix(value, "serving http://") && strings.HasSuffix(value, " (watching for changes)")
}

func developmentWorkerReadyLine(line []byte) bool {
	value := developmentOutputLine(line)
	return developmentOutputHasField(value, "INFO job worker_started") ||
		developmentOutputHasField(value, `msg="job worker_started"`)
}

func developmentSchedulerReadyLine(line []byte) bool {
	value := developmentOutputLine(line)
	message := developmentOutputHasField(value, "INFO scheduler started") ||
		developmentOutputHasField(value, `msg="scheduler started"`)
	return message &&
		developmentOutputHasField(value, "event=scheduler_started") &&
		developmentOutputHasField(value, "mode=work")
}

func developmentOutputLine(line []byte) string {
	return strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
}

func developmentOutputHasField(line, field string) bool {
	for offset := 0; ; {
		index := strings.Index(line[offset:], field)
		if index < 0 {
			return false
		}
		index += offset
		beforeOK := index == 0 || line[index-1] == ' ' || line[index-1] == '\t'
		after := index + len(field)
		afterOK := after == len(line) || line[after] == ' ' || line[after] == '\t'
		if beforeOK && afterOK {
			return true
		}
		offset = index + 1
	}
}

func developmentProcessTreeGrace() (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(developmentServiceProcessTreeGraceEnv))
	if raw == "" {
		return developmentServiceProcessTreeGraceDefault, nil
	}
	grace, err := time.ParseDuration(raw)
	if err != nil || grace <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", developmentServiceProcessTreeGraceEnv)
	}
	return grace, nil
}

func unexpectedDevelopmentShutdownError(err error) error {
	if err == nil || err == context.Canceled || err == context.DeadlineExceeded {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		unexpected := make([]error, 0, len(joined.Unwrap()))
		for _, cause := range joined.Unwrap() {
			if cause = unexpectedDevelopmentShutdownError(cause); cause != nil {
				unexpected = append(unexpected, cause)
			}
		}
		return errors.Join(unexpected...)
	}
	if wrapped := errors.Unwrap(err); wrapped != nil && unexpectedDevelopmentShutdownError(wrapped) == nil {
		return nil
	}
	return err
}
