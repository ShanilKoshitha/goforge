package cli

import (
	"io/fs"
	"strings"
)

// diagnosticScaffoldFiles also supports explicit candidate fixtures while the
// public scaffold remains on its previously released runtime and format.
func diagnosticScaffoldFiles(module string) (map[string]string, error) {
	files := make(map[string]string)
	err := fs.WalkDir(templateFiles, "templates/diagnostics", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.TrimSuffix(strings.TrimPrefix(path, "templates/diagnostics/"), ".tmpl")
		content, err := renderTemplate(path, name, scaffoldData{Module: module})
		if err != nil {
			return err
		}
		files[name] = content
		return nil
	})
	return files, err
}
