//go:build windows

package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"testing"
	"time"
)

const (
	createNewProcessGroup = 0x00000200
	ctrlBreakEvent        = 1
)

var generateConsoleCtrlEvent = syscall.NewLazyDLL("kernel32.dll").NewProc("GenerateConsoleCtrlEvent")

func ignoreProcessTreeGracefulSignal() { signal.Ignore(os.Interrupt) }

func configureCommandProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func stopCommandProcess(t *testing.T, command *exec.Cmd, expectGraceful bool) {
	t.Helper()
	if command.Process == nil || command.ProcessState != nil {
		return
	}
	if err := sendConsoleBreak(command.Process.Pid); err != nil {
		t.Logf("signal process group: %v", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	grace := 5 * time.Second
	if !expectGraceful {
		grace = 2 * time.Second
	}
	select {
	case <-waited:
		return
	case <-time.After(grace):
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F").Run(); err != nil {
		t.Logf("taskkill process tree: %v", err)
		_ = command.Process.Kill()
	}
	select {
	case <-waited:
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		t.Fatal("process did not stop within 10 seconds")
	}
}

func sendConsoleBreak(processGroupID int) error {
	result, _, callErr := generateConsoleCtrlEvent.Call(ctrlBreakEvent, uintptr(processGroupID))
	if result != 0 {
		return nil
	}
	return fmt.Errorf("GenerateConsoleCtrlEvent: %w", callErr)
}
