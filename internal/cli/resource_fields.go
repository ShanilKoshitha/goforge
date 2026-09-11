package cli

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"strings"
	"unicode"
)

const (
	maximumResourceFields      = 32
	maximumResourceBelongsTo   = 8
	maximumResourceFieldLength = 256
	maximumResourceIdentifier  = 63
)

type resourceFieldKind string

const (
	resourceFieldString  resourceFieldKind = "string"
	resourceFieldText    resourceFieldKind = "text"
	resourceFieldInteger resourceFieldKind = "integer"
	resourceFieldBoolean resourceFieldKind = "boolean"
)

// resourceField is the generator's one-shot, typed description of an editable
// resource field. Emitted Go and SQL become authoritative after generation.
type resourceField struct {
	Name     string
	GoName   string
	Label    string
	Kind     resourceFieldKind
	GoType   string
	DBType   string
	Required bool
	Nullable bool
}

// resourceBelongsTo is the generator's one-shot description of a required
// relationship to an existing generated resource. Emitted Go and SQL become
// authoritative after generation; this description is never runtime schema.
type resourceBelongsTo struct {
	Name             string
	GoName           string
	Label            string
	ForeignKey       string
	ForeignKeyGoName string
	Target           string
	Required         bool
}

// resourceDefinition is deliberately ephemeral. Persistent resource metadata
// retains identity and migration allocation, not a second application schema.
type resourceDefinition struct {
	resourceSpec
	Fields        []resourceField
	Relationships []resourceRelationship
	SchemaDriven  bool
}

var resourceFieldName = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)

var reservedResourceFieldNames = map[string]bool{
	"id": true, "user_id": true, "created_at": true, "updated_at": true,
	"version": true, "owner": true, "_token": true, "_method": true,
}

var reservedResourceFieldGoNames = map[string]bool{
	"ID": true, "UserID": true, "CreatedAt": true, "UpdatedAt": true,
	"Version": true, "Owner": true,
}

// fieldGrammar keeps the shared field/relationship syntax independent from
// the surface that emits it. HTTP resources reserve ownership and form names;
// ordinary models only reserve intrinsic/generated members and transport names.
type fieldGrammar struct {
	subject          string
	target           string
	reservedRawNames map[string]bool
	reservedGoNames  map[string]bool
}

var resourceFieldGrammar = fieldGrammar{
	subject:          "resource",
	target:           "ExistingResource",
	reservedRawNames: reservedResourceFieldNames,
	reservedGoNames:  reservedResourceFieldGoNames,
}

var modelFieldGrammar = fieldGrammar{
	subject: "model",
	target:  "ExistingModel",
	reservedRawNames: map[string]bool{
		"id": true, "created_at": true, "updated_at": true, "version": true,
		"_token": true, "_method": true,
	},
	reservedGoNames: map[string]bool{
		"ID": true, "CreatedAt": true, "UpdatedAt": true, "Version": true,
	},
}

func parseMakeResourceArguments(arguments []string) (string, []resourceField, []resourceBelongsTo, bool, error) {
	if len(arguments) == 0 || strings.HasPrefix(arguments[0], "--") {
		return "", nil, nil, false, resourceUsageError()
	}
	name := arguments[0]
	var fieldSpecifications []string
	var belongsToSpecifications []string
	for index := 1; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "--field":
			if index+1 >= len(arguments) || strings.HasPrefix(arguments[index+1], "--") {
				return "", nil, nil, false, fmt.Errorf("--field requires a field specification; %w", resourceUsageError())
			}
			index++
			fieldSpecifications = append(fieldSpecifications, arguments[index])
		case strings.HasPrefix(argument, "--field="):
			value := strings.TrimPrefix(argument, "--field=")
			if value == "" {
				return "", nil, nil, false, fmt.Errorf("--field requires a field specification; %w", resourceUsageError())
			}
			fieldSpecifications = append(fieldSpecifications, value)
		case argument == "--belongs-to":
			if index+1 >= len(arguments) || strings.HasPrefix(arguments[index+1], "--") {
				return "", nil, nil, false, fmt.Errorf("--belongs-to requires a relationship specification; %w", resourceUsageError())
			}
			index++
			belongsToSpecifications = append(belongsToSpecifications, arguments[index])
		case strings.HasPrefix(argument, "--belongs-to="):
			value := strings.TrimPrefix(argument, "--belongs-to=")
			if value == "" {
				return "", nil, nil, false, fmt.Errorf("--belongs-to requires a relationship specification; %w", resourceUsageError())
			}
			belongsToSpecifications = append(belongsToSpecifications, value)
		default:
			return "", nil, nil, false, fmt.Errorf("unknown resource option %q; %w", argument, resourceUsageError())
		}
	}
	fields, err := parseResourceFields(fieldSpecifications)
	if err != nil {
		return "", nil, nil, false, err
	}
	belongsTo, err := parseResourceBelongsTo(name, belongsToSpecifications)
	if err != nil {
		return "", nil, nil, false, err
	}
	if err := validateResourceMemberCollisions(fields, belongsTo); err != nil {
		return "", nil, nil, false, err
	}
	return name, fields, belongsTo, len(fieldSpecifications) > 0 || len(belongsToSpecifications) > 0, nil
}

