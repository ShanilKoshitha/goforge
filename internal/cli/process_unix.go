//go:build aix || android || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris

package cli

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

const processTreeControlSupported = true

func configureChildProcess(command *exec.Cmd) error {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return nil
}

type platformChildProcessTree struct{ processGroupID int }

func attachChildProcessTree(command *exec.Cmd) (platformChildProcessTree, error) {
	return platformChildProcessTree{processGroupID: command.Process.Pid}, nil
}

func closeChildProcessTree(tree platformChildProcessTree) {
	if tree.processGroupID > 0 {
		_ = syscall.Kill(-tree.processGroupID, syscall.SIGKILL)
	}
}

func stopChildProcessTree(command *exec.Cmd, waited <-chan error, _ platformChildProcessTree) error {
	if command.Process == nil {
		return nil
	}
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("stop child process group: %w", err)
	}
	select {
	case <-waited:
		// The direct process may exit before a descendant that ignored SIGTERM.
		// Kill the still-addressable process group before its ID can be reused.
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			return fmt.Errorf("kill remaining child process group: %w", err)
		}
		return nil
	case <-time.After(5 * time.Second):
	}
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill child process group: %w", err)
	}
	select {
	case <-waited:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("child process group did not exit after forced termination")
	}
}
