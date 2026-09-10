package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

const minimumWorkflowFormat = 4

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
	if err := requireProjectRoot(); err != nil {
		return err
	}
	if err := requireProjectFormatRange(minimumWorkflowFormat, currentProjectFormat); err != nil {
		return err
	}
	baseline, err := takeBuildSourceSnapshot(".")
	if err != nil {
		return fmt.Errorf("snapshot application source: %w", err)
	}
	if err := checkProjectArtifacts(ctx, stdin, stdout, stderr, processes); err != nil {
		return err
	}
	validated, err := takeBuildSourceSnapshot(".")
	if err != nil {
		return fmt.Errorf("snapshot validated application source: %w", err)
	}
	if validated != baseline {
		return errors.New("application source changed during build validation; retry the build")
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
	compiled, err := takeBuildSourceSnapshot(".")
	if err != nil {
		return fmt.Errorf("snapshot compiled application source: %w", err)
	}
	if compiled != baseline {
		return errors.New("application source changed during compilation; build was not published")
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
	if err := requireProjectFormatRange(minimumWorkflowFormat, currentProjectFormat); err != nil {
		return err
	}
	if err := generateORM(true, stdout); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := runProjectViewCompiler(ctx, stdin, stdout, stderr, processes, true); err != nil {
		return err
	}
	format, err := projectFormat()
	if err != nil {
		return err
	}
	if format >= 12 {
		return runProjectAssetCheck(ctx, stdin, stdout, stderr, processes)
	}
	return nil
}

func runProjectAssetCheck(
	ctx context.Context,
	stdin io.Reader,
	stdout, stderr io.Writer,
	processes processRunner,
) error {
	if err := requireProjectRoot(); err != nil {
		return err
	}
	if err := requireProjectFormatRange(12, currentProjectFormat); err != nil {
		return err
	}
	return processes.Run(ctx, stdin, stdout, stderr, "go", "run", "./cmd/assets")
}
