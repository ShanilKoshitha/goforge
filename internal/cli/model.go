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
	Fields           []resourceTemplateField
	Relationships    []modelRelationship
}

// modelRelationship is the resolved, one-shot relationship contract used to
// render ordinary model and migration source. It is never stored as metadata.
type modelRelationship struct {
	resourceBelongsTo
	TargetTable    string
	TargetKey      string
	TargetColumn   string
	ForeignKeyType string
	SQLType        string
	IndexName      string
	ConstraintName string
}

type modelExclusiveWriter func(string, string) error

func makeModel(name string, stdout io.Writer) error {
	return makeModelWithOptions(name, nil, nil, stdout)
}

func makeModelWithOptions(name string, fields []resourceField, relationships []resourceBelongsTo, stdout io.Writer) error {
	if err := requireProjectFormatRange(4, currentProjectFormat); err != nil {
		return err
	}
	return makeModelWithOptionsAndWriters(name, fields, relationships, stdout, writeExclusive, writeManagedFile)
}

func makeModelWithWriters(name string, stdout io.Writer, exclusive modelExclusiveWriter, managed ormArtifactWriter) error {
	return makeModelWithOptionsAndWriters(name, nil, nil, stdout, exclusive, managed)
}

func makeModelWithOptionsAndWriters(name string, fields []resourceField, relationships []resourceBelongsTo, stdout io.Writer, exclusive modelExclusiveWriter, managed ormArtifactWriter) error {
	state, err := loadMigrationResourceState()
	if err != nil {
		return err
	}
	spec, err := newModelSpec(name, state)
	if err != nil {
		return err
	}
	for _, field := range fields {
		spec.Fields = append(spec.Fields, projectResourceTemplateField(field))
	}
	spec.Relationships, err = resolveModelRelationships(spec, relationships)
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

func resolveModelRelationships(child modelSpec, requested []resourceBelongsTo) ([]modelRelationship, error) {
	if len(requested) == 0 {
		return nil, nil
	}
	schema, err := parseModelSchema(filepath.Join("internal", "models"))
	if err != nil {
		return nil, fmt.Errorf("inspect belongs-to targets: %w", err)
	}
	models := make(map[string]modelDefinition, len(schema.Models))
	for _, model := range schema.Models {
		models[model.Name] = model
	}
	resolved := make([]modelRelationship, 0, len(requested))
	for _, relation := range requested {
		target, exists := models[relation.Target]
		if !exists {
			return nil, fmt.Errorf("belongs-to relationship %q targets %s, which is not an existing model", relation.Name, relation.Target)
		}
		var targetKey *modelField
		for index := range target.Fields {
			field := &target.Fields[index]
			if field.Name == "ID" && field.Primary && field.Required && !field.Nullable {
				targetKey = field
				break
			}
		}
		if targetKey == nil {
			return nil, fmt.Errorf("belongs-to target %s must have a required primary ID field", target.Name)
		}
		if targetKey.GoImportPath != "" {
			return nil, fmt.Errorf("belongs-to target %s ID type %s requires an imported package", target.Name, targetKey.GoType)
		}
		indexName := child.Table + "_" + relation.ForeignKey + "_idx"
		constraintName := child.Table + "_" + relation.ForeignKey + "_fkey"
		for _, identifier := range []struct{ kind, value string }{
			{kind: "index", value: indexName},
			{kind: "constraint", value: constraintName},
		} {
			if !safeIdentifier(identifier.value) || len(identifier.value) > maximumResourceIdentifier {
				return nil, fmt.Errorf("belongs-to relationship %q produces %s name %q longer than PostgreSQL's %d-byte identifier limit", relation.Name, identifier.kind, identifier.value, maximumResourceIdentifier)
			}
		}
		resolved = append(resolved, modelRelationship{
			resourceBelongsTo: relation,
			TargetTable:       target.Table,
			TargetKey:         targetKey.Name,
			TargetColumn:      targetKey.Column,
			ForeignKeyType:    targetKey.GoType,
			SQLType:           strings.ToUpper(targetKey.DBType),
			IndexName:         indexName,
			ConstraintName:    constraintName,
		})
	}
	return resolved, nil
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
	modelPath := filepath.Join("internal", "models", spec.File+".go")
	model, err := renderTemplate("templates/model/model.go.tmpl", modelPath, spec)
	if err != nil {
		return nil, err
	}
	migrationBase := filepath.Join("database", "migrations", spec.MigrationVersion+"_create_"+spec.Table)
	up, err := renderTemplate("templates/model/up.sql.tmpl", migrationBase+".up.sql", spec)
	if err != nil {
		return nil, err
	}
	return []plannedFile{
		{path: modelPath, content: model},
		{path: migrationBase + ".up.sql", content: up},
		{path: migrationBase + ".down.sql", content: "DROP TABLE IF EXISTS " + spec.Table + ";\n"},
	}, nil
}
