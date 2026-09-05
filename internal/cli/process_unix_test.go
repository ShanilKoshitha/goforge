//go:build !windows

package cli

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func configureCommandProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func stopCommandProcess(t *testing.T, command *exec.Cmd, expectGraceful bool) {
	t.Helper()
	if command.Process == nil || command.ProcessState != nil {
		return
	}
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	select {
	case err := <-waited:
		if expectGraceful && err != nil {
			t.Fatalf("generated server did not shut down gracefully after SIGTERM: %v", err)
		}
	case <-time.After(10 * time.Second):
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-waited
		t.Fatal("process did not stop within 10 seconds")
	}
}
