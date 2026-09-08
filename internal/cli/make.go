package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func runMake(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, processes processRunner) (err error) {
	var kind string
	var rest []string
	if args[0] == "make" {
		if len(args) < 2 {
			return errors.New("usage: forge make <controller|request|migration|model|resource|component|job> <name>")
		}
		kind, rest = args[1], args[2:]
	} else {
		kind, rest = strings.TrimPrefix(args[0], "make:"), args[1:]
	}
	name := ""
	var resourceFields []resourceField
	resourceSchemaDriven := false
	if kind == "resource" {
		var err error
		name, resourceFields, resourceSchemaDriven, err = parseMakeResourceArguments(rest)
		if err != nil {
			return err
		}
	} else {
		if len(rest) != 1 {
			return fmt.Errorf("usage: forge make:%s <name>", kind)
		}
		name = rest[0]
	}
	if err := requireProjectRoot(); err != nil {
		return err
	}
	if err := requireProjectFormatRange(1, 8); err != nil {
		return err
	}
	lock, err := acquireGeneratorLock(ctx)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, lock.Close())
	}()

	var path, content string
	switch kind {
	case "controller":
		path, content, err = controllerFile(name)
	case "request":
		path, content, err = requestFile(name)
	case "migration":
		return makeMigration(name, stdout)
	case "model":
		return makeModel(name, stdout)
	case "resource":
		return makeResourceWithProcessFields(ctx, name, resourceFields, resourceSchemaDriven, stdin, stdout, stderr, processes)
	case "component":
		return makeComponent(ctx, name, stdin, stdout, stderr, processes)
	case "job":
		return makeJob(name, stdout)
	default:
		return fmt.Errorf("unknown generator %q", kind)
	}
	if err != nil {
		return err
	}
	if err := writeExclusive(path, content); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "created %s\n", filepath.ToSlash(path))
	return nil
}

func makeComponent(ctx context.Context, name string, stdin io.Reader, stdout, stderr io.Writer, processes processRunner) error {
	if err := requireProjectFormatRange(5, 8); err != nil {
		return err
	}
	componentName, err := snake(name)
	if err != nil {
		return err
	}
	componentName = strings.ReplaceAll(componentName, "_", "-")
	path := filepath.Join("resources", "views", "components", componentName+".forge.html")
	content, err := renderTemplate("templates/component/component.forge.html.tmpl", path, struct{ Name string }{Name: componentName})
	if err != nil {
		return err
	}
	if err := writeExclusive(path, content); err != nil {
		return err
	}
	if err := runProjectViewCompiler(ctx, stdin, stdout, stderr, processes, false); err != nil {
		if removeErr := os.Remove(path); removeErr != nil {
			return errors.Join(err, fmt.Errorf("remove incomplete component %s: %w", filepath.ToSlash(path), removeErr))
		}
		return err
	}
	fmt.Fprintf(stdout, "created %s\n", filepath.ToSlash(path))
	return nil
}

func controllerFile(name string) (string, string, error) {
	typeName, err := pascal(strings.TrimSuffix(name, "Controller"))
	if err != nil {
		return "", "", err
	}
	fileName, _ := snake(typeName)
	path := filepath.Join("internal", "http", "controllers", fileName+"_controller.go")
	content := fmt.Sprintf(`package controllers

import (
	"net/http"

	"github.com/ShanilKoshitha/goforge/httpx"
)

type %[1]sController struct{}

func New%[1]sController() *%[1]sController {
	return &%[1]sController{}
}

func (controller *%[1]sController) Index(ctx *httpx.Context) error {
	return ctx.JSON(http.StatusOK, map[string]any{"data": []any{}})
}
`, typeName)
	return path, content, nil
}

func requestFile(name string) (string, string, error) {
	typeName, err := pascal(strings.TrimSuffix(name, "Request"))
	if err != nil {
		return "", "", err
	}
	fileName, _ := snake(typeName)
	path := filepath.Join("internal", "http", "requests", fileName+"_request.go")
	content := fmt.Sprintf(`package requests

import "github.com/ShanilKoshitha/goforge/validation"

type %[1]sRequest struct {
	Name string %[2]sjson:"name"%[2]s
}

func (request %[1]sRequest) Validate() validation.Errors {
	return validation.Check(
		validation.String("name", request.Name).Required().Min(2),
	)
}
`, typeName, "`")
	return path, content, nil
}

func makeMigration(name string, stdout io.Writer) error {
	fileName, err := snake(name)
	if err != nil {
		return err
	}
	state, err := loadMigrationResourceState()
	if err != nil {
		return err
	}
	prefix, err := nextMigrationVersion(state)
	if err != nil {
		return err
	}
	up := filepath.Join("database", "migrations", prefix+"_"+fileName+".up.sql")
	down := filepath.Join("database", "migrations", prefix+"_"+fileName+".down.sql")
	if err := writeExclusive(up, "-- Write the forward migration here.\n"); err != nil {
		return err
	}
	if err := writeExclusive(down, "-- Write the rollback migration here.\n"); err != nil {
		if cleanupErr := os.Remove(up); cleanupErr != nil {
			return errors.Join(err, fmt.Errorf("remove incomplete migration %s: %w", up, cleanupErr))
		}
		return err
	}
	fmt.Fprintf(stdout, "created %s\ncreated %s\n", filepath.ToSlash(up), filepath.ToSlash(down))
	return nil
}
