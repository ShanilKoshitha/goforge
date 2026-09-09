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

type resourceManagedPublisher func(string, developmentFileState, []byte) error

func makeResource(name string, stdout io.Writer) error {
	if err := requireProjectFormatRange(8, 10); err != nil {
		return err
	}
	return makeResourceWithDependencies(context.Background(), name, defaultResourceFields(), false, nil, stdout, stdout, execProcessRunner{}, publishResourceManagedFile)
}

func makeResourceWithWriter(name string, stdout io.Writer, managedWrite func(string, []byte) error) error {
	publish := func(path string, _ developmentFileState, contents []byte) error {
		return managedWrite(path, contents)
	}
	return makeResourceWithDependencies(context.Background(), name, defaultResourceFields(), false, nil, stdout, stdout, execProcessRunner{}, publish)
}

func makeResourceWithProcess(ctx context.Context, name string, stdin io.Reader, stdout, stderr io.Writer, processes processRunner) error {
	return makeResourceWithProcessFields(ctx, name, defaultResourceFields(), false, stdin, stdout, stderr, processes)
}

func makeResourceWithProcessFields(ctx context.Context, name string, fields []resourceField, schemaDriven bool, stdin io.Reader, stdout, stderr io.Writer, processes processRunner) error {
	return makeResourceWithProcessRelationships(ctx, name, fields, nil, schemaDriven, stdin, stdout, stderr, processes)
}

func makeResourceWithProcessRelationships(ctx context.Context, name string, fields []resourceField, relationships []resourceBelongsTo, schemaDriven bool, stdin io.Reader, stdout, stderr io.Writer, processes processRunner) error {
	if err := requireProjectFormatRange(8, 10); err != nil {
		return err
	}
	if len(relationships) > 0 {
		if err := requireProjectFormat(10); err != nil {
			return fmt.Errorf("belongs-to resource generation requires project format 10: %w", err)
		}
	}
	return makeResourceWithRelationshipDependencies(ctx, name, fields, relationships, schemaDriven, stdin, stdout, stderr, processes, publishResourceManagedFile)
}

func makeResourceWithDependencies(ctx context.Context, name string, fields []resourceField, schemaDriven bool, stdin io.Reader, stdout, stderr io.Writer, processes processRunner, publishManaged resourceManagedPublisher) error {
	return makeResourceWithRelationshipDependencies(ctx, name, fields, nil, schemaDriven, stdin, stdout, stderr, processes, publishManaged)
}

