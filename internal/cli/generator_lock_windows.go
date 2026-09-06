//go:build windows

package cli

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
	errorLockViolation      = syscall.Errno(33)
)

var (
	kernel32GeneratorLock = syscall.NewLazyDLL("kernel32.dll")
	lockFileEx            = kernel32GeneratorLock.NewProc("LockFileEx")
	unlockFileEx          = kernel32GeneratorLock.NewProc("UnlockFileEx")
)

type platformGeneratorLock struct {
	overlapped syscall.Overlapped
}

func tryPlatformGeneratorLock(file *os.File, state *platformGeneratorLock) (bool, error) {
	// Lock one byte far beyond the manifest contents. Windows byte-range locks
	// are mandatory, so keeping the range away from real data lets other tools
	// continue reading forge.yaml while a generator runs.
	state.overlapped.Offset = 0
	state.overlapped.OffsetHigh = 1
	result, _, callErr := lockFileEx.Call(
		file.Fd(),
		lockfileExclusiveLock|lockfileFailImmediately,
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&state.overlapped)),
	)
	if result != 0 {
		return true, nil
	}
	if errors.Is(callErr, errorLockViolation) {
		return false, nil
	}
	return false, callErr
}

func unlockPlatformGeneratorLock(file *os.File, state *platformGeneratorLock) error {
	result, _, callErr := unlockFileEx.Call(
		file.Fd(),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&state.overlapped)),
	)
	if result == 0 {
		return callErr
	}
	return nil
}
