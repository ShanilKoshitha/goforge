package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type resourceState struct {
	Resources []resourceSpec `json:"resources"`
}

type resourceSpec struct {
	Name             string `json:"name"`
	Package          string `json:"package"`
	Plural           string `json:"plural"`
	MigrationVersion string `json:"migration_version"`
}

type plannedFile struct {
	path    string
	content string
}

func makeResource(name string, stdout io.Writer) error {
	if err := requireProjectFormatRange(8, 8); err != nil {
		return err
	}
	return makeResourceWithDependencies(context.Background(), name, nil, stdout, stdout, execProcessRunner{}, writeManagedFile)
}

func makeResourceWithWriter(name string, stdout io.Writer, managedWrite func(string, []byte) error) error {
	return makeResourceWithDependencies(context.Background(), name, nil, stdout, stdout, execProcessRunner{}, managedWrite)
}

func makeResourceWithProcess(ctx context.Context, name string, stdin io.Reader, stdout, stderr io.Writer, processes processRunner) error {
	if err := requireProjectFormatRange(8, 8); err != nil {
		return err
	}
	return makeResourceWithDependencies(ctx, name, stdin, stdout, stderr, processes, writeManagedFile)
}

func makeResourceWithDependencies(ctx context.Context, name string, stdin io.Reader, stdout, stderr io.Writer, processes processRunner, managedWrite func(string, []byte) error) error {
	state, err := loadResourceState()
	if err != nil {
		return err
	}
	spec, err := newResourceSpec(name, state)
	if err != nil {
		return err
	}
	module, err := projectModule()
	if err != nil {
		return err
	}
	files, err := resourceFiles(module, spec)
	if err != nil {
		return err
	}
	for index := range files {
		if _, err := os.Stat(files[index].path); err == nil {
			return fmt.Errorf("refusing to overwrite %s", filepath.ToSlash(files[index].path))
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	createdDirectories, err := missingParentDirectories(files)
	if err != nil {
		return err
	}

	next := state
	next.Resources = append(append([]resourceSpec(nil), state.Resources...), spec)
	sort.Slice(next.Resources, func(i, j int) bool { return next.Resources[i].Name < next.Resources[j].Name })
	registry, err := generatedResourceRegistry(module, next)
	if err != nil {
		return err
	}
	compiledViews, err := compileViewsWithPlanned(files)
	if err != nil {
		return err
	}
	modelSources, err := currentModelSourcesWithPlanned(files)
	if err != nil {
		return err
	}
	generatedORM, err := renderORMArtifactForModelSources(modelSources)
	if err != nil {
		return fmt.Errorf("generate resource ORM: %w", err)
	}
	encodedState, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	encodedState = append(encodedState, '\n')

	registryPath := filepath.Join("routes", "resources_gen.go")
	statePath := filepath.Join(".forge", "resources.json")
	oldState, err := os.ReadFile(statePath)
	if err != nil {
		return fmt.Errorf("read resource metadata: %w", err)
	}
	oldRegistry, err := os.ReadFile(registryPath)
	if err != nil {
		return fmt.Errorf("read generated resource registry: %w", err)
	}
	oldViews, err := os.ReadFile(filepath.FromSlash(generatedViewsPath))
	if err != nil {
		return fmt.Errorf("read generated views: %w", err)
	}
	oldORM, err := os.ReadFile(filepath.FromSlash(generatedORMPath))
	ormWasMissing := errors.Is(err, os.ErrNotExist)
	if err != nil && !ormWasMissing {
		return fmt.Errorf("read generated ORM: %w", err)
	}
	var created []string
	var registryChanged bool
	var viewsChanged bool
	var ormChanged bool
	var stateChanged bool
	rollback := func(cause error) error {
		for _, path := range created {
			if err := os.Remove(path); err != nil {
				cause = errors.Join(cause, fmt.Errorf("remove incomplete resource %s: %w", path, err))
			}
		}
		if registryChanged {
			cause = errors.Join(cause, managedWrite(registryPath, oldRegistry))
		}
		if viewsChanged {
			cause = errors.Join(cause, managedWrite(filepath.FromSlash(generatedViewsPath), oldViews))
		}
		if ormChanged {
			if ormWasMissing {
				if err := os.Remove(filepath.FromSlash(generatedORMPath)); err != nil && !errors.Is(err, os.ErrNotExist) {
					cause = errors.Join(cause, fmt.Errorf("remove incomplete generated ORM: %w", err))
				}
			} else {
				cause = errors.Join(cause, managedWrite(filepath.FromSlash(generatedORMPath), oldORM))
			}
		}
		if stateChanged {
			cause = errors.Join(cause, managedWrite(statePath, oldState))
		}
		for _, directory := range createdDirectories {
			entries, err := os.ReadDir(directory)
			if errors.Is(err, os.ErrNotExist) || err == nil && len(entries) > 0 {
				continue
			}
			if err != nil {
				cause = errors.Join(cause, fmt.Errorf("inspect generated directory %s: %w", directory, err))
				continue
			}
			if err := os.Remove(directory); err != nil && !errors.Is(err, os.ErrNotExist) {
				cause = errors.Join(cause, fmt.Errorf("remove generated directory %s: %w", directory, err))
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
	if err := managedWrite(registryPath, []byte(registry)); err != nil {
		return rollback(fmt.Errorf("write generated resource registry: %w", err))
	}
	registryChanged = true
	if err := managedWrite(filepath.FromSlash(generatedViewsPath), []byte(compiledViews)); err != nil {
		return rollback(fmt.Errorf("write generated views: %w", err))
	}
	viewsChanged = true
	if err := managedWrite(filepath.FromSlash(generatedORMPath), []byte(generatedORM)); err != nil {
		return rollback(fmt.Errorf("write generated ORM: %w", err))
	}
	ormChanged = true
	if err := managedWrite(statePath, encodedState); err != nil {
		return rollback(fmt.Errorf("write resource metadata: %w", err))
	}
	stateChanged = true
	if err := runProjectViewCompiler(ctx, stdin, stdout, stderr, processes, true); err != nil {
		return rollback(fmt.Errorf("validate generated views: %w", err))
	}

	for _, file := range files {
		fmt.Fprintf(stdout, "created %s\n", filepath.ToSlash(file.path))
	}
	fmt.Fprintln(stdout, "updated routes/resources_gen.go")
	fmt.Fprintln(stdout, "updated resources/views/views_gen.go")
	fmt.Fprintln(stdout, "updated internal/models/zz_orm_gen.go")
	return nil
}

func missingParentDirectories(files []plannedFile) ([]string, error) {
	missing := make(map[string]struct{})
	for _, file := range files {
		for directory := filepath.Dir(file.path); directory != "." && directory != string(filepath.Separator); directory = filepath.Dir(directory) {
			info, err := os.Stat(directory)
			if err == nil {
				if !info.IsDir() {
					return nil, fmt.Errorf("generated parent %s is not a directory", filepath.ToSlash(directory))
				}
				break
			}
			if !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("inspect generated parent %s: %w", filepath.ToSlash(directory), err)
			}
			missing[directory] = struct{}{}
		}
	}
	directories := make([]string, 0, len(missing))
	for directory := range missing {
		directories = append(directories, directory)
	}
	sort.Slice(directories, func(i, j int) bool {
		return strings.Count(filepath.Clean(directories[i]), string(filepath.Separator)) > strings.Count(filepath.Clean(directories[j]), string(filepath.Separator))
	})
	return directories, nil
}
