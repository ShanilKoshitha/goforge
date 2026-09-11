package cli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

var format13ResourceDeclarations = map[string]struct{}{
	"Access": {}, "AccessAll": {}, "AccessDenied": {}, "AccessOwner": {},
	"Action": {}, "ActionCreate": {}, "ActionDelete": {}, "ActionList": {}, "ActionUpdate": {}, "ActionView": {},
	"AllRecordsScope": {}, "Authorize": {}, "AuthorizeFunc": {},
	"ErrForbidden": {}, "ErrInvalidAuthorization": {}, "ErrInvalidScope": {},
	"OwnerScope": {}, "Scope": {},
}

func generatedResourceRegistry(module string, state resourceState) (string, error) {
	return generatedResourceRegistryForFormat(module, state, 8)
}

func generatedResourceRegistryForFormat(module string, state resourceState, format int) (string, error) {
	data := struct {
		Module                string
		Resources             []resourceSpec
		AuthorizationPolicies bool
	}{Module: module, Resources: state.Resources, AuthorizationPolicies: format >= 13}
	return renderTemplate("templates/resource/registry.go.tmpl", "routes/resources_gen.go", data)
}

type resourceTemplateData struct {
	resourceSpec
	Module                string
	QueryAccessor         string
	Fields                []resourceTemplateField
	Relationships         []resourceTemplateRelationship
	SchemaDriven          bool
	OwnerCandidateKey     bool
	OwnerKeyName          string
	HasTextualFields      bool
	FirstTextualField     *resourceTemplateField
	AuthorizationPolicies bool
}

// resourceTemplateRelationship exposes only resolved, deterministic names to
// templates. The command-line relationship description remains one-shot input.
type resourceTemplateRelationship struct {
	Name                string
	GoName              string
	Label               string
	ForeignKey          string
	ForeignKeyGoName    string
	TargetSpec          resourceSpec
	TargetQueryAccessor string
	IndexName           string
	ConstraintName      string
}

type resourceTemplateField struct {
	resourceField
	BaseGoType         string
	SQLType            string
	ForgeTag           string
	String             bool
	Text               bool
	Integer            bool
	Boolean            bool
	SampleGoValue      string
	UpdatedGoValue     string
	DriverValue        string
	UpdatedDriverValue string
	SampleJSONValue    string
	UpdatedJSONValue   string
	InvalidJSONValue   string
	SampleFormValue    string
	UpdatedFormValue   string
}

func resourceFiles(module string, definition resourceDefinition) ([]plannedFile, error) {
	format, err := projectFormat()
	if err != nil {
		return nil, err
	}
	return resourceFilesForFormat(module, definition, format)
}

