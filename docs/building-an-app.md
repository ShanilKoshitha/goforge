# Building an application

Create a PostgreSQL application with `forge new`. The generated server loads
typed settings, connects through pgx's visible `database/sql` adapter, constructs
the application explicitly, installs middleware, and registers routes.

The generated `.env` targets the PostgreSQL service in `compose.yaml` and contains
a unique local session secret. Start that service explicitly when needed:

```sh
docker compose up -d
```

## Add an endpoint

Generate a controller:

```sh
forge make:controller Users
```

Edit `internal/http/controllers/users_controller.go`, inject dependencies into
`NewUsersController`, and add the route in `routes/routes.go`:

```go
users := controllers.NewUsersController(repository)
router.GET("/users", users.Index)
```

That explicit line is intentional: it is searchable, checked by the compiler,
debuggable without framework tooling, and trivial to replace.

## Generate an authenticated CRUD resource

```sh
forge make:resource Issue
```

This creates an application-owned vertical slice under
`internal/resources/issue`: model, owner-scoped PostgreSQL repository, request
validation, JSON and browser controllers, controller tests, and paired SQL
migration. It also creates `.forge.html` index, new, show, edit, and shared form
templates under `resources/views/pages/issues` and regenerates the clearly
marked route and compiled-view registries. Human-owned files are preflighted and
never overwritten.

Fresh format-13 and format-14 resources also contain `authorization.go`. Its
typed `AuthorizeFunc` handles list, create, view, update, and delete decisions
for both controllers. The generated function grants owner access for every known action;
return `AccessAll` for a deliberate privileged action or `AccessDenied` to stop
an authenticated action with HTTP 403. The controller derives the repository
scope from the authenticated user, and update/delete apply it in their SQL rather
than trusting a prior fetch. Unknown decisions and invalid scopes fail closed.
The policy, route injection, repository predicates, and tests are ordinary
application Go and can be edited or replaced directly.

Run the generated migrations and development server through the project
commands:

```sh
forge migrate
forge dev
```

`forge migrate` delegates to the application-owned console. Format-11 through
format-14
`forge dev` starts the watched HTTP server, durable queue worker, and recurring
scheduler together. It labels each service's output and reports the stack ready
only after all three existing startup signals succeed. Any unexpected service
exit stops its peers, and Ctrl-C waits for the complete owned process tree.

`forge serve` remains the HTTP-only development command. It
compiles views with the application's own function map, stages an ordinary Go
server binary outside the repository, and keeps one public address while it
watches Go, `.forge.html`, SQL, module, environment, project-manifest, and every
regular format-12-or-newer file below `resources/assets/files`. Eligible full HTML browser pages carry a development-only same-origin
client and reload automatically after a valid replacement is completely
committed. Failed edits leave the last-good page and server in place. The proxy
does not rewrite compressed, streamed, ranged, downloadable, `no-transform`,
or non-document responses.

A broken view or Go edit prints its normal diagnostic while the last-good
server remains reachable. Correcting the source rebuilds automatically. View
compilation is serialized with generators, stable edit bursts converge on the
newest source, and Ctrl-C stops every owned process. `go run ./cmd/server`
remains the exact one-shot escape hatch when watching or the development proxy
does not fit.

`forge dev` does not start Compose/PostgreSQL/Mailpit, apply migrations,
generate a stale ORM artifact, change `.env`, or run a hidden frontend build. Its
browser reload client exists only at the development proxy; generated source
and production binaries remain unchanged. Worker and scheduler source or
registry changes require restarting `forge dev`; the server retains its
last-good hot replacement loop.
Candidate promotion checks `/health` liveness;
`/ready` continues to report PostgreSQL and exact migration readiness. A changed
migration may therefore leave `/ready` at 503 until you run `forge migrate`.
Run those operations explicitly.

Candidate listener ownership is checked without extra tools on Linux and
Windows, and with the system `lsof` on macOS and other supported Unix targets.
If `lsof` is unavailable on those targets, use `go run ./cmd/server` directly.

Register at `/register`, sign in at `/login`, and use the generated browser
resource at `/app/issues`. Existing JSON endpoints remain at `/auth/*` and
`/issues`; handlers do not silently switch behavior based on content negotiation.

