package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const generatedORMPath = "internal/models/zz_orm_gen.go"

type ormArtifactRenderer func(modelSchema) (string, error)
type ormArtifactWriter func(string, []byte) error

type ormTemplateData struct {
	Imports []ormTemplateImport
	Models  []ormTemplateModel
}

type ormTemplateImport struct {
	Alias string
	Path  string
}

type ormTemplateModel struct {
	Name         string
	Table        string
	Accessor     string
	Fields       []ormTemplateField
	CreateFields []ormTemplateField
	ChangeFields []ormTemplateField
	Relations    []ormTemplateRelation
}

type ormTemplateField struct {
	Name          string
	Column        string
	Type          string
	CreatePartial bool
}

type ormTemplateRelation struct {
	Name                 string
	Kind                 string
	Target               string
	DescriptorType       string
	Factory              string
	ParentColumn         string
	TargetColumn         string
	ParentKeyType        string
	TargetKeyType        string
	ParentKeyField       string
	TargetKeyField       string
	ParentKeyValue       string
	TargetKeyValue       string
	ParentKeyConditional bool
	TargetKeyConditional bool
	ParentKeyPointer     bool
	TargetKeyPointer     bool
	Pointer              bool
	Collection           bool
	JoinTable            string
	JoinForeignKey       string
	JoinReferenceKey     string
	TargetFields         []ormTemplateField
}

func runORMGenerate(ctx context.Context, args []string, stdout io.Writer) (err error) {
	check := false
	switch {
	case len(args) == 0:
	case len(args) == 1 && args[0] == "--check":
		check = true
	default:
		return errors.New("usage: forge orm:generate [--check]")
	}
	if err := requireProjectFormatRange(4, 9); err != nil {
		return err
	}
	if check {
		return generateORM(true, stdout)
	}
	lock, err := acquireGeneratorLock(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	return generateORM(check, stdout)
}

func generateORM(check bool, stdout io.Writer) error {
	return generateORMWith(check, stdout, renderORMArtifact, writeManagedFile)
}

func generateORMWith(check bool, stdout io.Writer, render ormArtifactRenderer, write ormArtifactWriter) error {
	schema, err := parseModelSchema(filepath.Join("internal", "models"))
	if err != nil {
		return fmt.Errorf("parse ORM models: %w", err)
	}
	artifact, err := render(schema)
	if err != nil {
		return fmt.Errorf("generate ORM artifact: %w", err)
	}
	path := filepath.FromSlash(generatedORMPath)
	current, readErr := os.ReadFile(path)
	if readErr == nil && bytes.Equal(current, []byte(artifact)) {
		fmt.Fprintf(stdout, "%s is current\n", generatedORMPath)
		return nil
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("read generated ORM: %w", readErr)
	}
	if check {
		if errors.Is(readErr, os.ErrNotExist) {
			return fmt.Errorf("generated ORM is missing: %s; run forge orm:generate", generatedORMPath)
		}
		return fmt.Errorf("generated ORM is stale: %s; run forge orm:generate", generatedORMPath)
	}
	if err := write(path, []byte(artifact)); err != nil {
		return fmt.Errorf("write generated ORM: %w", err)
	}
	if readErr == nil {
		fmt.Fprintf(stdout, "updated %s\n", generatedORMPath)
	} else {
		fmt.Fprintf(stdout, "created %s\n", generatedORMPath)
	}
	return nil
}

func renderORMArtifact(schema modelSchema) (string, error) {
	data, err := buildORMTemplateData(schema)
	if err != nil {
		return "", err
	}
	return renderTemplate("templates/orm/zz_orm_gen.go.tmpl", generatedORMPath, data)
}

// renderORMArtifactForModelSources validates and renders a complete models
// package without publishing any source into the application being changed.
// Scaffold and compound generators use it to preserve all-or-nothing writes.
func renderORMArtifactForModelSources(sources map[string]string) (artifact string, err error) {
	directory, err := os.MkdirTemp("", "goforge-models-")
	if err != nil {
		return "", fmt.Errorf("stage ORM models: %w", err)
	}
	defer func() {
		if removeErr := os.RemoveAll(directory); removeErr != nil && err == nil {
			err = fmt.Errorf("remove staged ORM models: %w", removeErr)
		}
	}()
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if filepath.Base(name) != name || filepath.Ext(name) != ".go" {
			return "", fmt.Errorf("invalid staged ORM model filename %q", name)
		}
		if err := os.WriteFile(filepath.Join(directory, name), []byte(sources[name]), 0o644); err != nil {
			return "", fmt.Errorf("stage ORM model %s: %w", name, err)
		}
	}
	schema, err := parseModelSchema(directory)
	if err != nil {
		return "", fmt.Errorf("parse ORM models: %w", err)
	}
	return renderORMArtifact(schema)
}

