//go:build plan9 || js || wasip1

package cli

import (
	"errors"
	"os"
)

var errGeneratorLockUnsupported = errors.New("project generators require interprocess file locking, which is not supported on this platform")

type platformGeneratorLock struct{}

func tryPlatformGeneratorLock(_ *os.File, _ *platformGeneratorLock) (bool, error) {
	// Fail closed rather than run a mutating generator without the promised
	// project-wide serialization guarantee.
	return false, errGeneratorLockUnsupported
}

func unlockPlatformGeneratorLock(_ *os.File, _ *platformGeneratorLock) error {
	return nil
}
