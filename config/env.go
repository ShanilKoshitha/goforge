// Package config provides explicit, typed environment configuration without
// reflection. A Reader accumulates all configuration errors for one useful
// startup failure instead of panicking at the first missing value.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"maps"
	"os"
	"strconv"
	"strings"
	"time"
)

// Reader holds a snapshot of configuration and accumulates typed conversion errors.
// Construct a Reader with FromEnvironment or FromMap before loading files.
type Reader struct {
	values map[string]string
	errors []error
}

// FieldError identifies an invalid configuration field without retaining its
// supplied value or the value-bearing conversion error. Key is the name passed
// by application code, not an untrusted field name read from a configuration file.
// Rule is one of required, integer, boolean, or duration for Reader errors.
// Errors accumulated by Reader remain discoverable with errors.As.
type FieldError struct {
	Key  string
	Rule string
}

func (problem *FieldError) Error() string {
	switch problem.Rule {
	case "required":
		return fmt.Sprintf("%s is required", problem.Key)
	case "integer":
		return fmt.Sprintf("%s must be an integer", problem.Key)
	case "boolean":
		return fmt.Sprintf("%s must be a boolean", problem.Key)
	case "duration":
		return fmt.Sprintf("%s must be a duration", problem.Key)
	default:
		return fmt.Sprintf("%s has an invalid value", problem.Key)
	}
}

func FromEnvironment() *Reader {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	return &Reader{values: values}
}

// FromMap copies values so later caller changes do not affect configuration.
func FromMap(values map[string]string) *Reader {
	snapshot := make(map[string]string, len(values))
	maps.Copy(snapshot, values)
	return &Reader{values: snapshot}
}

// LoadFile loads KEY=VALUE pairs as defaults; existing reader values win.
// Matching outer quotes are stripped, without escape expansion or interpolation.
// A missing optional development file is ignored.
func (reader *Reader) LoadFile(path string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load configuration %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return fmt.Errorf("%s:%d: expected KEY=VALUE", path, lineNumber)
		}
		if _, exists := reader.values[key]; exists {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
			value = value[1 : len(value)-1]
		}
		reader.values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read configuration %s: %w", path, err)
	}
	return nil
}

func (reader *Reader) String(key, fallback string) string {
	if value, ok := reader.values[key]; ok {
		return value
	}
	return fallback
}

func (reader *Reader) Required(key string) string {
	if value, ok := reader.values[key]; ok && strings.TrimSpace(value) != "" {
		return value
	}
	reader.errors = append(reader.errors, &FieldError{Key: key, Rule: "required"})
	return ""
}

func (reader *Reader) Int(key string, fallback int) int {
	value, ok := reader.values[key]
	if !ok || value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		reader.errors = append(reader.errors, &FieldError{Key: key, Rule: "integer"})
		return fallback
	}
	return parsed
}

func (reader *Reader) Bool(key string, fallback bool) bool {
	value, ok := reader.values[key]
	if !ok || value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		reader.errors = append(reader.errors, &FieldError{Key: key, Rule: "boolean"})
		return fallback
	}
	return parsed
}

func (reader *Reader) Duration(key string, fallback time.Duration) time.Duration {
	value, ok := reader.values[key]
	if !ok || value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		reader.errors = append(reader.errors, &FieldError{Key: key, Rule: "duration"})
		return fallback
	}
	return parsed
}

// Err returns all errors accumulated by Required, Int, Bool, and Duration.
func (reader *Reader) Err() error {
	return errors.Join(reader.errors...)
}
