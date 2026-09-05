package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func loadResourceState() (resourceState, error) {
	contents, err := os.ReadFile(filepath.Join(".forge", "resources.json"))
	if err != nil {
		return resourceState{}, fmt.Errorf("read resource metadata: %w", err)
	}
	var state resourceState
	if err := json.Unmarshal(contents, &state); err != nil {
		return resourceState{}, fmt.Errorf("decode resource metadata: %w", err)
	}
	return state, nil
}

func loadMigrationResourceState() (resourceState, error) {
	state, err := loadResourceState()
	if errors.Is(err, os.ErrNotExist) {
		return resourceState{}, nil
	}
	return state, err
}

func newResourceSpec(name string, state resourceState) (resourceSpec, error) {
	typeName, err := pascal(name)
	if err != nil {
		return resourceSpec{}, err
	}
	tableName, _ := snake(typeName)
	packageName := strings.ToLower(typeName)
	if !token.IsIdentifier(typeName) || !token.IsIdentifier(packageName) || token.IsKeyword(packageName) {
		return resourceSpec{}, fmt.Errorf("resource name %q does not produce valid Go identifiers", name)
	}
	switch typeName {
	case "Controller", "WebController", "Repository", "PostgresRepository", "WriteRequest", "ErrNotFound", "NewController", "NewWebController", "NewPostgresRepository":
		return resourceSpec{}, fmt.Errorf("resource name %s conflicts with a generated declaration", typeName)
	}
	plural := pluralize(tableName)
	if plural == "users" || plural == "sessions" || plural == "schema_migrations" || plural == "jobs" || plural == "failed_jobs" || plural == "goforge_jobs" || plural == "goforge_failed_jobs" {
		return resourceSpec{}, fmt.Errorf("resource %s conflicts with the built-in %s table", typeName, plural)
	}
	for _, existing := range state.Resources {
		if strings.EqualFold(existing.Name, typeName) || existing.Package == packageName || existing.Plural == plural {
			return resourceSpec{}, fmt.Errorf("resource %s already exists", typeName)
		}
	}
	version, err := nextMigrationVersion(state)
	if err != nil {
		return resourceSpec{}, err
	}
	return resourceSpec{Name: typeName, Package: packageName, Plural: plural, MigrationVersion: version}, nil
}

func nextMigrationVersion(state resourceState) (string, error) {
	candidate, err := strconv.ParseInt(time.Now().UTC().Format("20060102150405"), 10, 64)
	if err != nil {
		return "", fmt.Errorf("build migration timestamp: %w", err)
	}
	maximum := candidate - 1
	consider := func(version, source string) error {
		value, err := strconv.ParseInt(version, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid migration version %q in %s", version, source)
		}
		if value > maximum {
			maximum = value
		}
		return nil
	}
	for _, existing := range state.Resources {
		if err := consider(existing.MigrationVersion, "resource metadata"); err != nil {
			return "", err
		}
	}

	directory := filepath.Join("database", "migrations")
	entries, err := os.ReadDir(directory)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("scan migrations: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		version, suffix, ok := strings.Cut(entry.Name(), "_")
		if !ok || (!strings.HasSuffix(suffix, ".up.sql") && !strings.HasSuffix(suffix, ".down.sql")) {
			continue
		}
		if err := consider(version, filepath.ToSlash(filepath.Join(directory, entry.Name()))); err != nil {
			return "", err
		}
	}

	if maximum >= candidate {
		if maximum == int64(^uint64(0)>>1) {
			return "", errors.New("migration version space exhausted")
		}
		candidate = maximum + 1
	}
	return fmt.Sprintf("%014d", candidate), nil
}
