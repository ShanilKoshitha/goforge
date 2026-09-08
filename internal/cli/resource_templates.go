package cli

import (
	"path/filepath"
	"strconv"
	"strings"
)

func generatedResourceRegistry(module string, state resourceState) (string, error) {
	data := struct {
		Module    string
		Resources []resourceSpec
	}{Module: module, Resources: state.Resources}
	return renderTemplate("templates/resource/registry.go.tmpl", "routes/resources_gen.go", data)
}

type resourceTemplateData struct {
	resourceSpec
	Module            string
	QueryAccessor     string
	Fields            []resourceTemplateField
	SchemaDriven      bool
	HasTextualFields  bool
	FirstTextualField *resourceTemplateField
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
	spec := definition.resourceSpec
	base := filepath.Join("internal", "resources", spec.Package)
	queryAccessor, err := pascal(spec.Plural)
	if err != nil {
		return nil, err
	}
	data := resourceTemplateData{
		resourceSpec:  spec,
		Module:        module,
		QueryAccessor: queryAccessor,
		SchemaDriven:  definition.SchemaDriven,
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
	var files []plannedFile
	modelDestination := filepath.Join("internal", "models", spec.Package+".go")
	modelSource, err := renderTemplate("templates/resource/source_model.go.tmpl", modelDestination, data)
	if err != nil {
		return nil, err
	}
	files = append(files, plannedFile{path: modelDestination, content: modelSource})
	for _, name := range []string{"model.go", "repository.go", "repository_test.go", "request.go", "controller.go", "controller_test.go", "web_controller.go", "web_controller_test.go"} {
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