func currentModelSourcesWithPlanned(files []plannedFile) (map[string]string, error) {
	directory := filepath.Join("internal", "models")
	entries, err := os.ReadDir(directory)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read model directory: %w", err)
	}
	sources := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read model %s: %w", entry.Name(), err)
		}
		sources[entry.Name()] = string(contents)
	}
	for _, file := range files {
		clean := filepath.Clean(file.path)
		if filepath.Dir(clean) == directory && filepath.Ext(clean) == ".go" {
			sources[filepath.Base(clean)] = file.content
		}
	}
	return sources, nil
}

func buildORMTemplateData(schema modelSchema) (ormTemplateData, error) {
	imports := map[string]string{"orm": "github.com/ShanilKoshitha/goforge/orm"}
	models := make(map[string]*modelDefinition, len(schema.Models))
	for _, model := range schema.Models {
		model := model
		models[model.Name] = &model
		for _, field := range model.Fields {
			switch field.GoImportPath {
			case "":
			case "time":
				imports["time"] = "time"
			case "encoding/json":
				imports["json"] = "encoding/json"
			case "database/sql":
				imports["sql"] = "database/sql"
			default:
				return ormTemplateData{}, fmt.Errorf("model %s field %s requires unsupported generated import %s", model.Name, field.Name, field.GoImportPath)
			}
		}
	}
	declared := make(map[string]struct{}, len(schema.DeclaredNames))
	for _, name := range schema.DeclaredNames {
		declared[name] = struct{}{}
	}
	generated := make(map[string]string)
	claim := func(name, owner string) error {
		if _, exists := declared[name]; exists {
			return fmt.Errorf("generated declaration %s for %s conflicts with application source", name, owner)
		}
		if previous, exists := generated[name]; exists {
			return fmt.Errorf("generated declaration %s for %s conflicts with %s", name, owner, previous)
		}
		generated[name] = owner
		return nil
	}
	if err := claim("Store", "ORM store"); err != nil {
		return ormTemplateData{}, err
	}
	if err := claim("NewStore", "ORM store"); err != nil {
		return ormTemplateData{}, err
	}

	data := ormTemplateData{Models: make([]ormTemplateModel, 0, len(schema.Models))}
	accessors := make(map[string]string)
	for _, model := range schema.Models {
		accessor, err := pascal(model.Table)
		if err != nil {
			return ormTemplateData{}, fmt.Errorf("derive query accessor for model %s: %w", model.Name, err)
		}
		if previous, exists := accessors[accessor]; exists {
			return ormTemplateData{}, fmt.Errorf("query accessor %s is ambiguous for models %s and %s", accessor, previous, model.Name)
		}
		accessors[accessor] = model.Name
		for _, suffix := range []string{"Mapper", "Table", "Columns", "CreateInput", "Changes", "Query", "Relations"} {
			if suffix == "Relations" && len(model.Relations) == 0 {
				continue
			}
			if err := claim(model.Name+suffix, model.Name); err != nil {
				return ormTemplateData{}, err
			}
		}
		generatedModel := ormTemplateModel{Name: model.Name, Table: model.Table, Accessor: accessor}
		for _, field := range model.Fields {
			if field.GoImportPath == "" {
				bare := strings.TrimLeft(field.GeneratedGoType, "*[]")
				if _, collision := imports[bare]; collision {
					return ormTemplateData{}, fmt.Errorf("model %s field %s type %s conflicts with a generated import", model.Name, field.Name, field.GeneratedGoType)
				}
			}
			generatedField := ormTemplateField{
				Name: field.Name, Column: field.Column, Type: field.GeneratedGoType,
				CreatePartial: field.Nullable || field.Default != nil,
			}
			generatedModel.Fields = append(generatedModel.Fields, generatedField)
			if !field.Primary && !field.Generated && !field.Protected {
				generatedModel.CreateFields = append(generatedModel.CreateFields, generatedField)
				generatedModel.ChangeFields = append(generatedModel.ChangeFields, generatedField)
			}
		}
		for _, relation := range model.Relations {
			generatedRelation, err := buildORMTemplateRelation(model, relation, models)
			if err != nil {
				return ormTemplateData{}, err
			}
			if err := claim(generatedRelation.Factory, model.Name+"."+relation.Name); err != nil {
				return ormTemplateData{}, err
			}
			generatedModel.Relations = append(generatedModel.Relations, generatedRelation)
			imports["context"] = "context"
		}
		data.Models = append(data.Models, generatedModel)
	}

	aliases := make([]string, 0, len(imports))
	for alias := range imports {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		data.Imports = append(data.Imports, ormTemplateImport{Alias: alias, Path: imports[alias]})
	}
	return data, nil
}

