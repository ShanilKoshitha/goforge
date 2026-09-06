package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// generatorLock serializes every mutating make command for a project. The
// operating system releases the file lock if a generator exits or crashes.
// Locking the existing manifest also avoids leaving a stale lock artifact.
type generatorLock struct {
	file  *os.File
	state platformGeneratorLock
}

func acquireGeneratorLock(ctx context.Context) (*generatorLock, error) {
	// Exclusive POSIX record locks require a descriptor opened for writing.
	// O_RDWR grants that capability without truncating or otherwise changing
	// the manifest used as the stable project-wide lock target.
	file, err := os.OpenFile("forge.yaml", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open project manifest for generator lock: %w", err)
	}
	lock := &generatorLock{file: file}
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("wait for project generator lock: %w", err)
		}
		locked, err := tryPlatformGeneratorLock(file, &lock.state)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("lock project generators: %w", err)
		}
		if locked {
			return lock, nil
		}

		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			_ = file.Close()
			return nil, fmt.Errorf("wait for project generator lock: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (lock *generatorLock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	unlockErr := unlockPlatformGeneratorLock(lock.file, &lock.state)
	closeErr := lock.file.Close()
	lock.file = nil
	if unlockErr != nil {
		unlockErr = fmt.Errorf("unlock project generators: %w", unlockErr)
	}
	return errors.Join(unlockErr, closeErr)
}
