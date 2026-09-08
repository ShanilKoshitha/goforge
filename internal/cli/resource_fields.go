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

// resourceDefinition is deliberately ephemeral. Persistent resource metadata
// retains identity and migration allocation, not a second application schema.
type resourceDefinition struct {
	resourceSpec
	Fields       []resourceField
	SchemaDriven bool
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

func parseMakeResourceArguments(arguments []string) (string, []resourceField, bool, error) {
	if len(arguments) == 0 || strings.HasPrefix(arguments[0], "--") {
		return "", nil, false, resourceUsageError()
	}
	name := arguments[0]
	var specifications []string
	for index := 1; index < len(arguments); index++ {
		argument := arguments[index]
		switch {
		case argument == "--field":
			if index+1 >= len(arguments) || strings.HasPrefix(arguments[index+1], "--") {
				return "", nil, false, fmt.Errorf("--field requires a field specification; %w", resourceUsageError())
			}
			index++
			specifications = append(specifications, arguments[index])
		case strings.HasPrefix(argument, "--field="):
			value := strings.TrimPrefix(argument, "--field=")
			if value == "" {
				return "", nil, false, fmt.Errorf("--field requires a field specification; %w", resourceUsageError())
			}
			specifications = append(specifications, value)
		default:
			return "", nil, false, fmt.Errorf("unknown resource option %q; %w", argument, resourceUsageError())
		}
	}
	fields, err := parseResourceFields(specifications)
	if err != nil {
		return "", nil, false, err
	}
	return name, fields, len(specifications) > 0, nil
}

func resourceUsageError() error {
	return fmt.Errorf("usage: forge make:resource <name> [--field <name>:<string|text|integer|boolean>[:required|nullable]]...")
}

func parseResourceFields(specifications []string) ([]resourceField, error) {
	if len(specifications) == 0 {
		return defaultResourceFields(), nil
	}
	if len(specifications) > maximumResourceFields {
		return nil, fmt.Errorf("resource fields exceed the maximum of %d", maximumResourceFields)
	}

	fields := make([]resourceField, 0, len(specifications))
	seenNames := make(map[string]struct{}, len(specifications))
	seenGoNames := make(map[string]string, len(specifications))
	for _, specification := range specifications {
		field, err := parseResourceField(specification)
		if err != nil {
			return nil, err
		}
		if _, exists := seenNames[field.Name]; exists {
			return nil, fmt.Errorf("duplicate resource field %q", field.Name)
		}
		if previous, exists := seenGoNames[field.GoName]; exists {
			return nil, fmt.Errorf("resource fields %q and %q produce the same Go name %s", previous, field.Name, field.GoName)
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
	if specification == "" {
		return resourceField{}, fmt.Errorf("resource field specification cannot be empty")
	}
	if len(specification) > maximumResourceFieldLength {
		return resourceField{}, fmt.Errorf("resource field specification exceeds %d bytes", maximumResourceFieldLength)
	}
	if strings.TrimSpace(specification) != specification {
		return resourceField{}, fmt.Errorf("resource field specification %q cannot contain surrounding whitespace", specification)
	}
	parts := strings.Split(specification, ":")
	if len(parts) < 2 {
		return resourceField{}, fmt.Errorf("resource field %q must use name:type[:required|nullable]", specification)
	}
	name := parts[0]
	if reservedResourceFieldNames[name] {
		return resourceField{}, fmt.Errorf("resource field %q is reserved", name)
	}
	if !resourceFieldName.MatchString(name) {
		return resourceField{}, fmt.Errorf("resource field name %q must be lower_snake_case", name)
	}
	if !safeIdentifier(name) || len(name) > maximumResourceIdentifier {
		return resourceField{}, fmt.Errorf("resource field name %q is not a safe PostgreSQL identifier", name)
	}
	goName, err := pascal(name)
	if err != nil || !token.IsIdentifier(goName) || !ast.IsExported(goName) || len(goName) > maximumResourceIdentifier {
		return resourceField{}, fmt.Errorf("resource field name %q does not produce a safe exported Go identifier", name)
	}
	if reservedResourceFieldGoNames[goName] {
		return resourceField{}, fmt.Errorf("resource field %q produces reserved Go name %s", name, goName)
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
		return resourceField{}, fmt.Errorf("resource field %q has unknown type %q", name, parts[1])
	}

	required, nullable := false, false
	for _, modifier := range parts[2:] {
		switch modifier {
		case "required":
			if required {
				return resourceField{}, fmt.Errorf("resource field %q repeats modifier required", name)
			}
			required = true
		case "nullable":
			if nullable {
				return resourceField{}, fmt.Errorf("resource field %q repeats modifier nullable", name)
			}
			nullable = true
		default:
			return resourceField{}, fmt.Errorf("resource field %q has unknown modifier %q", name, modifier)
		}
	}
	if required && nullable {
		return resourceField{}, fmt.Errorf("resource field %q cannot be both required and nullable", name)
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
