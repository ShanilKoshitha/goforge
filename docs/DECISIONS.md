# Product decisions

This log records choices that materially shape generated applications. Newer
entries supersede older ones only when they say so explicitly.

## D001 — PostgreSQL is the v0.2 database path

**Status:** accepted

GoForge v0.2 will make PostgreSQL exceptional before presenting multiple
dialects as equally supported. Generated applications use `database/sql` with
pgx's standard-library adapter, PostgreSQL SQL, and a database-level advisory
lock around migrations. The existing generic migration API may remain, but
documentation must not promise transactional DDL on MySQL.

Reason: the milestone requires one complete production-shaped workflow, and
PostgreSQL can provide transactional DDL and session-scoped advisory locks.

## D002 — Project commands are conventional application code

**Status:** accepted

Generated projects own `cmd/server` and `cmd/console` entry points. The installed
`forge` CLI delegates `serve` to `go run ./cmd/server`, database-aware commands to
`go run ./cmd/console`. For the current milestone, testing and building remain the
direct, conventional `go test ./...` and `go build ./cmd/server` commands; thin
wrappers for them are deferred.

Reason: database construction, embedded migrations, and route registration stay
inspectable in the application. The framework CLI does not discover packages or
load application code dynamically.

## D003 — Generated registries are separate from application-owned files

**Status:** accepted

`forge make:resource` creates application-owned model, repository, request,
controller, migration, and test files exactly once. It records resource metadata
under `.forge/` and regenerates only files carrying a generated-code header.
Handwritten routes remain in `routes/routes.go`; resource route wiring lives in a
separate generated registry.

Reason: repeated generation must be deterministic without rewriting human code.

## D004 — The milestone workflow uses JSON auth and JSON CRUD

**Status:** accepted

New applications include registration, login, logout, PostgreSQL users, password
hashing, server-side sessions, and auth middleware. Resource generators produce
authenticated, owner-scoped JSON CRUD endpoints. Keeping this milestone's HTTP
workflow consistently JSON proves authentication, validation, sessions, and
authorization without adding a second browser/CSRF workflow. Views remain a
supported battery and HTML auth can be a later explicit milestone.

## D005 — Authentication sessions are database-backed

**Status:** accepted

Generated production-shaped applications store session payloads in PostgreSQL.
The browser receives only the signed random session ID already defined by the
framework session package. In-memory sessions remain available for tests and
small local programs, but are not the generated default.

## D006 — Background jobs are deferred from v0.2

**Status:** accepted for current milestone

Jobs are part of the north star but are not part of the current milestone's
four-command authenticated CRUD acceptance target. Adding an in-memory queue now
would create the appearance of a battery without a production-shaped workflow.
Job generation and a durable PostgreSQL worker are candidates for the next
written milestone.

## D007 — External PostgreSQL is explicit

**Status:** accepted

`forge new` generates a Compose file and a conventional local database URL, but
does not silently start containers. Tests that do not need SQL use repository
fakes. PostgreSQL integration tests run when `GOFORGE_TEST_DATABASE_URL` is set,
and CI provides that service explicitly.

Reason: local process orchestration should be visible, and the four framework
commands must remain deterministic even on machines without Docker.

## D008 — v0.2 stops at the ruthless four-command target

**Status:** accepted for current milestone

The current milestone implements `new`, `make:resource`, `migrate`, and `serve`,
plus the infrastructure corrections explicitly called out in the objective.
`rollback`, `routes`, wrapper commands for `test`/`build`, HTML auth, and jobs are
recorded but deferred. Direct `go test`, `go build`, and `Router.Routes` remain
available today.

Reason: the milestone instructions require the smallest complete workflow and
explicitly say not to expand after its acceptance criteria pass.

## D009 — Templates mirror the generated source tree

**Status:** accepted

Scaffold and resource source lives in embedded `.tmpl` files under
`internal/cli/templates`. A shared renderer uses `[[...]]` delimiters, reports
missing substitutions, and formats Go before writing it. Generator orchestration,
resource metadata, and file publication have separate source files. This removes
the obsolete scaffold overlay and long escaped source literals while retaining a
single self-contained CLI binary.

## D010 — JSON binding requires its declared media type

**Status:** accepted

`BindJSON` requires `application/json`, with optional valid parameters; missing or
other media types receive HTTP 415. Generated tests and clients send the header.
This also prevents browser forms from reaching cookie-based JSON login through
`text/plain` submissions. HTML forms would need a separately designed CSRF flow.

## D011 — Generator names follow Go initialism and package conventions

**Status:** accepted

New types preserve common initialisms, such as `APIClient`. Resource package
names are lowercase without underscores (`apiclient`), while SQL tables retain
snake case (`api_clients`). Existing resource metadata retains its recorded
package paths. Import aliases prevent resource names from colliding with
framework or standard-library imports. Names that collide with built-in tables
or generated declarations are rejected before writing files.

## D012 — Browser applications extend rather than negotiate the JSON surface

**Status:** accepted for v0.3

Generated applications keep the existing JSON controllers and routes and add
explicit browser controllers and routes. Handlers do not switch semantics based
on `Accept` headers. This preserves existing clients, keeps route behavior
searchable, and makes the batteries-included scaffold useful for both HTML and
JSON without hidden content negotiation.

## D013 — GoForge templates compile to the standard library

**Status:** accepted for v0.3

GoForge owns a small `.forge.html` source language and deterministic compiler.
The first grammar supports layouts, named sections and yields, and includes with
explicit data; expressions, conditionals, iteration, and contextual escaping
remain standard `html/template`. Generated applications embed canonical compiler
output in ordinary Go and parse only that output at production startup. Direct
`html/template` parsing remains supported as an escape hatch.

The managed `forge views:compile` workflow currently validates templates with
the standard function set. Applications that require a custom `template.FuncMap`
can bypass the managed artifact and parse ordinary `html/template` source
directly; first-class compiler-time registration is deferred until the core
browser workflow is accepted.

Reason: familiar full-stack ergonomics should not require a second expression
language, a proprietary escaping implementation, or runtime template discovery.