func makeResourceWithRelationshipDependencies(ctx context.Context, name string, fields []resourceField, requestedRelationships []resourceBelongsTo, schemaDriven bool, stdin io.Reader, stdout, stderr io.Writer, processes processRunner, publishManaged resourceManagedPublisher) error {
	state, err := loadResourceState()
	if err != nil {
		return err
	}
	spec, err := newResourceSpec(name, state)
	if err != nil {
		return err
	}
	relationships, err := resolveResourceRelationships(spec, state, requestedRelationships)
	if err != nil {
		return err
	}
	module, err := projectModule()
	if err != nil {
		return err
	}
	definition := resourceDefinition{
		resourceSpec:  spec,
		Fields:        append([]resourceField(nil), fields...),
		Relationships: append([]resourceRelationship(nil), relationships...),
		SchemaDriven:  schemaDriven,
	}
	files, err := resourceFiles(module, definition)
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
	oldState, err := captureDevelopmentFile(statePath)
	if err != nil {
		return fmt.Errorf("read resource metadata: %w", err)
	}
	oldRegistry, err := captureDevelopmentFile(registryPath)
	if err != nil {
		return fmt.Errorf("read generated resource registry: %w", err)
	}
	oldViews, err := captureDevelopmentFile(filepath.FromSlash(generatedViewsPath))
	if err != nil {
		return fmt.Errorf("read generated views: %w", err)
	}
	oldORM, err := captureDevelopmentFile(filepath.FromSlash(generatedORMPath))
	if err != nil {
		return fmt.Errorf("read generated ORM: %w", err)
	}
	type publication struct {
		path  string
		state developmentFileState
	}
	var created []publication
	var publishedRegistry developmentFileState
	var publishedViews developmentFileState
	var publishedORM developmentFileState
	var publishedState developmentFileState
	var registryChanged bool
	var viewsChanged bool
	var ormChanged bool
	var stateChanged bool
	rollback := func(cause error) error {
		for index := len(created) - 1; index >= 0; index-- {
			file := created[index]
			cause = errors.Join(cause, rollbackResourcePublication(file.path, file.state, developmentFileState{}))
		}
		if registryChanged {
			cause = errors.Join(cause, rollbackResourcePublication(registryPath, publishedRegistry, oldRegistry))
		}
		if viewsChanged {
			cause = errors.Join(cause, rollbackResourcePublication(filepath.FromSlash(generatedViewsPath), publishedViews, oldViews))
		}
		if ormChanged {
			cause = errors.Join(cause, rollbackResourcePublication(filepath.FromSlash(generatedORMPath), publishedORM, oldORM))
		}
		if stateChanged {
			cause = errors.Join(cause, rollbackResourcePublication(statePath, publishedState, oldState))
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
		if err := ctx.Err(); err != nil {
			return rollback(err)
		}
		if err := writeExclusive(file.path, file.content); err != nil {
			return rollback(err)
		}
		published, err := captureDevelopmentFile(file.path)
		if err != nil {
			return rollback(fmt.Errorf("capture generated resource %s: %w", filepath.ToSlash(file.path), err))
		}
		created = append(created, publication{path: file.path, state: published})
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	publishedRegistry = resourceManagedPublicationState(registryPath, oldRegistry, []byte(registry))
	registryChanged = true
	if err := publishManaged(registryPath, oldRegistry, publishedRegistry.data); err != nil {
		return rollback(fmt.Errorf("write generated resource registry: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	viewsPath := filepath.FromSlash(generatedViewsPath)
	publishedViews = resourceManagedPublicationState(viewsPath, oldViews, []byte(compiledViews))
	viewsChanged = true
	if err := publishManaged(viewsPath, oldViews, publishedViews.data); err != nil {
		return rollback(fmt.Errorf("write generated views: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	ormPath := filepath.FromSlash(generatedORMPath)
	publishedORM = resourceManagedPublicationState(ormPath, oldORM, []byte(generatedORM))
	ormChanged = true
	if err := publishManaged(ormPath, oldORM, publishedORM.data); err != nil {
		return rollback(fmt.Errorf("write generated ORM: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	publishedState = resourceManagedPublicationState(statePath, oldState, encodedState)
	stateChanged = true
	if err := publishManaged(statePath, oldState, publishedState.data); err != nil {
		return rollback(fmt.Errorf("write resource metadata: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	if err := runProjectViewCompiler(ctx, stdin, stdout, stderr, processes, true); err != nil {
		return rollback(fmt.Errorf("validate generated views: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return rollback(err)
	}
	currentModels, err := currentModelSourcesWithPlanned(nil)
	if err != nil {
		return rollback(fmt.Errorf("recheck resource models: %w", err))
	}
	currentORM, err := renderORMArtifactForModelSources(currentModels)
	if err != nil {
		return rollback(fmt.Errorf("recheck resource ORM: %w", err))
	}
	if currentORM != generatedORM {
		return rollback(errors.New("application models changed during resource generation; retry the command"))
	}
	if err := recheckResourceRelationshipDependencies(relationships); err != nil {
		return rollback(err)
	}

	for _, file := range files {
		fmt.Fprintf(stdout, "created %s\n", filepath.ToSlash(file.path))
	}
	fmt.Fprintln(stdout, "updated routes/resources_gen.go")
	fmt.Fprintln(stdout, "updated resources/views/views_gen.go")
	fmt.Fprintln(stdout, "updated internal/models/zz_orm_gen.go")
	return nil
}

func resourceManagedPublicationState(path string, previous developmentFileState, contents []byte) developmentFileState {
	mode := generatedFileMode(path)
	if previous.exists {
		mode = previous.mode
	}
	return developmentFileState{exists: true, mode: mode, data: append([]byte(nil), contents...)}
}

func publishResourceManagedFile(path string, expected developmentFileState, contents []byte) error {
	current, err := captureDevelopmentFile(path)
	if err != nil {
		return fmt.Errorf("inspect %s before publication: %w", filepath.ToSlash(path), err)
	}
	if !sameResourceFileState(current, expected) {
		return fmt.Errorf("refusing to overwrite changed %s; retry the command", filepath.ToSlash(path))
	}
	if err := writeManagedFile(path, contents); err != nil {
		return err
	}
	published := resourceManagedPublicationState(path, expected, contents)
	current, err = captureDevelopmentFile(path)
	if err != nil {
		return fmt.Errorf("inspect %s after publication: %w", filepath.ToSlash(path), err)
	}
	if !sameResourceFileState(current, published) {
		return fmt.Errorf("%s changed during publication; newer bytes were preserved", filepath.ToSlash(path))
	}
	return nil
}

func rollbackResourcePublication(path string, published, previous developmentFileState) error {
	current, err := captureDevelopmentFile(path)
	if err != nil {
		return fmt.Errorf("inspect generated resource publication %s: %w", filepath.ToSlash(path), err)
	}
	if sameResourceFileState(current, previous) {
		return nil
	}
	if !sameResourceFileState(current, published) {
		return fmt.Errorf("refusing to roll back changed %s; newer bytes were preserved", filepath.ToSlash(path))
	}
	if err := previous.restore(path); err != nil {
		return fmt.Errorf("restore generated resource publication %s: %w", filepath.ToSlash(path), err)
	}
	return nil
}

func sameResourceFileState(left, right developmentFileState) bool {
	return left.equal(right) && (!left.exists || left.mode == right.mode)
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
