package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func requireProjectFormat(supported int) error {
	version, err := projectFormat()
	if err != nil {
		return err
	}
	if version < supported {
		return fmt.Errorf("project format version %d cannot be changed by this CLI; upgrade the project to format %d first", version, supported)
	}
	if version > supported {
		return fmt.Errorf("project format version %d is newer than this CLI supports (format %d); upgrade the GoForge CLI", version, supported)
	}
	return nil
}

func requireProjectFormatRange(minimum, maximum int) error {
	version, err := projectFormat()
	if err != nil {
		return err
	}
	if version < minimum {
		return fmt.Errorf("project format version %d cannot be changed by this CLI; upgrade the project to format %d first", version, minimum)
	}
	if version > maximum {
		return fmt.Errorf("project format version %d is newer than this CLI supports (format %d); upgrade the GoForge CLI", version, maximum)
	}
	return nil
}

func projectFormat() (int, error) {
	file, err := os.Open("forge.yaml")
	if err != nil {
		return 0, fmt.Errorf("read forge.yaml: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || fields[0] != "version:" {
			continue
		}
		version, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0, fmt.Errorf("forge.yaml has an invalid version: %w", err)
		}
		return version, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("read forge.yaml: %w", err)
	}
	return 0, errors.New("forge.yaml does not declare a version")
}

func projectModule() (string, error) {
	file, err := os.Open("go.mod")
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line, _, _ := strings.Cut(scanner.Text(), "//")
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "module" {
			continue
		}
		if len(fields) != 2 {
			return "", errors.New("go.mod has an invalid module directive")
		}
		module := fields[1]
		if strings.HasPrefix(module, `"`) {
			module, err = strconv.Unquote(module)
			if err != nil {
				return "", fmt.Errorf("parse module path: %w", err)
			}
		}
		if !modulePattern.MatchString(module) {
			return "", fmt.Errorf("invalid Go module path %q", module)
		}
		return module, nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("go.mod does not declare a module")
}
