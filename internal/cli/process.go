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

type execProcessRunner struct{}

func (execProcessRunner) Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	return command.Run()
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
