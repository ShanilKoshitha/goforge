# GoForge

GoForge is an opinionated, batteries-included Go application framework for
developers who value Laravel, Symfony, Rails, or Spring ergonomics but want the
result to remain unmistakably Go.

Its contract is simple:

- **Generate source, do not conceal it.** Routes, controllers, requests,
  configuration, and migrations are ordinary files in the application.
- **Build on the standard library.** The HTTP kernel is `net/http`; persistence is
  `database/sql`; dependencies are constructor parameters.
- **Fail at startup.** Typed configuration reports all invalid values together.
- **Keep every exit visible.** Use a standard `http.Handler`, `*sql.DB`, custom
  middleware, or a hand-built server whenever the defaults stop fitting.
- **Prefer boring production behavior.** Request IDs, structured logs, panic
  recovery, secure headers, body limits, graceful shutdown, and reversible SQL
  migrations are first-class.

## Status

GoForge v0.11.1 is PostgreSQL-first and accepted against its written milestone
scorecard. It includes explicit database wiring, parallel JSON and
server-rendered authentication, database-backed sessions, CSRF-protected HTML
forms, production middleware, embedded migrations, owner-scoped JSON and HTML
CRUD generation, a reflection-free typed ORM, a static Go-model parser,
deterministic mapping generation, explicit eager-loaded relationships, and
ORM-backed fresh scaffolds/resources. Its positioned `.forge.html` compiler adds
static components, strict props and slots, control directives, page-local
stacks, explicit form helpers, application-owned functions, and source-mapped
diagnostics while emitting ordinary `html/template`. The API can still change
before v1. Durable typed jobs add transactional dispatch, delays, active
deduplication, bounded PostgreSQL workers, lease fencing and crash recovery,
finite retries, failed-job operations, and structured payload-free lifecycle
events without hidden worker startup or handler discovery. Its explicit request
boundary adds typed composable validation, exact JSON/form failure semantics,
bounded HTTP timeouts and headers, correlated completion logs, validated CORS
and security policy, trusted-proxy parsing, and PostgreSQL authentication
throttles shared across processes and restarts.
Account integrity now includes JSON and browser password change, transparent
hash upgrades, immediate cross-process session revocation, and independently
configured idle and absolute session lifetimes. Credential writes are ordinary
application-owned compare-and-swap SQL, and stale concurrent requests cannot
restore an old password or overwrite a winning rotated browser session.
The v0.9 developer-loop milestone is accepted. It adds opinionated,
non-mutating `forge test` and `forge build` gates while retaining the underlying
Go commands as complete escape hatches. The v0.10 development server closes the
compiled-view edit loop: `forge serve` watches application inputs, builds and
checks candidate liveness off-port, and promotes only a live replacement
while preserving the last-good server through invalid edits.
The v0.11 resource generator accepts ordered typed scalar fields and emits a
complete owner-scoped JSON and browser CRUD slice as ordinary Go, SQL, and
Forge templates. Required and nullable string, text, integer, and boolean
semantics remain explicit from transport decoding through PostgreSQL, and new
schema-driven resources require optimistic versions on every update.

## Install and try it

Install the released CLI and generate an application:

```sh
go install github.com/ShanilKoshitha/goforge/cmd/forge@v0.11.1
forge new myapp --module example.com/myapp
cd myapp
docker compose up -d
forge make:resource Issue
forge migrate
# For plain-HTTP development, set APP_ENV=local and APP_URL=http://localhost:8080 in .env.
forge serve
```

For framework development from this checkout:

```sh
go install ./cmd/forge
forge new myapp --module example.com/myapp --replace /path/to/goforge
cd myapp
docker compose up -d
forge make:resource Issue
forge migrate
forge test
forge build
# For plain-HTTP development, set APP_ENV=local and APP_URL=http://localhost:8080 in .env.
forge serve
```

Generated projects retain `APP_ENV=production` in `.env`, which keeps session
cookies HTTPS-only. Change it to `APP_ENV=local` and set
`APP_URL=http://localhost:8080` only for local plain-HTTP development, before
running `forge serve` or the worker. Never use `APP_ENV=local` in a
deployed process because it disables secure cookies.

Visit `http://localhost:8080/register` for the browser workflow or
`http://localhost:8080/health` for liveness. Readiness, including PostgreSQL
and expected migration state, is available at `http://localhost:8080/ready`.

## The generated request path

```text
cmd/server/main.go
  -> internal/config.Load()       explicit typed environment
  -> internal/application.New()   explicit dependency construction
  -> routes.Register()            complete searchable route table
  -> controller method            normal Go function
  -> httpx.Context                thin request/response convenience
```

There is no annotation scanning, reflection-driven container, global application
state, hidden route discovery, or ORM query language.

## CLI

