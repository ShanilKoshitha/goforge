//go:build windows

package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

const (
	processTreeControlSupported         = true
	childCreateSuspended                = 0x00000004
	childCreateNewProcessGroup          = 0x00000200
	childCtrlBreakEvent                 = 1
	jobObjectExtendedLimitInformation   = 9
	jobObjectBasicAccountingInformation = 1
	jobObjectLimitKillOnJobClose        = 0x00002000
	childProcessTerminate               = 0x0001
	childProcessSetQuota                = 0x0100
	childSnapshotThreads                = 0x00000004
	childThreadSuspendResume            = 0x0002
	childResumeFailed                   = uintptr(^uint32(0))
)

var (
	childKernel32                  = syscall.NewLazyDLL("kernel32.dll")
	generateChildConsoleCtrlEvent  = childKernel32.NewProc("GenerateConsoleCtrlEvent")
	createChildJobObject           = childKernel32.NewProc("CreateJobObjectW")
	setChildJobObjectInformation   = childKernel32.NewProc("SetInformationJobObject")
	assignChildProcessToJobObject  = childKernel32.NewProc("AssignProcessToJobObject")
	terminateChildJobObject        = childKernel32.NewProc("TerminateJobObject")
	queryChildJobObjectInformation = childKernel32.NewProc("QueryInformationJobObject")
	thread32First                  = childKernel32.NewProc("Thread32First")
	thread32Next                   = childKernel32.NewProc("Thread32Next")
	openChildThread                = childKernel32.NewProc("OpenThread")
	resumeChildThread              = childKernel32.NewProc("ResumeThread")
)

type childThreadEntry struct {
	Size           uint32
	Usage          uint32
	ThreadID       uint32
	OwnerProcessID uint32
	BasePriority   int32
	DeltaPriority  int32
	Flags          uint32
}

type childJobBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type childJobIOCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type childJobExtendedLimitInformation struct {
	BasicLimitInformation childJobBasicLimitInformation
	IOInfo                childJobIOCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

type childJobBasicAccountingInformation struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

type platformChildProcessTree struct{ job syscall.Handle }

func configureChildProcess(command *exec.Cmd) error {
	// Suspending creation closes the otherwise unavoidable gap between Start and
	// AssignProcessToJobObject. The process cannot create escaping descendants
	// before attachChildProcessTree assigns it to the kill-on-close job.
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: childCreateNewProcessGroup | childCreateSuspended}
	return nil
}

func attachChildProcessTree(command *exec.Cmd) (platformChildProcessTree, error) {
	jobValue, _, createErr := createChildJobObject.Call(0, 0)
	if jobValue == 0 {
		return platformChildProcessTree{}, fmt.Errorf("create child process job: %w", createErr)
	}
	job := syscall.Handle(jobValue)
	limits := childJobExtendedLimitInformation{}
	limits.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	result, _, setErr := setChildJobObjectInformation.Call(
		uintptr(job), jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits),
	)
	if result == 0 {
		_ = syscall.CloseHandle(job)
		return platformChildProcessTree{}, fmt.Errorf("configure child process job: %w", setErr)
	}
	process, err := syscall.OpenProcess(childProcessSetQuota|childProcessTerminate, false, uint32(command.Process.Pid))
	if err != nil {
		_ = syscall.CloseHandle(job)
		return platformChildProcessTree{}, fmt.Errorf("open child process for job: %w", err)
	}
	defer syscall.CloseHandle(process)
	result, _, assignErr := assignChildProcessToJobObject.Call(uintptr(job), uintptr(process))
	if result == 0 {
		_ = syscall.CloseHandle(job)
		return platformChildProcessTree{}, fmt.Errorf("assign child process job: %w", assignErr)
	}
	if err := resumeSuspendedChild(uint32(command.Process.Pid)); err != nil {
		_ = syscall.CloseHandle(job)
		return platformChildProcessTree{}, err
	}
	return platformChildProcessTree{job: job}, nil
}