## D014 — Browser sessions require revocation-safe persistence

**Status:** accepted for v0.3

Session stores distinguish creating a fresh session, updating a live session,
and atomically rotating an authenticated session. Updating a revoked ID must not
recreate it. Browser session middleware and flash persistence build on this
contract so a concurrent stale request cannot restore a session invalidated by
login, logout, or privilege changes.

## D015 — Project format v3 is an explicit pre-v1 boundary

**Status:** accepted

Fresh browser-capable applications declare project format version 3. The v0.3
resource generator refuses both older and newer formats before planning or
writing files rather than partially applying browser templates and incompatible
route registries. A newer project instructs the developer to upgrade the CLI.
Automatic upgrades for previously generated pre-v1 applications are deferred;
the current compatibility guarantee covers the JSON behavior retained inside a
fresh v3 application, not source compatibility for old generated projects.

## D016 — Cookie-authenticated browser and JSON mutation surfaces stay distinct

**Status:** accepted for v0.3

Browser routes use synchronizer-token CSRF protection, and form method override
is scoped to `/app/`. JSON mutation routes retain explicit HTTP methods and JSON
media types; API logout requires a JSON value so an ordinary form cannot submit
ambient cookie credentials. Responses that access browser sessions are marked
`Cache-Control: no-store`, and generated `.env` secrets use owner-only Unix
permissions. Application redirects accept local absolute paths by default, with
a conspicuously named HTTP(S)-only external escape hatch.

Generated login and registration apply bounded source and normalized-account
limits before password hashing. The limiter has a replaceable store contract;
the default in-memory implementation has bounded storage and fails closed.
Browser and JSON clients retain their respective HTML and JSON error contracts.

Reason: retaining both browser and JSON surfaces must not silently widen either
surface's security boundary.

## D017 — The ORM is a generated, PostgreSQL-first Data Mapper

**Status:** accepted for v0.4

GoForge will provide a full typed ORM without Active Record or runtime model
discovery. Application-owned Go model declarations are statically parsed by
`forge orm:generate`; deterministic generated Go contains table/column metadata,
scanners, changesets, queries, mutations, and relationship loaders. Production
startup performs no package scanning or reflection.

The runtime stays a small layer over an explicit executor implemented by both
`*sql.DB` and `*sql.Tx`. Builders expose their final SQL and arguments, and raw
SQL can execute through the same transaction. PostgreSQL is the only promised
dialect for v0.4; pretending to offer shallow dialect parity would weaken the
complete workflow.

Relationships load explicitly and in batches; model field access never performs
I/O. Mutations require typed allowlisted changes, and update/delete builders
refuse unconstrained execution unless the caller uses a conspicuous whole-table
escape hatch. Generated owner-scoped repository adapters remain at controller
boundaries so applications can replace the ORM without rewriting HTTP code.

Fresh ORM-capable projects use format 4 and older/newer formats are refused
before managed generation. Existing format-3 applications continue compiling
against public v0.3 runtime APIs and may adopt the executor and generated models
incrementally; automatic pre-v1 source upgrades are not claimed.

Physical schema remains authoritative plain paired SQL migrations. Model
declarations drive generated mapping, not automatic production schema changes.

## D018 — Optimistic concurrency is explicit at generated write boundaries

**Status:** accepted for v0.4

Generated resource models carry a protected numeric version. Every update
increments it atomically. JSON clients may supply the version they observed;
when present, generated repositories add the version predicate and increment as
one statement. Omitting it retains the pre-v0.4 JSON update contract. Browser
edit forms always carry the observed version because the framework controls
both ends of that interaction.

A guarded zero-row update is not automatically called stale. The repository
first performs an owner-scoped existence check: an accessible row means a stale
write and returns HTTP 409, while a missing or differently owned row remains
HTTP 404. This preserves tenant non-disclosure and keeps the policy in ordinary
generated repository/controller code. The browser conflict page retains the
submitted value, explains the conflict, and carries the current safe version
for an explicit retry.

Reason: automatic last-write-wins is a poor full-stack default, but forcing a
new precondition on every existing JSON client would break the retained v0.3
contract. The optional JSON guard and mandatory generated browser guard expose
concurrency deliberately without hidden unit-of-work behavior.

## D019 — GoForge owns structure, not a second expression language

**Status:** accepted for v0.5

The v0.5 `.forge.html` language owns server-rendered composition: layouts,
sections, includes, static components, declared props, default and named slots,
page-local stacks, control directives, and explicit form conveniences. Values
and conditions remain standard Go-template pipelines. Ordinary `{{...}}`
actions remain available locally, while `view.Parse`, direct `html/template`,
and `Engine.Templates` remain complete escape hatches.

The regex preprocessor is replaced by a positioned lexer/parser. Component and
slot bodies are expanded into one ordinary template source before
`html/template` performs contextual analysis. Slot content is never pre-rendered
or transported as `template.HTML`; application strings therefore retain the
standard library's context-sensitive escaping. Dynamic component names and
runtime source discovery are deliberately unsupported.

Component files live under `resources/views/components` and declare props with
`@props(title, tone="info")`. Bare props are required and v0.5 defaults are
quoted scalar literals. Callers use
`@component("components/card", title=.Title)`, optional named
`@slot("actions")...@endslot` blocks, remaining default-slot content, and
`@endcomponent`. Missing or unknown props and slots fail at compile time.

Control directives accept Go-template pipelines: `@if`/`@elseif`/`@else`,
`@for`/`@empty`, and `@with`. `@push` and `@stack` are resolved independently
for each root page in deterministic encounter order; pushes that depend on
runtime loop/with scope are rejected. Form directives read only an explicit
`.Form` value: `@csrf`, `@method`, `@old`, and `@errors` never inspect ambient
request state.

Fresh format-5 applications own one editable `template.FuncMap` and expose a
view constructor used by production. Their inspectable project compiler uses
the same registry, so custom functions are validated before replacing the
generated artifact. The installed CLI delegates format-5 view compilation to
that application command; format-4 projects retain the legacy compiler path.