func resourceUsageError() error {
	return fmt.Errorf("usage: forge make:resource <name> [--field <name>:<string|text|integer|boolean>[:required|nullable]]... [--belongs-to <name>:<ExistingResource>]...")
}

func parseResourceBelongsTo(resourceName string, specifications []string) ([]resourceBelongsTo, error) {
	return parseBelongsTo(resourceName, specifications, resourceFieldGrammar)
}

func parseModelBelongsTo(modelName string, specifications []string) ([]resourceBelongsTo, error) {
	return parseBelongsTo(modelName, specifications, modelFieldGrammar)
}

func parseBelongsTo(subjectName string, specifications []string, grammar fieldGrammar) ([]resourceBelongsTo, error) {
	if len(specifications) > maximumResourceBelongsTo {
		return nil, fmt.Errorf("%s relationships exceed the maximum of %d", grammar.subject, maximumResourceBelongsTo)
	}
	if len(specifications) == 0 {
		return nil, nil
	}
	subjectType, err := pascal(subjectName)
	if err != nil {
		return nil, err
	}

	belongsTo := make([]resourceBelongsTo, 0, len(specifications))
	seenNames := make(map[string]struct{}, len(specifications))
	for _, specification := range specifications {
		relation, err := parseRequiredBelongsToWithGrammar(specification, grammar)
		if err != nil {
			return nil, err
		}
		if _, exists := seenNames[relation.Name]; exists {
			return nil, fmt.Errorf("duplicate belongs-to relationship %q", relation.Name)
		}
		if relation.Target == subjectType {
			return nil, fmt.Errorf("belongs-to relationship %q cannot target its own %s %s", relation.Name, grammar.subject, subjectType)
		}
		seenNames[relation.Name] = struct{}{}
		belongsTo = append(belongsTo, relation)
	}
	return belongsTo, nil
}

func parseRequiredBelongsTo(specification string) (resourceBelongsTo, error) {
	return parseRequiredBelongsToWithGrammar(specification, resourceFieldGrammar)
}

