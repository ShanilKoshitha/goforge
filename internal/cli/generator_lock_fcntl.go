//go:build aix || solaris

package cli

import (
	"errors"
	"io"
	"os"
	"syscall"
)

type platformGeneratorLock struct{}

func tryPlatformGeneratorLock(file *os.File, _ *platformGeneratorLock) (bool, error) {
	lock := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: int16(io.SeekStart), Len: 1}
	err := syscall.FcntlFlock(file.Fd(), syscall.F_SETLK, &lock)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EAGAIN) {
		return false, nil
	}
	return false, err
}

func unlockPlatformGeneratorLock(file *os.File, _ *platformGeneratorLock) error {
	lock := syscall.Flock_t{Type: syscall.F_UNLCK, Whence: int16(io.SeekStart), Len: 1}
	return syscall.FcntlFlock(file.Fd(), syscall.F_SETLK, &lock)
}
