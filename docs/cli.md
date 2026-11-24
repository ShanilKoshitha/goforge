# CLI reference

This reference covers the v0.23.1 CLI. It creates format-17 applications that
use the published v0.23.0 runtime. Both the v0.22.1 and v0.23.0 CLIs create
format-16 applications pinned to runtime v0.22.0. Installing a newer CLI
preserves the format and source of existing applications.

Run project commands from the generated application's root. For installation
and a first run, see [Quick Start](quick-start.md).

## Project setup

| Command | What it does |
| --- | --- |
| `forge new <directory> [--module <path>] [--replace <goforge-path>]` | Creates a project. Use `--replace` with a local GoForge checkout when developing the framework. |
| `forge migrate` | Applies pending migrations through the application-owned `./cmd/console`. |
| `forge version` | Prints the installed CLI version. |
| `forge help` | Prints command syntax. Running `forge` with no arguments also shows help. |

### Try changes from a GoForge checkout

From the framework checkout, install its CLI and generate a separate app that
uses the checkout as its runtime. Replace `/path/to/goforge` with the checkout's
absolute path.

```sh
go install ./cmd/forge
forge new myapp --module example.com/myapp --replace "/path/to/goforge"
```

Continue from `cd myapp` in [Quick Start](quick-start.md#2-create-the-project-and-start-its-services).

## Development

| Command | What it runs | Project formats |
| --- | --- | --- |
| `forge dev` | Watched HTTP server, queue worker, and scheduler | 11–17 |
| `forge serve` | Watched HTTP server | 4–17 |

`forge dev` labels each process's output and reports ready after all three
startup checks pass. If a process exits, the supervisor stops its peers. Use
Ctrl-C to stop the stack. Restart `forge dev` after editing worker or scheduler
code or registries. The default process-tree shutdown allowance is 20 seconds.
If the application needs longer to drain, set `FORGE_DEV_SHUTDOWN_TIMEOUT` to a
Go duration greater than the longest configured application shutdown timeout.

`forge serve` watches Go, Forge views, SQL, module, environment, and project
manifest files, plus embedded asset files under `resources/assets/files` in
format 12 and newer. It compiles views and builds a candidate server outside
the application tree, checks candidate liveness on a private loopback address,
then switches the stable public proxy to the candidate. Invalid edits leave
the last working server running. Use `/health` to check liveness and `/ready`
to check database and migration readiness. Run `forge migrate` explicitly
after adding migrations.

Eligible full HTML navigations receive a development-only same-origin reload
client. The browser reloads once the replacement server is active. The proxy
preserves application CSP and cookies. Compressed, streamed, ranged,
downloadable, `no-transform`, and non-document responses pass through unchanged.

Linux and Windows verify candidate listener ownership without extra utilities.
macOS, AIX, DragonFly BSD, FreeBSD, illumos, iOS, NetBSD, OpenBSD, and Solaris
use `lsof`. Use `go run ./cmd/server` to start the application directly when
the watched proxy is unavailable or unnecessary.

Start PostgreSQL and Mailpit separately when using the generated Compose stack,
and edit `.env` explicitly. The development commands use the ORM artifact
already in the application. Run `forge orm:generate` after changing Go models.

## Test and build

`forge test` and `forge build` take no arguments. The v0.23.1 CLI supports
formats 4–17 for both commands. The v0.23.0 CLI supports formats 4–16.

Both commands check generated ORM code, compiled views, and (in formats 12–17)
embedded assets in that order. They stop at the first failed check and report
how to correct it. These checks leave source files and the database untouched.
After preflight, `forge test` runs `go test ./...`. `forge build` stages a
trimmed `./cmd/server` binary and publishes it atomically to `bin/app` on Unix
or `bin/app.exe` on Windows. A failed build leaves the previous binary in place.

Run the checks and Go tools directly when you need custom packages, flags,
build tags, a different output path, or separate process binaries.

```sh
forge orm:generate --check
forge views:compile --check
forge assets:check
go test ./...
go build -trimpath -o bin/app ./cmd/server
go build -trimpath -o bin/worker ./cmd/worker
go build -trimpath -o bin/scheduler ./cmd/scheduler
go build -trimpath -o bin/console ./cmd/console
```

Add `.exe` to output names when building Windows executables.

## Generation

Generators preserve existing application-owned files and update their managed
artifacts as needed. Both `forge make:controller Users` and
`forge make controller Users` work. The colon and spaced forms apply to every
`make` generator.

| Command | What it generates | Project formats |
| --- | --- | --- |
| `forge make:controller <name>` | Controller under `internal/http/controllers` | 1–17 |
| `forge make:request <name>` | Request type under `internal/http/requests` | 1–17 |
| `forge make:migration <name>` | Paired up/down SQL files | 1–17 |
| `forge make:model <name>` | Model, paired SQL migration, and typed store. See [Typed ORM](orm.md). | 4–17 |
| `forge make:resource <name>` | Authenticated browser and JSON CRUD with a repository, migration, and views. See [Building an application](building-an-app.md#generate-an-authenticated-crud-resource). | 8–17 |
| `forge make:component <name>` | Component under `resources/views/components`. See [Forge view language](view-language.md#components-props-and-slots). | 5–17 |
| `forge make:job <name>` | Typed job, test, and registration. See [Durable jobs](jobs.md). | 6–17 |
| `forge make:mail <name>` | Text and Forge HTML mail templates, a Go renderer, and its test. | 9–17 |

### Model and resource options

```text
forge make:model <name> [--field <name>:<type>[:required|nullable]]... [--belongs-to <name>:<ExistingModel>]...
forge make:resource <name> [--field <name>:<type>[:required|nullable]]... [--belongs-to <name>:<ExistingResource>]... [--choice-label <relationship>:<field>]...
```

Repeat `--field` in the order you want fields generated. Types are `string`,
`text`, `integer`, and `boolean`. Fields default to required. Append
`:nullable` to allow SQL `NULL`.

```sh
forge make:resource Issue --field title:string --field notes:text:nullable --field priority:integer --field active:boolean
```

A resource generated with neither `--field` nor `--belongs-to` has a required
`name:string` and accepts versionless updates. Any explicit field or
relationship selects schema-driven output, which requires a positive `version`
for JSON and browser updates. Updates replace the writable fields. A missing,
`null`, or empty nullable value clears the column to SQL `NULL`. Numeric `0`
and boolean `false` are present values.

Standalone models can refer to an existing model with `--belongs-to`. Format
10 and newer can use that option on resources to refer to an existing generated
resource. The generated Go handles validation, eager loading, and browser
choices. Generated SQL enforces the composite owner and target constraint.
Format 16 and newer accepts `--choice-label` for an explicit target text field
that can appear in relationship choices. Omit the option for ID-only labels.
The flags describe a single generation run. Edit the resulting Go, SQL, and
views to evolve the application.

## Artifact maintenance and diagnostics

| Command | What it does | Project formats |
| --- | --- | --- |
| `forge orm:generate [--check]` | Regenerates the typed ORM artifact, or checks freshness with `--check`. | 4–17 |
| `forge views:compile [--check]` | Compiles Forge view sources, or checks freshness with `--check`. Formats 1–4 use the legacy compiler and require view sources. | 1–17 |
| `forge assets:check` | Validates the embedded asset inventory. | 12–17 |
| `forge doctor` | Reports configuration, database/migration, ORM, view, and asset checks without applying repairs. | 17 |
| `forge routes:list` | Lists routes from the actual constructed router without starting the server or connecting to PostgreSQL in the default scaffold. | 17 |

See [Diagnostics](diagnostics.md) for exit behavior, interpretation, and direct
Go commands.

## Workers and schedules

| Command | What it does | Project formats |
| --- | --- | --- |
| `forge queue:work` | Runs the application-owned worker. | 6–17 |
| `forge queue:failed` | Lists terminal failures. | 6–17 |
| `forge queue:retry <id\|--all>` | Makes a failed job, or all failed jobs, eligible for retry. | 6–17 |
| `forge queue:forget <id>` | Deletes one terminally failed job. | 6–17 |
| `forge schedule:work` | Runs the scheduler continuously. | 11–17 |
| `forge schedule:run` | Runs one scheduler pass. | 11–17 |
| `forge schedule:list` | Lists configured schedules and their stored state. | 11–17 |

Run `go run ./cmd/scheduler --once` to execute a scheduling pass directly. See
[Durable jobs](jobs.md#run-and-operate-workers) and
[Recurring schedules](schedules.md#run-the-scheduler-directly) for operation
and configuration.