Reason: familiar full-stack authoring requires more than layout substitution,
but runtime components, a second evaluator, or trusted-HTML slot transport
would violate GoForge's inspectability and safety contract.

## D020 — Jobs are PostgreSQL-backed, typed, fenced, and at least once

**Status:** accepted for v0.6

GoForge's first job path is a complete durable PostgreSQL workflow rather than
an in-memory queue. The public `job` runtime owns typed definitions, explicit
registration, dispatch policy, worker lifecycle, and replaceable store and
observer contracts. The `job/postgres` adapter owns parameterized storage
operations. Fresh applications own their plain SQL queue migration,
application dependencies and handlers, generated registry, `cmd/worker`, and
queue administration through `cmd/console`.

Dispatch uses a small executor interface structurally satisfied by `*sql.DB`
and `*sql.Tx`. Callers can therefore enqueue in the same transaction as a
domain mutation. Definition name and payload shape are versioned application
contracts; production performs no package scan, reflection-based handler
discovery, or application import from the installed CLI.

Workers claim only available local capacity in short PostgreSQL transactions
using `FOR UPDATE SKIP LOCKED`, commit before invoking handlers, and use the
database clock. Claims increment attempts and carry a lease owner plus a
monotonic lease generation. Heartbeat, acknowledgement, retry, release, and
terminal-failure writes must match all fencing fields so a stale worker cannot
mutate a reclaimed delivery. Successful rows are deleted; terminal rows move
atomically to a separate failure table. Unknown job names remain unclaimed so
rolling deployments do not destroy work produced by newer application code.

Delivery is explicitly **at least once**. A process can crash after an external
effect and before acknowledgement, so queue leases do not imply exactly-once
business behavior. An optional dispatch key deduplicates only concurrent active
jobs with the same queue, versioned name, and key; it reports whether a row was
inserted or already existed and is not marketed as handler idempotency.

Fresh queue-capable projects use format 6. They expose familiar
`make:job`, `queue:work`, `queue:failed`, `queue:retry`, and `queue:forget`
commands, but those commands generate or delegate to conventional project Go.
One-off delayed dispatch is included because retry storage already requires an
availability timestamp. Recurring schedules are deferred: cron syntax,
timezones and DST, missed-run/catch-up behavior, overlap policy, occurrence
deduplication, and leader election form a separate operational contract.

Reason: jobs are a north-star battery and require production durability, crash
semantics, and operations to be credible. PostgreSQL reuses the generated
application's mandatory infrastructure while keeping every schema and state
transition inspectable and replaceable.

## D021 — Request policy is explicit application code over a bounded kernel

**Status:** accepted for v0.7

The next format boundary strengthens validation and middleware as one complete
request path. Application-owned request types explicitly decode JSON and
URL-encoded forms, normalize values, and invoke reusable exported validation
rules. GoForge will not infer rules from tags, reflect over arbitrary structs,
discover validators, or merge browser and API controllers through content
negotiation. A structured validation result adds stable rule codes while the
format-6 `validation.Errors` surface remains compatible.

HTTP middleware configuration is constructed once, validated before serving,
and defensively copied. Credentialed wildcard CORS is invalid rather than being
silently converted into origin reflection. Accepted request IDs are bounded and
available through a typed accessor; logging and recovery share one correlated
completion event. Generated server header/read/write/idle and handler deadlines
plus maximum header bytes are typed configuration with conservative defaults.
Applications can still pass any standard handler to their own `http.Server`,
and streaming routes remain outside buffering/cooperative timeout middleware.

PostgreSQL replaces the generated in-memory authentication limiter because the
production default must hold across replicas and restarts. Keys remain hashed,
counter updates use database time, expired rows are pruned in bounded work, and
the public store interface plus memory implementation remain replacement and
test seams. Forwarded client addresses are used only when the direct peer is in
an explicitly parsed trusted-proxy CIDR; no forwarding header is trusted by
default.

Reason: the accepted pieces work independently, but an unsafe CORS combination,
unbounded transport phases, process-local abuse limits, and an unusable custom-
rule interface contradict the north-star production promise at the public
request boundary. Password lifecycle/session revocation and schema-driven CRUD
are separate coherent milestones and are not partially introduced here.

## D022 — Format upgrades are explicit and releases prove public resolution

**Status:** accepted for v0.7

The v0.7 resource templates depend on the structured validation contract, so
`make:resource` supports format 7 only. Running it in a format-4, format-5, or
format-6 project returns an explicit upgrade error before writing anything.
Generators whose emitted contracts remain compatible continue to support their
older formats. A frozen format-6 application is retained as executable source
and tested against the current framework and CLI.

Fresh format-7 applications pin the public `github.com/ShanilKoshitha/goforge
v0.7.0` runtime module. That lightweight unsigned tag remains immutable. The
first public clean-cache smoke exposed its missing generated checksum, so the
fix shipped as a separate lightweight unsigned `v0.7.1` CLI tag rather than
moving or rewriting `v0.7.0`. The v0.7.1 scaffold carries the verified v0.7.0
module checksums and passed an empty-directory smoke without a local `replace`.

Reason: silently writing newer APIs into an older project is worse than an
explicit migration boundary, and checkout-only tests cannot prove that a public
user can resolve the generated module.

## D023 — Credential generations revoke sessions without coupling the store

**Status:** accepted for v0.8 implementation

Fresh format-8 users carry a monotonically increasing `credential_version`.
Every authenticated session records the version observed when authentication
completed, and the explicit API and browser middleware compare it with the
current user on every protected request. Password change conditionally replaces
the observed hash, increments the version, rotates the caller's session, and
writes the new version into that session. Sign out everywhere increments the
version unconditionally and destroys the caller's session, so it remains
linearizable with a concurrent conditional password change. Any older cookie
is rejected and its server state invalidated on its next use, including by
another process or after restart.

The generic session store remains an opaque ID/payload store rather than gaining
application-specific user columns or revocation methods. Expired and revoked
physical rows are removed through the existing bounded expiry pruning; logical
revocation is immediate because authorization already loads the current user.
Device inventory and per-session administration are deferred until their user
experience is deliberately designed.

