package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	generatedJobRegistryPath = "internal/jobs/registry_gen.go"
	jobStatePath             = ".forge/jobs.json"
)

type jobState struct {
	Jobs []jobSpec `json:"jobs"`
}

type jobSpec struct {
	Name       string `json:"name"`
	File       string `json:"file"`
	Definition string `json:"definition"`
}

type jobExclusiveWriter func(string, string) error

func makeJob(name string, stdout io.Writer) error {
	if err := requireProjectFormatRange(6, 9); err != nil {
		return err
	}
	return makeJobWithWriters(name, stdout, writeExclusive, writeManagedFile)
}

func makeJobWithWriters(name string, stdout io.Writer, exclusive jobExclusiveWriter, managed func(string, []byte) error) error {
	state, err := loadJobState()
	if err != nil {
		return err
	}
	spec, err := newJobSpec(name, state)
	if err != nil {
		return err
	}
	module, err := projectModule()
	if err != nil {
		return err
	}
	format, err := projectFormat()
	if err != nil {
		return err
	}
	files, err := jobFiles(spec)
	if err != nil {
		return err
	}
	for _, file := range files {
		if _, err := os.Stat(file.path); err == nil {
			return fmt.Errorf("refusing to overwrite %s", filepath.ToSlash(file.path))
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect %s: %w", filepath.ToSlash(file.path), err)
		}
	}
	next := jobState{Jobs: append(append([]jobSpec(nil), state.Jobs...), spec)}
	sort.Slice(next.Jobs, func(i, j int) bool { return next.Jobs[i].Name < next.Jobs[j].Name })
	registry, err := generatedJobRegistryForFormat(module, next, format)
	if err != nil {
		return err
	}
	encodedState, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("encode job metadata: %w", err)
	}
	encodedState = append(encodedState, '\n')

	oldRegistry, registryMissing, err := readOptionalGeneratedFile(generatedJobRegistryPath)
	if err != nil {
		return fmt.Errorf("read generated job registry: %w", err)
	}
	oldState, stateMissing, err := readOptionalGeneratedFile(jobStatePath)
	if err != nil {
		return fmt.Errorf("read job metadata: %w", err)
	}
	createdDirectories, err := missingParentDirectories(files)
	if err != nil {
		return err
	}
	for _, path := range []string{generatedJobRegistryPath, jobStatePath} {
		directories, err := missingParentDirectories([]plannedFile{{path: filepath.FromSlash(path)}})
		if err != nil {
			return err
		}
		createdDirectories = append(createdDirectories, directories...)
	}

	var created []string
	registryChanged := false
	stateChanged := false
	rollback := func(cause error) error {
		for index := len(created) - 1; index >= 0; index-- {
			if err := os.Remove(created[index]); err != nil && !errors.Is(err, os.ErrNotExist) {
				cause = errors.Join(cause, fmt.Errorf("remove incomplete job %s: %w", filepath.ToSlash(created[index]), err))
			}
		}
		if registryChanged {
			cause = errors.Join(cause, restoreOptionalManagedFile(generatedJobRegistryPath, oldRegistry, registryMissing, managed))
		}
		if stateChanged {
			cause = errors.Join(cause, restoreOptionalManagedFile(jobStatePath, oldState, stateMissing, managed))
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
		if err := exclusive(file.path, file.content); err != nil {
			return rollback(err)
		}
		created = append(created, file.path)
	}
	for _, path := range []string{generatedJobRegistryPath, jobStatePath} {
		if err := os.MkdirAll(filepath.Dir(filepath.FromSlash(path)), 0o755); err != nil {
			return rollback(fmt.Errorf("create generated job parent: %w", err))
		}
	}
	registryChanged = true
	if err := managed(filepath.FromSlash(generatedJobRegistryPath), []byte(registry)); err != nil {
		return rollback(fmt.Errorf("write generated job registry: %w", err))
	}
	stateChanged = true
	if err := managed(filepath.FromSlash(jobStatePath), encodedState); err != nil {
		return rollback(fmt.Errorf("write job metadata: %w", err))
	}

	for _, file := range files {
		fmt.Fprintf(stdout, "created %s\n", filepath.ToSlash(file.path))
	}
	fmt.Fprintln(stdout, "updated internal/jobs/registry_gen.go")
	return nil
}

