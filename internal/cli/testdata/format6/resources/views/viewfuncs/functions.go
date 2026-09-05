package viewfuncs

import (
	"html/template"
	"strings"
)

// Functions is the application-owned template function registry. It is kept
// independent from generated view source so the compiler can always rebuild
// a missing or corrupt artifact.
func Functions() template.FuncMap {
	return template.FuncMap{
		"headline": strings.TrimSpace,
	}
}