Transparent rehash uses a conditional old-hash replacement and does not advance
the credential version because the password itself did not change. A conflict
reloads and verifies the latest hash once: a concurrent equivalent rehash may
still authenticate, while a concurrent password change cannot be overwritten.
Failed verification of an older hash is padded to the current work policy so
transparent migration does not expose which accounts still have weaker storage
parameters.

The session runtime gains a backward-compatible explicit policy with separate
idle and absolute lifetimes. Issuance time is stored inside the opaque payload;
saves can slide idle expiry only up to the original absolute deadline. Fresh
applications validate both environment durations at startup. Existing callers
of `session.NewManager` retain their prior idle-only contract. Deployments must
keep application and database clocks synchronized because absolute issuance is
recorded by the application while durable expiry is enforced by both layers.

Credential-change rotation may race with a stale request invalidating the old
row. The explicit `RegenerateOrCreate` path lets the already-authenticated
winning request create only its independent new ID if atomic rotation reports
the old row missing. Ordinary `Regenerate` remains fail-closed so a stale
request cannot recreate a session after logout or another rotation. Stale
authentication failures remove pending session-cookie headers instead of
emitting deletion cookies, because an out-of-order browser response cannot
conditionally delete only an old cookie. Explicit logout still sends a deletion
cookie, and stale IDs remain unusable server-side.

Reason: credential generations provide constant-cost, cross-process revocation
using the database read already required by authentication, without teaching a
generic session package about application users. Compare-and-swap credential
writes and a non-sliding absolute deadline close the important recovery races
while keeping every policy and mutation visible in ordinary Go and SQL.

## D024 — v0.8 uses a runtime tag followed by a checksum-bearing CLI tag

**Status:** accepted for v0.8

The public lightweight unsigned `v0.8.0` tag fixes the reviewed runtime and
format-8 generator source at commit `cf4a66d`. After that immutable module was
available through the Go proxy and checksum database, the scaffold requirement
and checksums were updated to v0.8.0 and the CLI was released as lightweight
unsigned `v0.8.1` at commit `c4d206a`.

Both commits passed public race, vet, build, and twice-run fresh PostgreSQL
application workflows. A separate clean module cache then installed the public
v0.8.1 CLI, generated without a local replacement, verified modules, and passed
the generated tests, vet, and command builds.

Reason: a module cannot embed its own public zip checksum before the tag exists.
Keeping the runtime tag immutable and shipping only the resolved checksum pin in
a patch CLI tag makes that bootstrap boundary explicit and reproducible.

## D025 — Mutation and operational safety fail closed across processes

**Status:** accepted after v0.8

Every `forge make:*` operation takes one operating-system-backed lock on the
project manifest before reading or publishing managed state. The lock is shared
by all generator kinds, is released by the OS after a crash, and waits with the
caller's cancellation context. This deliberately serializes the short planning
and publication transaction while leaving unrelated builds and edits alone.

The CLI root derives cancellation from interrupt and termination signals.
Delegated project commands run in an isolated process group and receive bounded
graceful tree termination followed by forced cleanup, so `go run` descendants
cannot outlive `forge`. Root build artifacts are no longer ignored; local CLI
builds belong under `.tmp` or another explicit output directory so a stale
`forge` executable is visible rather than silently preferred by an agent.

Generated applications distinguish `/health` liveness from `/ready` readiness.
Readiness uses a short deadline, PostgreSQL ping, and the migration manifest;
applied migrations retain and verify their recorded name and exact up-script
checksum. Legacy rows use an explicit one-time checksum backfill after their
name matches, then the ledger enforces non-null checksums so it cannot be
silently re-baselined. Production is the generated environment default, local HTTP mode
must be opted into, and Compose exposes PostgreSQL only on loopback.

Default PostgreSQL registration binds account insertion, account-throttle reset,
and initial session persistence to one transaction. Alternate session or
limiter stores must supply an explicit transaction coordinator outside tests;
startup otherwise fails closed. Queue failure storage uses
allowlisted operator diagnostics instead of handler-controlled errors or panic
data. A handler that ignores cancellation has a bounded grace window; after it
expires the worker stops heartbeating and exits without mutating the fenced
delivery, allowing another worker to reclaim it after lease expiry.

Reason: local correctness under one coordinator or one happy process is not a
production guarantee. Cross-process mutation, shutdown, readiness, schema
identity, authentication commits, and durable failure surfaces must retain safe
behavior when processes overlap or dependencies fail.

## D026 — Test and build are non-mutating freshness gates

**Status:** accepted for v0.9

`forge test` and `forge build` are no-argument project commands for formats 4
through 8. Both first prove that the inspectable ORM artifact is current and
then prove that compiled views are current. Formats 5 through 8 delegate view
checking to the application-owned `cmd/views` compiler so custom functions and
the production renderer retain one contract; format 4 retains its frozen
compiler semantics. A missing, stale, or invalid generated artifact stops the
workflow with the explicit generation command needed to repair it.

These preflights never write generated source. `forge test` then delegates
exactly to `go test ./...`. `forge build` compiles only `./cmd/server` with
`-trimpath` into a temporary file beside the destination, then publishes
`bin/app` on Unix or `bin/app.exe` on Windows only after compilation succeeds.
A failed or cancelled build removes its temporary file and preserves any
last-good canonical binary byte for byte.

The commands do not start services, apply migrations, modify `.env`, select a
test database, or infer deployment policy. `forge test` intentionally does not
accept package or test flags, and `forge build` does not add targets, tags,
cross-compilation, or release packaging. Developers use `go test` and `go build`
directly when the opinionated default does not fit. Workers and the console also
retain their direct build commands. No runtime API or generated source contract
changes, so format 8 remains current and formats 4 through 8 can share the
workflow.

Watch/reload, browser LiveReload or HMR, frontend assets, a multi-process
development supervisor, service orchestration, migration automation, and a
diagnostic `doctor` command remain separate milestones. `forge serve` keeps its
visible compile-then-`go run ./cmd/server` behavior.