func parseRequiredBelongsToWithGrammar(specification string, grammar fieldGrammar) (resourceBelongsTo, error) {
	if specification == "" {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship specification cannot be empty")
	}
	if len(specification) > maximumResourceFieldLength {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship specification exceeds %d bytes", maximumResourceFieldLength)
	}
	if strings.TrimSpace(specification) != specification {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship specification %q cannot contain surrounding whitespace", specification)
	}
	parts := strings.Split(specification, ":")
	if len(parts) != 2 {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship %q must use name:%s", specification, grammar.target)
	}
	name, targetName := parts[0], parts[1]
	if strings.TrimSpace(name) != name || strings.TrimSpace(targetName) != targetName || targetName == "" {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship %q must use name:%s without whitespace", specification, grammar.target)
	}
	if grammar.reservedRawNames[name] {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship %q is reserved", name)
	}
	if !resourceFieldName.MatchString(name) {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship name %q must be lower_snake_case", name)
	}
	if !safeIdentifier(name) || len(name) > maximumResourceIdentifier {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship name %q is not a safe PostgreSQL identifier", name)
	}
	goName, err := pascal(name)
	if err != nil || !token.IsIdentifier(goName) || !ast.IsExported(goName) || len(goName) > maximumResourceIdentifier {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship name %q does not produce a safe exported Go identifier", name)
	}
	if grammar.reservedGoNames[goName] {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship %q produces reserved Go name %s", name, goName)
	}
	foreignKey := name + "_id"
	if grammar.reservedRawNames[foreignKey] {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship %q produces reserved foreign key %s", name, foreignKey)
	}
	if !safeIdentifier(foreignKey) || len(foreignKey) > maximumResourceIdentifier {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship %q produces unsafe foreign key %q", name, foreignKey)
	}
	foreignKeyGoName, err := pascal(foreignKey)
	if err != nil || !token.IsIdentifier(foreignKeyGoName) || !ast.IsExported(foreignKeyGoName) || len(foreignKeyGoName) > maximumResourceIdentifier {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship %q does not produce a safe foreign-key Go identifier", name)
	}
	if grammar.reservedGoNames[foreignKeyGoName] {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship %q produces reserved Go name %s", name, foreignKeyGoName)
	}
	target, err := pascal(targetName)
	packageName := strings.ToLower(target)
	if err != nil || !token.IsIdentifier(target) || !ast.IsExported(target) || len(target) > maximumResourceIdentifier ||
		!token.IsIdentifier(packageName) || token.IsKeyword(packageName) {
		return resourceBelongsTo{}, fmt.Errorf("belongs-to relationship %q target %q does not produce valid Go identifiers", name, targetName)
	}
	return resourceBelongsTo{
		Name: name, GoName: goName, Label: resourceFieldLabel(name),
		ForeignKey: foreignKey, ForeignKeyGoName: foreignKeyGoName,
		Target: target, Required: true,
	}, nil
}

func validateResourceMemberCollisions(fields []resourceField, belongsTo []resourceBelongsTo) error {
	return validateMemberCollisions(fields, belongsTo, resourceFieldGrammar)
}

func validateModelMemberCollisions(fields []resourceField, belongsTo []resourceBelongsTo) error {
	return validateMemberCollisions(fields, belongsTo, modelFieldGrammar)
}

