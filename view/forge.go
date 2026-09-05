package view

import (
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"sort"
	"strings"
)

const forgeSuffix = ".forge.html"

// SourceMapping connects generated html/template bytes to application source.
type SourceMapping struct {
	Start       int
	End         int
	SourceStart int
	Path        string
	Line        int
	Column      int
	Synthetic   bool
	Context     []string
}

// CompiledTemplate is canonical html/template source produced from a GoForge
// template. Component files are expanded into these values at compile time.
type CompiledTemplate struct {
	Name     string
	Source   string
	Mappings []SourceMapping
}

// CompileForge resolves GoForge structure into ordinary html/template source.
// With no patterns it recursively compiles every *.forge.html file.
func CompileForge(files fs.FS, patterns ...string) ([]CompiledTemplate, error) {
	if files == nil {
		return nil, fmt.Errorf("view filesystem is required")
	}
	paths, err := forgePaths(files, patterns)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no %s templates found", forgeSuffix)
	}
	documents := make(map[string]*document, len(paths))
	for _, filePath := range paths {
		source, err := fs.ReadFile(files, filePath)
		if err != nil {
			return nil, fmt.Errorf("read view %s: %w", filePath, err)
		}
		name := templateName(filePath)
		if _, exists := documents[name]; exists {
			return nil, fmt.Errorf("duplicate view name %q", name)
		}
		doc, err := parseDocument(filePath, name, string(source))
		if err != nil {
			return nil, err
		}
		documents[name] = doc
	}
	compiler := forgeCompiler{documents: documents}
	if err := compiler.validateDependencies(); err != nil {
		return nil, err
	}
	compiled := make([]CompiledTemplate, 0, len(documents))
	for _, name := range sortedDocumentNames(documents) {
		if documents[name].component {
			continue
		}
		item, err := compiler.compile(name)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, item)
	}
	if len(compiled) == 0 {
		return nil, fmt.Errorf("no renderable %s templates found", forgeSuffix)
	}
	if err := validateTemplateDependencies(compiled); err != nil {
		return nil, err
	}
	return compiled, nil
}

// ParseCompiled parses compiler output using html/template contextual escaping.
func ParseCompiled(compiled []CompiledTemplate, functions template.FuncMap) (*Engine, error) {
	if len(compiled) == 0 {
		return nil, fmt.Errorf("compiled views are required")
	}
	parsed := template.New("views").Option("missingkey=error").Funcs(functions)
	seen := make(map[string]struct{}, len(compiled))
	mappings := make(map[string]compiledMapping, len(compiled))
	includes := make(map[string][]includeEdge, len(compiled))
	for _, item := range compiled {
		if strings.TrimSpace(item.Name) == "" {
			return nil, fmt.Errorf("compiled view name is required")
		}
		if _, exists := seen[item.Name]; exists {
			return nil, fmt.Errorf("duplicate compiled view %q", item.Name)
		}
		seen[item.Name] = struct{}{}
		mappings[item.Name] = compiledMapping{source: item.Source, mappings: item.Mappings}
		includes[item.Name] = templateIncludeEdges(item)
	}
	for _, item := range compiled {
		prefix := `{{define ` + fmt.Sprintf("%q", item.Name) + `}}`
		definition := prefix + item.Source + `{{end}}`
		_, err := parsed.New(item.Name).Parse(definition)
		if err != nil {
			return nil, mapCompiledError("parse", item, len(prefix), includes, err)
		}
		mappings[item.Name] = compiledMapping{source: item.Source, mappings: item.Mappings, prefix: len(prefix)}
	}
	return &Engine{templates: parsed, mappings: mappings, includes: includes}, nil
}

// ParseForge is a development convenience. Production applications should
// parse embedded compiler output with ParseCompiled.
func ParseForge(files fs.FS, functions template.FuncMap, patterns ...string) (*Engine, error) {
	compiled, err := CompileForge(files, patterns...)
	if err != nil {
		return nil, err
	}
	return ParseCompiled(compiled, functions)
}

func forgePaths(files fs.FS, patterns []string) ([]string, error) {
	unique := make(map[string]struct{})
	if len(patterns) == 0 {
		err := fs.WalkDir(files, ".", func(filePath string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && strings.HasSuffix(filePath, forgeSuffix) {
				unique[filePath] = struct{}{}
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk views: %w", err)
		}
	} else {
		for _, pattern := range patterns {
			matches, err := fs.Glob(files, pattern)
			if err != nil {
				return nil, fmt.Errorf("match views %q: %w", pattern, err)
			}
			for _, match := range matches {
				unique[match] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(unique))
	for filePath := range unique {
		result = append(result, filePath)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeReference(reference string) string {
	reference = strings.TrimPrefix(path.Clean(strings.ReplaceAll(reference, `\`, "/")), "./")
	return strings.TrimSuffix(reference, forgeSuffix)
}

func templateName(filePath string) string {
	return strings.TrimSuffix(strings.TrimPrefix(path.Clean(strings.ReplaceAll(filePath, `\`, "/")), "./"), forgeSuffix)
}

func sortedDocumentNames(documents map[string]*document) []string {
	names := make([]string, 0, len(documents))
	for name := range documents {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
