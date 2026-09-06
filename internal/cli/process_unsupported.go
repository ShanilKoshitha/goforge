//go:build plan9 || js || wasip1

package cli

import (
	"errors"
	"os/exec"
)

const processTreeControlSupported = false

type platformChildProcessTree struct{}

func configureChildProcess(*exec.Cmd) error {
	return errors.New("project command process-tree control is unsupported on this platform")
}

func attachChildProcessTree(*exec.Cmd) (platformChildProcessTree, error) {
	return platformChildProcessTree{}, nil
}

func closeChildProcessTree(platformChildProcessTree) {}

func stopChildProcessTree(*exec.Cmd, <-chan error, platformChildProcessTree) error { return nil }