func resumeSuspendedChild(processID uint32) error {
	snapshot, err := syscall.CreateToolhelp32Snapshot(childSnapshotThreads, 0)
	if err != nil {
		return fmt.Errorf("snapshot child process threads: %w", err)
	}
	defer syscall.CloseHandle(snapshot)

	entry := childThreadEntry{Size: uint32(unsafe.Sizeof(childThreadEntry{}))}
	result, _, firstErr := thread32First.Call(uintptr(snapshot), uintptr(unsafe.Pointer(&entry)))
	for result != 0 {
		if entry.OwnerProcessID == processID {
			threadValue, _, openErr := openChildThread.Call(childThreadSuspendResume, 0, uintptr(entry.ThreadID))
			if threadValue == 0 {
				return fmt.Errorf("open suspended child thread: %w", openErr)
			}
			thread := syscall.Handle(threadValue)
			resumeResult, _, resumeErr := resumeChildThread.Call(uintptr(thread))
			_ = syscall.CloseHandle(thread)
			if resumeResult == childResumeFailed {
				return fmt.Errorf("resume child process: %w", resumeErr)
			}
			return nil
		}
		entry.Size = uint32(unsafe.Sizeof(childThreadEntry{}))
		result, _, _ = thread32Next.Call(uintptr(snapshot), uintptr(unsafe.Pointer(&entry)))
	}
	return fmt.Errorf("find suspended child process thread: %w", firstErr)
}

func closeChildProcessTree(tree platformChildProcessTree) {
	if tree.job != 0 {
		_ = syscall.CloseHandle(tree.job)
	}
}

func stopChildProcessTree(command *exec.Cmd, waited <-chan error, tree platformChildProcessTree, grace time.Duration) error {
	if command.Process == nil {
		return nil
	}
	result, _, signalErr := generateChildConsoleCtrlEvent.Call(childCtrlBreakEvent, uintptr(command.Process.Pid))
	directDone := false
	if result != 0 && tree.job != 0 {
		finished, inspectErr := waitForWindowsProcessTree(waited, tree, grace, &directDone)
		if inspectErr == nil && finished {
			return nil
		}
	}
	if tree.job != 0 {
		if result, _, terminateErr := terminateChildJobObject.Call(uintptr(tree.job), 1); result == 0 {
			return fmt.Errorf("terminate child process job: %w", terminateErr)
		}
		finished, inspectErr := waitForWindowsProcessTree(waited, tree, 10*time.Second, &directDone)
		if inspectErr != nil {
			return inspectErr
		}
		if finished {
			return nil
		}
		return errors.New("child process job did not exit after forced termination")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(command.Process.Pid), "/T", "/F").Run()
	if err != nil {
		_ = command.Process.Kill()
		if result == 0 {
			err = errors.Join(err, fmt.Errorf("signal process group: %w", signalErr))
		}
	}
	select {
	case <-waited:
		return err
	case <-ctx.Done():
		return errors.Join(err, errors.New("child process tree did not exit after forced termination"))
	}
}

func waitForWindowsProcessTree(waited <-chan error, tree platformChildProcessTree, timeout time.Duration, directDone *bool) (bool, error) {
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
		active, err := childJobActiveProcesses(tree)
		if err != nil {
			return false, err
		}
		if *directDone && active == 0 {
			return true, nil
		}
		select {
		case <-deadline.C:
			return false, nil
		case <-ticker.C:
		}
	}
}

func childJobActiveProcesses(tree platformChildProcessTree) (uint32, error) {
	if tree.job == 0 {
		return 0, nil
	}
	accounting := childJobBasicAccountingInformation{}
	result, _, queryErr := queryChildJobObjectInformation.Call(
		uintptr(tree.job), jobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&accounting)), unsafe.Sizeof(accounting), 0,
	)
	if result == 0 {
		return 0, fmt.Errorf("inspect child process job: %w", queryErr)
	}
	return accounting.ActiveProcesses, nil
}
