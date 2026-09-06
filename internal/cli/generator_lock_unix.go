//go:build android || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd

package cli

import (
	"errors"
	"os"
	"syscall"
)

type platformGeneratorLock struct{}

func tryPlatformGeneratorLock(file *os.File, _ *platformGeneratorLock) (bool, error) {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return false, nil
	}
	return false, err
}

func unlockPlatformGeneratorLock(file *os.File, _ *platformGeneratorLock) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