## Authenticate a machine client

Fresh format-14 applications let a signed-in user create a named, expiring
personal API token at `/settings/security` or with `POST /auth/tokens`. Creation
requires the current password and returns the complete
`goforge_pat_<selector>.<secret>` value once. Send that value as one exact
`Authorization: Bearer ...` header to `/auth/me` or generated JSON resources.
The request then uses the same application-owned user, policy, and repository
scope as cookie authentication.

Token management stays session-only, and browser management is CSRF protected.
An invalid Authorization header is authoritative: it never falls back to a
valid cookie or touches its session. The generated migration stores only the
public selector and SHA-256 digest. Read the
[personal API token guide](api-tokens.md) for lifecycle, revocation, and
replacement seams.

Fresh format-10 through format-14 JSON resource indexes are bounded and paginated. `GET /issues`
defaults to `page=1&per_page=20`; each parameter must appear at most once and be
a positive integer. Page numbers are capped at 10,000 and page sizes at 100.
The response is `{"data": [...], "pagination": {"page": 1, "per_page": 20,
"has_previous": false, "has_next": false}}`. Formats 8 and 9 retain their
original complete-list JSON contract.

GoForge compiles layouts, sections, includes, static components, strict props
and slots, control directives, page-local stacks, and explicit form helpers into
the inspectable `resources/views/views_gen.go` artifact. Source mappings retain
the original file and composition chain for diagnostics. After editing
`.forge.html` source, run `forge views:compile`; use `forge views:compile
--check` in CI. `forge serve` performs the same function-aware compilation
before starting.

Generate a component with `forge make:component Notice`. Application template
functions live in `resources/views/viewfuncs/functions.go`; the project compiler
and production renderer use that same editable `template.FuncMap`. See the
[view language reference](view-language.md) for the complete bounded grammar and
standard-library escape hatches.

## Ship frontend assets

Fresh format-12-or-newer applications keep opaque production inputs in
`resources/assets/files`. The visible `resources/assets` package embeds the
exact bytes, validates a bounded inventory at startup, resolves logical names,
and supplies the standard `http.Handler` mounted in `routes/routes.go`. The
default layout calls `asset "app.css"` and `asset "app.js"`; those names are
declared in the application-owned `assets.Required` list so the build gate and
startup fail before serving a binary with a missing required asset.

The default stable `/assets/<logical-name>` URLs use strong SHA-256 ETags and
`Cache-Control: public, max-age=0, must-revalidate`, with GET, HEAD, conditional,
and byte-range semantics. Stable revalidation is deliberate: current-only
fingerprinted URLs can break old HTML across rolling deployments unless prior
asset generations are retained. Stable paths preserve availability, not exact
generation consistency: keep asset changes backward compatible across a roll
or use deployment stickiness. A future retained-publication contract may add
immutable fingerprints with exact HTML/asset version pairing.

There is no asset compile command because v0.17 performs no transformations.
`go build ./cmd/server` is the complete direct production path and fails closed
at application startup. The opinionated `forge build` additionally copies the
application into a private temporary source tree, revalidates that exact copy,
and compiles only the validated copy before publication. Tool output, VCS/cache
directories, `node_modules`, and symlinks are excluded; keep Node-produced
browser output in an application directory such as `public/dist` when using
this format-12-or-newer gate. Formats 4–11 retain their in-place build behavior. Use the
direct Go command for a format-12-or-newer monorepo that relies on relative external
replacements, an implicit parent workspace, or symlink traversal.
Replace the application package or route with ordinary
`net/http`, a CDN, or a Node-backed pipeline when those tradeoffs fit.

## Test and build

Run the complete generated application test suite and publish the production
server binary with two opinionated commands:

```sh
forge test
forge build
```

Both commands support project formats 4 through 13 and accept no arguments. They
first check `internal/models/zz_orm_gen.go`, then
`resources/views/views_gen.go`, and for format 12 or newer the embedded asset inventory,
without rewriting application files. A missing, stale, or invalid input stops
before tests or compilation and reports the explicit repair or check command.

