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

GoForge v0.17.1 is the accepted PostgreSQL-first embedded-production-assets
release. Fresh format-12 applications pin the published
v0.17.0 asset runtime without a local replace. It includes explicit database
wiring,
parallel JSON and server-rendered authentication, database-backed sessions,
CSRF-protected HTML forms, production middleware, embedded migrations,
owner-scoped JSON and HTML CRUD generation, a reflection-free typed ORM, a
static Go-model parser,
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
The v0.12 account-recovery workflow adds browser and JSON forgot/reset routes,
single-use hashed reset tokens, immediate session revocation, application-owned
text and Forge HTML mail, an encrypted durable outbox, and an explicit SMTP
adapter. Generated applications own the routes, policy, SQL, templates, jobs,
and dependency wiring; every boundary remains replaceable ordinary Go.
The v0.13 resource workflow adds repeatable required belongs-to generation
across ordinary models, composite owner-safe PostgreSQL constraints, exact JSON
and form input, eager-loaded repositories, bounded browser choices, Forge
views, and generated tests. Missing and cross-owner targets are deliberately
indistinguishable, while direct SQL and handwritten relationship code remain
complete escape hatches.
The v0.14 runtime adds explicit versioned recurring schedules that atomically
materialize typed jobs through PostgreSQL. It defines strict five-field cron,
IANA civil time and DST behavior, bounded misfire coalescing, conservative
active-job overlap suppression, fail-closed definition fingerprints,
competing-process row coordination, read-only inspection, and payload-free
observers.
Fresh format-11 and format-12 scaffolds own the schedule registry, PostgreSQL
migration, isolated configuration, scheduler process, inspection command, and
direct Go escape hatches. The v0.16.0 release pinned the immutable public
v0.14.2 runtime and demonstrated its cancellation-aware dynamic schedule
factory; format 12 pins v0.17.0 because its embedded package uses the new public
asset runtime.
The v0.14.2 runtime added cancellation-aware dynamic schedule factories,
dependency-free validation between generated schedule and worker registries,
and deterministic password-recovery equalization acceptance without runtime
discovery.
The v0.15 CLI composes the watched server, durable worker, and recurring
scheduler behind one labelled, truthful development lifecycle. The v0.16
release closes the server-rendered browser loop with a randomized same-origin
client and committed-generation SSE while keeping all reload machinery outside
generated and production application code. The v0.17 release gives fresh
applications an ordinary embedded CSS/JavaScript package, explicit route and
Forge `asset` helper. Direct Go builds contain the exact application-owned
bytes; stable URLs use strong ETags and mandatory revalidation without a hidden
asset build. Rolling deployments must keep same-path asset changes backward
compatible or use stickiness until retained generations provide exact HTML and
asset version consistency.

## Install and try it

Install the released CLI and generate an application:

```sh
go install github.com/ShanilKoshitha/goforge/cmd/forge@v0.17.1
forge new myapp --module example.com/myapp
cd myapp
docker compose up -d
forge make:resource Issue
forge migrate
# For plain-HTTP development, set APP_ENV=local and APP_URL=http://localhost:8080 in .env.
forge dev
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
forge dev
```

Generated projects retain `APP_ENV=production` in `.env`, which keeps session
cookies HTTPS-only. Change it to `APP_ENV=local` and set
`APP_URL=http://localhost:8080` only for local plain-HTTP development, before
running `forge dev`, `forge serve`, or the worker. Never use `APP_ENV=local` in a
deployed process because it disables secure cookies.

Visit `http://localhost:8080/register` for the browser workflow or
`http://localhost:8080/health` for liveness. Readiness, including PostgreSQL
and expected migration state, is available at `http://localhost:8080/ready`.
Password recovery starts at `http://localhost:8080/forgot-password`; delivered
development mail is visible in Mailpit at `http://localhost:8025`.

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

## CLI (current source)

The command surface below is included in the v0.17.1 release. Fresh
applications use format 12 and pin the matching asset runtime.

