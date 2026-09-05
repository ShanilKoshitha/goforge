package cli

import "path/filepath"

func generatedResourceRegistry(module string, state resourceState) (string, error) {
	data := struct {
		Module    string
		Resources []resourceSpec
	}{Module: module, Resources: state.Resources}
	return renderTemplate("templates/resource/registry.go.tmpl", "routes/resources_gen.go", data)
}

type resourceTemplateData struct {
	resourceSpec
	Module        string
	QueryAccessor string
}

func resourceFiles(module string, spec resourceSpec) ([]plannedFile, error) {
	base := filepath.Join("internal", "resources", spec.Package)
	queryAccessor, err := pascal(spec.Plural)
	if err != nil {
		return nil, err
	}
	data := resourceTemplateData{resourceSpec: spec, Module: module, QueryAccessor: queryAccessor}
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