func loadJobState() (jobState, error) {
	contents, err := os.ReadFile(filepath.FromSlash(jobStatePath))
	if errors.Is(err, os.ErrNotExist) {
		return jobState{}, nil
	}
	if err != nil {
		return jobState{}, fmt.Errorf("read job metadata: %w", err)
	}
	var state jobState
	if err := json.Unmarshal(contents, &state); err != nil {
		return jobState{}, fmt.Errorf("decode job metadata: %w", err)
	}
	seenNames := make(map[string]struct{}, len(state.Jobs))
	seenFiles := make(map[string]struct{}, len(state.Jobs))
	seenDefinitions := make(map[string]struct{}, len(state.Jobs))
	for _, job := range state.Jobs {
		if job.Name == "" || job.File == "" || job.Definition == "" {
			return jobState{}, errors.New("job metadata contains an incomplete entry")
		}
		name, file, definition := strings.ToLower(job.Name), strings.ToLower(job.File), strings.ToLower(job.Definition)
		if _, exists := seenNames[name]; exists {
			return jobState{}, fmt.Errorf("job metadata contains duplicate name %s", job.Name)
		}
		if _, exists := seenFiles[file]; exists {
			return jobState{}, fmt.Errorf("job metadata contains duplicate file %s", job.File)
		}
		if _, exists := seenDefinitions[definition]; exists {
			return jobState{}, fmt.Errorf("job metadata contains duplicate definition %s", job.Definition)
		}
		seenNames[name], seenFiles[file], seenDefinitions[definition] = struct{}{}, struct{}{}, struct{}{}
	}
	if err := validateJobDeclarations(state.Jobs); err != nil {
		return jobState{}, err
	}
	return state, nil
}

func newJobSpec(name string, state jobState) (jobSpec, error) {
	typeName, err := pascal(strings.TrimSuffix(name, "Job"))
	if err != nil {
		return jobSpec{}, err
	}
	if !token.IsIdentifier(typeName) || !ast.IsExported(typeName) {
		return jobSpec{}, fmt.Errorf("job name %q does not produce an exported Go identifier", name)
	}
	file, err := snake(typeName)
	if err != nil {
		return jobSpec{}, err
	}
	definition := file + ".v1"
	for _, existing := range state.Jobs {
		if strings.EqualFold(existing.Name, typeName) || strings.EqualFold(existing.File, file) || strings.EqualFold(existing.Definition, definition) {
			return jobSpec{}, fmt.Errorf("job %s already exists", typeName)
		}
	}
	spec := jobSpec{Name: typeName, File: file, Definition: definition}
	if err := validateJobDeclarations(append(append([]jobSpec(nil), state.Jobs...), spec)); err != nil {
		return jobSpec{}, err
	}
	return spec, nil
}

func validateJobDeclarations(jobs []jobSpec) error {
	owners := map[string]string{
		strings.ToLower("Dependencies"):  "scaffold declaration Dependencies",
		strings.ToLower("Registry"):      "reserved declaration Registry",
		strings.ToLower("NewRegistry"):   "scaffold declaration NewRegistry",
		strings.ToLower("NewDispatcher"): "scaffold declaration NewDispatcher",
	}
	for _, spec := range jobs {
		for _, declaration := range jobDeclarations(spec) {
			key := strings.ToLower(declaration)
			if owner, exists := owners[key]; exists {
				return fmt.Errorf("job %s declaration %s conflicts with %s", spec.Name, declaration, owner)
			}
			owners[key] = fmt.Sprintf("job %s", spec.Name)
		}
	}
	return nil
}

func jobDeclarations(spec jobSpec) []string {
	return []string{
		spec.Name,
		spec.Name + "Definition",
		spec.Name + "Handler",
		"Dispatch" + spec.Name,
		"Test" + spec.Name + "Handler",
	}
}

func jobFiles(spec jobSpec) ([]plannedFile, error) {
	data := struct {
		Name       string
		Definition string
	}{Name: spec.Name, Definition: spec.Definition}
	base := filepath.Join("internal", "jobs", spec.File)
	source, err := renderTemplate("templates/job/job.go.tmpl", base+".go", data)
	if err != nil {
		return nil, err
	}
	test, err := renderTemplate("templates/job/job_test.go.tmpl", base+"_test.go", data)
	if err != nil {
		return nil, err
	}
	return []plannedFile{{path: base + ".go", content: source}, {path: base + "_test.go", content: test}}, nil
}

func generatedJobRegistry(module string, state jobState) (string, error) {
	return generatedJobRegistryForFormat(module, state, 6)
}

func generatedJobRegistryForFormat(module string, state jobState, format int) (string, error) {
	return renderTemplate("templates/job/registry_gen.go.tmpl", generatedJobRegistryPath, struct {
		Module   string
		Jobs     []jobSpec
		Builtins bool
	}{Module: module, Jobs: state.Jobs, Builtins: format >= 9})
}

func readOptionalGeneratedFile(path string) ([]byte, bool, error) {
	contents, err := os.ReadFile(filepath.FromSlash(path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, true, nil
	}
	return contents, false, err
}

func restoreOptionalManagedFile(path string, contents []byte, missing bool, managed func(string, []byte) error) error {
	path = filepath.FromSlash(path)
	if missing {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove incomplete %s: %w", filepath.ToSlash(path), err)
		}
		return nil
	}
	if err := managed(path, contents); err != nil {
		return fmt.Errorf("restore %s: %w", filepath.ToSlash(path), err)
	}
	return nil
}

func uniqueDeepestDirectories(directories []string) []string {
	unique := make(map[string]struct{}, len(directories))
	for _, directory := range directories {
		unique[filepath.Clean(directory)] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for directory := range unique {
		result = append(result, directory)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.Count(result[i], string(filepath.Separator)) > strings.Count(result[j], string(filepath.Separator))
	})
	return result
}