Reason: a thin alias alone would not improve correctness, while silently
regenerating source during a test or release build would hide a dirty checkout.
Explicit non-mutating checks close the stale-artifact gap and the staged binary
closes the partial-publication gap without replacing Go's tools or adding
runtime magic.

## D027 — v0.9 uses a runtime tag followed by a checksum-bearing CLI tag

**Status:** accepted for v0.9

The lightweight unsigned `v0.9.0` tag fixes the reviewed runtime and source at
merge commit `81cffa3`. Only after that immutable tag resolved through the
public Go proxy and checksum database did the distribution scaffold move its
framework requirement to v0.9.0. The CLI patch release reports v0.9.1 and
embeds both public checksums in generated `go.sum` files.

The v0.9.1 distribution gate builds the CLI, generates without `--replace`,
requires the public v0.9.0 runtime, runs `forge test` and `forge build`, and
checks the canonical binary. The existing public v0.8.1 application smoke
remains alongside it as backward-compatibility evidence.

Reason: the framework cannot know its own public module zip checksum before an
immutable tag exists. Separating the runtime/source tag from the installable
checksum-bearing CLI patch keeps generated applications reproducible without
rewriting or weakening the public checksum contract.

## D028 — `forge serve` owns a last-good development loop

**Status:** accepted for v0.10 implementation

`forge serve` becomes the opinionated edit-to-render development command. It
validates the project before side effects, compiles views with the existing
format-specific contract, builds an ordinary `./cmd/server` executable into a
unique operating-system temporary directory, starts that executable, and then
watches application inputs. The exact one-shot escape hatch remains `go run
./cmd/server`; a separate `forge dev` command is reserved for a future
multi-process workflow that may include workers and frontend assets.

The watcher uses recursive content snapshots rather than platform notification
APIs. This intentionally trades sub-millisecond events for identical atomic-save,
new-directory, OneDrive, Linux, and Windows behavior without another dependency.
It watches Go, Forge view, SQL, module, environment, and project-manifest inputs;
repository metadata, dependencies, caches, build outputs, and GoForge-generated
artifacts are excluded. Stable bursts are serialized, and a source generation
observed during compile or build makes that candidate stale rather than lost.

Every rebuild compiles views first and stages a candidate server while the
last-good server remains active. A compiler or Go build failure reports its
ordinary diagnostic, removes its candidate, keeps the current server and
generated view artifact intact, and waits for the next edit. Only a successful
candidate whose `/health` endpoint proves liveness may replace the active
server. This deliberately does not redefine `/ready`: database and exact
migration readiness remain dynamic application concerns. A SQL edit may make
`/ready` return 503 until the developer explicitly runs `forge migrate`, after
which readiness recovers without a server restart. One process owner reuses the
existing Unix process-group and Windows Job Object controls so cancellation cannot leak
the server or its descendants. An unexpected active-server exit is terminal;
the supervisor does not hide application crashes behind an automatic loop.

Candidate view compilation never writes the canonical generated artifact.
Formats 5 through 8 first build the application-owned compiler, then run it
against an isolated copy of the Forge sources; format 4 compiles in memory. The
server build consumes that candidate with Go's standard `-overlay` mechanism.
Publication acquires the same interprocess lock as every mutating make, view,
and ORM generator and retains it through proxy promotion, rollback, or commit,
so an identical-content concurrent write cannot create an ABA race. Cleanup
retries transient rollback failures before the candidate becomes unreachable.

No generated source or production runtime contains the watcher. Format 4 keeps
its frozen view semantics, formats 5 through 8 continue using the
application-owned `cmd/views` and function map, and format 8 remains current.
The command does not start services, apply migrations, change configuration,
generate ORM source, or start workers. Browser LiveReload and frontend asset
handling remain separate milestones because response rewriting and CSP are an
independent security boundary.

Reason: the template language already has Blade/Twig-class composition and
diagnostics, but compiled templates are not pleasant if each edit needs a manual
restart. Moving that loop into the CLI improves the default without placing file
discovery, source compilation, or reload machinery in the application binary.

## D029 — v0.10 uses a runtime tag followed by a checksum-bearing CLI tag

**Status:** accepted for v0.10

The lightweight unsigned `v0.10.0` tag fixes the reviewed development-loop
runtime and source at merge commit `f6a26e5`. Only after that tag resolved
through the public Go proxy and checksum database did the distribution scaffold
move its framework requirement to v0.10.0. The CLI patch release reports
v0.10.1 and embeds both public checksums in generated `go.sum` files.

The v0.10.1 distribution gate builds the CLI, generates without `--replace`,
requires the public v0.10.0 runtime, and runs the generated test, build, and
development-server workflows. The existing released-format compatibility
smoke remains alongside it.

Reason: a generated application must resolve reproducibly without depending on
the framework checkout. Separating the runtime tag from the checksum-bearing
CLI patch preserves that contract while keeping both release commits immutable.

## D030 — Resource fields are one-shot generator input, not application schema

**Status:** accepted for v0.11 implementation

`forge make:resource` accepts a repeatable `--field` option whose value is
`<name>:<type>[:required|nullable]`. The bounded v0.11 type vocabulary is
`string`, `text`, `integer`, and `boolean`. Fields are required unless marked
`nullable`; the explicit `required` spelling is accepted for readability. The
CLI rejects unknown types or modifiers, conflicting nullability, duplicate or
unsafe names, and collisions with generated identity, ownership, timestamp,
version, and relationship fields before it writes anything.

The ordered field descriptions exist only for that generator invocation. They
produce ordinary application-owned Go models, a paired SQL migration, typed
transport decoding and validation, a generated `Attributes` value used by the
repository, JSON and browser controllers, Forge views, and tests. No field list
is stored in `.forge`, embedded into the binary, loaded during startup, or
interpreted during a request. The emitted Go and SQL become authoritative and
remain directly editable. Calling the command without `--field` preserves the
existing required `name` field workflow.

