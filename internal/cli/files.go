package cli

import (
	"errors"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
)

func writeExclusive(path, content string) (err error) {
	if filepath.Ext(path) == ".go" {
		formatted, err := format.Source([]byte(content))
		if err != nil {
			return fmt.Errorf("format generated %s: %w", filepath.ToSlash(path), err)
		}
		content = string(formatted)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, generatedFileMode(path))
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("refusing to overwrite %s", filepath.ToSlash(path))
	}
	if err != nil {
		return err
	}
	// A failed write must not leave an incomplete file that blocks the next run.
	defer func() {
		err = errors.Join(err, file.Close())
		if err != nil {
			err = errors.Join(err, os.Remove(path))
		}
	}()
	_, err = io.WriteString(file, content)
	return err
}

func generatedFileMode(path string) os.FileMode {
	if filepath.Base(path) == ".env" {
		return 0o600
	}
	return 0o644
}

func writeManagedFile(path string, content []byte) error {
	mode := generatedFileMode(path)
	if info, err := os.Stat(path); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("managed destination %s is not a regular file", filepath.ToSlash(path))
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".goforge-managed-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return nil
}
