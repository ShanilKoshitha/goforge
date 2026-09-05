package cli

import (
	"bytes"
	"embed"
	"fmt"
	"go/format"
	"path/filepath"
	"text/template"
)

// Include dotfiles such as .env and .forge; plain directory embedding omits them.
//
//go:embed all:templates
var templateFiles embed.FS

func renderTemplate(path, destination string, data any) (string, error) {
	source, err := templateFiles.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", path, err)
	}
	// Separate delimiters preserve Go HTML template expressions in generated views.
	parsed, err := template.New(path).Delims("[[", "]]").Option("missingkey=error").Parse(string(source))
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", path, err)
	}
	var output bytes.Buffer
	if err := parsed.Execute(&output, data); err != nil {
		return "", fmt.Errorf("render template %s: %w", path, err)
	}
	if filepath.Ext(destination) != ".go" {
		return output.String(), nil
	}
	formatted, err := format.Source(output.Bytes())
	if err != nil {
		return "", fmt.Errorf("format generated %s: %w", destination, err)
	}
	return string(formatted), nil
}
