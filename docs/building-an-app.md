# Building an application

Use this guide to add features to the PostgreSQL application you created in the
[Quick Start](quick-start.md). Run the commands from the application directory.

- Add [routes](#add-an-endpoint), [CRUD resources](#generate-an-authenticated-crud-resource),
  [input validation](#validate-input), and [migrations](#migrations).
- Build browser features with [resource browsing](#browse-resources-and-select-relationships-at-scale),
  [views](#render-views), [assets](#ship-frontend-assets), and [sessions](#sessions).
- API clients use [personal API tokens](#authenticate-a-machine-client).
- Run [diagnostics](#inspect-without-repairing), [tests and builds](#test-and-build),
  and [background jobs](#run-durable-background-work).

## Inspect without repairing

Format-17 applications from the v0.23.1 release own `cmd/diagnostics`
and `internal/diagnostics`. Run `forge doctor` to collect independent safe
configuration, database/migration, ORM, view, and asset verdicts without changing
the application. Run `forge routes:list` to inspect the actual constructed router,
even when PostgreSQL is offline. Failed/skipped checks exit nonzero. Neither
command starts services or upgrades older projects. See the
[diagnostic guide](diagnostics.md) for direct Go commands, privacy, external cache
boundaries, and explicit application-owned replacement seams.

## Add an endpoint

Generate a controller for a reports endpoint.

```sh
forge make:controller Reports
```

In `routes/routes.go`, add these two lines inside `Register`, alongside the
built-in `/health` route.

```go
reports := controllers.NewReportsController()
router.GET("/reports", reports.Index)
```

Open `http://localhost:8080/reports` when the development server is running. The
generated `Index` in `internal/http/controllers/reports_controller.go` returns
`{"data":[]}`. To load reports from your own data source, pass the dependency to
`NewReportsController` and call it in `Index`.

### Follow a request through the application

The server wires the application together once at startup.

```text
cmd/server/main.go
  -> internal/config.Load()       reads typed environment settings
  -> internal/database.Open()     opens PostgreSQL through database/sql and pgx
  -> internal/application.New()   constructs the application and middleware
  -> routes.Register()            registers handlers on the router
```

For each request, the router matches the method and path, runs the applicable
middleware, and calls the registered handler. The controller uses
`*httpx.Context` to read the request and write the response.

## Generate an authenticated CRUD resource

Generate an authenticated browser and JSON resource with explicit fields.

```sh
forge make:resource Task --field title:string --field notes:text:nullable --field priority:integer --field active:boolean
forge migrate
```

The model lives in `internal/models/task.go`. Open `internal/resources/task` to
edit the owner-scoped PostgreSQL repository, requests, JSON and browser
controllers, and controller tests. Edit browser pages and forms in
`resources/views/pages/tasks` and SQL in `database/migrations`. The CLI checks
for existing files before generation and updates the route and compiled-view
registries.

With `forge dev` running, register at `/register` or sign in at `/login`, then
open `/app/tasks`. The JSON API uses separate `/auth/*` and `/tasks` routes.
See the [development commands](cli.md#development).

### Choose fields and relationships

Repeat `--field` in the order you want the fields to appear. Each field uses
`<name>:<string|text|integer|boolean>[:required|nullable]` and defaults to
`required`. With any explicit field, the generated JSON and browser update
handlers require a positive optimistic `version`. A stale version returns HTTP
409. Sending an update replaces all writable fields. Missing, null, or empty
nullable inputs clear the column to SQL `NULL`, while `0` and `false` are values.
Running `make:resource` with no field or relationship flags generates a single
required `name:string` field with backward-compatible versionless updates.

For a resource that needs a required owner-scoped relationship, generate its
target first. Format 10 and later support this example.

```sh
forge make:resource Team --field name:string
forge make:resource Project --field title:string --belongs-to team:Team
forge migrate
```

The generated Go code owns the foreign key, validation, eager loading, and form
choices. The migration enforces the owner and target together with a composite
foreign key. Field and relationship flags are used once during generation. To
change the result, edit the generated Go, SQL, and templates in your application.
For persistence without HTTP routes, see the [typed ORM guide](orm.md).

### Customize who can access a resource

Resources generated in formats 13 through 17 include `authorization.go`.
`Authorize` defaults to `AccessOwner` for the typed list, create, view, update,
and delete actions. Return `AccessAll` for an authorized action across owners or
`AccessDenied` to stop an authenticated action with HTTP 403 before repository
work. For owner-scoped actions, missing and cross-owner records both return
404. Update and delete queries apply the validated scope in their SQL predicates.

Edit `Authorize` to keep the generated route wiring. Both controllers receive
the same `AuthorizeFunc` through `routes/resources_gen.go`. You can also wire
your own controller or policy in an application-owned route registration.

### Page through JSON results

Formats 10 through 17 paginate resource indexes such as `GET /tasks`. The
defaults are `page=1&per_page=20`, with a maximum page of 10,000 and a maximum
size of 100. Each parameter accepts one positive integer. Responses include
`data` and a `pagination` object with `page`, `per_page`, `has_previous`, and
`has_next`. Formats 8 and 9 keep their original complete-list JSON contract.

## Browse resources and select relationships at scale

Formats 16 and 17 add paginated relationship choices with an ID-only default.
Declare a target field explicitly when it is safe to expose its text through
the source resource's create and edit forms.

```sh
forge make:resource Category --field title:string
forge make:resource Ticket --field title:string --field notes:text:nullable --belongs-to category:Category --choice-label category:title
forge migrate
```

`--choice-label category:title` permits actors authorized to create or edit a
Ticket to see/search that Category field in the corresponding form, even when
they cannot list Categories. It does not grant target
list access or relax ownership constraints. Without the declaration, the picker
shows IDs and offers ID lookup. Declarations accept editable plain Go `string`
or `*string` fields backed by PostgreSQL `text` or sized `varchar`/`character varying`;
standard/custom wrappers use application-owned choice code. Labels include IDs,
truncate text to 120 UTF-8
runes before HTML escaping, and fall back to IDs for null text. The choice
declaration is generation input, not a runtime schema or discovery mechanism.

Browser `GET /app/tickets` and JSON `GET /tickets` share one typed query contract:

- `q` searches declared string/text fields with literal case-insensitive contains.
  Matches across fields use OR; other filters combine with AND. `%`, `_`, and
  backslash are literal text. PostgreSQL supplies case/collation behavior.
- `id` and declared foreign-key names such as `category_id` apply exact positive
  integer filters. Empty optional ID controls mean no filter.
- `sort` accepts `id`, declared scalar names, and declared foreign-key names;
  `direction` accepts `asc` or `desc`. Defaults are `sort=id&direction=desc`.
  Every sort has an ID tie-breaker. PostgreSQL puts nulls last ascending and
  first descending.
- `page` defaults to 1 and `per_page` to 20. Limits are page 10,000 and size 100;
  exceeding them rejects the request. Search permits at most 200 UTF-8
  runes, with no control characters. Unknown/duplicate parameters, malformed
  encoding, invalid UTF-8, and invalid values return 400 before repository access.
  Resources without text fields reject nonempty `q`.

For example, `/app/tickets?q=urgent&category_id=7&sort=title&direction=asc&page=2&per_page=20`
retains its filters in Previous/Next links. The JSON route keeps the existing
`data` and `pagination` envelope. Choice pages contain at most 20 options plus a
separately retained selected item. Bounded pages/offsets do not promise constant
database execution cost.

Search, ID Lookup, Previous, and Next work without JavaScript. Each button posts
the complete CSRF-protected form with `formnovalidate`, preserving unfinished
fields, independent relationship states, selections outside the current results,
and optimistic versions without writing domain data. Text Search clears that
relationship's ID filter; ID Lookup clears its text filter; both reset its page.
Draft display strings survive refresh, including scalar spellings such as `000`;
validation and persistence retain their normalization rules. Native textareas
normalize CRLF/CR to LF. A 422 remains correctable, and a valid stale version
still produces a conflict when saved.

When editing the emitted form, preserve the explicit controls:
`choice[category][q]`, `choice[category][id]`, and `choice[category][page]`.
Submit one `intent=save`, or `intent=choice:category:search` with `search`
replaced by `lookup`, `previous`, or `next` for those buttons. Keep
`validation_attempted=0` initially and `1` after a failed save. That flag controls
error display only; save always validates. Missing, duplicate, or unknown intents
reject without writes. The generated buttons supply these values; there is no default-save
fallback for submissions lacking a submitter.

The emitted `internal/resources/ticket/browse.go`, repository, controllers,
requests, and `resources/views/pages/tickets/*.forge.html` remain ordinary
application-owned source. Edit or replace their typed options, query predicates,
labels, form controls, or repository wiring directly, then run
`forge views:compile` after template changes. Existing format-15 and older
resources keep their released contracts and are not rewritten automatically.

## Authenticate a machine client

Fresh format-14 through format-17 applications let a signed-in user create a named, expiring
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

## Render views

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

Format-15 through format-17 applications also own `components/input`, `components/textarea`, and
`components/select`. Each accepts an explicit `view.Form`, emits ordinary static
HTML attributes, associates labels and help/error text with a page-unique ID,
and groups every field message in one stable alert container. Resource forms
use these components without runtime field discovery. Input callers must choose
`recover=true` or `recover=false`; password inputs remain blank even if a
controller accidentally supplies a password old value.

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

Both commands check the generated ORM, compiled views, and embedded assets
before running. If a check fails, follow the repair command printed by the CLI
and retry. The server binary is written to `bin/app` or `bin/app.exe` on
Windows. For additional build targets and direct Go commands, see
[Test and build in the CLI reference](cli.md#test-and-build).

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

To dispatch jobs on a recurring timetable, define them in the application’s
`internal/schedules/registry.go`. See the [scheduling guide](schedules.md).

## Validate input

Generate a request for a handler that expects a report name.

```sh
forge make:request CreateReport
```

Import your application's `internal/http/requests` package in the reports
controller from [Add an endpoint](#add-an-endpoint), then add this method.

```go
func (controller *ReportsController) Preview(ctx *httpx.Context) error {
    var request requests.CreateReportRequest
    if err := ctx.BindJSON(&request); err != nil {
        return err
    }
    if problems := request.Validate(); !problems.Empty() {
        return httpx.NewHTTPError(http.StatusUnprocessableEntity, "validation failed").WithDetails(problems)
    }
    return ctx.JSON(http.StatusOK, map[string]string{"name": request.Name})
}
```

Register `router.POST("/reports/preview", reports.Preview)` beside the GET
route. A request with `Content-Type: application/json` and a body such as
`{"name":"Quarterly report"}` returns the validated name. Missing or unsupported
media types receive HTTP 415. Malformed JSON receives HTTP 400, and invalid
field values receive HTTP 422.

## Change the HTTP stack

Pass `app.Handler()` to your own `http.Server`, mount a standard handler with
`httpx.Adapt`, or replace the GoForge router entirely. Application lifecycle
helpers are conveniences, not a required host.

## Migrations

Generate a migration with `forge make:migration create_orders` and edit the
resulting `.up.sql` and `.down.sql` files in `database/migrations`. The prefix
determines the order.

```text
20260904120000_create_orders.up.sql
20260904120000_create_orders.down.sql
```

Run `forge migrate` to apply the forward migrations. The application's console
uses `database/migrations/embed.go` and passes the application's `*sql.DB` to
the migrator. See `internal/database/database.go` for the pgx adapter and
connection setup.

## Sessions

The scaffold's browser routes use PostgreSQL-backed sessions and protect HTML
form submissions with CSRF tokens. Users can change their password or revoke
sessions from `/settings/security`. JSON clients can use `PUT /auth/password`
or `POST /auth/logout-all` with a signed-in session. Password changes revoke
prior sessions across processes. Configure the inactivity and hard deadlines
with `SESSION_IDLE_LIFETIME` and `SESSION_ABSOLUTE_LIFETIME` in `.env`.

To exercise account recovery locally, run `forge dev`, submit the form at
`/forgot-password`, then open Mailpit at `http://localhost:8025` to follow the
reset link. The generated worker delivers the message through the durable mail
outbox. The routes, controllers, and templates live in `routes/routes.go`,
`internal/http/controllers/password_recovery.go`, and `resources/views/auth`.

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
session ID. Auth middleware in `routes/routes.go` protects the resource routes,
and resource policies determine the database scope for an authenticated actor.

Session writes prune up to 100 expired rows, including abandoned sessions. An
expired session presented by a client is also removed on access.
