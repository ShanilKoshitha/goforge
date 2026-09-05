// Package view renders html/template files to a buffer before committing an
// HTTP response. Templates are standard-library templates with no hidden DSL.
package view

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
)

type Engine struct {
	templates *template.Template
	mappings  map[string]compiledMapping
	includes  map[string][]includeEdge
}

func Parse(files fs.FS, functions template.FuncMap, patterns ...string) (*Engine, error) {
	if files == nil {
		return nil, fmt.Errorf("view filesystem is required")
	}
	if len(patterns) == 0 {
		patterns = []string{"*.html"}
	}
	parsed, err := template.New("views").Option("missingkey=error").Funcs(functions).ParseFS(files, patterns...)
	if err != nil {
		return nil, fmt.Errorf("parse views: %w", err)
	}
	return &Engine{templates: parsed}, nil
}

func (engine *Engine) Render(response http.ResponseWriter, status int, name string, data any) error {
	var body bytes.Buffer
	if err := engine.templates.ExecuteTemplate(&body, name, data); err != nil {
		return mapExecutionError(name, engine.mappings, engine.includes, err)
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	if _, err := response.Write(body.Bytes()); err != nil {
		return fmt.Errorf("write view %q: %w", name, err)
	}
	return nil
}

// Templates exposes the underlying templates for configuration before serving requests.
// Callers must not modify them concurrently with Render.
func (engine *Engine) Templates() *template.Template {
	return engine.templates
}
