package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/ShanilKoshitha/goforge/internal/cli"
)

type cliRunner func(context.Context, []string, io.Reader, io.Writer, io.Writer) error

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := runCLI(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, cli.RunContext)
	stop()
	if code != 0 {
		os.Exit(code)
	}
}

func runCLI(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	run cliRunner,
) int {
	err := run(ctx, args, stdin, stdout, stderr)
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		if code := exitError.ExitCode(); code >= 0 {
			return code
		}
		// A process terminated by a signal has no portable numeric exit code.
		// Its output has already reached stderr, so retain a quiet failure.
		return 1
	}
	fmt.Fprintln(stderr, "error:", err)
	return 1
}
