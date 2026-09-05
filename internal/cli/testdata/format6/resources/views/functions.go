package views

import (
	"html/template"

	"github.com/ShanilKoshitha/goforge/view"

	"example.com/format6/resources/views/viewfuncs"
)

// Functions preserves the convenient views package API while the editable
// registry lives independently in viewfuncs so compilation can recreate a
// missing or corrupt generated artifact.
func Functions() template.FuncMap {
	return viewfuncs.Functions()
}

// New constructs the production renderer from generated, inspectable source.
func New() (*view.Engine, error) {
	return view.ParseCompiled(Compiled, Functions())
}