func validateMemberCollisions(fields []resourceField, belongsTo []resourceBelongsTo, grammar fieldGrammar) error {
	rawNames := make(map[string]string, len(fields)+len(belongsTo)*2)
	goNames := make(map[string]string, len(fields)+len(belongsTo)*2)
	claim := func(names map[string]string, name, owner, namespace string) error {
		if previous, exists := names[name]; exists {
			return fmt.Errorf("%s %s %s conflicts with %s", grammar.subject, namespace, name, previous)
		}
		names[name] = owner
		return nil
	}
	for _, field := range fields {
		owner := fmt.Sprintf("field %q", field.Name)
		if err := claim(rawNames, field.Name, owner, "name"); err != nil {
			return err
		}
		if err := claim(goNames, field.GoName, owner, "Go name"); err != nil {
			return err
		}
	}
	for _, relation := range belongsTo {
		owner := fmt.Sprintf("belongs-to relationship %q", relation.Name)
		for _, name := range []string{relation.Name, relation.ForeignKey} {
			if err := claim(rawNames, name, owner, "name"); err != nil {
				return err
			}
		}
		for _, name := range []string{relation.GoName, relation.ForeignKeyGoName} {
			if err := claim(goNames, name, owner, "Go name"); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseResourceFields(specifications []string) ([]resourceField, error) {
	return parseFields(specifications, resourceFieldGrammar)
}

func parseModelFields(specifications []string) ([]resourceField, error) {
	if len(specifications) == 0 {
		return nil, nil
	}
	return parseFields(specifications, modelFieldGrammar)
}

func parseFields(specifications []string, grammar fieldGrammar) ([]resourceField, error) {
	if len(specifications) == 0 {
		return defaultResourceFields(), nil
	}
	if len(specifications) > maximumResourceFields {
		return nil, fmt.Errorf("%s fields exceed the maximum of %d", grammar.subject, maximumResourceFields)
	}

	fields := make([]resourceField, 0, len(specifications))
	seenNames := make(map[string]struct{}, len(specifications))
	seenGoNames := make(map[string]string, len(specifications))
	for _, specification := range specifications {
		field, err := parseField(specification, grammar)
		if err != nil {
			return nil, err
		}
		if _, exists := seenNames[field.Name]; exists {
			return nil, fmt.Errorf("duplicate %s field %q", grammar.subject, field.Name)
		}
		if previous, exists := seenGoNames[field.GoName]; exists {
			return nil, fmt.Errorf("%s fields %q and %q produce the same Go name %s", grammar.subject, previous, field.Name, field.GoName)
		}
		seenNames[field.Name] = struct{}{}
		seenGoNames[field.GoName] = field.Name
		fields = append(fields, field)
	}
	return fields, nil
}

func defaultResourceFields() []resourceField {
	return []resourceField{{
		Name: "name", GoName: "Name", Label: "Name", Kind: resourceFieldString,
		GoType: "string", DBType: "text", Required: true,
	}}
}

func parseResourceField(specification string) (resourceField, error) {
	return parseField(specification, resourceFieldGrammar)
}

func parseField(specification string, grammar fieldGrammar) (resourceField, error) {
	if specification == "" {
		return resourceField{}, fmt.Errorf("%s field specification cannot be empty", grammar.subject)
	}
	if len(specification) > maximumResourceFieldLength {
		return resourceField{}, fmt.Errorf("%s field specification exceeds %d bytes", grammar.subject, maximumResourceFieldLength)
	}
	if strings.TrimSpace(specification) != specification {
		return resourceField{}, fmt.Errorf("%s field specification %q cannot contain surrounding whitespace", grammar.subject, specification)
	}
	parts := strings.Split(specification, ":")
	if len(parts) < 2 {
		return resourceField{}, fmt.Errorf("%s field %q must use name:type[:required|nullable]", grammar.subject, specification)
	}
	name := parts[0]
	if grammar.reservedRawNames[name] {
		return resourceField{}, fmt.Errorf("%s field %q is reserved", grammar.subject, name)
	}
	if !resourceFieldName.MatchString(name) {
		return resourceField{}, fmt.Errorf("%s field name %q must be lower_snake_case", grammar.subject, name)
	}
	if !safeIdentifier(name) || len(name) > maximumResourceIdentifier {
		return resourceField{}, fmt.Errorf("%s field name %q is not a safe PostgreSQL identifier", grammar.subject, name)
	}
	goName, err := pascal(name)
	if err != nil || !token.IsIdentifier(goName) || !ast.IsExported(goName) || len(goName) > maximumResourceIdentifier {
		return resourceField{}, fmt.Errorf("%s field name %q does not produce a safe exported Go identifier", grammar.subject, name)
	}
	if grammar.reservedGoNames[goName] {
		return resourceField{}, fmt.Errorf("%s field %q produces reserved Go name %s", grammar.subject, name, goName)
	}

	kind := resourceFieldKind(parts[1])
	goType, databaseType := "", ""
	switch kind {
	case resourceFieldString:
		goType, databaseType = "string", "varchar(255)"
	case resourceFieldText:
		goType, databaseType = "string", "text"
	case resourceFieldInteger:
		goType, databaseType = "int64", "bigint"
	case resourceFieldBoolean:
		goType, databaseType = "bool", "boolean"
	default:
		return resourceField{}, fmt.Errorf("%s field %q has unknown type %q", grammar.subject, name, parts[1])
	}

	required, nullable := false, false
	for _, modifier := range parts[2:] {
		switch modifier {
		case "required":
			if required {
				return resourceField{}, fmt.Errorf("%s field %q repeats modifier required", grammar.subject, name)
			}
			required = true
		case "nullable":
			if nullable {
				return resourceField{}, fmt.Errorf("%s field %q repeats modifier nullable", grammar.subject, name)
			}
			nullable = true
		default:
			return resourceField{}, fmt.Errorf("%s field %q has unknown modifier %q", grammar.subject, name, modifier)
		}
	}
	if required && nullable {
		return resourceField{}, fmt.Errorf("%s field %q cannot be both required and nullable", grammar.subject, name)
	}
	if !required && !nullable {
		required = true
	}
	if nullable {
		goType = "*" + goType
	}
	return resourceField{
		Name: name, GoName: goName, Label: resourceFieldLabel(name), Kind: kind,
		GoType: goType, DBType: databaseType, Required: required, Nullable: nullable,
	}, nil
}

func resourceFieldLabel(name string) string {
	parts := strings.Split(name, "_")
	for index, part := range parts {
		if commonInitialisms[strings.ToUpper(part)] {
			parts[index] = strings.ToUpper(part)
			continue
		}
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		parts[index] = string(runes)
	}
	return strings.Join(parts, " ")
}
