package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func stageProjectSource(ctx context.Context, root string) (stagedRoot string, err error) {
	stagedRoot, err = os.MkdirTemp("", "goforge-build-source-")
	if err != nil {
		return "", fmt.Errorf("create isolated build source: %w", err)
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, os.RemoveAll(stagedRoot))
		}
	}()

	err = filepath.WalkDir(root, func(name string, entry os.DirEntry, walkErr error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if walkErr != nil {
			return walkErr
		}
		relative, relativeErr := filepath.Rel(root, name)
		if relativeErr != nil {
			return relativeErr
		}
		if relative == "." {
			return nil
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if excludedStagedSourceDirectory(relative, entry.Name()) {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(stagedRoot, filepath.FromSlash(relative)), 0o755)
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("stage source %s: path is not a regular file", relative)
		}
		destination := filepath.Join(stagedRoot, filepath.FromSlash(relative))
		if mkdirErr := os.MkdirAll(filepath.Dir(destination), 0o755); mkdirErr != nil {
			return mkdirErr
		}
		if copyErr := copyStagedSourceFile(ctx, name, destination, info.Mode().Perm()); copyErr != nil {
			return fmt.Errorf("stage source %s: %w", relative, copyErr)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("copy isolated build source: %w", err)
	}
	return stagedRoot, nil
}

func excludedStagedSourceDirectory(relative, name string) bool {
	if name == ".git" || name == "node_modules" {
		return true
	}
	if strings.Contains(relative, "/") {
		return false
	}
	switch relative {
	case "bin", ".cache", ".tmp", "tmp":
		return true
	default:
		return false
	}
}

func copyStagedSourceFile(ctx context.Context, source, destination string, mode os.FileMode) (err error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, input.Close()) }()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, output.Close()) }()

	buffer := make([]byte, 64<<10)
	for {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		read, readErr := input.Read(buffer)
		if read > 0 {
			if _, writeErr := output.Write(buffer[:read]); writeErr != nil {
				return writeErr
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}
