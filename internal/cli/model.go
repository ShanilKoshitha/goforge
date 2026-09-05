package cli

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type modelSpec struct {
	Name             string
	File             string
	Table            string
	MigrationVersion string
}

type modelExclusiveWriter func(string, string) error

func makeModel(name string, stdout io.Writer) error {
	if err := requireProjectFormatRange(4, 7); err != nil {
		return err
	}
	return makeModelWithWriters(name, stdout, writeExclusive, writeManagedFile)
}

func makeModelWithWriters(name string, stdout io.Writer, exclusive modelExclusiveWriter, managed ormArtifactWriter) error {
	state, err := loadMigrationResourceState()
	if err != nil {
		return err
	}
	spec, err := newModelSpec(name, state)
	if err != nil {
		return err
	}
	files, err := modelFiles(spec)
	if err != nil {
		return err
	}
	for _, file := range files {
		if _, err := os.Stat(file.path); err == nil {
			return fmt.Errorf("refusing to overwrite %s", filepath.ToSlash(file.path))
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect %s: %w", filepath.ToSlash(file.path), err)
		}
	}
	sources, err := currentModelSourcesWithPlanned(files)
	if err != nil {
		return err
	}
	generatedORM, err := renderORMArtifactForModelSources(sources)
	if err != nil {
		return fmt.Errorf("generate model ORM: %w", err)
	}
	oldORM, err := os.ReadFile(filepath.FromSlash(generatedORMPath))
	ormWasMissing := errors.Is(err, os.ErrNotExist)
	if err != nil && !ormWasMissing {
		return fmt.Errorf("read generated ORM: %w", err)
	}
	createdDirectories, err := missingParentDirectories(files)
	if err != nil {
		return err
	}
	created := make([]string, 0, len(files))
	ormChanged := false
	rollback := func(cause error) error {
		for _, path := range created {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				cause = errors.Join(cause, fmt.Errorf("remove incomplete model file %s: %w", filepath.ToSlash(path), err))
			}
		}
		if ormChanged {
			if ormWasMissing {
				if err := os.Remove(filepath.FromSlash(generatedORMPath)); err != nil && !errors.Is(err, os.ErrNotExist) {
					cause = errors.Join(cause, fmt.Errorf("remove incomplete generated ORM: %w", err))
				}
			} else if err := managed(filepath.FromSlash(generatedORMPath), oldORM); err != nil {
				cause = errors.Join(cause, fmt.Errorf("restore generated ORM: %w", err))
			}
		}
		for _, directory := range createdDirectories {
			entries, err := os.ReadDir(directory)
			if errors.Is(err, os.ErrNotExist) || err == nil && len(entries) > 0 {
				continue
			}
			if err != nil {
				cause = errors.Join(cause, fmt.Errorf("inspect generated directory %s: %w", filepath.ToSlash(directory), err))
				continue
			}
			if err := os.Remove(directory); err != nil && !errors.Is(err, os.ErrNotExist) {
				cause = errors.Join(cause, fmt.Errorf("remove generated directory %s: %w", filepath.ToSlash(directory), err))
			}
		}
		return cause
	}
	for _, file := range files {
		if err := exclusive(file.path, file.content); err != nil {
			return rollback(err)
		}
		created = append(created, file.path)
	}
	ormChanged = true
	if err := managed(filepath.FromSlash(generatedORMPath), []byte(generatedORM)); err != nil {
		return rollback(fmt.Errorf("write generated ORM: %w", err))
	}

	for _, file := range files {
		fmt.Fprintf(stdout, "created %s\n", filepath.ToSlash(file.path))
	}
	fmt.Fprintln(stdout, "updated internal/models/zz_orm_gen.go")
	return nil
}

func newModelSpec(name string, state resourceState) (modelSpec, error) {
	typeName, err := pascal(name)
	if err != nil {
		return modelSpec{}, err
	}
	if !token.IsIdentifier(typeName) || !ast.IsExported(typeName) {
		return modelSpec{}, fmt.Errorf("model name %q does not produce an exported Go identifier", name)
	}
	fileName, err := snake(typeName)
	if err != nil {
		return modelSpec{}, err
	}
	table := pluralize(fileName)
	if !safeIdentifier(table) {
		return modelSpec{}, fmt.Errorf("model name %q produces unsafe table %q", name, table)
	}
	switch table {
	case "users", "sessions", "schema_migrations", "jobs", "failed_jobs", "goforge_jobs", "goforge_failed_jobs":
		return modelSpec{}, fmt.Errorf("model %s conflicts with the built-in %s table", typeName, table)
	}
	for _, resource := range state.Resources {
		if strings.EqualFold(resource.Name, typeName) || resource.Plural == table {
			return modelSpec{}, fmt.Errorf("model %s conflicts with generated resource %s", typeName, resource.Name)
		}
	}
	version, err := nextMigrationVersion(state)
	if err != nil {
		return modelSpec{}, err
	}
	return modelSpec{Name: typeName, File: fileName, Table: table, MigrationVersion: version}, nil
}

func modelFiles(spec modelSpec) ([]plannedFile, error) {
	data := struct {
		Name  string
		Table string
	}{Name: spec.Name, Table: spec.Table}
	modelPath := filepath.Join("internal", "models", spec.File+".go")
	model, err := renderTemplate("templates/model/model.go.tmpl", modelPath, data)
	if err != nil {
		return nil, err
	}
	migrationBase := filepath.Join("database", "migrations", spec.MigrationVersion+"_create_"+spec.Table)
	up, err := renderTemplate("templates/model/up.sql.tmpl", migrationBase+".up.sql", data)
	if err != nil {
		return nil, err
	}
	return []plannedFile{
		{path: modelPath, content: model},
		{path: migrationBase + ".up.sql", content: up},
		{path: migrationBase + ".down.sql", content: "DROP TABLE IF EXISTS " + spec.Table + ";\n"},
	}, nil
}
