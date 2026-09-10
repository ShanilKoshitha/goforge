package cli

import (
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// managedProcessSpec describes one child without relying on ambient process
// state. An empty Directory uses the caller's working directory. A nil
// Environment inherits the caller's environment, matching exec.Cmd.
type managedProcessSpec struct {
	Name             string
	Arguments        []string
	Directory        string
	Environment      []string
	Stdin            io.Reader
	Stdout           io.Writer
	Stderr           io.Writer
	ProcessTreeGrace time.Duration
}

// managedProcess owns a child and its complete platform process tree. Wait is
// safe to call repeatedly, and Stop is safe to call repeatedly or concurrently.
type managedProcess struct {
	command          *exec.Cmd
	tree             platformChildProcessTree
	processTreeGrace time.Duration

	done   chan struct{}
	waited chan error

	stateMu  sync.Mutex
	exitErr  error
	finished bool
	stopping bool

	stopOnce  sync.Once
	stopErr   error
	closeOnce sync.Once
}

func startManagedProcess(spec managedProcessSpec) (*managedProcess, error) {
	if spec.Name == "" {
		return nil, fmt.Errorf("managed process command is required")
	}
	command := exec.Command(spec.Name, spec.Arguments...)
	command.Dir = spec.Directory
	if spec.Environment != nil {
		command.Env = append([]string(nil), spec.Environment...)
	}
	command.Stdin = spec.Stdin
	command.Stdout = spec.Stdout
	command.Stderr = spec.Stderr
	if err := configureChildProcess(command); err != nil {
		return nil, fmt.Errorf("configure managed process %s: %w", spec.Name, err)
	}
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start managed process %s: %w", spec.Name, err)
	}
	tree, err := attachChildProcessTree(command)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return nil, fmt.Errorf("attach managed process %s: %w", spec.Name, err)
	}

	process := &managedProcess{
		command:          command,
		tree:             tree,
		processTreeGrace: normalizedProcessTreeGrace(spec.ProcessTreeGrace),
		done:             make(chan struct{}),
		waited:           make(chan error, 1),
	}
	go process.collect()
	return process, nil
}

func (process *managedProcess) collect() {
	err := process.command.Wait()

	process.stateMu.Lock()
	process.exitErr = err
	process.finished = true
	closeWithoutStop := !process.stopping
	process.stateMu.Unlock()

	if closeWithoutStop {
		process.close()
	}
	close(process.done)
	process.waited <- err
	close(process.waited)
}

// Done is closed after the child exits and its exit error has been cached. If
// the child exits without an explicit Stop, its process-tree handle has also
// been released before Done is closed.
func (process *managedProcess) Done() <-chan struct{} {
	return process.done
}

func (process *managedProcess) PID() int {
	return process.command.Process.Pid
}

// Wait waits for the child and returns the cached result. Repeated calls return
// the same error without calling exec.Cmd.Wait again.
func (process *managedProcess) Wait() error {
	<-process.done
	process.stateMu.Lock()
	defer process.stateMu.Unlock()
	return process.exitErr
}

// Stop requests bounded platform-specific termination of the complete child
// tree. Concurrent and repeated calls share the same result.
func (process *managedProcess) Stop() error {
	process.stopOnce.Do(func() {
		process.stopErr = process.stop()
	})
	return process.stopErr
}

func (process *managedProcess) stop() error {
	process.stateMu.Lock()
	if process.finished {
		process.stateMu.Unlock()
		process.close()
		return nil
	}
	process.stopping = true
	process.stateMu.Unlock()

	err := stopChildProcessTree(process.command, process.waited, process.tree, process.processTreeGrace)
	process.close()
	return err
}

func (process *managedProcess) close() {
	process.closeOnce.Do(func() {
		closeChildProcessTree(process.tree)
	})
}
