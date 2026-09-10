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

func stopChildProcessTree(command *exec.Cmd, waited <-chan error, tree platformChildProcessTree, grace time.Duration) error {
	if command.Process == nil {
		return nil
	}
	processGroupID := tree.processGroupID
	if processGroupID <= 0 {
		processGroupID = command.Process.Pid
	}
	if err := syscall.Kill(-processGroupID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("stop child process group: %w", err)
	}
	directDone := false
	finished, err := waitForUnixProcessTree(waited, processGroupID, grace, &directDone)
	if err != nil {
		return err
	}
	if finished {
		return nil
	}
	if err := syscall.Kill(-processGroupID, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill child process group: %w", err)
	}
	finished, err = waitForUnixProcessTree(waited, processGroupID, 5*time.Second, &directDone)
	if err != nil {
		return err
	}
	if finished {
		return nil
	}
	return errors.New("child process group did not exit after forced termination")
}

func waitForUnixProcessTree(waited <-chan error, processGroupID int, timeout time.Duration, directDone *bool) (bool, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !*directDone {
			select {
			case <-waited:
				*directDone = true
			default:
			}
		}
		alive, err := unixProcessGroupAlive(processGroupID)
		if err != nil {
			return false, err
		}
		if *directDone && !alive {
			return true, nil
		}
		select {
		case <-deadline.C:
			return false, nil
		case <-ticker.C:
		}
	}
}

func unixProcessGroupAlive(processGroupID int) (bool, error) {
	err := syscall.Kill(-processGroupID, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true, nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	return false, fmt.Errorf("inspect child process group: %w", err)
}