func buildORMTemplateRelation(model modelDefinition, relation modelRelation, models map[string]*modelDefinition) (ormTemplateRelation, error) {
	target := models[relation.Target]
	if target == nil {
		return ormTemplateRelation{}, fmt.Errorf("model %s relation %s targets missing model %s", model.Name, relation.Name, relation.Target)
	}
	localFields := ormFieldMap(model)
	targetFields := ormFieldMap(*target)
	generated := ormTemplateRelation{
		Name: relation.Name, Kind: relation.Kind, Target: target.Name, Pointer: relation.Pointer,
		Factory:    model.Name + relation.Name + "Relation",
		Collection: relation.Kind == relationHasMany || relation.Kind == relationManyToMany,
		JoinTable:  relation.JoinTable, JoinForeignKey: relation.JoinForeignKey, JoinReferenceKey: relation.JoinReferenceKey,
	}
	var parentField, targetField *modelField
	switch relation.Kind {
	case relationBelongsTo:
		parentField, targetField = localFields[relation.ForeignKey], targetFields[relation.References]
	case relationHasOne, relationHasMany:
		parentField, targetField = localFields[relation.References], targetFields[relation.ForeignKey]
	case relationManyToMany:
		parentField, targetField = localFields[relation.References], targetFields[relation.TargetKey]
	default:
		return ormTemplateRelation{}, fmt.Errorf("model %s relation %s has unsupported kind %s", model.Name, relation.Name, relation.Kind)
	}
	if parentField == nil || targetField == nil {
		return ormTemplateRelation{}, fmt.Errorf("model %s relation %s has unresolved keys", model.Name, relation.Name)
	}
	generated.ParentColumn = parentField.Name
	generated.TargetColumn = targetField.Name
	generated.ParentKeyField = parentField.Name
	generated.TargetKeyField = targetField.Name
	if relation.Kind == relationBelongsTo {
		generated.ParentKeyType = normalizedRelationKeyType(*targetField)
	} else {
		generated.ParentKeyType = normalizedRelationKeyType(*parentField)
	}
	if relation.Kind == relationManyToMany {
		generated.TargetKeyType = normalizedRelationKeyType(*targetField)
	} else {
		generated.TargetKeyType = generated.ParentKeyType
	}
	if generated.ParentKeyType == "" || generated.TargetKeyType == "" {
		return ormTemplateRelation{}, fmt.Errorf("model %s relation %s uses a key type that cannot be normalized", model.Name, relation.Name)
	}
	generated.ParentKeyValue, generated.ParentKeyConditional = relationKeyCallback(*parentField, generated.ParentKeyType, "parent."+parentField.Name)
	generated.TargetKeyValue, generated.TargetKeyConditional = relationKeyCallback(*targetField, generated.TargetKeyType, "target."+targetField.Name)
	generated.ParentKeyPointer = strings.HasPrefix(parentField.GeneratedGoType, "*")
	generated.TargetKeyPointer = strings.HasPrefix(targetField.GeneratedGoType, "*")
	switch relation.Kind {
	case relationBelongsTo:
		generated.DescriptorType = fmt.Sprintf("orm.BelongsTo[%s, %s, %s]", model.Name, target.Name, generated.ParentKeyType)
	case relationHasOne:
		generated.DescriptorType = fmt.Sprintf("orm.HasOne[%s, %s, %s]", model.Name, target.Name, generated.ParentKeyType)
	case relationHasMany:
		generated.DescriptorType = fmt.Sprintf("orm.HasMany[%s, %s, %s]", model.Name, target.Name, generated.ParentKeyType)
	case relationManyToMany:
		generated.DescriptorType = fmt.Sprintf("orm.ManyToMany[%s, %s, %s, %s]", model.Name, target.Name, generated.ParentKeyType, generated.TargetKeyType)
		for _, field := range target.Fields {
			generated.TargetFields = append(generated.TargetFields, ormTemplateField{Name: field.Name, Type: field.GeneratedGoType, Column: field.Column})
		}
	}
	return generated, nil
}

