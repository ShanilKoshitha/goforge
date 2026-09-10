package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

const (
	developmentReadyMessage  = "development services ready\n"
	developmentLineBufferMax = 64 << 10
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
	mu        sync.Mutex
	buffer    []byte
	prefix    []byte
	marker    []byte
	service   string
	output    *synchronizedDevelopmentOutput
	dest      io.Writer
	readiness *developmentReadiness
}

func newDevelopmentLineWriter(
	service, marker string,
	output *synchronizedDevelopmentOutput,
	destination io.Writer,
	readiness *developmentReadiness,
) *developmentLineWriter {
	return &developmentLineWriter{
		prefix:    []byte("[" + service + "] "),
		marker:    []byte(marker),
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
	writer.buffer = append(writer.buffer, value...)
	for {
		newline := bytes.IndexByte(writer.buffer, '\n')
		if newline >= 0 {
			line := append([]byte(nil), writer.buffer[:newline+1]...)
			writer.buffer = writer.buffer[newline+1:]
			if err := writer.writeLine(line); err != nil {
				return consumed, err
			}
			continue
		}
		if len(writer.buffer) < developmentLineBufferMax {
			break
		}
		line := append([]byte(nil), writer.buffer[:developmentLineBufferMax]...)
		writer.buffer = writer.buffer[developmentLineBufferMax:]
		if err := writer.writeLine(line); err != nil {
			return consumed, err
		}
	}
	return consumed, nil
}

func (writer *developmentLineWriter) Flush() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.buffer) == 0 {
		return nil
	}
	line := append([]byte(nil), writer.buffer...)
	writer.buffer = writer.buffer[:0]
	return writer.writeLine(line)
}

func (writer *developmentLineWriter) writeLine(line []byte) error {
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
	if bytes.Contains(line, writer.marker) {
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
		name   string
		marker string
		run    func(context.Context, io.Writer, io.Writer) error
	}
	services := []serviceDefinition{
		{
			name:   "server",
			marker: "serving http://",
			run: func(serviceContext context.Context, serviceStdout, serviceStderr io.Writer) error {
				return serve(serviceContext, stdin, serviceStdout, serviceStderr, processes)
			},
		},
		{
			name:   "worker",
			marker: "worker_started",
			run: func(serviceContext context.Context, serviceStdout, serviceStderr io.Writer) error {
				return processes.Run(serviceContext, nil, serviceStdout, serviceStderr, "go", "run", "./cmd/worker")
			},
		},
		{
			name:   "scheduler",
			marker: "event=scheduler_started",
			run: func(serviceContext context.Context, serviceStdout, serviceStderr io.Writer) error {
				return processes.Run(serviceContext, nil, serviceStdout, serviceStderr, "go", "run", "./cmd/scheduler")
			},
		},
	}

	for _, service := range services {
		service := service
		go func() {
			serviceStdout := newDevelopmentLineWriter(service.name, service.marker, output, stdout, readiness)
			serviceStderr := newDevelopmentLineWriter(service.name, service.marker, output, stderr, readiness)
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
	return fmt.Errorf("forge dev: %s service exited %s: %w", root.name, phase, cause)
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