```text
forge new <directory> [--module <path>] [--replace <goforge-path>]
forge dev
forge serve
forge test
forge build
forge assets:check
forge migrate
forge views:compile
forge orm:generate [--check]
forge make:model <name>
forge make:resource <name> [--field <name>:<type>[:required|nullable]]... [--belongs-to <name>:<ExistingResource>]...
forge make:controller <name>
forge make:request <name>
forge make:migration <name>
forge make:job <name>
forge make:mail <name>
forge queue:work
forge queue:failed
forge queue:retry <id|--all>
forge queue:forget <id>
forge schedule:list
forge schedule:run
forge schedule:work
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

Format-10 through format-12 applications can generate a required owner-scoped
relationship to an existing resource:

```sh
forge make:resource Category --field name:string
forge make:resource Issue \
  --field title:string \
  --belongs-to category:Category
```

The relationship description is also one-shot input. Generated Go owns the
protected foreign key, association value, validation, eager loading, browser
choices, and presentation; generated SQL owns the composite owner/target
constraint. No relationship registry or runtime schema is added.

`forge test` and `forge build` are intentionally no-argument defaults for
format-4 through format-12 projects. Both non-mutating preflights check the
generated ORM first, compiled views second, and format-12 embedded assets third.
Testing then runs exactly `go test ./...`. Building stages a trimmed
`./cmd/server` executable and publishes
it atomically as `bin/app` on Unix or `bin/app.exe` on Windows, so a failed build
does not replace the last-good binary.

The exact direct escape hatches are:

```sh
forge orm:generate --check
forge views:compile --check
forge assets:check
go test ./...
go build -trimpath -o bin/app ./cmd/server
go build -trimpath -o bin/scheduler ./cmd/scheduler
go run ./cmd/server
go run ./cmd/scheduler --once
```

Use direct Go commands for custom packages, flags, tags, targets, output paths,
or worker and console builds. `forge dev` is the complete format-11/12 development
default: it labels and supervises `forge serve`, the application-owned worker,
and the scheduler under one cancellation boundary, and reports ready only after
all three startup contracts succeed. Any service exit stops its peers. It does
not start PostgreSQL or Mailpit, apply migrations, or change configuration.
The supervisor allows 20 seconds for graceful process-tree shutdown by default.
If an application configures a longer server or worker drain, export
`FORGE_DEV_SHUTDOWN_TIMEOUT` with a longer Go duration before starting the CLI;
this outer timeout must exceed the longest application shutdown timeout.

`forge serve` remains the HTTP-only watched development command;
it compiles application-owned views, stages ordinary server binaries outside the
repository, health-checks them on private loopback addresses, and switches its
stable public proxy only after success. Invalid view or Go source leaves the
last-good server reachable and recovers on the next correction. Eligible full
HTML browser navigations receive one randomized same-origin external client and
reload automatically only after a replacement is fully committed. The proxy
preserves application CSP and cookies and leaves compressed, streamed, ranged,
downloadable, `no-transform`, and non-document responses untouched. Promotion
proves `/health` liveness, not `/ready` database or
migration readiness; run `forge migrate` explicitly after SQL changes.

PID-verified candidate promotion is batteries-included on Linux, Windows, and
macOS. AIX, DragonFly BSD, FreeBSD, illumos, iOS, NetBSD, OpenBSD, and Solaris
use the system `lsof` command for the same check. Where that utility is absent,
use the exact one-shot escape hatch `go run ./cmd/server`.

`forge test`, `forge build`, and `forge serve` never start external services,
apply migrations, generate a stale ORM, modify `.env`, or start workers.
`forge dev` starts only the three application processes; background-process
source changes require restarting that one command. Format-12 asset files below
`resources/assets/files` participate in the watched server's last-good rebuild
and committed browser reload. Asset transforms, HMR, external publication, and
environment diagnosis remain separate milestones.

## Framework packages

- `forge`: application lifecycle and graceful shutdown
- `asset`: bounded immutable embedded files, canonical URLs, strong validators,
  byte ranges, and deterministic media policy through standard `http.Handler`
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