func ormFieldMap(model modelDefinition) map[string]*modelField {
	fields := make(map[string]*modelField, len(model.Fields))
	for index := range model.Fields {
		fields[model.Fields[index].Name] = &model.Fields[index]
	}
	return fields
}

func normalizedRelationKeyType(field modelField) string {
	if field.typeFamily == "bytes" || field.typeFamily == "json" {
		return "string"
	}
	return relationFieldValueType(field)
}

func relationFieldValueType(field modelField) string {
	if strings.HasPrefix(field.GeneratedGoType, "*") {
		return strings.TrimPrefix(field.GeneratedGoType, "*")
	}
	switch field.GeneratedGoType {
	case "sql.NullString":
		return "string"
	case "sql.NullInt64":
		return "int64"
	case "sql.NullInt32":
		return "int32"
	case "sql.NullInt16":
		return "int16"
	case "sql.NullByte":
		return "byte"
	case "sql.NullFloat64":
		return "float64"
	case "sql.NullBool":
		return "bool"
	case "sql.NullTime":
		return "time.Time"
	default:
		return field.GeneratedGoType
	}
}

func relationKeyCallback(field modelField, keyType, selector string) (string, bool) {
	valueType := relationFieldValueType(field)
	value := selector
	conditional := false
	if strings.HasPrefix(field.GeneratedGoType, "*") {
		value = "*" + selector
		conditional = true
	} else {
		suffixes := map[string]string{
			"sql.NullString": ".String", "sql.NullInt64": ".Int64", "sql.NullInt32": ".Int32",
			"sql.NullInt16": ".Int16", "sql.NullByte": ".Byte", "sql.NullFloat64": ".Float64",
			"sql.NullBool": ".Bool", "sql.NullTime": ".Time",
		}
		if suffix := suffixes[field.GeneratedGoType]; suffix != "" {
			value += suffix
			conditional = true
		}
	}
	if field.typeFamily == "bytes" || field.typeFamily == "json" {
		value = "string(" + value + ")"
	} else if valueType != keyType {
		value = keyType + "(" + value + ")"
	}
	return value, conditional
}
