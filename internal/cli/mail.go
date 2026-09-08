package cli

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func makeMail(ctx context.Context, name string, stdin io.Reader, stdout, stderr io.Writer, processes processRunner) error {
	if err := requireProjectFormatRange(9, 9); err != nil {
		return err
	}
	typeName, err := pascal(strings.TrimSuffix(name, "Mail"))
	if err != nil {
		return err
	}
	if !token.IsIdentifier(typeName) || !ast.IsExported(typeName) {
		return fmt.Errorf("mail name %q does not produce an exported Go identifier", name)
	}
	fileName, err := snake(typeName)
	if err != nil {
		return err
	}
	if err := validateMailDeclarations(typeName); err != nil {
		return err
	}
	module, err := projectModule()
	if err != nil {
		return err
	}
	title := strings.Join(splitIdentifierWords(typeName), " ")
	data := struct {
		Module, Name, File, Title string
	}{Module: module, Name: typeName, File: fileName, Title: title}
	plans := []struct {
		template, path string
	}{
		{"templates/mail/mail.go.tmpl", filepath.Join("internal", "mail", fileName+".go")},
		{"templates/mail/mail_test.go.tmpl", filepath.Join("internal", "mail", fileName+"_test.go")},
		{"templates/mail/mail.txt.tmpl", filepath.Join("internal", "mail", "templates", fileName+".txt.tmpl")},
		{"templates/mail/mail.forge.html.tmpl", filepath.Join("resources", "views", "mail", fileName+".forge.html")},
	}
	files := make([]plannedFile, 0, len(plans))
	for _, plan := range plans {
		content, err := renderTemplate(plan.template, plan.path, data)
		if err != nil {
			return err
		}
		if _, err := os.Stat(plan.path); err == nil {
			return fmt.Errorf("refusing to overwrite %s", filepath.ToSlash(plan.path))
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect %s: %w", filepath.ToSlash(plan.path), err)
		}
		files = append(files, plannedFile{path: plan.path, content: content})
	}
	createdDirectories, err := missingParentDirectories(files)
	if err != nil {
		return err
	}
	created := make([]string, 0, len(files))
	rollback := func(cause error) error {
		for index := len(created) - 1; index >= 0; index-- {
			if err := os.Remove(created[index]); err != nil && !errors.Is(err, os.ErrNotExist) {
				cause = errors.Join(cause, fmt.Errorf("remove incomplete mail %s: %w", filepath.ToSlash(created[index]), err))
			}
		}
		for _, directory := range uniqueDeepestDirectories(createdDirectories) {
			entries, err := os.ReadDir(directory)
			if errors.Is(err, os.ErrNotExist) || err == nil && len(entries) != 0 {
				continue
			}
			if err != nil {
				cause = errors.Join(cause, fmt.Errorf("inspect generated directory %s: %w", filepath.ToSlash(directory), err))
				continue
			}
			if err := os.Remove(directory); err != nil && !errors.Is(err, os.ErrNotExist) {
				cause = errors.Join(cause, fmt.Errorf("remove generated directory %s: %w", filepath.ToSlash(directory), err))
			}
		}
		return cause
	}
	for _, file := range files {
		if err := writeExclusive(file.path, file.content); err != nil {
			return rollback(err)
		}
		created = append(created, file.path)
	}
	if err := runProjectViewCompiler(ctx, stdin, stdout, stderr, processes, false); err != nil {
		return rollback(err)
	}
	for _, file := range files {
		fmt.Fprintf(stdout, "created %s\n", filepath.ToSlash(file.path))
	}
	return nil
}

func validateMailDeclarations(typeName string) error {
	generated := []string{typeName, typeName + "Data", "New" + typeName, "Test" + typeName + "BuildsTextAndHTML"}
	wanted := make(map[string]string, len(generated))
	for _, declaration := range generated {
		wanted[strings.ToLower(declaration)] = declaration
	}
	directory := filepath.Join("internal", "mail")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect mail package: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("inspect mail declarations in %s: %w", filepath.ToSlash(path), err)
		}
		for _, declaration := range topLevelDeclarations(file) {
			if generatedName, exists := wanted[strings.ToLower(declaration)]; exists {
				return fmt.Errorf("mail %s declaration %s conflicts with %s in %s", typeName, generatedName, declaration, filepath.ToSlash(path))
			}
		}
	}
	return nil
}

func topLevelDeclarations(file *ast.File) []string {
	var declarations []string
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			declarations = append(declarations, typed.Name.Name)
		case *ast.GenDecl:
			for _, specification := range typed.Specs {
				switch spec := specification.(type) {
				case *ast.TypeSpec:
					declarations = append(declarations, spec.Name.Name)
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						declarations = append(declarations, name.Name)
					}
				}
			}
		}
	}
	return declarations
}

func splitIdentifierWords(value string) []string {
	var words []string
	start := 0
	for index := 1; index < len(value); index++ {
		if value[index] >= 'A' && value[index] <= 'Z' && (value[index-1] < 'A' || value[index-1] > 'Z') {
			words = append(words, value[start:index])
			start = index
		}
	}
	words = append(words, value[start:])
	for index := range words {
		words[index] = strings.ToLower(words[index])
	}
	if len(words) > 0 {
		words[0] = strings.ToUpper(words[0][:1]) + words[0][1:]
	}
	return words
}
