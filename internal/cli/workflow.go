package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const (
	minimumWorkflowFormat = 4
	maximumWorkflowFormat = 8
)

func runProjectTests(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	processes processRunner,
) error {
	if err := checkProjectArtifacts(ctx, stdin, stdout, stderr, processes); err != nil {
		return err
	}
	return processes.Run(ctx, stdin, stdout, stderr, "go", "test", "./...")
}

func runProjectBuild(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	processes processRunner,
) error {
	if err := checkProjectArtifacts(ctx, stdin, stdout, stderr, processes); err != nil {
		return err
	}
	if err := os.MkdirAll("bin", 0o755); err != nil {
		return fmt.Errorf("create build output directory: %w", err)
	}

	extension := ""
	if runtime.GOOS == "windows" {
		extension = ".exe"
	}
	temporary, err := os.CreateTemp("bin", ".goforge-build-*"+extension)
	if err != nil {
		return fmt.Errorf("create temporary build artifact: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close temporary build artifact: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		return fmt.Errorf("prepare temporary build artifact: %w", err)
	}
	defer os.Remove(temporaryPath)

	if err := processes.Run(ctx, stdin, stdout, stderr, "go", "build", "-trimpath", "-o", temporaryPath, "./cmd/server"); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(temporaryPath)
	if err != nil {
		return fmt.Errorf("inspect temporary build artifact: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("temporary build artifact %s is not a non-empty regular file", filepath.ToSlash(temporaryPath))
	}
	destination := filepath.Join("bin", "app"+extension)
	if err := publishBuildArtifact(temporaryPath, destination); err != nil {
		return fmt.Errorf("publish build artifact %s: %w", filepath.ToSlash(destination), err)
	}
	fmt.Fprintf(stdout, "built %s\n", filepath.ToSlash(destination))
	return nil
}

func checkProjectArtifacts(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	processes processRunner,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := requireProjectRoot(); err != nil {
		return err
	}
	if err := requireProjectFormatRange(minimumWorkflowFormat, maximumWorkflowFormat); err != nil {
		return err
	}
	if err := generateORM(true, stdout); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return runProjectViewCompiler(ctx, stdin, stdout, stderr, processes, true)
}
