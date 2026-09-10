package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const Version = "0.16.0"

var errUsage = errors.New("invalid command; run forge help")

func Run(args []string, stdout, stderr io.Writer) error {
	return RunContext(context.Background(), args, os.Stdin, stdout, stderr)
}

// RunContext executes the CLI with explicit process streams. Project commands
// use the context to stop their child Go process when the caller is cancelled.
func RunContext(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	return run(ctx, args, stdin, stdout, stderr, execProcessRunner{})
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, processes processRunner) error {
	if len(args) == 0 {
		printHelp(stdout)
		return nil
	}

	switch args[0] {
	case "help", "-h", "--help":
		printHelp(stdout)
		return nil
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "forge %s\n", Version)
		return nil
	case "new":
		return runNew(args[1:], stdout)
	case "make", "make:controller", "make:request", "make:migration", "make:model", "make:resource", "make:component", "make:job", "make:mail":
		return runMake(ctx, args, stdin, stdout, stderr, processes)
	case "serve":
		if len(args) != 1 {
			return errors.New("usage: forge serve")
		}
		return runProjectServe(ctx, stdin, stdout, stderr, processes)
	case "dev":
		if len(args) != 1 {
			return errors.New("usage: forge dev")
		}
		return runProjectDev(ctx, stdin, stdout, stderr, processes)
	case "test":
		if len(args) != 1 {
			return errors.New("usage: forge test")
		}
		return runProjectTests(ctx, stdin, stdout, stderr, processes)
	case "build":
		if len(args) != 1 {
			return errors.New("usage: forge build")
		}
		return runProjectBuild(ctx, stdin, stdout, stderr, processes)
	case "views:compile":
		if len(args) > 2 || len(args) == 2 && args[1] != "--check" {
			return errors.New("usage: forge views:compile [--check]")
		}
		return runLockedProjectViewCompiler(ctx, stdin, stdout, stderr, processes, len(args) == 2)
	case "orm:generate":
		return runORMGenerate(ctx, args[1:], stdout)
	case "migrate":
		if len(args) != 1 {
			return errors.New("usage: forge migrate")
		}
		return runProjectCommand(ctx, stdin, stdout, stderr, processes, "go", "run", "./cmd/console", "migrate")
	case "queue:work":
		if err := requireQueueProject(args, "usage: forge queue:work"); err != nil {
			return err
		}
		return runProjectCommand(ctx, stdin, stdout, stderr, processes, "go", "run", "./cmd/worker")
	case "queue:failed":
		if err := requireQueueProject(args, "usage: forge queue:failed"); err != nil {
			return err
		}
		return runProjectCommand(ctx, stdin, stdout, stderr, processes, "go", "run", "./cmd/console", "queue:failed")
	case "queue:retry":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" {
			return errors.New("usage: forge queue:retry <id|--all>")
		}
		if err := requireProjectRoot(); err != nil {
			return err
		}
		if err := requireProjectFormatRange(6, currentProjectFormat); err != nil {
			return err
		}
		return runProjectCommand(ctx, stdin, stdout, stderr, processes, "go", "run", "./cmd/console", args[0], args[1])
	case "queue:forget":
		if len(args) != 2 || strings.TrimSpace(args[1]) == "" || args[1] == "--all" {
			return errors.New("usage: forge queue:forget <id>")
		}
		if err := requireProjectRoot(); err != nil {
			return err
		}
		if err := requireProjectFormatRange(6, currentProjectFormat); err != nil {
			return err
		}
		return runProjectCommand(ctx, stdin, stdout, stderr, processes, "go", "run", "./cmd/console", "queue:forget", args[1])
	case "schedule:work":
		if err := requireScheduleProject(args, "usage: forge schedule:work"); err != nil {
			return err
		}
		return runProjectCommand(ctx, stdin, stdout, stderr, processes, "go", "run", "./cmd/scheduler")
	case "schedule:run":
		if err := requireScheduleProject(args, "usage: forge schedule:run"); err != nil {
			return err
		}
		return runProjectCommand(ctx, stdin, stdout, stderr, processes, "go", "run", "./cmd/scheduler", "--once")
	case "schedule:list":
		if err := requireScheduleProject(args, "usage: forge schedule:list"); err != nil {
			return err
		}
		return runProjectCommand(ctx, stdin, stdout, stderr, processes, "go", "run", "./cmd/console", "schedule:list")
	default:
		if strings.HasPrefix(args[0], "make:") {
			return fmt.Errorf("unknown generator %q", args[0])
		}
		return fmt.Errorf("%w: %q", errUsage, args[0])
	}
}

func requireQueueProject(args []string, usage string) error {
	if len(args) != 1 {
		return errors.New(usage)
	}
	if err := requireProjectRoot(); err != nil {
		return err
	}
	return requireProjectFormatRange(6, currentProjectFormat)
}

func requireScheduleProject(args []string, usage string) error {
	if len(args) != 1 {
		return errors.New(usage)
	}
	if err := requireProjectRoot(); err != nil {
		return err
	}
	return requireProjectFormatRange(11, currentProjectFormat)
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `GoForge — conventional Go, exceptional defaults

Usage:
  forge new <directory> [--module <path>] [--replace <goforge-path>]
  forge serve
  forge dev
  forge test
  forge build
  forge migrate
  forge queue:work
  forge queue:failed
  forge queue:retry <id|--all>
  forge queue:forget <id>
  forge schedule:work
  forge schedule:run
  forge schedule:list
  forge views:compile [--check]
  forge orm:generate [--check]
  forge make controller <name>
  forge make request <name>
  forge make migration <name>
  forge make model <name>
  forge make resource <name> [--field <name>:<type>[:required|nullable]]... [--belongs-to <name>:<ExistingResource>]...
  forge make component <name>
  forge make job <name>
  forge make:controller <name>
  forge make:request <name>
  forge make:migration <name>
  forge make:model <name>
  forge make:resource <name> [--field <name>:<type>[:required|nullable]]... [--belongs-to <name>:<ExistingResource>]...
  forge make:component <name>
  forge make:job <name>
  forge make:mail <name>
  forge version

Resource field types: string, text, integer, boolean. Fields are required by
default; append :nullable to allow null. Without --field, resources retain the
legacy name:string and versionless-update contract. Any explicit --field uses
schema-driven output and requires a positive version on updates.

Format-10 and format-11 projects may add repeatable required relationships with
--belongs-to <name>:<ExistingResource>. The target must already be a generated
resource. Relationship IDs remain explicit, owner-scoped, and database-backed.

Generators refuse to overwrite files. Generated applications keep routes,
handlers, configuration, and SQL as ordinary source files you can edit.
`)
}
