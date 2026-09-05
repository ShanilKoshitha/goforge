package cli

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

type scaffoldData struct {
	Module        string
	ProjectName   string
	SessionSecret string
}

func scaffoldFiles(module, replace, projectName string) (map[string]string, error) {
	secret := make([]byte, 48)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate session secret: %w", err)
	}
	data := scaffoldData{
		Module: module, ProjectName: strconv.Quote(projectName),
		SessionSecret: base64.RawURLEncoding.EncodeToString(secret),
	}
	files := make(map[string]string)
	err := fs.WalkDir(templateFiles, "templates/scaffold", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.TrimSuffix(strings.TrimPrefix(path, "templates/scaffold/"), ".tmpl")
		content, err := renderTemplate(path, name, data)
		if err != nil {
			return err
		}
		files[name] = content
		return nil
	})
	if err != nil {
		return nil, err
	}
	compiledViews, err := compileViewArtifact(files)
	if err != nil {
		return nil, err
	}
	files[generatedViewsPath] = compiledViews
	modelSources := make(map[string]string)
	for name, content := range files {
		clean := filepath.Clean(name)
		if filepath.Dir(clean) == filepath.Join("internal", "models") && filepath.Ext(clean) == ".go" {
			modelSources[filepath.Base(clean)] = content
		}
	}
	generatedORM, err := renderORMArtifactForModelSources(modelSources)
	if err != nil {
		return nil, fmt.Errorf("generate scaffold ORM: %w", err)
	}
	files[generatedORMPath] = generatedORM
	if replace != "" {
		absolute, err := filepath.Abs(replace)
		if err != nil {
			return nil, fmt.Errorf("resolve local framework path: %w", err)
		}
		files["go.mod"] += fmt.Sprintf("\nreplace github.com/ShanilKoshitha/goforge => %s\n", strconv.Quote(filepath.ToSlash(absolute)))
	}
	return files, nil
}
