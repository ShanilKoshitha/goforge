package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

type processRunner interface {
	Run(context.Context, io.Reader, io.Writer, io.Writer, string, ...string) error
}

type directoryProcessRunner interface {
	RunInDirectory(context.Context, io.Reader, io.Writer, io.Writer, string, string, ...string) error
}

type execProcessRunner struct{}

func (execProcessRunner) Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	return runExecProcess(ctx, stdin, stdout, stderr, "", name, args...)
}

func (execProcessRunner) RunInDirectory(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	directory, name string,
	args ...string,
) error {
	return runExecProcess(ctx, stdin, stdout, stderr, directory, name, args...)
}

func runExecProcess(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	directory, name string,
	args ...string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	command := exec.Command(name, args...)
	command.Dir = directory
	if err := configureChildProcess(command); err != nil {
		return err
	}
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return err
	}
	tree, err := attachChildProcessTree(command)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
	}
	defer closeChildProcessTree(tree)
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	select {
	case err := <-waited:
		return err
	case <-ctx.Done():
		if err := stopChildProcessTree(command, waited, tree); err != nil {
			return errors.Join(ctx.Err(), err)
		}
		return ctx.Err()
	}
}

func runProjectCommand(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	processes processRunner,
	name string,
	args ...string,
) error {
	if err := requireProjectRoot(); err != nil {
		return err
	}
	return processes.Run(ctx, stdin, stdout, stderr, name, args...)
}

func requireProjectRoot() error {
	info, err := os.Stat("forge.yaml")
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("forge.yaml not found; run this command from a GoForge project root")
	}
	if err != nil {
		return fmt.Errorf("inspect forge.yaml: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("forge.yaml is not a regular file; run this command from a GoForge project root")
	}
	return nil
}