```text
forge new <directory> [--module <path>] [--replace <goforge-path>]
forge serve
forge test
forge build
forge migrate
forge views:compile
forge orm:generate [--check]
forge make:model <name>
forge make:resource <name> [--field <name>:<type>[:required|nullable]]...
forge make:controller <name>
forge make:request <name>
forge make:migration <name>
forge make:job <name>
forge make:mail <name>
forge queue:work
forge queue:failed
forge queue:retry <id|--all>
forge queue:forget <id>
```

The spaced forms (`forge make controller Users`) also work. Generators never
overwrite existing files.

Typed resources accept repeated fields in declaration order:

```sh
forge make:resource Issue \
  --field title:string \
  --field notes:text:nullable \
  --field priority:integer:required \
  --field active:boolean
```

The field grammar is `<name>:<string|text|integer|boolean>[:required|nullable]`.
Fields are required by default. Calling `make:resource` without `--field`
retains the legacy required `name:string` resource and its versionless-update
compatibility. Supplying any explicit field—even `--field name:string`—selects
schema-driven output whose JSON and browser updates require a positive
optimistic `version`. Field descriptions are one-shot generation input; the
generated application contains ordinary static Go, SQL, and HTML rather than a
runtime schema. Updates replace the complete writable resource: a missing,
`null`, or empty nullable value clears that column to SQL `NULL`, while numeric
`0` and boolean `false` remain present values.

`forge test` and `forge build` are intentionally no-argument defaults for
format-4 through format-9 projects. Both non-mutating preflights check the
generated ORM first and compiled views second. Testing then runs exactly `go
test ./...`. Building stages a trimmed `./cmd/server` executable and publishes
it atomically as `bin/app` on Unix or `bin/app.exe` on Windows, so a failed build
does not replace the last-good binary.

The exact direct escape hatches are:

```sh
forge orm:generate --check
forge views:compile --check
go test ./...
go build -trimpath -o bin/app ./cmd/server
go run ./cmd/server
```

Use direct Go commands for custom packages, flags, tags, targets, output paths,
or worker and console builds. `forge serve` is the watched development default;
it compiles application-owned views, stages ordinary server binaries outside the
repository, health-checks them on private loopback addresses, and switches its
stable public proxy only after success. Invalid view or Go source leaves the
last-good server reachable and recovers on the next correction. Refresh the
browser after a successful reload; the proxy does not inject scripts or rewrite
responses. Promotion proves `/health` liveness, not `/ready` database or
migration readiness; run `forge migrate` explicitly after SQL changes.

PID-verified candidate promotion is batteries-included on Linux, Windows, and
macOS. AIX, DragonFly BSD, FreeBSD, illumos, iOS, NetBSD, OpenBSD, and Solaris
use the system `lsof` command for the same check. Where that utility is absent,
use the exact one-shot escape hatch `go run ./cmd/server`.

The wrappers never start services, apply migrations, generate a stale ORM,
modify `.env`, or start workers. Browser LiveReload/HMR, frontend assets,
multi-process development, and environment diagnosis remain separate
milestones.

## Framework packages

- `forge`: application lifecycle and graceful shutdown
- `httpx`: routing, JSON binding, errors, middleware, and `net/http` adapters
- `config`: typed environment and optional dotenv loading
- `validation`: fluent validation without reflection
- `cache`: cache contracts, typed JSON helpers, and an in-memory store
- `view`: `.forge.html` compilation to inspectable, contextually escaped
  standard-library templates
- `web`: buffered browser sessions, CSRF, flash-safe responses, and redirects
- `session`: signed-ID server-side sessions with flash data and swappable storage
- `security/ratelimit`: bounded authentication throttling with a replaceable store
- `database`: transaction helper over `database/sql`
- `database/migrate`: paired SQL migrations; PostgreSQL runs are advisory-locked
  and transactionally applied when the migration SQL is transaction-safe. MySQL
  DDL may commit implicitly and is not promised to be atomic.
- `orm`: typed, inspectable PostgreSQL builders, mappings, partial fields,
  optimistic guards, bulk inserts, and explicit batched relationships over
  ordinary `*sql.DB` or `*sql.Tx` executors
- `orm/postgres`: disclosure-safe PostgreSQL constraint and retryable-error
  classification that preserves native causes
- `job`: typed definitions, explicit registries, dispatch policy, at-least-once
  worker lifecycle, retry semantics, and replaceable store/observer contracts
- `job/postgres`: parameterized durable queue storage with database-clock
  claims, `SKIP LOCKED`, leases, generation fencing, and failed-job operations

See [docs/philosophy.md](docs/philosophy.md) for design constraints and
[docs/building-an-app.md](docs/building-an-app.md) for the end-to-end workflow.
Developers arriving from full-stack ecosystems can use
[docs/framework-map.md](docs/framework-map.md) as a concept-by-concept translation.
See [docs/jobs.md](docs/jobs.md) for the durable-jobs contract and operating
workflow.

See [CONTRIBUTING.md](CONTRIBUTING.md) for code conventions, template ownership,
and verification, and [docs/CODE_QUALITY.md](docs/CODE_QUALITY.md) for the repository
quality assessment.
