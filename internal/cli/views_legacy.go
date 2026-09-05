package cli

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ShanilKoshitha/goforge/view"
)

// The format-4 compiler is intentionally frozen. It recognizes only the four
// structural directives shipped by v0.4; all later Forge syntax remains
// literal application text and cannot silently change meaning under a newer
// CLI.
var (
	legacyExtendsPattern = regexp.MustCompile(`@extends\(\s*"([^"]+)"\s*\)`)
	legacySectionPattern = regexp.MustCompile(`(?s)@section\(\s*"([^"]+)"\s*\)(.*?)@endsection`)
	legacyYieldPattern   = regexp.MustCompile(`@yield\(\s*"([^"]+)"\s*\)`)
	legacyIncludePattern = regexp.MustCompile(`@include\(\s*"([^"]+)"(?:\s*,\s*([^)]*?))?\s*\)`)
)

type legacyViewDocument struct {
	name     string
	file     string
	source   string
	extends  string
	sections map[string]string
	body     string
}

type legacyViewCompiler struct {
	documents map[string]legacyViewDocument
}

func compileLegacyForge(files fs.FS) ([]view.CompiledTemplate, error) {
	paths := make([]string, 0)
	err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(name, ".forge.html") {
			paths = append(paths, name)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk legacy views: %w", err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no .forge.html templates found")
	}
	compiler := legacyViewCompiler{documents: make(map[string]legacyViewDocument, len(paths))}
	for _, file := range paths {
		source, err := fs.ReadFile(files, file)
		if err != nil {
			return nil, fmt.Errorf("read legacy view %s: %w", file, err)
		}
		document, err := parseLegacyView(file, string(source))
		if err != nil {
			return nil, err
		}
		if _, exists := compiler.documents[document.name]; exists {
			return nil, fmt.Errorf("duplicate legacy view %q", document.name)
		}
		compiler.documents[document.name] = document
	}
	if err := compiler.validateIncludes(); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(compiler.documents))
	for name := range compiler.documents {
		names = append(names, name)
	}
	sort.Strings(names)
	compiled := make([]view.CompiledTemplate, 0, len(names))
	for _, name := range names {
		allowOpen := compiler.documents[name].extends == "" || compiler.isExtended(name)
		source, err := compiler.resolve(name, nil, nil, allowOpen)
		if err != nil {
			return nil, err
		}
		source, err = compiler.rewriteIncludes(source)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, view.CompiledTemplate{Name: name, Source: source})
	}
	return compiled, nil
}

func parseLegacyView(file, source string) (legacyViewDocument, error) {
	document := legacyViewDocument{
		name: legacyViewName(file), file: file, source: source,
		sections: make(map[string]string), body: source,
	}
	extends := legacyExtendsPattern.FindAllStringSubmatchIndex(source, -1)
	if len(extends) > 1 {
		return legacyViewDocument{}, fmt.Errorf("legacy view %s has more than one @extends", file)
	}
	if len(extends) == 1 {
		match := extends[0]
		if strings.TrimSpace(source[:match[0]]) != "" {
			return legacyViewDocument{}, fmt.Errorf("legacy view %s must place @extends first", file)
		}
		document.extends = normalizeLegacyReference(source[match[2]:match[3]])
		document.body = source[:match[0]] + source[match[1]:]
	}
	sections := legacySectionPattern.FindAllStringSubmatchIndex(document.body, -1)
	var body strings.Builder
	last := 0
	for _, match := range sections {
		name := document.body[match[2]:match[3]]
		if _, exists := document.sections[name]; exists {
			return legacyViewDocument{}, fmt.Errorf("legacy view %s has duplicate section %q", file, name)
		}
		document.sections[name] = document.body[match[4]:match[5]]
		body.WriteString(document.body[last:match[0]])
		last = match[1]
	}
	body.WriteString(document.body[last:])
	document.body = body.String()
	if document.extends != "" && strings.TrimSpace(document.body) != "" {
		return legacyViewDocument{}, fmt.Errorf("legacy view %s with @extends may only contain sections", file)
	}
	if document.extends == "" && len(document.sections) > 0 {
		return legacyViewDocument{}, fmt.Errorf("legacy view %s uses @section without @extends", file)
	}
	return document, nil
}

