package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/module"
)

type newOptions struct {
	directory string
	module    string
	replace   string
}

func runNew(args []string, stdout io.Writer) error {
	options, err := parseNew(args)
	if err != nil {
		return err
	}
	if err := createProject(options); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Created %s\n\nNext:\n  cd %s\n  docker compose up -d\n  forge make:resource Issue\n  forge make:job SendWelcome\n  forge migrate\n  # For plain-HTTP development, first set APP_ENV=local in .env.\n  forge serve\n", options.module, options.directory)
	return nil
}

func parseNew(args []string) (newOptions, error) {
	var options newOptions
	var positional []string
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--module" || argument == "--replace":
			if index+1 >= len(args) {
				return options, fmt.Errorf("%s requires a value", argument)
			}
			index++
			if argument == "--module" {
				options.module = args[index]
			} else {
				options.replace = args[index]
			}
		case strings.HasPrefix(argument, "--module="):
			options.module = strings.TrimPrefix(argument, "--module=")
		case strings.HasPrefix(argument, "--replace="):
			options.replace = strings.TrimPrefix(argument, "--replace=")
		case strings.HasPrefix(argument, "-"):
			return options, fmt.Errorf("unknown option %q", argument)
		default:
			positional = append(positional, argument)
		}
	}
	if len(positional) != 1 {
		return options, errors.New("usage: forge new <directory> [--module <path>] [--replace <goforge-path>]")
	}
	options.directory = filepath.Clean(positional[0])
	if options.directory == "." {
		return options, errors.New("choose a new project directory; scaffolding into the current directory is intentionally refused")
	}
	if options.module == "" {
		options.module = filepath.Base(options.directory)
	}
	if err := module.CheckPath(options.module); err != nil {
		return options, fmt.Errorf("invalid Go module path %q: %w", options.module, err)
	}
	return options, nil
}

func createProject(options newOptions) error {
	_, err := os.Stat(options.directory)
	if err == nil {
		return fmt.Errorf("destination %q already exists", options.directory)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	absolute, err := filepath.Abs(options.directory)
	if err != nil {
		return err
	}
	parent := filepath.Dir(absolute)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create destination parent: %w", err)
	}
	stage, err := os.MkdirTemp(parent, ".goforge-new-")
	if err != nil {
		return fmt.Errorf("create staging directory: %w", err)
	}
	defer os.RemoveAll(stage)

	files, err := scaffoldFiles(options.module, options.replace, filepath.Base(options.directory))
	if err != nil {
		return err
	}
	for name, content := range files {
		path := filepath.Join(stage, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), generatedFileMode(path)); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	if err := os.Rename(stage, absolute); err != nil {
		return fmt.Errorf("publish project: %w", err)
	}
	return nil
}