New field-aware resource output requires the record's optimistic version on
both JSON and browser updates. It never accepts generated IDs, owner IDs,
timestamps, versions, or relationship values through `Attributes`; the owner
continues to come from the authenticated request, and every read and mutation
continues to include the owner predicate. Nullable fields retain explicit typed
state across JSON, forms, repository values, ORM changes, PostgreSQL, and
rendering. Because updates are full replacements, missing, `null`, and empty
nullable values deliberately clear to SQL `NULL`; numeric `0` and boolean
`false` remain present values and never collapse into absence.

This is an additive generator capability within the existing format-8
application contract, so v0.11 does not bump the project format or introduce an
upgrade command. An application generated by the public v0.10 CLI must continue
to work with the current runtime and compatible CLI commands. Existing
format-8 resources are not rewritten, and the established no-field generator
contract remains compatibility evidence.

Reason: repeated command-line values are shell-visible, deterministic, and
sufficient to remove the most repetitive scalar CRUD work. Persisting them
would create a competing schema whose reconciliation rules outlive generation;
runtime interpretation would reintroduce exactly the reflection and hidden
discovery that GoForge excludes. A small one-shot vocabulary improves the
default path while leaving the generated application as conventional Go and
SQL with complete escape hatches.

## D031 — v0.11 uses a runtime tag followed by a checksum-bearing CLI tag

**Status:** accepted for v0.11

The lightweight unsigned `v0.11.0` tag fixes the reviewed typed-resource
runtime and generator source at merge commit `2af6000`. After that tag resolved
through the public Go proxy, the distribution scaffold moved its framework
requirement to v0.11.0 and embedded checksum
`h1:PpJ4gjHJxAM8tZmQ7Ajh++Fce05+w0l9la6F4oe8Dcc=` plus the unchanged module
file checksum. The checksum-bearing CLI patch reports v0.11.1.

The distribution gate installs the released v0.11.0 CLI for frozen format-8
compatibility, then uses the candidate v0.11.1 CLI to generate implicit legacy
and mixed typed resources without `replace`, verify the public module, test,
build, and serve the generated application.

Reason: a scaffold must pin an immutable public module whose checksums are
already available. The two-step release keeps every tag reproducible while the
generated application remains independent of the framework checkout.

## D032 — Account recovery uses hashed tokens and an encrypted mail outbox

**Status:** accepted for v0.12 implementation

Fresh format-9 applications add a complete forgotten-password workflow for the
browser and JSON API. A reset request always returns the same public result,
whether or not the normalized address belongs to an account. Known accounts
receive a high-entropy, short-lived `selector.secret` token. PostgreSQL stores
the selector and a SHA-256 digest of the secret, never the presented token.
Issuing a newer token invalidates the account's prior token. Reset consumes the
token and advances the user's credential version with the password replacement
in one transaction, so replay and concurrent submissions have at most one
winner and every existing session becomes invalid immediately.

Issuance deliberately has two time boundaries: all account-dependent database,
rendering, outbox, and dispatch work receives a 400ms child context, while the
public response is padded with cryptographic jitter to 650–850ms. A timeout or
internal failure rolls back and remains silent at this boundary. This prevents
a slow lock or persistence failure from turning the endpoint into an account
existence oracle; applications can replace this ordinary service when their
latency budget or observability model differs. A fixed-stage observer retains
an operational signal without receiving the address, token, message, or cause.
The service dispatches at most one observer callback concurrently and never
waits for it, so a slow or panicking handler cannot alter the public envelope.

Mail is an explicit framework boundary rather than controller-owned SMTP.
The public `mail.Sender` contract and SMTP adapter accept fully rendered text
and HTML messages with bounded connection and operation lifetimes. Fresh
applications own sender construction, password-reset mail data and rendering,
and a generated `forge make:mail` starting point. HTML uses the same compiled
Forge view artifact as browser pages; plain text remains an explicit standard
`text/template` embedded by the application. Constructors and direct
`html/template`, `text/template`, or a replacement sender remain complete
escape hatches.

Reset delivery is durable without placing the raw reset URL in the job table.
The issuance transaction encrypts the rendered envelope with an
application-configured 256-bit outbox key, inserts the outbox row, and dispatches
an application-owned mail job whose payload contains only the outbox ID. The
worker decrypts, sends, and removes the envelope. Queue delivery and SMTP remain
at least once: a crash after SMTP acceptance can produce a duplicate message,
but the shared reset token is single-use. Logs, errors, queue administration,
and database columns must not disclose credentials, reset tokens, message
bodies, or recipient addresses.

The application URL and SMTP security policy are validated before serving or
working. TLS is required outside explicit local development; plaintext local
SMTP is a conspicuous opt-in used with the generated loopback-only mail catcher.
Source- and account-scoped throttles bound issuance, while independent source-
and selector-scoped throttles bound reset attempts. Invalid, expired, consumed,
superseded, throttled, and cross-account tokens share one disclosure-safe
response contract.

Each issuance transaction also schedules an application-owned cleanup job with
the same opaque outbox ID and an explicit seven-day delay. Successful delivery
removes the envelope immediately; terminal delivery failure or administrative
removal cannot retain its encrypted contents indefinitely. The retention
constant, job definition, handler, and dispatch helper are all generated Go and
remain directly replaceable.

This milestone does not add email verification, magic links, MFA, OAuth, SMS or
push notifications, marketing/bulk mail, a mail dashboard, or a general event
bus. Recurring scheduling and required belongs-to resource generation remain
the next documented candidates after this account-lifecycle gap is accepted.

Reason: generated authentication already covers registration, login, password
change, and immediate session revocation, but a production application cannot
recover a forgotten credential. This workflow composes existing configuration,
validation, views, jobs, transactions, PostgreSQL, and security boundaries while
keeping every policy and integration in ordinary application-owned Go and SQL.

## D033 — v0.12 uses a runtime tag followed by a checksum-bearing CLI tag

**Status:** accepted for v0.12