func (compiler legacyViewCompiler) resolve(name string, inherited map[string]string, stack []string, allowOpen bool) (string, error) {
	document, exists := compiler.documents[name]
	if !exists {
		return "", fmt.Errorf("unknown legacy view %q", name)
	}
	for _, ancestor := range stack {
		if ancestor == name {
			return "", fmt.Errorf("legacy template inheritance cycle: %s", strings.Join(append(stack, name), " -> "))
		}
	}
	sections := make(map[string]string, len(document.sections)+len(inherited))
	for section, body := range document.sections {
		sections[section] = body
	}
	for section, body := range inherited {
		sections[section] = body
	}
	if document.extends != "" {
		if _, exists := compiler.documents[document.extends]; !exists {
			return "", fmt.Errorf("legacy view %s has unknown layout %q", document.file, document.extends)
		}
		return compiler.resolve(document.extends, sections, append(stack, name), allowOpen)
	}
	return substituteLegacyYields(document.file, document.body, sections, allowOpen, make(map[string]bool))
}

func substituteLegacyYields(file, source string, sections map[string]string, allowOpen bool, resolving map[string]bool) (string, error) {
	matches := legacyYieldPattern.FindAllStringSubmatchIndex(source, -1)
	if len(matches) == 0 {
		return source, nil
	}
	var output strings.Builder
	last := 0
	for _, match := range matches {
		output.WriteString(source[last:match[0]])
		name := source[match[2]:match[3]]
		body, exists := sections[name]
		if !exists {
			if !allowOpen {
				return "", fmt.Errorf("legacy view %s requires section %q", file, name)
			}
			last = match[1]
			continue
		}
		if resolving[name] {
			return "", fmt.Errorf("legacy section yield cycle through %q", name)
		}
		resolving[name] = true
		expanded, err := substituteLegacyYields(file, body, sections, allowOpen, resolving)
		delete(resolving, name)
		if err != nil {
			return "", err
		}
		output.WriteString(expanded)
		last = match[1]
	}
	output.WriteString(source[last:])
	return output.String(), nil
}

func (compiler legacyViewCompiler) rewriteIncludes(source string) (string, error) {
	matches := legacyIncludePattern.FindAllStringSubmatchIndex(source, -1)
	var output strings.Builder
	last := 0
	for _, match := range matches {
		output.WriteString(source[last:match[0]])
		name := normalizeLegacyReference(source[match[2]:match[3]])
		if _, exists := compiler.documents[name]; !exists {
			return "", fmt.Errorf("unknown legacy include %q", name)
		}
		data := "."
		if match[4] >= 0 {
			data = strings.TrimSpace(source[match[4]:match[5]])
			if data == "" || data[0] != '.' && data[0] != '$' {
				return "", fmt.Errorf("legacy include %q data must start with dot or variable", name)
			}
		}
		output.WriteString("{{template ")
		output.WriteString(strconv.Quote(name))
		output.WriteByte(' ')
		output.WriteString(data)
		output.WriteString("}}")
		last = match[1]
	}
	output.WriteString(source[last:])
	return output.String(), nil
}

func (compiler legacyViewCompiler) validateIncludes() error {
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var visit func(string, []string) error
	visit = func(name string, stack []string) error {
		if visiting[name] {
			return fmt.Errorf("legacy template include cycle: %s", strings.Join(append(stack, name), " -> "))
		}
		if visited[name] {
			return nil
		}
		visiting[name] = true
		document := compiler.documents[name]
		for _, match := range legacyIncludePattern.FindAllStringSubmatch(document.source, -1) {
			dependency := normalizeLegacyReference(match[1])
			if _, exists := compiler.documents[dependency]; !exists {
				return fmt.Errorf("legacy view %s has unknown include %q", document.file, dependency)
			}
			if err := visit(dependency, append(stack, name)); err != nil {
				return err
			}
		}
		visiting[name] = false
		visited[name] = true
		return nil
	}
	names := make([]string, 0, len(compiler.documents))
	for name := range compiler.documents {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := visit(name, nil); err != nil {
			return err
		}
	}
	return nil
}

func (compiler legacyViewCompiler) isExtended(name string) bool {
	for _, document := range compiler.documents {
		if document.extends == name {
			return true
		}
	}
	return false
}

func legacyViewName(file string) string {
	return strings.TrimSuffix(strings.TrimPrefix(path.Clean(strings.ReplaceAll(file, `\`, "/")), "./"), ".forge.html")
}

func normalizeLegacyReference(reference string) string {
	return strings.TrimSuffix(strings.TrimPrefix(path.Clean(strings.ReplaceAll(reference, `\`, "/")), "./"), ".forge.html")
}