func resourceFilesForFormat(module string, definition resourceDefinition, format int) ([]plannedFile, error) {
	spec := definition.resourceSpec
	if format >= 13 {
		if _, conflicts := format13ResourceDeclarations[spec.Name]; conflicts {
			return nil, fmt.Errorf("resource %s conflicts with a format-13 authorization declaration", spec.Name)
		}
	}
	base := filepath.Join("internal", "resources", spec.Package)
	queryAccessor, err := pascal(spec.Plural)
	if err != nil {
		return nil, err
	}
	data := resourceTemplateData{
		resourceSpec:          spec,
		Module:                module,
		QueryAccessor:         queryAccessor,
		SchemaDriven:          definition.SchemaDriven,
		OwnerCandidateKey:     format >= 10,
		AuthorizationPolicies: format >= 13,
	}
	if data.OwnerCandidateKey {
		data.OwnerKeyName = spec.Plural + "_owner_id_key"
		if !safeIdentifier(data.OwnerKeyName) || len(data.OwnerKeyName) > maximumResourceIdentifier {
			return nil, fmt.Errorf("resource %s produces owner candidate-key name %q longer than PostgreSQL's %d-byte identifier limit", spec.Name, data.OwnerKeyName, maximumResourceIdentifier)
		}
	}
	for _, field := range definition.Fields {
		projected := projectResourceTemplateField(field)
		data.Fields = append(data.Fields, projected)
		if projected.String || projected.Text {
			data.HasTextualFields = true
			if data.FirstTextualField == nil {
				copy := projected
				data.FirstTextualField = &copy
			}
		}
	}
	for _, relationship := range definition.Relationships {
		data.Relationships = append(data.Relationships, resourceTemplateRelationship{
			Name:                relationship.Name,
			GoName:              relationship.GoName,
			Label:               relationship.Label,
			ForeignKey:          relationship.ForeignKey,
			ForeignKeyGoName:    relationship.ForeignKeyGoName,
			TargetSpec:          relationship.TargetSpec,
			TargetQueryAccessor: relationship.TargetQueryAccessor,
			IndexName:           relationship.IndexName,
			ConstraintName:      relationship.ConstraintName,
		})
	}
	var files []plannedFile
	modelDestination := filepath.Join("internal", "models", spec.Package+".go")
	modelSource, err := renderTemplate("templates/resource/source_model.go.tmpl", modelDestination, data)
	if err != nil {
		return nil, err
	}
	files = append(files, plannedFile{path: modelDestination, content: modelSource})
	names := []string{"model.go", "repository.go", "repository_test.go", "request.go", "controller.go", "controller_test.go", "web_controller.go", "web_controller_test.go"}
	if data.AuthorizationPolicies {
		names = append(names, "authorization.go", "authorization_test.go")
	}
	for _, name := range names {
		destination := filepath.Join(base, name)
		content, err := renderTemplate("templates/resource/"+name+".tmpl", destination, data)
		if err != nil {
			return nil, err
		}
		files = append(files, plannedFile{path: destination, content: content})
	}
	viewBase := filepath.Join("resources", "views", "pages", spec.Plural)
	for _, name := range []string{"index.forge.html", "form.forge.html", "new.forge.html", "edit.forge.html", "show.forge.html"} {
		destination := filepath.Join(viewBase, name)
		content, err := renderTemplate("templates/resource/views/"+name+".tmpl", destination, data)
		if err != nil {
			return nil, err
		}
		files = append(files, plannedFile{path: destination, content: content})
	}
	migration := filepath.Join("database", "migrations", spec.MigrationVersion+"_create_"+spec.Plural)
	up, err := renderTemplate("templates/resource/up.sql.tmpl", migration+".up.sql", data)
	if err != nil {
		return nil, err
	}
	files = append(files,
		plannedFile{path: migration + ".up.sql", content: up},
		plannedFile{path: migration + ".down.sql", content: "DROP TABLE IF EXISTS " + spec.Plural + ";\n"},
	)
	return files, nil
}

func projectResourceTemplateField(field resourceField) resourceTemplateField {
	projected := resourceTemplateField{
		resourceField: field,
		BaseGoType:    strings.TrimPrefix(field.GoType, "*"),
		SQLType:       strings.ToUpper(field.DBType),
	}
	modifier := "required"
	if field.Nullable {
		modifier = "nullable"
	}
	projected.ForgeTag = modifier
	switch field.Kind {
	case resourceFieldString:
		projected.String = true
		projected.ForgeTag += ",type=" + field.DBType
		setResourceFieldExamples(&projected, "Example "+field.Label, "Updated "+field.Label)
		projected.InvalidJSONValue = "7"
	case resourceFieldText:
		projected.Text = true
		projected.ForgeTag += ",type=" + field.DBType
		setResourceFieldExamples(&projected, "Example "+field.Label, "Updated "+field.Label)
		projected.InvalidJSONValue = "7"
	case resourceFieldInteger:
		projected.Integer = true
		projected.SampleGoValue, projected.UpdatedGoValue = "int64(7)", "int64(11)"
		projected.DriverValue, projected.UpdatedDriverValue = "int64(7)", "int64(11)"
		projected.SampleJSONValue, projected.UpdatedJSONValue = "7", "11"
		projected.InvalidJSONValue = strconv.Quote("not-an-integer")
		projected.SampleFormValue, projected.UpdatedFormValue = "7", "11"
	case resourceFieldBoolean:
		projected.Boolean = true
		projected.SampleGoValue, projected.UpdatedGoValue = "true", "false"
		projected.DriverValue, projected.UpdatedDriverValue = "true", "false"
		projected.SampleJSONValue, projected.UpdatedJSONValue = "true", "false"
		projected.InvalidJSONValue = strconv.Quote("not-a-boolean")
		projected.SampleFormValue, projected.UpdatedFormValue = "true", "false"
	}
	if field.Nullable {
		projected.SampleGoValue = "pointer(" + projected.SampleGoValue + ")"
		projected.UpdatedGoValue = "pointer(" + projected.UpdatedGoValue + ")"
	}
	return projected
}

func setResourceFieldExamples(field *resourceTemplateField, sample, updated string) {
	field.SampleGoValue, field.UpdatedGoValue = strconv.Quote(sample), strconv.Quote(updated)
	field.DriverValue, field.UpdatedDriverValue = strconv.Quote(sample), strconv.Quote(updated)
	field.SampleJSONValue, field.UpdatedJSONValue = strconv.Quote(sample), strconv.Quote(updated)
	field.SampleFormValue, field.UpdatedFormValue = sample, updated
}