The lightweight unsigned `v0.12.0` tag fixes the reviewed account-recovery
runtime and format-9 generator source at merge commit `bc1dd2b`. Merged-main CI
run 34294151854 passed Linux/PostgreSQL and native Windows before publication.
The public Go proxy resolves the module checksum
`h1:/wmB+d+3au9s6Ei3iJLPgmfE3DIMyBO3kFaeg0hXEX0=` and unchanged module-file
checksum `h1:o585rLjoR42pVE79JjdmwrIvum0C5QRRilYqZAJcAkM=`. The checksum-bearing
CLI patch reports v0.12.1 and generates applications that pin v0.12.0.

The distribution gate retains the public v0.11.0 CLI and frozen format-8
application compatibility proof. Separately, the candidate v0.12.1 CLI
generates a format-9 application without `replace`, verifies the public v0.12.0
module and exact checksums, creates legacy and typed resources plus
application-owned mail, and passes tidy, tests, vet, server and worker builds,
serve, and direct Go module verification.

Reason: a scaffold must pin an immutable public module whose checksums already
exist. The two-step release keeps each tag reproducible and proves generated
applications do not depend on the framework checkout.

## D034 — Required belongs-to is generated policy, not runtime schema

**Status:** accepted for v0.13 implementation

Fresh applications move to project format 10. Formats 8 and 9 keep their
compatible scalar generators, but relation flags fail before writing because
their resource tables do not promise the owner/identity candidate key needed
for the database invariant. `forge make:resource` accepts a repeatable
`--belongs-to <lower_snake_name>:<ExistingResource>` option. The target must be
an already-generated owner-scoped resource whose current application model has
the conventional integer identity and owner fields. Target metadata is used
only to locate that source; Go source remains authoritative and an incompatible
human edit fails generation before any write. Relationships are required in
this milestone. The generated foreign-key name is `<name>_id`, and all scalar,
database, Go, transport, and presentation name collisions are rejected.

The option produces an ordinary protected foreign-key field, explicit
`belongs_to` model field, `BIGINT NOT NULL` column, index, separate
`AssociationIDs` value, exact JSON/form input, field-level validation,
repository policy, eager loader, browser choice loader, and rendered
relationship output. Scalar `Attributes` stays relationship-free. Each
format-10 resource table exposes `UNIQUE (user_id, id)`; a child uses
`FOREIGN KEY (user_id, target_id) REFERENCES targets (user_id, id) ON DELETE
NO ACTION`. Direct parent deletion is therefore an intentional conflict rather
than surprising child deletion, while same-statement account cascades can
settle before the constraint is checked.

Application repositories verify the target with both identity and authenticated
owner predicates before create or update. Missing and cross-owner targets share
one disclosure-safe validation result. The composite foreign key also prevents
cross-owner attachment through custom SQL and closes the parent-deletion race:
either the child write wins and deletion is restricted, or deletion wins and
the child write is reported as an invalid relationship. Reads use explicit generated ORM loaders
with owner-scoped target queries in bounded batches. Browser choices are bounded
and owner-scoped; generated presentation code is the visible place to replace
the conservative `Resource #ID` label.

Relationship declarations remain one-shot command input. They are not added to
`.forge/resources.json`, embedded as a schema, discovered from packages at
runtime, or used for lazy loading. Generated Go and SQL become application-owned
and directly editable. Existing resources are never rewritten; direct ORM
descriptors, handwritten SQL, custom repositories, controllers, and views remain
complete escape hatches.

Nullable relationships, inverse generation, nested routes, collection
relationships, configurable delete actions, searchable widgets, and regeneration
are deferred. Recurring schedules remain the next separately gated operational
milestone after this core resource workflow passes.

Reason: GoForge already has a capable reflection-free relationship engine, but
its flagship resource generator cannot produce a normal parent/child domain
slice. Exposing one conservative required relation through static generation
turns proven ORM machinery into Laravel/Rails-class productivity without adding
runtime magic or a competing application schema.

## D035 — v0.13 uses a runtime tag followed by a checksum-bearing CLI tag

**Status:** accepted for v0.13

The lightweight unsigned `v0.13.0` tag fixes the reviewed required-belongs-to
runtime and format-10 generator source at merge commit `9078e56`. Merged-main
CI run 34371037790 passed Linux/PostgreSQL and native Windows before
publication. The public Go proxy resolves module checksum
`h1:ky4g5QeWdG2M3rZdeYd7pkD+8Cz1dVHHWlqewm/SMcw=` and unchanged module-file
checksum `h1:o585rLjoR42pVE79JjdmwrIvum0C5QRRilYqZAJcAkM=`. The checksum-bearing
CLI patch reports v0.13.1 and generates applications that pin v0.13.0.

The distribution gate retains the released v0.11.0 format-8 compatibility
proof. Separately, the v0.13.1 CLI generates a format-10 application without a
`replace`, verifies the public v0.13.0 module and exact checksums, creates both
typed scalar and required relationship resources plus application-owned mail,
and passes tidy, tests, vet, server and worker builds, serve, and direct Go
module verification.

The release smoke also exposed a transient Windows/OneDrive sharing violation
while a failed resource generation removed a newly emptied directory. Managed
files and metadata were already restored, but leaving the directory violated
the generator's all-or-nothing contract. Resource rollback now rechecks that a
directory remains empty before each bounded removal retry, preserves any new
editor content, and reports the final error instead of hiding a persistent
failure.

Reason: the scaffold must pin an immutable public module whose checksums already
exist. The two-step release keeps each tag reproducible and proves generated
applications do not depend on the framework checkout.

## D036 — Recurring schedules are explicit durable job materializers

**Status:** accepted for v0.14 implementation

Recurring schedules are code-owned definitions in an ordinary application
registry. Each definition has a stable versioned name, a strict five-field cron
expression, an IANA time zone, a bounded misfire grace, a typed job definition,
and a deterministic payload factory. The framework performs no package
scanning, reflection, annotation discovery, or interpretation of a persisted
application schema.