After preflight, `forge test` executes exactly:

```sh
go test ./...
```

`forge build` compiles `./cmd/server` with `-trimpath` into a temporary file and
publishes it only after success as `bin/app` on Unix or `bin/app.exe` on Windows.
A failed or cancelled build leaves any existing canonical binary byte-identical.

Nothing prevents using the underlying tools directly. The equivalent explicit
checks and commands are:

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

Use direct `go test` or `go build` for custom packages, flags, build tags,
targets, cross-compilation, or output paths. Build the generated worker with
`go build -trimpath -o bin/worker ./cmd/worker`, the scheduler with `go build
-trimpath -o bin/scheduler ./cmd/scheduler`, and the console with `go build
-trimpath -o bin/console ./cmd/console` when those deployment processes are
needed.

Test and build do not start services, provision a database, change `.env`, or
apply migrations. Run `docker compose up -d` and `forge migrate` explicitly when
the application workflow requires them. Browser LiveReload/HMR, frontend asset
compilation, and environment diagnosis remain separate milestones.

## Run durable background work

Generate a typed, application-owned job and start the generated worker:

```sh
forge make:job SendWelcome
forge queue:work
```

The generator writes the payload, handler, test, explicit registry entry, and
stable versioned wire name. Dispatch through the generated helper, or use
`dispatcher.Using(tx)` to enqueue atomically with domain writes. `job.Delay`,
`job.At`, and explicit active deduplication keys are per-dispatch options.

Delivery is at least once. PostgreSQL leases and generation fencing make crash
recovery safe for queue state, but handlers remain responsible for idempotent
external effects. Operate terminal failures with `forge queue:failed`,
`queue:retry`, and `queue:forget`; payloads are omitted from listings by
default. The worker, migration, registry, dispatcher, store, and observer all
remain visible and replaceable. See [durable jobs](jobs.md) for policy,
configuration, failure behavior, observability, and escape hatches.

## Validate input

Generate a request and bind JSON in the controller:

Clients must send `Content-Type: application/json` (parameters such as
`charset=utf-8` are accepted). Missing or other media types receive HTTP 415;
invalid JSON receives HTTP 400, and validation errors receive HTTP 422.

```go
var request requests.CreateUserRequest
if err := ctx.BindJSON(&request); err != nil {
    return err
}
if errors := request.Validate(); !errors.Empty() {
    return httpx.NewHTTPError(422, "validation failed").WithDetails(errors)
}
```

## Change the HTTP stack

Pass `app.Handler()` to your own `http.Server`, mount a standard handler with
`httpx.Adapt`, or replace the GoForge router entirely. Application lifecycle
helpers are conveniences, not a required host.

## Migrations

Migration files are paired and ordered:

```text
20260904120000_create_orders.up.sql
20260904120000_create_orders.down.sql
```

Embed the directory and construct `migrate.Migrator` with the application's
`*sql.DB`. Generated v0.3 applications explicitly import pgx's `database/sql`
adapter, so the selected driver remains visible in `go.mod` and
`internal/database/database.go`; replace that ordinary code to choose a different
driver.

## Sessions

`session.Manager` keeps data in an explicit `session.Store`; the browser receives
only a cryptographically signed random ID. Use `session.MemoryStore` for local
development and implement the `Get`, `Create`, `Update`, atomic `Rotate`, and
`Delete` store contract for Redis or SQL. Distinguishing creation from update
prevents a stale request from restoring a revoked ID. Call `Save` before writing
the response header. Authentication flows should call `Regenerate` after login
to stage an atomic rotation during `Save`. Cookies are HTTP-only and
SameSite=Lax by default; relaxing HTTP-only requires the deliberately named
`UnsafeAllowJavaScript` option. Secure cookies are also the default; local HTTP
development must opt out explicitly with `UnsafeAllowHTTP`.

Generated applications use the visible PostgreSQL session adapter in
`internal/auth/session_store.go`. Registration and login rotate the signed random
session ID; resource routes are wrapped with the explicit auth middleware and
repositories scope every query by the authenticated user ID.

Session writes prune up to 100 expired rows, including abandoned sessions. An
expired session presented by a client is also removed on access.