A separate scheduler process evaluates definitions and materializes typed jobs
into the existing durable queue; it never executes handler effects and is not
embedded in the HTTP server or queue worker. PostgreSQL stores one cursor row
per schedule. Competing processes coordinate with short per-row transactions
and `FOR UPDATE SKIP LOCKED`, so unrelated schedules can progress without a
global leader. Dispatch through `job.Dispatcher.Using(tx)` and cursor advance
commit atomically. A crash, cancellation, encoding failure, enqueue failure, or
state-update failure therefore leaves the occurrence available for retry.

Cron uses one-minute resolution. Day-of-month and day-of-week use traditional
cron OR semantics when both are restricted. Civil time is evaluated in the
declared location: nonexistent spring-forward minutes do not occur, while both
distinct instants in a fall-back repeated minute are eligible. A newly observed
schedule may materialize the current civil-minute occurrence when it is inside
the grace window, but does not replay older history.

Missed occurrences coalesce to the latest occurrence inside the inclusive
grace window; if none remains eligible, the scheduler records a skip and moves
the cursor after database time. The default and only v0.14 overlap policy uses
the existing active-job unique key `goforge:schedule:<schedule-name>`. A queued,
leased, or retrying prior job suppresses the next materialization, records that
outcome, and still advances the occurrence cursor. Queue delivery and handler
effects remain at least once, not exactly once.

The durable row stores a SHA-256 fingerprint of every declarative behavioral
input, including canonical cron, location, job name and policy, queue, payload
strategy, grace, and overlap policy. Static payload fingerprints also include
their canonical encoded bytes. Dynamic factories use a distinct strategy
marker and rely on the versioned schedule name as their deployment contract. A
same-name fingerprint mismatch fails closed without dispatch or automatic state
rewrite. Rows missing from the explicit registry remain inspectable but inert.
Changing a deployed schedule requires a new versioned name in v0.14; automatic
in-place replacement is deferred because mixed scheduler releases must not
alternate between definitions.

Scheduler releases with identical registries may overlap, and a purely
additive registry change is safe because older processes cannot see the new
definition. Removing or replacing a definition is different: an old process
can continue materializing the removed name while a new process materializes
its replacement. v0.14 therefore requires a scheduler-only maintenance window
for removals and replacements: stop every old scheduler, deploy the changed
registry, then start the new schedulers. HTTP servers and queue workers do not
need to stop. A durable retirement protocol for mixed-registry rolling deploys
is deferred rather than implied by the per-row coordination guarantee.

The release is split to preserve a compileable public scaffold at every main
commit. First, the schedule runtime and PostgreSQL adapter merge while fresh
applications remain format 10; lightweight tag v0.14.0 then makes that package
public. Second, the checksum-bearing v0.14.1 CLI moves fresh applications to
format 11 and pins v0.14.0. This avoids generating imports that do not yet exist
in the scaffold's public module dependency.

Database-authored or runtime-generated schedules, per-tenant definitions,
seconds/macros/RRULE, arbitrary catch-up, overlap, occurrence history,
dashboards, alternate stores, runtime discovery, HTTP-server embedding,
durable mixed-registry retirement, and unified development orchestration
remain outside this milestone.

Reason: delayed jobs alone do not provide the operational contract developers
expect from Laravel, Symfony, Rails, or Spring. A small durable materializer
composes the queue and transaction seams GoForge already owns while keeping
every definition, payload, process, and database boundary visible and
replaceable.

## D037 — Schedule deadlines are cooperative and generated targets fail closed

**Status:** accepted for v0.14.2–v0.14.3 hardening

The scheduler adds `schedule.DynamicContext` while retaining the existing
`schedule.Dynamic` API. Internally every payload builder receives the scheduler
operation context. Static builders and legacy dynamic builders remain source
compatible, while the new typed callback can stop database access or other
blocking work when the operation deadline expires. The definition checks
cancellation before and after payload construction and dispatches with the same
context and transaction executor. Both dynamic APIs retain the existing
`dynamic-factory-v1` fingerprint marker so adopting cancellation does not create
false durable definition drift.

GoForge does not run an uncooperative callback in an abandoned goroutine. That
would release the database transaction while application work continued,
permit side effects after rollback, and leak one goroutine per stuck callback.
Legacy `Dynamic` is therefore retained for prompt computation-only factories;
blocking or I/O-capable factories use `DynamicContext`. A hard limit regardless
of application cooperation would require process isolation or a two-phase
reservation design and is outside this compatibility hardening milestone.

Generated job wiring exposes a dependency-free set of registered job names from
a dedicated generated manifest package. Keeping that API outside the existing
`jobs` package avoids retroactively reserving identifiers in format-11 projects.
The schedule registry validates every definition's target name against that set
when the scheduler or schedule-list command starts. The set is derived from the
same generator metadata that emits handler registration, including built-ins,
so no handler dependencies are constructed and no packages are scanned or
reflected. An absent target name fails before durable evaluation. This check
does not claim to identify a manually shadowed same-name definition or prove
that deployed workers select every configured queue; those remain visible
application and deployment contracts.

Password-recovery acceptance no longer compares two sequential HTTP wall
times. `Request` has one public return path through an envelope around its
private, branch-complete work method. Each request therefore selects one
absolute response deadline before account-dependent work, runs that work inside
the shorter child context, and awaits the selected deadline once after it
returns. Pure tests exhaust the jitter bounds and prove exact select-work-wait
ordering; live PostgreSQL acceptance proves identical known, absent, and forced
failure responses and headers, successful durable delivery, and no state for
absent accounts. Relaxing the old timing threshold or repeatedly sampling CI
latency is not accepted evidence of enumeration resistance.

Publication remains compile-safe. v0.14.2 publishes the backward-compatible
runtime factory seam and deterministic acceptance first. v0.14.3 then pins that
immutable module from fresh format-11 applications, uses `DynamicContext` in
the generated journey, and activates dependency-free target validation. Neither
stage changes project format or database schema.

Reason: an operation timeout that application code cannot observe, a schedule
target absent from generated worker wiring, and a flaky security stopwatch all weaken the
north-star promise of production-shaped defaults. Explicit typed context,
generated metadata, and deterministic invariants close those gaps without
runtime magic or hidden state.
