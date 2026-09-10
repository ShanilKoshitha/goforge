# v0.17 embedded production assets scorecard

Status: **candidate** — 2026-09-10

Target:

> From a fresh format-12 application, reference application-owned CSS and
> JavaScript through the Forge `asset` function, serve those exact bytes from
> the production binary, edit either file under `forge serve` or `forge dev`,
> and reload the browser exactly once only after the last-good replacement is
> committed. Keep asset ownership, embedding, URL resolution, route wiring, and
> replacement seams visible as ordinary Go; require no Node toolchain or hidden
> production build step.

The smallest complete workflow treats opaque files below
`resources/assets/files` as the production inputs. A generated application
embeds them directly and constructs the framework's bounded asset set in its
explicit application object graph. Stable same-origin URLs, strong validators,
and mandatory revalidation are the safe first production cache contract:
current-only fingerprint URLs are intentionally not claimed because they break
old HTML across rolling deployments unless prior generations are retained.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| A fresh browser application has a complete asset path | candidate | The scaffold owns CSS and JavaScript files, an explicit embedded asset package and handler route, and a Forge `asset` function used by the default layout; normal browser pages fetch both assets successfully |
| Production bytes are conventional and self-contained | candidate | Direct `go build ./cmd/server` embeds the source bytes without a compiler, manifest, or runtime disk access; application startup validates the bounded inventory, `forge assets:check` and the opinionated test/build gates run the same application-owned validator without writing, and the handler can be replaced with ordinary `net/http` or external storage wiring |
| HTTP caching and representation semantics are safe | candidate | Canonical stable URLs support GET, HEAD, strong ETag revalidation and byte ranges with exact media type and length; responses use `public, max-age=0, must-revalidate`, never falsely claim immutable fingerprints, and preserve global security middleware |
| The public edge fails closed | candidate | Traversal, encoded separators, directories, dotfiles, case-fold collisions, unsupported active types, irregular files, excessive file count, per-file size, total size, unknown names, and unsupported methods are rejected deterministically without directory listing or content sniffing |
| The development loop includes every asset edit | candidate | Regular files below `resources/assets/files` participate in source snapshots regardless of extension; a valid edit rebuilds the embedded binary, promotes through the existing last-good transaction, then emits one LiveReload event, while unreadable, failed, stale, or reverted candidates do not reload or displace the previous bytes |
| Existing applications remain compatible | candidate | Formats 4–11 retain their existing test/build/serve behavior and direct commands; format 12 is required only for the new scaffold contract, and the public asset runtime is independently usable |
| Documentation tells the truth | candidate | Framework and generated-project guides explain stable revalidated URLs, direct-Go use, supported defaults, replacement seams, and why transforms, fingerprints, and external asset hosts are separate contracts |
| The milestone passes twice without regression | candidate | Framework race/vet/build, compatibility, current scaffold, fresh generated PostgreSQL browser/development/production asset journeys, native Windows tests, and independent adversarial review pass twice on the final revision |

## Runnable baseline — 2026-09-10

- Public v0.16.0 and `origin/main` are merge `479fb2b`. Its main and tag
  workflows passed the complete Linux/PostgreSQL and native Windows matrices,
  and public `go install` reports `forge 0.16.0`.
- The merged framework suite passes from a clean worktree. Fresh format-11
  applications compile Blade/Twig-class Forge views and LiveReload accepted HTML
  edits, but the layout references no stylesheet or script, the application
  registers no static route, and the template function map has no asset helper.
- The source watcher recognizes only Go, Forge view, SQL, and root configuration
  files. CSS, JavaScript, images, and fonts cannot trigger a last-good rebuild.
- v0.16 explicitly excludes static-file conventions, production assets,
  manifests, fingerprints, transforms, and Node integration.

## Explicit non-goals

- Bundling, minification, transpilation, dependency graph or CSS `url()`
  rewriting, source maps, image optimization, or arbitrary compiler plugins.
- Node, npm, pnpm, Bun, Vite, esbuild, Tailwind, Sass, PostCSS, TypeScript, or
  framework-specific browser adapters.
- Immutable fingerprint URLs, retained asset generations, CDN/object-storage
  publication, external asset hosts, signed URLs, precompressed variants, or
  automatic cache purging.
- HMR, CSS-only replacement, hydration, client-side routing, or production
  LiveReload endpoints.

# v0.16 browser LiveReload scorecard

Status: **accepted** — 2026-09-10

Target:

> From a fresh format-11 application, open a server-rendered browser page through
> `forge serve` or `forge dev`, make a valid Go or Forge-view edit, and have the
> browser reload exactly once after the last-good candidate is fully committed.
> Failed, stale, or reverted candidates must never reload the browser. Keep the
> development mechanism outside generated and production application binaries,
> preserve application security policy, and retain direct Go execution as the
> complete no-rewrite escape hatch.

The smallest complete workflow extends the existing development proxy only.
The proxy injects one external same-origin client into eligible full HTML
documents and exposes randomized process-local script/event paths. Successful
promotion advances a monotonic generation observed through bounded SSE. This is
page reload, not HMR or an asset pipeline.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| The generated browser loop closes automatically | passing | A fresh format-11 page fetched through literal `forge serve` and `forge dev` contains one development-only external client; a committed view or Go edit produces one reload event and the next page contains the accepted content |
| Reload truth follows promotion truth | passing | Initial startup, build/compile failure, unhealthy candidate, stale candidate, failed target swap, rollback, and last-good serving emit no reload; notification occurs only after target promotion, old-candidate cleanup, and compiled-view commit all succeed |
| Response rewriting is narrow and representation-safe | passing | Only successful full `text/html` browser documents with identity encoding and bounded bodies are injected; HEAD, partial/range, attachment, compressed, oversized, fragmentary, and non-HTML responses retain exact bodies and representation headers |
| CSP and application semantics remain authoritative | passing | The client is an external same-origin script compatible with the generated CSP; existing CSP, cookies, status, and unrelated headers remain unchanged, while rewritten development documents discard invalidated validators and become `no-store`; no inline script or policy weakening is used |
| The event edge is bounded and race-safe | passing | A cryptographically random unguessable path prevents application-route collisions; exact GET endpoints, subscriber limits, missed-generation recovery, concurrent clients, heartbeats, disconnect cleanup, proxy shutdown, and malformed/wrong-method requests are deterministic and tested |
| Production remains conventional | passing | Generated source, routes, templates, runtime packages, and production binaries contain no reload endpoint, watcher, injected client, or hidden lifecycle; `go run ./cmd/server` and direct handler/proxy replacement remain exact escape hatches |
| Existing development guarantees remain intact | passing | Stable public address, private health promotion, in-flight request target pinning, last-good view/Go recovery, process labelling, aggregate readiness, and cross-platform tree cleanup continue to pass under `forge serve` and `forge dev` |
| The milestone passes twice without regression | passing | Framework race/vet/build, compatibility, current scaffold, fresh generated PostgreSQL browser journey, native Windows tests, and independent adversarial review pass twice on the final revision |

## Runnable baseline — 2026-09-10

- Public v0.15.0 and `origin/main` are the same merge `ce69301`. Tag workflow
  `34508599218` passed the full Linux/PostgreSQL matrix in 14m05s and native
  Windows in 5m48s; public `go install` reports `forge 0.15.0`.
- Focused merged-main proxy, development-supervisor, and generated-development
  tests pass locally. The proxy currently forwards every application response
  byte-for-byte and exposes no development route or browser notification.
- `forge serve` already identifies the single safe publication boundary: after
  a healthy target swap, stale-source checks, previous-candidate cleanup, and
  compiled-view commit. Failed edits keep the last-good server but require a
  manual browser refresh after the later successful repair.

## Accepted evidence — 2026-09-10

- The complete framework suite passes on the final implementation tree. Focused
  LiveReload, development-proxy, promotion-supervisor, generated-workflow, and
  view-engine tests also pass under the race detector; `go vet ./...` and a
  fresh CLI build pass.
- Deterministic tests prove exact randomized routing, same-origin external
  script delivery, monotonic/missed generations, bounded concurrent SSE,
  idle-deadline recovery, prompt shutdown, strict document eligibility,
  byte-preserving streamed/trailer/no-transform fallbacks, large buffered Forge
  views, and unchanged CSP/cookies/status.
- Supervisor tests prove no notification for initial, failed, unhealthy, stale,
  crashed, reverted, or failed-commit candidates and exact cleanup, compiled-view
  commit, then one reload ordering for an accepted promotion.
- The fresh PostgreSQL journey exercises initial client discovery,
  valid-edit notification and accepted HTML, invalid-edit silence with the
  byte-identical last-good page, repair notification, and active-SSE Ctrl-C
  shutdown. Push workflow `34515521526` passed Linux/PostgreSQL in 13m59s and
  Windows in 6m37s; pull-request workflow `34515548278` independently passed
  Linux/PostgreSQL in 13m53s and Windows in 6m04s.
- Independent adversarial review initially found transformation opt-out,
  streaming/trailer, idle deadline, large-view length, and duplicate-marker
  defects. Each now has a regression test; the final re-review reports no
  remaining P0, P1, or P2 finding after ten repeated focused race runs and vet.

## Explicit non-goals

- JavaScript/CSS compilation, bundling, minification, fingerprints, manifests,
  npm/Node/Tailwind integration, static-file conventions, or production assets.
- Hot module replacement, CSS-only replacement, component state preservation,
  client hydration, framework-specific browser adapters, or browser automation.
- WebSockets, a public runtime event API, generated application routes, template
  directives, service workers, browser extensions, or production middleware.
- Rewriting compressed, streamed, ranged, downloadable, fragmentary, or
  unbounded responses; weakening or replacing an application CSP to force the
  client to run under a deliberately restrictive custom policy.

# v0.15 unified development workflow scorecard

Status: **accepted** — 2026-09-10

Target:

> From a fresh format-11 application with its explicit services and migrations
> already configured, run one `forge dev` command to start the watched HTTP
> server, durable queue worker, and recurring scheduler; receive process-labelled
> output and one truthful all-ready signal; exercise HTTP and scheduled work; and
> stop every owned process tree together. Keep infrastructure, migrations,
> configuration, source, and every production process independently inspectable
> and runnable through their existing commands.

The smallest complete workflow is intentionally format 11 only. Earlier project
formats do not contain the same three-process application contract. `forge dev`
composes the existing development server, worker, and scheduler; it does not
create a combined runtime or a production supervisor.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| One command starts the complete generated application | passing | Exact no-argument `forge dev` validation starts only the watched server, `go run ./cmd/worker`, and `go run ./cmd/scheduler` from a format-11 project; older, future, malformed, and non-project invocations fail before spawning work |
| Readiness is truthful and output remains usable | passing | Every stdout/stderr line is atomically labelled by service, partial and concurrent writes cannot interleave prefixes, and the single stack-ready message appears only after the existing server, worker, and scheduler startup signals have all been forwarded |
| Failure is fail-fast and attributable | passing | A startup failure, clean premature exit, or later unexpected exit identifies the service, retains the underlying cause, cancels and reaps both peers, and never prints a false ready signal |
| Cancellation owns the whole tree | passing | Ctrl-C/SIGTERM cancels one shared context, waits for all three services within an outer grace that exceeds generated defaults and is explicitly configurable for longer application drains, returns successfully, and leaves no server listener, Go wrapper, worker, scheduler, or descendant alive on Linux or Windows |
| Existing development guarantees remain intact | passing | `forge serve` retains last-good view/Go compilation, private liveness promotion, recovery after invalid edits, one stable public address, and its current direct `go run ./cmd/server` escape hatch under `forge dev` |
| Side effects remain explicit | passing | The command does not start Compose/PostgreSQL/Mailpit, apply migrations, mutate `.env`, generate application resources or ORM source, open a browser, respawn crashes, or conceal missing configuration; only the existing managed view compilation/publication behavior remains |
| Production remains conventional | passing | Generated `cmd/server`, `cmd/worker`, and `cmd/scheduler` stay independent ordinary Go entrypoints; the existing `forge serve`, `queue:work`, and `schedule:work` commands and direct `go run`/`go build` paths remain exact escape hatches |
| Fresh application journey is end to end | passing | A fresh no-replace format-11 application with PostgreSQL materializes a registered scheduled job, executes and acknowledges its durable effect, serves HTTP, survives and recovers from an invalid view edit through the server path, then shuts down cleanly from one `forge dev` invocation |
| The milestone passes twice without regression | passing | Framework race/vet/build, released-format compatibility, current scaffold, generated PostgreSQL workflow, native Windows process tests, and independent adversarial review pass twice on the final revision |

## Accepted evidence — 2026-09-10

- Implementation revision `7961c2a` passed the complete local framework suite
  twice, vet, Linux cross-compilation, repeated native Windows/race process
  tests, and a fresh no-replace format-11 application's verify, zero-diff tidy,
  tests, vet, and server/worker/scheduler/console builds.
- GitHub push run `34505278948` and pull-request run `34505331431` independently
  passed the full Linux/PostgreSQL and native Windows matrices. Each live matrix
  ran the generated PostgreSQL journeys twice, including `forge dev` schedule
  materialization, worker effect, HTTP/view recovery, signal shutdown, and port
  release.
- Two independent adversarial reviews found no remaining P0/P1 issue after the
  canonical readiness, bounded output, attributable failure, and configurable
  descendant-aware shutdown repairs.

## Runnable baseline — 2026-09-10

- Public v0.14.3 is fixed at merge `f1550c0`; its merged-main and tag CI runs
  passed the Linux/PostgreSQL and native Windows matrices, and the public Go
  proxy resolves the exact lightweight unsigned tag and checksums.
- A clean branch from `origin/main` starts with independent `forge serve`,
  `forge queue:work`, and `forge schedule:work` commands. The generated guide
  requires separate terminals, and there is no `forge dev` command or aggregate
  process lifecycle.
- `forge serve` already owns cross-platform descendant cleanup, last-good server
  replacement, and an explicit startup line. Generated worker and scheduler
  processes already emit stable structured startup events. The new workflow can
  compose these contracts without changing application runtime packages,
  project format, or database schema.

## Explicit non-goals

- Starting or configuring Docker Compose, PostgreSQL, Mailpit, or any other
  external service; applying migrations; changing `.env`; or repairing missing
  dependencies.
- Browser LiveReload/HMR, response rewriting, frontend asset compilation, Node,
  npm, Tailwind, or a production asset manifest. Browser reload is the next
  independent development-loop boundary after this process workflow.
- Production supervision, daemonization, crash restart, a TUI, persisted logs,
  Procfile/process discovery, arbitrary user-defined processes, or remote
  development.
- Hot replacement of worker or scheduler processes. The watched HTTP server
  keeps its accepted last-good reload behavior; background-process source or
  registry changes require restarting the single `forge dev` command.
- A new project format, generated runtime abstraction, service container,
  reflection, process discovery, or changes to queue/schedule delivery semantics.

# v0.14.2–v0.14.3 schedule integration hardening scorecard

Status: **accepted** — 2026-09-10

Target:

> From a fresh format-11 application, bind a scheduled job through an explicit
> worker registration contract, run an occurrence-aware payload factory that
> cooperates with the scheduler operation deadline, and fail at startup before
> dispatch when the target name is absent from generated worker wiring.
> Preserve the existing durable cursor and public APIs, and prove password-recovery response
> equalization through deterministic control-flow invariants rather than noisy
> sequential wall-clock comparisons.

The release is staged. v0.14.2 publishes the backward-compatible runtime
factory seam, deterministic security acceptance, and generated target-name
validation that compiles against v0.14.0. v0.14.3 pins the immutable new
runtime from generated format-11 applications and adopts `DynamicContext` in
their schedule journey. No project-format or database migration boundary changes.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Dynamic schedule work can cooperate with cancellation | passing | `schedule.DynamicContext` receives the exact operation context; deadline cancellation returns promptly, preserves `errors.Is`, dispatches nothing, rolls back the schedule transaction, releases its row lock/connection, and permits a later healthy retry |
| Existing schedule source and durable identity remain compatible | passing | Existing `schedule.Dynamic` callers compile unchanged; static and legacy factories keep their behavior; switching an equivalent factory to `DynamicContext` retains the existing payload-strategy fingerprint and durable cursor |
| Schedule target names must be wired into the generated worker registry | passing | A collision-proof generated manifest package lists built-in and application job-definition target names from the generator metadata; scheduler and schedule-list startup reject every schedule target absent from that set before configuration or durable work; no reflection, scanning, or handler construction is used. Application-authored same-name definitions and deployed worker queue selection remain explicit operator-owned contracts |
| Password-recovery equalization is deterministic | passing | `Request` has one public return path through an envelope around its private branch-complete work method; the response deadline is selected before that work, its child context is shorter, envelope order and delay bounds are pure-tested, and live known/absent/failure acceptance retains identical public responses plus absent-account state invariants without comparing sequential wall times |
| Generated concurrency evidence proves actual contention | passing | Two separate scheduler processes are synchronized around one locked due row and report one enqueue plus one contention result, while the worker still executes exactly one durable effect |
| The public workflow remains ordinary Go | passing | The generated registry, registered-name set, validation, context-aware factory, scheduler process, SQL adapter, and direct commands remain inspectable and replaceable without runtime discovery |
| The milestone passes twice without regression | passing | Framework race/vet/build, fresh public no-replace format-11 application, generated PostgreSQL acceptance, native Windows CLI, and independent adversarial review passed twice on the final code revision |

## Runnable baseline — 2026-09-10

- PR 16 merged at `2ad9216`; exact-head push and pull-request CI runs
  34425850063 and 34425853148 passed Linux/PostgreSQL and native Windows.
- Merged-main run 34426817857 passed, and lightweight unsigned public tag
  `v0.14.1` points directly to that merge. Release run 34427762091 passed on
  attempt 2.
- The public Go proxy resolves v0.14.1 to `2ad9216` with module checksum
  `h1:yId9EVdFJ7ZM6MXQFh7bZ3xQb2PT6c1qsS0fqohGirA=` and module-file checksum
  `h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=`. A clean public install
  reports `forge 0.14.1` and generates a no-replace format-11 application that
  passes tests, vet, and server, worker, scheduler, and console builds.
- The first v0.14.1 tag attempt exposed the baseline defect: two sequential
  password-recovery requests independently sample response jitter and absorb
  unbounded runner scheduling noise, so their elapsed times differed by
  507.4ms against a 500ms threshold even though every functional workflow
  passed. A rerun passed; threshold relaxation is explicitly not acceptance.
- Independent review also found that legacy dynamic factories cannot observe
  the operation deadline, generated scheduler startup does not prove its job
  targets have worker handlers, and the process-level concurrency journey does
  not force or assert actual row contention.

## v0.14.2 public runtime evidence — 2026-09-10

- PR 17 exact-head push and pull-request runs 34481655944 and 34481719203
  passed Linux race/vet/build, released-format compatibility, public v0.14.1
  format-11 upgrade, the current scaffold, generated PostgreSQL applications,
  and native Windows checks. Merged-main run 34483256073 passed the same gates.
- Lightweight unsigned tag `v0.14.2` points directly to merge `5114808`.
  The public Go proxy resolves that tag with module checksum
  `h1:HIjhuU2nsE17gLc6C537lmILYB5aRkfs2dWlXFEuLYA=` and module-file checksum
  `h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=`. A clean public install
  reports `forge 0.14.2`.
- Two consecutive final-head framework suites passed locally with module files
  read-only, alongside vet, schedule race tests, tidy, a native Windows CLI
  build, and an untouched no-replace format-11 application pinned to the public
  module. Independent exact-head review found no P0–P3 actionable finding.
- Tag run 34484640732 passed Linux/PostgreSQL in 13m24s and native Windows in
  6m07s, closing the public runtime stage before v0.14.3 adoption.

## v0.14.3 adoption evidence — 2026-09-10

- Final code head `e8c8f78` passed two consecutive local read-only framework
  suites. Vet, schedule race tests, module tidy, whitespace checks, and a native
  Windows CLI build reporting `forge 0.14.3` also passed.
- An untouched no-replace format-11 application pinned the checksum-verified
  public v0.14.2 module, contained the `DynamicContext` example, passed module
  verification and tidy, and passed framework test/vet/build plus server,
  worker, scheduler, and console compilation.
- Exact-head push run 34487616892 passed Linux/PostgreSQL in 12m50s and native
  Windows in 6m31s. Pull-request run 34487651117 independently passed those
  gates in 13m21s and 5m40s. Both included full race/vet/build, released
  format-8 compatibility, public v0.14.1 format-11 upgrade compatibility, the
  current public-runtime scaffold, and generated PostgreSQL acceptance with
  forced scheduler contention.
- Independent repaired-head review found no P0–P3 actionable findings and
  verified that the commits contain only Shanil authorship with no co-author,
  sign-off, or assistant-related trailers.
- The documentation-only final-head push run 34489329926 exposed an older
  development-supervisor test race: the test mutated source after proxy
  creation but before initial startup was committed, so its intended promotion
  crash never occurred. Duplicate run 34489335374 passed. The regression test
  now waits for the existing committed-startup readiness message before either
  candidate-exit scenario; both scenarios pass 1,000 ordinary iterations and
  100 race-instrumented iterations.

## Explicit non-goals

- Preempting arbitrary Go callbacks that ignore context. Safe hard bounds for
  uncooperative application code require process isolation or a different
  reservation protocol; v0.14.2 instead provides the normal Go cancellation
  contract and documents legacy `Dynamic` as prompt computation-only code.
- A new project format, schema migration, automatic schedule retirement,
  seconds-level cron, overlap, replay, dashboards, or unified development
  orchestration.

# v0.14 durable recurring schedules scorecard

Status: **in progress** — 2026-09-09

Target:

> From a fresh application, explicitly bind a typed application job to a
> five-field recurring schedule, migrate durable PostgreSQL schedule state,
> inspect it, materialize a due occurrence once across competing scheduler
> processes, and execute it with the existing queue worker. Time zones, DST,
> missed runs, overlap, crash atomicity, and definition changes must have
> written conservative behavior while the registry, payload, SQL, processes,
> and direct database/queue escape hatches remain ordinary inspectable Go.

The intended workflow is:

```text
forge new reportboard --module example.com/reportboard
cd reportboard
forge make:job RebuildSearchIndex
# Bind RebuildSearchIndexDefinition in internal/schedules/registry.go.
forge migrate
forge schedule:list
forge schedule:run
forge schedule:work
forge queue:work
```

The scheduler only materializes typed jobs into the existing durable queue. A
separate queue worker executes them. Fresh applications move to project format
11 only after the new schedule runtime is available from a public immutable
module tag.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Schedule definitions are explicit and exact | passing | An application-owned registry binds a validated, versioned schedule name and five-field cron expression to one typed job definition and payload factory without reflection, scanning, or persisted application schema |
| Civil-time behavior is deterministic | passing | UTC and IANA zones, DOM/DOW semantics, leap dates, spring-forward gaps, fall-back repeated minutes, and strictly increasing occurrences pass fixed-clock tests |
| Durable materialization is atomic | passing | PostgreSQL row locking plus the existing transactional dispatcher commit the job and cursor together; cancellation, enqueue failure, state failure, panic, and process death commit neither half |
| Multiple scheduler processes are safe | passing | Concurrent runners coordinate per schedule without a global leader; one occurrence creates at most one queue row while unrelated schedule rows can progress independently |
| Missed-run and overlap policy are bounded | passing | Missed occurrences coalesce to the latest occurrence inside a bounded grace window, older windows record a skip, and the default active-job deduplication policy suppresses overlap while advancing the cursor |
| Definition drift fails closed | passing | A canonical declarative fingerprint is stored durably; a same-name mismatch stops without rewriting state or dispatching, dormant rows absent from the explicit registry never run, and changed-registry deployment boundaries are explicit |
| Operational commands form one inspectable workflow | candidate | `schedule:list`, one-shot `schedule:run`, daemon `schedule:work`, direct `go run ./cmd/scheduler`, deterministic output, signal cancellation, and bounded configuration all work from outside the source directory |
| Fresh and existing applications remain coherent | candidate | Format 11 includes visible registry, scheduler binary, config, migrations, and docs; formats 8–10 retain compatible commands; format 10 belongs-to remains supported; server, worker, scheduler, console, views, and ORM all compile |
| Escape hatches stay complete | candidate | Public registry, scheduler, store/coordinator, observer, and PostgreSQL primitives are usable directly; application teams can replace cron registration, payload factories, process wiring, SQL adapter, or the entire scheduler |
| The milestone passes twice without regression | in progress | Full framework race/vet/build, fresh generated application, competing-process PostgreSQL acceptance, native Windows CLI coverage, and independent architecture/adversarial review pass twice on the final revision |

## Runnable baseline — 2026-09-09

- `main` merge `a189fc2` passes Linux/PostgreSQL and native Windows in
  [CI run 34380649800](https://github.com/ShanilKoshitha/goforge/actions/runs/34380649800).
  A clean worktree at that commit passes `go test ./... -count=1`, `go vet
  ./...`, and a Windows CLI build after its isolated public module cache is
  populated.
- Lightweight public tag `v0.13.1` resolves to that exact merge, installs as
  `forge 0.13.1`, and generates a no-replace format-10 application that passes
  required belongs-to generation, module verification, tests, and build.
- Durable one-off typed jobs already provide PostgreSQL enqueue, active-job
  deduplication, delayed/absolute dispatch, transactional `Dispatcher.Using`,
  fenced workers, retries, failures, observers, administration, and direct
  store/SQL escape hatches.
- Recurrence is absent: there is no cron parser, explicit schedule registry,
  durable occurrence cursor, missed-run/overlap contract, scheduler process,
  or schedule command surface.
- The implementation is intentionally staged. The runtime/PostgreSQL contract
  lands first while fresh apps remain format 10, then an immutable v0.14.0 tag
  allows the format-11 scaffold to pin resolvable public code in a
  checksum-bearing v0.14.1 CLI release.

## Stage A runtime bridge evidence — 2026-09-09

- The explicit runtime, PostgreSQL adapter, direct operations guide, and
  v0.14.0 bridge version are committed as `678adf7`, `ded8dcf`, `c3d8e78`, and
  `a913230` after the contract commit `c7fa178`.
- `go test ./... -count=1` passed twice on the final local revision. Focused
  `go test -race ./schedule/... ./job -count=2`, `go vet ./...`, module tidy,
  whitespace, and a Windows CLI build reporting `forge 0.14.0` also passed.
- That CLI generated a fresh format-10 application pinned to public v0.13.0
  with no `replace`. Module verification and tidy-diff, generated `forge test`,
  `go vet ./...`, `forge build`, and the worker build all passed.
- An independent adversarial review found no P0. It found and closed unsafe
  mixed-registry replacement guidance and UUID-only schedule job IDs. The
  final contract documents the scheduler-only replacement window, while the
  adapter now persists bounded opaque IDs and tests a custom queue-store ID.
- The live PostgreSQL suite covers 16 competing transactions, restart state,
  row independence, current-minute initialization, misfire coalescing and
  expiry, overlap suppression, drift, cancellation, state conflict, backend
  termination, dormant rows, opaque IDs, and schema constraints. Local
  PostgreSQL credentials did not match the test role, so CI remains the
  authoritative live execution gate before merge.

## Stage A public release evidence — 2026-09-09

- Push run [34419676292](https://github.com/ShanilKoshitha/goforge/actions/runs/34419676292)
  and pull-request run
  [34419733008](https://github.com/ShanilKoshitha/goforge/actions/runs/34419733008)
  independently passed Linux race, vet, compatibility, current-scaffold,
  generated PostgreSQL, and native Windows gates on exact head `5aad387`.
- PR 15 merged as `88f0891`, and merged-main run
  [34420781683](https://github.com/ShanilKoshitha/goforge/actions/runs/34420781683)
  passed the same Linux/PostgreSQL and Windows matrix before publication.
- Lightweight unsigned tag `v0.14.0` points directly to that merge commit. The
  public Go proxy and checksum database resolve it to module checksum
  `h1:Dt5aHth+6iG5ODwUL8mLGL0ye6piP1klyYoGgZ3TE7k=` and module-file checksum
  `h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=`.

## Stage B format-11 candidate evidence — 2026-09-09

- Unsigned commits `59722eb`, `c962cae`, and `045f2d6` add the centralized
  format-11 compatibility boundary and schedule commands, the generated
  application scheduler/registry/configuration/migration, and the process-level
  PostgreSQL acceptance journey respectively.
- `go test -mod=readonly ./... -count=1` passed twice on the integrated code;
  `go vet ./...`, `go mod tidy -diff`, focused command/scaffold tests, whitespace,
  and a Windows CLI build reporting `forge 0.14.1` also pass.
- A fresh local-replace format-11 application passes tests, vet, and builds for
  server, worker, scheduler, and console. A separate fresh no-replace application
  pins checksum-verified public v0.14.0, passes module download, verification and
  tidy-diff, direct tests/vet/builds, `forge test`, and `forge build`.
- The generated live journey creates an application job and explicit `.v1`
  schedule, applies the schedule schema, launches two separate one-shot scheduler
  processes from outside the source directory, asserts one current-minute queue
  row and cursor, lets the queue worker produce one durable effect and acknowledge
  the job, inspects status, and proves signal-driven daemon shutdown. It compiles
  locally; CI PostgreSQL remains the execution gate.
- Independent audit found no P0/P1 scheduler-core design blocker. It required and
  verified format-10 relationship compatibility, format-12 future refusal,
  same-database queue/schedule transactions, worker registration, synchronous
  startup signalling, exact schema/checksums, and process-level acceptance. The
  final two clean CI runs remain pending.

## Explicit non-goals for v0.14

- Database-authored, dynamic, or per-tenant schedules.
- Seconds fields, cron macros, RRULE, holiday/business calendars, or arbitrary
  catch-up and replay.
- Overlapping scheduled jobs, exactly-once handler effects, chains, batches,
  DAGs, dashboards, or a scheduler web UI.
- Alternate Redis, cloud-queue, or Kubernetes scheduler adapters.
- Runtime package scanning, annotations, reflection, server embedding, or a
  generated schedule metadata file.
- Mixed-registry rolling replacement or an automatic durable retirement
  protocol; removals and replacements use a scheduler-only maintenance window.
- A `make:schedule` generator, unified `forge dev`, or frontend asset/HMR work.

# v0.13 required belongs-to resource generation scorecard

Status: **accepted** — 2026-09-09

Target:

> From a fresh application, generate one owner-scoped parent resource and one
> child resource with a required belongs-to relationship. The relationship must
> remain consistent across ordinary Go models, PostgreSQL ownership constraints, exact
> JSON and form decoding, validation, repositories, eager loading, browser
> choices, rendered views, and generated tests. Missing and cross-owner parents
> must be indistinguishable, concurrent parent deletion must fail predictably,
> and all generated behavior must remain directly inspectable and replaceable.

The intended workflow is:

```text
forge new issueboard --module example.com/issueboard
forge make:resource Category --field name:string
forge make:resource Issue \
  --field title:string \
  --belongs-to category:Category
forge migrate
forge serve
```

Fresh applications use project format 10. `--belongs-to` is repeatable and accepts
`<lower_snake_name>:<ExistingResource>`. Relationships in this milestone are
always required. Targets must already be generated owner-scoped resources;
their current model source, not persisted field metadata, is authoritative.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| CLI parsing and preflight are exact | passing | Both flag forms, declaration order, bounds, unsafe or duplicate names, scalar/FK/Go collisions, missing or self targets, and incompatible edited target models fail before any write |
| One ephemeral contract drives every generated layer | passing | The relationship reaches the application model, migration, generated ORM, separate association IDs, JSON/form request, repository, controllers, Forge views, and generated tests without being stored as runtime schema |
| Persistence is explicit and conservative | passing | Format-10 resources expose an owner/ID candidate key; child SQL uses a required indexed `BIGINT` and composite `(user_id, target_id)` foreign key with non-cascading `NO ACTION`; the model exposes the protected key and explicit `belongs_to` field; target deletion maps to an intentional conflict |
| Ownership cannot be crossed | passing | Create and update accept only a target owned by the authenticated user; a missing target and another user's target return the same field-level 422 response; raw cross-owner SQL fails; a target-delete race produces either a valid child or a disclosure-safe relation failure |
| JSON and browser workflows are complete | passing | Exact JSON/form semantics require a positive relation ID; new/edit forms provide bounded owner-only choices and preserve submitted selections and errors; list/show/create/update responses render the loaded relationship |
| Relationship reads remain bounded | passing | List and pagination eager-load in batches with constant queries per batch rather than per row; single-record paths load explicitly; every related query retains the owner predicate |
| Optimistic and rollback safety are retained | passing | Relationship-aware updates require a positive version; stale forms keep safe submitted values and fresh versions; collisions, cancellation, compilation failures, managed-write failures, and concurrent generators preserve complete state |
| Existing applications keep working | passing | Formats 8 and 9 retain implicit and scalar-only generation plus every compatible command, relationship flags fail before writes outside format 10, frozen released applications remain compatible, and handwritten ORM relationships plus direct SQL remain available |
| A fresh PostgreSQL application works end to end | passing | A two-user Category/Issue application migrates, tests, races, vets, builds, and proves owned create/read/update, cross-owner rejection, eager loading, browser choices, stale writes, delete restriction, rollback, and direct-Go escape hatches |
| The milestone passes twice without regression | passing | Push run 34367665049 and pull-request run 34367670306 independently passed Linux/PostgreSQL and native Windows on commit `88ee62f`; independent architecture/security reviews found no P0–P2 blocker |

## Acceptance evidence — 2026-09-09

- [Push CI run 34367665049](https://github.com/ShanilKoshitha/goforge/actions/runs/34367665049)
  and [pull-request CI run 34367670306](https://github.com/ShanilKoshitha/goforge/actions/runs/34367670306)
  independently passed Linux race tests, vet, CLI build, released format-8
  compatibility, current format-10 scaffold checks, the generated PostgreSQL
  suite, and native Windows tests, vet, and build on commit `88ee62f`.
- The PostgreSQL gate ran the relationship workflow twice per run. It generated
  a two-user Category/Issue application, applied and reversed its migrations,
  proved owner-only create/read/update and browser choices, indistinguishable
  missing/cross-owner rejection, exact query budgets, raw composite-FK
  enforcement, stale writes, delete restriction, and the create/delete race.
- The final local revision passed `go test ./... -count=1`, `go test -race
  ./... -count=1`, `go vet ./...`, a Windows CLI build, multi-relationship
  generated-app compilation, format-8/9 scalar compatibility, and adversarial
  candidate-key/dependency-publication tests.
- Independent architecture and security re-reviews found no remaining P0–P2
  blocker after repeatable-template scoping, bounded JSON pagination,
  association-field allowlisting, target-migration DDL validation, and final
  dependency fingerprinting were added.
- PR 13 merged as `9078e56`, and
  [main CI run 34371037790](https://github.com/ShanilKoshitha/goforge/actions/runs/34371037790)
  passed Linux/PostgreSQL and native Windows before the lightweight unsigned
  `v0.13.0` tag was published on that exact merge commit. The public Go proxy
  resolves module checksum
  `h1:ky4g5QeWdG2M3rZdeYd7pkD+8Cz1dVHHWlqewm/SMcw=` and module-file checksum
  `h1:o585rLjoR42pVE79JjdmwrIvum0C5QRRilYqZAJcAkM=`.
- The checksum-bearing v0.13.1 candidate reports the new version and generates
  a format-10 application without `replace` while pinning public v0.13.0. The
  candidate's [push CI run 34375859749](https://github.com/ShanilKoshitha/goforge/actions/runs/34375859749)
  and [pull-request CI run 34375894479](https://github.com/ShanilKoshitha/goforge/actions/runs/34375894479)
  both passed Linux/PostgreSQL and native Windows. The distribution gate
  verified the exact embedded checksums, scalar and required relationship
  generation, generated tests, vet, server and worker builds, serve, and all
  PostgreSQL workflows.
- A blocked public-module download reproduced a Windows transient-sharing
  rollback edge. Resource generation now retries removal of only still-empty
  generated directories, preserves concurrent editor content, and returns the
  final persistent error; deterministic retry, preservation, and exhaustion
  tests pass on the release candidate.
- [Final evidence run 34377344124](https://github.com/ShanilKoshitha/goforge/actions/runs/34377344124)
  exposed an overly aggressive reclaim acceptance budget: the generated worker
  had only 250ms for each PostgreSQL operation, compared with the production
  default of 5s. The same head and the test's second iteration passed, isolating
  a timing flake. The probe now retains a short 3s lease but allows 1s operations
  and reports both worker event streams on timeout, keeping ordinary CI load
  from masquerading as a failed lease-recovery invariant.

## Runnable baseline — 2026-09-09

- Public `main` commit `f23e204` passes Linux/PostgreSQL and native Windows in
  CI run 34353173213. A clean worktree at that commit also passes
  `go test ./... -count=1` after its pinned modules are available.
- Format 9 already generates complete owner-scoped JSON and browser CRUD for
  required and nullable scalar fields with exact request semantics, optimistic
  versions, failure-safe publication, and real PostgreSQL acceptance coverage.
- The static ORM already validates and generates typed belongs-to, has-one,
  has-many, and many-to-many descriptors and batched loaders without runtime
  reflection. Every resource already uses an implicit protected Owner/User
  belongs-to relationship.
- `make:resource` cannot describe any domain relationship. Treating a foreign
  key as an integer field would omit the database constraint, allow cross-owner
  attachment, provide no eager loading or browser choices, and surface races as
  raw persistence failures.
- Format 9 resource tables do not declare the owner/ID candidate key required
  for a database-enforced cross-owner relationship invariant. A visible format
  10 boundary is therefore required instead of silently weakening old projects.
- Recurring schedules remain the next operational milestone. Durable one-off
  jobs already provide delayed dispatch and production workers, while this
  milestone closes a gap in the north star's primary resource-generation path.

## Explicit non-goals for v0.13

- Nullable belongs-to, inverse relationship generation, nested routes,
  relationship mutation endpoints, has-one, has-many, many-to-many,
  polymorphism, composite keys, or configurable cascading deletes.
- Updating or regenerating an existing application-owned resource after its
  initial generation.
- Persisting relationship or field definitions in `.forge`, interpreting them
  at runtime, reflection-driven serialization, lazy loading, package scanning,
  or hidden database queries.
- Searchable/autocomplete relationship widgets, relationship pagination APIs,
  arbitrary display-label configuration, or nested form creation.
- Recurring schedules, scheduler leadership, frontend assets/HMR, or a
  multi-process `forge dev` command.

# v0.12 delivery-backed account recovery scorecard

Status: **accepted** — 2026-09-08

Target:

> From a fresh application, request a password reset through either the browser
> or JSON API, deliver a text-and-HTML message through an explicit durable mail
> path, consume the short-lived link once, and invalidate every prior session.
> Public responses must not disclose account or token state, durable storage and
> logs must not expose the reset secret, and all mail, token, persistence,
> rendering, job, and transport boundaries must remain ordinary replaceable Go.

The intended browser journey is:

```text
GET  /forgot-password
POST /forgot-password
GET  /reset-password?token=<selector.secret>
POST /reset-password
POST /login
```

The JSON equivalents are `POST /auth/password/forgot` and
`POST /auth/password/reset`.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Mail transport is explicit and production-safe | passing | Immutable messages and the small `mail.Sender` seam have bounded RFC 5322 encoding; the SMTP adapter enforces TLS/auth/plaintext policy, timeouts, cancellation, and disclosure-safe errors under focused race tests in both public runs |
| Mail authoring is application-owned | passing | `forge make:mail` and the built-in password-reset message use ordinary generated Go, compiled Forge HTML, and embedded standard-library text templates; callers directly replace the renderer, message, or sender |
| Reset issuance is enumeration-safe and bounded | passing | The generated service silently account-throttles normalized addresses, caps account-dependent work at 400ms beneath a 650–850ms response envelope, emits only a nonblocking fixed-stage failure signal, uses database-clock expiry, and supersedes by user; both PostgreSQL runs proved healthy and deliberately delayed failure parity |
| Durable delivery does not persist the reset secret in plaintext | passing | One generated transaction stores the selector/digest, AES-256-GCM envelope, outbox-ID-only delivery job, and outbox-ID-only seven-day cleanup job; both PostgreSQL runs injected rollback at token, outbox, and final job writes and proved SMTP retry/disclosure behavior |
| Reset consumption is single-use and revocation-safe | passing | A cheap selector/digest/generation preflight rejects invalid links before password hashing, then the generated transaction locks and revalidates token plus user, advances credentials, and consumes once; both PostgreSQL runs covered rollback/retry, replay, selector throttling across sources, concurrency, and session revocation |
| Browser and JSON contracts are complete | passing | Generated request/controller/view suites cover exact JSON/form decoding, validation, privacy headers, generic wrapped-sentinel failures, 202/204, PRG flashes/redirects, secret preservation, and contextual Forge escaping |
| Generation and configuration fail before partial state | passing | Format 9, random outbox keys, loopback-only Mailpit, strict process-specific settings, recovery migration/routes/job wiring, and rollback-safe `make:mail` pass fresh generated tests |
| Existing applications remain conventional and compatible | passing | A frozen format-6 application still compiles after `make:job`; format-aware registry generation adds built-ins only to format 9, and both public runs passed the released format-8 compatibility gate |
| A fresh PostgreSQL application works end to end | passing | Both isolated format-9 journeys generated, raced, vetted, built server/worker, migrated, captured SMTP, injected transaction faults, and exercised browser plus JSON recovery |
| The milestone passes twice without regression | passing | Push run 34292249546 and pull-request run 34292252232 independently passed Linux/PostgreSQL and native Windows on commit `198c6f9`; independent security and architecture reviews report no P0–P2 blocker |

## Runnable baseline — 2026-09-08

- Released `main` at `v0.11.1` passes `go test ./...`, `go vet ./...`, and a
  native Windows CLI build. Public CI already exercises every generated
  PostgreSQL workflow twice.
- Fresh applications have registration, login, password change, account-wide
  credential-version session revocation, source/account throttles, durable typed
  jobs, application-owned Forge views, and explicit transaction helpers.
- No `mail` package, SMTP settings, mail templates or generator, reset-token
  schema, forgotten/reset routes, durable encrypted outbox, or reset acceptance
  journey exists. Registration through password change is therefore strong, but
  a user who forgets the credential cannot recover the account.
- Recurring schedules and relationship-aware resource generation are valuable
  next milestones. They do not close this production authentication gap and are
  deliberately deferred until the written v0.12 target passes.

## Implementation evidence — 2026-09-08

- `go test -race ./mail/...` and `go vet ./mail/...` pass for immutable message
  construction plus STARTTLS, implicit TLS, and explicit loopback-only plaintext
  SMTP delivery.
- Focused CLI and fresh-application tests pass for strict recovery/mail
  configuration and `forge make:mail AuditNotice`, including view compilation,
  collision preflight, rollback, and generated Go tests.
- Generated mailbox race tests pass repeated AES-256-GCM round trips and reject
  ciphertext tampering, row swaps, version changes, wrong keys, invalid IDs,
  invalid messages, and nonce reuse. The migration exposes no plaintext message
  columns and stores reset selectors plus SHA-256 digests only.
- Fresh generated whole-application tests compile the recovery service, strict
  browser/JSON presentation, application-owned dual-template message, atomic
  outbox dispatch adapter, built-in delivery handler, explicit routes, and SMTP
  worker. Focused format-6 compatibility tests confirm later job generation does
  not inject format-9 built-ins into an older application.
- A dedicated real-PostgreSQL acceptance journey compiles and vets. Both public
  CI events passed its generated race and vet gates; the journey
  measures known/unknown response timing, including a two-second database delay
  cancelled by the 400ms internal budget; injects token, outbox, cleanup-job,
  and reset-update failures; and covers hostile Host headers, SMTP 451 retry,
  bounded encrypted retention, opaque payloads, supersession, malformed,
  cross-account, expired, replayed and selector-throttled links, browser and
  JSON completion, credential invalidation, sign-in, session revocation, and
  concurrent one-winner consumption.
- After the final hardening changes, `go test ./... -count=1`,
  `go test -race ./... -count=1`, `go vet ./...`, and a native Windows CLI
  build pass. The reviewed runtime source reports v0.11.1; the
  checksum-bearing distribution patch reports v0.12.1.
- [Push CI run 34292249546](https://github.com/ShanilKoshitha/goforge/actions/runs/34292249546)
  and [pull-request CI run 34292252232](https://github.com/ShanilKoshitha/goforge/actions/runs/34292252232)
  independently passed Linux/PostgreSQL and native Windows on `198c6f9`. Both
  Linux jobs passed framework race/vet/build, released-format compatibility,
  fresh-checkout scaffolding, and every generated PostgreSQL application.
- Independent architecture and security reviews of the final implementation
  found no P0–P2 blocker.
- PR 9 merged as `bc1dd2b`, and
  [main CI run 34294151854](https://github.com/ShanilKoshitha/goforge/actions/runs/34294151854)
  passed Linux/PostgreSQL and native Windows before the lightweight unsigned
  `v0.12.0` tag was published on that exact merge commit. The public Go proxy
  resolves checksum `h1:/wmB+d+3au9s6Ei3iJLPgmfE3DIMyBO3kFaeg0hXEX0=`.
- A clean v0.12.1 candidate reports the new version and generated a format-9
  application without `replace` while pinning public v0.12.0. Module
  verification, resource and mail generation, generated tests, and server
  and worker builds passed; the resulting binary reports the exact public
  runtime checksum.
- [Distribution push run 34296296919](https://github.com/ShanilKoshitha/goforge/actions/runs/34296296919)
  and [pull-request run 34296351515](https://github.com/ShanilKoshitha/goforge/actions/runs/34296351515)
  independently passed Linux/PostgreSQL and native Windows on `047cf77`. Each
  Linux job proved the frozen format-8 path and the checksum-bearing v0.12.1
  format-9 path before running every generated PostgreSQL journey twice.

## Explicit non-goals for v0.12

- Email verification, passwordless or magic-link login, invitations, MFA,
  WebAuthn/passkeys, OAuth/social login, API tokens, or device administration.
- SMS, push, webhooks, marketing/bulk delivery, campaigns, provider analytics,
  inbound mail, attachments, DKIM signing, or a web mail/queue dashboard.
- Exactly-once SMTP delivery. Queue and transport effects remain at least once;
  reset-token consumption is the single-use security boundary.
- A proprietary mail expression language, runtime template discovery, package
  scanning, dependency injection, or reflection-based message registration.
- Recurring job schedules, relationship-aware resources, frontend asset
  bundling/HMR, or a multi-process `forge dev` command.

# v0.11 typed scalar resource generation scorecard

Status: **accepted** — 2026-09-08

Target:

> From a fresh format-8 application, describe an authenticated resource with
> repeated one-shot `--field` arguments and generate one complete,
> production-shaped JSON and browser CRUD slice. String, text, integer, and
> boolean fields must have consistent required or nullable behavior across
> ordinary Go models, PostgreSQL migrations, typed requests, validation,
> repositories, compiled views, and generated tests. New output requires an
> optimistic version for updates and keeps every query owner-scoped. Invalid,
> cancelled, failed, or concurrent generation must not corrupt an existing
> project. The field description is generation input only: no persisted schema,
> runtime interpretation, reflection, or project-format change is introduced.

The intended command shape is:

```text
forge make:resource Issue \
  --field title:string \
  --field notes:text:nullable \
  --field priority:integer:required \
  --field active:boolean
```

Each `--field` is `<name>:<type>[:required|nullable]`. Required is the default;
spelling it explicitly is supported. Repeating a modifier, combining
`required` with `nullable`, or using any undeclared type or modifier is an
error. Calling `forge make:resource Issue` without `--field` retains the
existing single required `name` field workflow.

## Acceptance criteria

| Criterion | State | Evidence |
| --- | --- | --- |
| CLI parsing is exact and useful | passing locally | Parser and CLI tests cover both command spellings, both flag forms, declaration order, bounds, malformed values, unsafe/duplicate/reserved names, types, modifiers, and no-write failure |
| One field contract drives every layer | passing locally | Deterministic generation tests inspect the same ordered four-type contract through models, SQL, ORM input, requests, validation, repositories, controllers, views, and generated tests |
| Generated persistence is typed and inspectable | passing locally | Generated application tests compile concrete Go/PostgreSQL types and prove nullable pointers plus an application-writable-only `Attributes` boundary |
| JSON and form semantics are deliberate | passing locally | Generated controller suites cover missing/null/empty/zero/false, malformed and repeated input, exact JSON member names and duplicates, 400/422 classification, nullable clearing, and contextual escaping |
| New updates are concurrency-safe by default | passing locally | Generated JSON and browser tests require positive versions, prove one winner and one 409, retain the winning record, and refresh stale browser forms with safe submitted values |
| Authentication and ownership remain structural | passing | Generated repositories and controller tests retain authenticated owner derivation and predicates; two public real-PostgreSQL runs proved cross-owner JSON and browser isolation |
| Generation is one failure-safe transaction | passing locally | Failure-injection tests cover collisions, invalid state, rendering/compiler failures, every cancellation/publication boundary, exclusive/managed writes, mode restoration, editor races, and stale ORM rejection |
| Concurrent generators converge without orphaned state | passing locally | Twelve independent CLI processes and same-name contenders converge on complete source, routes, metadata, views, ORM, and migrations; compare-and-swap rollback preserves newer editor bytes |
| Production contains no field-schema machinery | passing locally | State inspection finds no persisted field descriptions; fresh generated tests/builds use static application-owned Go, SQL, and Forge templates with direct repository, ORM, `database/sql`, router, and template seams |
| Format-8 and public compatibility are retained | passing | Implicit no-field output retains TEXT, 2–200 validation, legacy rendering, and versionless updates; both public runs generated legacy and typed resources against v0.10.0 with no `replace`, then verified, tested, built, and served the application |
| A fresh PostgreSQL application works end to end | passing | Both public runs created an all-branch typed resource and exercised ORM/views, migration, JSON/browser CRUD, validation, CSRF, ownership, stale writes, zero/false versus NULL, build, test, serve, and direct-Go paths twice per run |
| The milestone passes twice without regression | passing | Push run 34265651666 and pull-request run 34265714830 independently passed Linux/PostgreSQL and native Windows on commit `8a666c7`; independent final reviews report no P0–P2 finding |

## Local evidence — 2026-09-08

- `go test ./...`, `go test -race ./...`, `go vet ./...`, and Windows CLI
  builds pass. The reviewed runtime CLI reports `forge 0.11.0`; the
  checksum-bearing distribution patch reports `forge 0.11.1`.
- Focused generation tests compile and test implicit legacy, explicit
  `name:string`, mixed scalar, text-only nullable, and nullable integer/boolean
  applications. Twelve independent CLI generator processes converge without
  orphaned registry, metadata, ORM, view, migration, or source state.
- Transaction tests inject editor changes, cancellation at every managed
  boundary, stale model/ORM input, compiler failures, and exclusive/managed
  publication failures. Rollback restores exact prior bytes and modes while
  refusing to overwrite newer editor bytes.
- A clean temporary application with no `replace` retained public runtime
  v0.10.0, generated both implicit legacy and mixed typed resources, passed
  `go mod verify` and `go test ./...`, and built the conventional application
  binary. The CI distribution gate now repeats this exact compatibility path.
- [Push CI run 34265651666](https://github.com/ShanilKoshitha/goforge/actions/runs/34265651666)
  and [pull-request CI run 34265714830](https://github.com/ShanilKoshitha/goforge/actions/runs/34265714830)
  independently passed Linux/PostgreSQL and native Windows. Each Linux job ran
  the complete generated PostgreSQL application suite twice, including the
  nullable zero/false versus SQL `NULL` proof.
- Independent architecture, security, and release reviews identified the
  public-runtime binding gap and release identity as blockers. Strict JSON
  decoding was moved into application-owned generated source, legacy behavior
  was restored, the public compatibility gate was expanded, and the release
  identity was advanced to v0.11.0. Final architecture, security, and release
  reviews found no remaining P0–P2 issue.
- PR 6 merged as `2af6000`, and
  [main CI run 34269752964](https://github.com/ShanilKoshitha/goforge/actions/runs/34269752964)
  passed Linux/PostgreSQL and native Windows before the lightweight unsigned
  `v0.11.0` tag was published on that exact merge commit. The public Go proxy
  resolves checksum `h1:PpJ4gjHJxAM8tZmQ7Ajh++Fce05+w0l9la6F4oe8Dcc=`.
- A clean v0.11.1 candidate distribution generated implicit legacy, explicit
  `name:string`, and mixed typed resources without `replace` while pinning
  public v0.11.0; `go mod verify`, generated tests, and `forge build` passed.
- Merged-main CI run 34272845400 passed the complete Linux/PostgreSQL gate but
  exposed a Windows-only ordering race in the development-supervisor test: the
  test could inject its reload before initial serving readiness. The test now
  waits for that readiness boundary; the paired startup/reload scenarios pass
  1,000 repetitions, 100 race-enabled repetitions, and the complete native
  Windows CLI suite twice.

## Baseline — 2026-09-08

- Format 8 already generates a complete authenticated JSON and browser resource,
  but every layer is fixed to one required `Name string` / `name TEXT` field.
- Resource generation already holds the project-wide interprocess generator
  lock, preflights all destination paths, renders candidate ORM and view
  artifacts, and attempts rollback after a failed managed publication. These
  are reusable primitives, not evidence that arbitrary fields are safe.
- The ORM model parser already understands concrete scalar and nullable Go
  types, protected fields, indexes, defaults, and relationships. This milestone
  uses only the four bounded resource types above and does not expose the full
  model-tag surface as a generator language.
- Existing JSON and browser resource tests prove authentication, owner-scoped
  CRUD, CSRF, escaping, pagination, validation, and optional optimistic
  concurrency for the fixed field. Field-aware output must preserve those
  behaviors while making the optimistic version mandatory for newly described
  resources.
- Public CI already runs race, vet, build, released-project compatibility,
  native Windows CLI checks, and fresh PostgreSQL applications. The milestone
  must extend the real generated-application journey rather than relying only
  on template substring or compilation tests.

## Explicit non-goals for v0.11

- Persisting field definitions in `.forge`, loading a project schema at runtime,
  reflecting over request or model structs, or adding a project format.
- Updating, merging, or regenerating the fields of an existing resource. The
  command remains a one-shot generator that refuses to overwrite human-owned
  files.
- Relationships, foreign-key selection, enums, dates/times, decimal or money
  policy, JSON, binary data, uploads, rich text, array fields, or polymorphism.
- Field defaults, unique or custom indexes, arbitrary database types or SQL
  expressions, database-backed validation, custom validation-rule syntax, or
  schema inference from a live database.
- PATCH semantics, bulk CRUD, search/filter generation, sorting policy, soft
  deletion, API versioning, authorization roles, or automatic migration at
  application startup.
- Interactive prompts, schema files, configuration-driven generation, admin UI
  builders, frontend JavaScript generation, or client-side validation output.

# v0.10 last-good development server scorecard

Status: **accepted** — 2026-09-06

Target:

> From a generated format-4 through format-8 application, run `forge serve`
> once and keep a stable development server while Go, Forge view, environment,
> module, and embedded SQL inputs change. Every stable edit recompiles views
> with the project's own semantics and builds an ordinary server binary. A
> broken edit leaves the last-good server and generated view artifact intact;
> correcting it recovers without restarting the CLI. Cancellation removes the
> complete child process tree and every development artifact. “Last-good” is a
> compile-and-liveness guarantee; `/ready` remains the dynamic PostgreSQL and
> exact-migration-state signal.

## Acceptance criteria

| Criterion | State | Evidence |
| --- | --- | --- |
| The default closes the edit-to-render gap | passing locally | Supervisor tests and the frozen format-4 journey prove compile, build, start, watch, reload, and the documented direct `go run ./cmd/server` escape hatch |
| Watch coverage is complete and loop-free | passing locally | Content-snapshot tests cover writes, creates, deletes, renames, new nested directories, every declared input, exclusions, transient read recovery, and generated-output exclusion; one observer owns polling and generation |
| Bursts converge on the newest source | passing locally | Race tests cover edits during builds, A→B→A content round trips, generation handoff before waiter registration, pre/post-promotion checks, stale discard, proxy revert, and serialized builds |
| Invalid views preserve the last-good state | passing locally | Application-owned compilers run against an isolated source tree; compiler and transaction tests retain exact bytes and mode on failure, while promotion holds the project generator lock through commit or rollback |
| Invalid Go preserves the last-good server | passing locally | Candidate views reach `go build` through a Go overlay without touching the canonical artifact; failures preserve the active process and recover after the next stable edit |
| Process replacement and exit semantics are bounded | passing locally | Managed process trees, joined cleanup failures, candidate exits during proxy startup/promotion, exact PID/address listener ownership, cancellation, proxy limits, and port cleanup have native Windows race coverage |
| Template compatibility is retained | passing locally | The frozen format-4 watched journey passes twice; format-specific compiler tests retain application-owned format 5–8 function maps and format/ORM gates are revalidated for every candidate |
| Production remains conventional | passing locally | Candidate binaries use an OS temporary directory; inspection and scaffold tests show no watcher, compiler, reload endpoint, or injected browser script in application/runtime source |
| Fresh application journey passes twice | passing | Public push and pull-request runs each generated PostgreSQL applications twice and proved valid view/Go edits, invalid-edit preservation and recovery, atomic-save/new-directory detection, readiness, response changes, cancellation cleanup, and port reuse |
| Independent compatibility and platform review passes | passing | Framework race/vet/build, frozen and public released-project checks, Linux PostgreSQL acceptance, and native Windows watcher/process tests passed in both public runs; independent review found no P0–P2 blocker |

## Local evidence — 2026-09-06

- `go test -race ./...`, `go vet ./...`, and `go build ./cmd/forge` pass on
  Windows; the CLI reports `forge 0.10.0`.
- The supervisor, observer, listener-owner, proxy, and cleanup suites pass ten
  consecutive race-enabled runs. Exact Windows PID/address ownership rejects a
  same-port wrong-address match.
- Off-tree view staging, overlay builds, promotion-lock ownership, identical
  concurrent publications, and transient rollback retry pass ten consecutive
  race-enabled runs without exposing an unpromoted generated artifact.
- The frozen format-4 build/serve/check/last-good journey passes twice with a
  real watched server and complete cancellation cleanup.
- A CGO-free Linux CLI test binary compiles successfully. The fresh PostgreSQL
  development journey remains unavailable on this workstation because it has
  no `GOFORGE_TEST_DATABASE_URL`; the public evidence below closes that gate.
- [Push CI run 34065415454](https://github.com/ShanilKoshitha/goforge/actions/runs/34065415454)
  passed Linux/PostgreSQL in 6m36s and native Windows in 3m18s.
- [Pull-request CI run 34065753884](https://github.com/ShanilKoshitha/goforge/actions/runs/34065753884)
  independently passed Linux/PostgreSQL in 6m27s and native Windows in 2m57s.
- Independent product and architecture reviews approved the final transaction,
  process, compatibility, and acceptance boundaries with no P0–P2 finding.
- Merged `main` passed [CI run 34250734126](https://github.com/ShanilKoshitha/goforge/actions/runs/34250734126),
  then lightweight unsigned v0.10.0 fixed merge commit `f6a26e5`. The public Go
  proxy resolved that exact origin and returned module checksum
  `h1:HfC1lNHqsbaJGLGSY/auzY0kg+PWBUTh/5Whj/h/jWQ=`.
- A clean-cache v0.10.1 distribution smoke generated without `--replace`,
  resolved public v0.10.0 with its checksum, passed `go mod verify`, `forge
  test`, and `forge build`, and published the canonical `bin/app.exe`.

## Baseline — 2026-09-06

- The accepted v0.9.1 CLI compiles application-owned views once and then blocks
  in `go run ./cmd/server`; every source edit requires a manual stop and rerun.
- View compilation already has deterministic, last-good publication and mapped
  diagnostics. Process delegation already owns Unix process groups and Windows
  kill-on-close Job Objects, but exposes only a blocking one-shot runner.
- The generated server, view compiler, ORM, readiness route, and direct Go
  commands are already explicit. This milestone needs no template grammar,
  generated application contract, production runtime, or project-format change.

## Explicit non-goals for v0.10

- Browser LiveReload script injection, SSE/WebSocket reload endpoints, CSS/JS
  hot replacement, hydration, frontend asset compilation, npm, or Tailwind.
- Starting Compose/PostgreSQL, applying migrations, changing `.env`, generating
  ORM source, starting workers, or supervising multiple application processes.
- A new Forge view directive, runtime template interpretation, production file
  watching, route discovery, dependency injection, or framework-owned app state.
- Test watching, `forge doctor`, remote development, TLS termination, deployment,
  or changing the strict `forge test` and `forge build` contracts.

# v0.9 cohesive test and build loop scorecard

Status: **accepted** — 2026-09-06

Target:

> From a generated format-4 through format-8 application, use one explicit
> command to prove the inspectable ORM and compiled views are current before
> running the complete Go test suite, and one explicit command to publish a
> production server binary without replacing the last-good artifact on failure.
> Both workflows remain non-mutating freshness gates over conventional Go
> commands with direct, documented escape hatches.

## Acceptance criteria

| Criterion | State | Evidence |
| --- | --- | --- |
| Command boundaries are exact | passing | Focused tests prove both commands accept no arguments, require a project root, support formats 4 through 8, and reject older, future, or malformed manifests before spawning a child or creating `bin` |
| Freshness is checked before execution | passing | Both commands check ORM first and views second; format 4 uses the frozen compiler while formats 5 through 8 delegate to application-owned `cmd/views`; missing, stale, and invalid artifacts stop execution |
| Preflight never rewrites source | passing | Unit, frozen-format, released-format, and fresh PostgreSQL tests retain generated ORM and view artifacts byte for byte; stale diagnostics name `forge orm:generate` or `forge views:compile` |
| Testing remains ordinary Go | passing | Runner tests prove exact `go test ./...` delegation with inherited context and streams; CLI tests preserve child exit codes without duplicate diagnostics |
| Production output is canonical | passing | `forge build` uses `go build -trimpath -o <staged-output> ./cmd/server` and publishes only `bin/app` or `bin/app.exe`; Linux and native Windows CI both execute the command |
| Failed builds preserve the last-good binary | passing | Simulated compiler failures, post-compile cancellation, a real cancelled child process, and a deliberately invalid fresh application all leave the canonical binary byte-identical and remove temporary output |
| Escape hatches remain complete | passing | Framework and generated-project guides give exact ORM/view checks plus direct `go test`, `go build`, and `go run` alternatives for custom flags, packages, tags, targets, and outputs |
| Existing projects remain compatible | passing | A hand-built frozen format-4 application and the frozen format-6 fixture execute both workflows; CI also installs public v0.8.1, generates a no-`replace` format-8 application, and retains its module and generated artifacts unchanged |
| Full workflow passes twice | passing | Public push and pull-request runs each passed framework race/vet/build, released-format compatibility, native Windows checks, and two fresh format-8 PostgreSQL journeys with canonical out-of-tree binary execution and failure-preservation probes |

## Passing evidence — 2026-09-06

- Local `go test -race ./...`, `go vet ./...`, and `go build ./cmd/forge`
  passed. The complete CLI package also passed after the frozen-format and real
  process-cancellation additions.
- A locally installed public v0.8.1 CLI generated a format-8 application with
  its public v0.8.0 dependency and no local replacement. The current CLI ran
  `forge test` and `forge build`, left ORM/views unchanged, and published a
  working `bin/app.exe`.
- [Push CI run 34057241836](https://github.com/ShanilKoshitha/goforge/actions/runs/34057241836)
  passed the Linux/PostgreSQL job in 5m41s and native Windows in 2m44s.
- [Pull-request CI run 34057252760](https://github.com/ShanilKoshitha/goforge/actions/runs/34057252760)
  independently passed the Linux/PostgreSQL job in 5m42s and native Windows in
  3m19s. Each Linux job ran both generated PostgreSQL workflows twice.
- Final independent review found no P0, P1, or P2 blocker after the frozen
  format, public release, real cancellation, version, and evidence closures and
  approved the source milestone for merge and the v0.9.0 runtime tag.
- Merged `main` passed [CI run 34058166132](https://github.com/ShanilKoshitha/goforge/actions/runs/34058166132),
  then lightweight unsigned v0.9.0 fixed merge commit `81cffa3`. The public Go
  proxy resolved that exact origin and returned module checksum
  `h1:qFK3Y42+UV0QyYGKbJfoS+zv/aekEZgtYpMNZ7lq8PY=`.
- The local v0.9.1 distribution smoke generated without `--replace`, required
  public v0.9.0 with its proxy checksums, passed `forge test` and `forge build`,
  retained ORM/views byte for byte, and published `bin/app.exe`.

## Baseline — 2026-09-06

- Post-v0.8 production hardening is accepted. The merged `main` branch passed
  the framework race/vet/build gates and both generated PostgreSQL workflows
  twice in public CI.
- `forge serve` already checks the project root, compiles views, and delegates
  to `go run ./cmd/server`; the CLI has no `test` or `build` command.
- Generated documentation currently sends developers directly to `go test
  ./...` and `go build -trimpath -o bin/app ./cmd/server`. These conventional
  commands work but do not first prove that inspectable ORM and view artifacts
  match their source inputs.
- ORM and view compilers already expose deterministic, non-mutating `--check`
  behavior. Format-4 view semantics are frozen, while formats 5 through 8 own
  an application compiler with the production function map.

## Explicit non-goals for v0.9

- `forge doctor`, environment diagnosis, dependency installation, or toolchain
  repair.
- File watching, process reload, browser LiveReload/HMR, frontend asset
  bundling, or a multi-process `forge dev` command.
- Starting or stopping Compose/PostgreSQL, provisioning a test database, or
  changing `.env`.
- Applying migrations before tests, builds, or server startup. Database state
  remains an explicit `forge migrate` operation.
- Test arguments, package selection, coverage policy, test watching, build
  tags, cross-compilation, release archives, signing, or container images.
- Building the worker or console. Their conventional `go build` commands remain
  documented escape hatches.
- A new runtime abstraction, generated application contract, project-format
  version, or automatic upgrade of older projects.

# Post-v0.8 P1 and production-hardening scorecard

Status: **accepted** — 2026-09-06

## Acceptance criteria

| Criterion | State | Evidence |
| --- | --- | --- |
| Parallel generators preserve managed state | passing | One project-wide interprocess lock covers every `make:*` form; a 12-process job-generation regression verifies every source, metadata, and registry entry and passed repeated local runs |
| Local CLI cannot remain silently stale | passing | The stale root `forge.exe` was removed, root CLI artifacts are no longer ignored, and verified builds target `.tmp/forge.exe` |
| Module paths follow Go's contract | passing | `new` and existing-project parsing use `golang.org/x/mod/module.CheckPath`; `../evil` and other invalid forms are rejected in tests |
| CLI cancellation stops descendants | passing | The executable uses a signal-derived context; Unix process groups and Windows kill-on-close Job Objects retain control after the direct child exits; a signal-resistant descendant-heartbeat regression proves cancellation reaches the child tree |
| Liveness and readiness are truthful | passing | `/health` is process liveness; `/ready` applies a two-second PostgreSQL ping and migration status/checksum check; acceptance polling uses `/ready` with an HTTP client timeout, and the fixture rebuilds its embedded migration manifest after adding the transactional probe |
| Applied schema identity is verified | passing | Migration rows retain name and exact up-script SHA-256; `Up`, `Down`, and `Status` reject missing, renamed, changed, or re-nullified checksums; legacy backfill is one-time and ends with a non-null constraint |
| Deployment defaults fail secure | passing | Generated `.env` defaults to production/secure cookies, both the CLI next steps and generated guide require explicit local HTTP opt-in before `forge serve`, and development PostgreSQL binds only to `127.0.0.1` |
| Durable queue failures do not disclose handler data | passing | Runtime classification, PostgreSQL writes, and historical admin reads allowlist generic diagnostics; panic values, stacks, raw errors, and malformed payload details are excluded |
| Uncooperative jobs cannot hold a live worker forever | passing | Cancellation has a configurable bound; expiry stops the worker and heartbeats without mutating the fenced delivery so lease recovery remains safe |
| Signup commits as one unit | passing | Generated JSON and browser registration create the user, reset the account limiter, and persist the initial session through one PostgreSQL transaction; custom stores require an explicit coordinator outside tests; the acceptance probe forces session insertion failure and expects the user/reset to roll back |
| Migration rollback and status are covered | passing | Unit tests cover reverse ordering, default steps, empty scripts, applied/pending state, drift, legacy tables, and PostgreSQL locking behavior |
| Repository gates | passing | `go test -race ./...`, `go vet ./...`, generated-application compilation, process-tree regression, and a CLI build to `.tmp/forge.exe` pass locally |

The PostgreSQL workflow contains the atomic-signup and readiness probes but was
not executed locally because `GOFORGE_TEST_DATABASE_URL` and a Docker engine
were unavailable. The first branch run correctly rejected an acceptance binary
built before the transactional probe migration existed; the fixture now
rebuilds that binary against the final embedded migration manifest. The
corrected branch passed the race suite and both generated PostgreSQL workflows
twice in [CI run 34054530630](https://github.com/ShanilKoshitha/goforge/actions/runs/34054530630).

# v0.8 account integrity and bounded sessions scorecard

Status: **accepted** — 2026-09-05

Target:

> From an empty directory, generate a PostgreSQL application where an
> authenticated user can change a password through explicit JSON and browser
> flows, immediately invalidate every prior session across processes and
> restarts, and continue safely in one rotated current session. Successful
> login upgrades outdated password hashes, while activity may extend idle
> expiry but can never cross a configured absolute session deadline.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Password change is complete on both transports | passing | One application-owned request explicitly decodes JSON and form fields, applies the same validation policy, verifies the current password, never redisplays password values, and drives both an authenticated API endpoint and a CSRF-protected browser form |
| Credential mutation is atomic and race-safe | passing | PostgreSQL replaces the hash and increments a visible credential generation with a compare-and-swap predicate; deterministic SQL and same-cookie tests prove stale changes and logins cannot restore an obsolete hash, both win, or erase the winning rotated session |
| Revocation is immediate and global | passing | Password change preserves only a rotated current session, while sign-out-everywhere performs an unconditional atomic generation advance linearizable with password change; stale API and browser cookies fail on another process and after restart |
| Authenticated sessions prove current credentials | passing | Sessions carry the observed credential generation, both authentication middleware compare it with the current user before protected work, and mismatches invalidate server state without emitting an out-of-order cookie that could overwrite a concurrent rotation |
| Password hashes upgrade transparently | passing | Successful login conditionally persists a current-strength replacement without advancing the credential generation; failed weak-hash verification is padded to bounded current work and concurrent password change remains authoritative |
| Idle and absolute lifetime are independent | passing | A validated explicit session policy lets activity slide idle expiry without crossing durable issuance plus the absolute deadline; fresh applications expose both durations as typed environment configuration |
| Failures remain safe and bounded | passing | Exact JSON/form failures, authenticated-before-CSRF ordering, account/source throttles, safe current-password errors, and generated HTML tests prevent password, hash, identity, and implementation-cause disclosure |
| Generated behavior stays inspectable | passing | Credential generation, repository compare-and-swap methods, routes, controllers, requests, configuration, views, and SQL are ordinary application-owned files; the generic session store and password hasher remain replaceable |
| Compatibility is explicit | passing | An application generated by immutable v0.7.1 passes current runtime tests and compatible CLI checks; format-8 resource and future-format generators refuse before writes instead of injecting incompatible contracts |
| Full workflow passes twice | passing | Framework race/vet/build and two fresh format-8 PostgreSQL applications pass the complete password, revocation, rehash, expiry, migration, browser, API, CRUD, ORM, and job journeys on both the runtime and final distribution commits |

## Baseline — 2026-09-05

- Released v0.7.1 passes `go test ./... -count=1`; the generator package
  completed in 62.172 seconds. Public CI already proves the existing generated
  PostgreSQL auth, session rotation, throttling, request-boundary, CRUD, ORM,
  views, and jobs workflows.
- Password hashes are parameterized PBKDF2-SHA256 values with dummy
  verification and a working `NeedsRehash` predicate, but login never persists
  a stronger replacement.
- Generated auth exposes registration, login, current-user, and single-session
  logout only. The user schema has no credential generation, repository writes
  no credentials after registration, and sessions carry only `user_id`.
- Browser activity continually assigns `now + lifetime`; there is no absolute
  deadline. API and browser refresh behavior is inconsistent, and the
  two-hour lifetime is hard-coded in route construction.
- Independent read-only security and product audits found no current P0
  exploit. They independently ranked missing password recovery controls and
  bounded session lifetime ahead of CLI convenience wrappers.

## Explicit non-goals for v0.8

- Forgotten-password reset, email verification, magic links, notifications,
  or outbound mail. Those require a dedicated delivery and token-lifecycle
  milestone rather than a development-only mail stub.
- MFA, WebAuthn/passkeys, OAuth/social login, API tokens, device/session
  inventory, remembered devices, impersonation, or an authentication event
  ledger.
- Password-strength breach services, organization policy, localization, or a
  proprietary password-policy language.
- Signing-key rotation or encrypted session payloads. Cookies contain only a
  signed random identifier; key rotation needs an explicit multi-key deployment
  contract.
- `forge test`, `forge build`, or a development watcher. Conventional Go
  commands already work; their cohesive CLI milestone remains next in product
  priority after account integrity.

## Acceptance evidence — 2026-09-05

- Public CI passed on the immutable `v0.8.0` runtime commit `cf4a66d` in
  [run 34002623263](https://github.com/ShanilKoshitha/goforge/actions/runs/34002623263).
  The final `v0.8.1` distribution commit `c4d206a` passed the same gates in
  [run 34002985340](https://github.com/ShanilKoshitha/goforge/actions/runs/34002985340).
  Each run executes the fresh PostgreSQL application workflows with `-count=2`.
- A clean-cache `go run
  github.com/ShanilKoshitha/goforge/cmd/forge@v0.8.1 new` generated a format-8
  application without `--replace`. Its public `v0.8.0` dependency passed
  `go mod verify`, `go test ./... -count=1`, `go vet ./...`, and
  `go build ./cmd/...`.
- The immutable `v0.7.1` CLI at commit `415449c` generated a real format-7
  application. Pointed at the v0.8 runtime, it passed `go test ./...`, current
  `views:compile --check`, and `orm:generate --check`; `make:resource` required
  format 8 and left no files behind.
- Two independent final read-only reviews reported no remaining P0, P1, or P2
  defect after the concurrency, stale-cookie, middleware-ordering, weak-hash
  timing, request-boundary, and template-disclosure fixes.
- Lightweight unsigned tags `v0.8.0` and `v0.8.1` point to their immutable
  runtime and distribution commits. The public v0.8.0 checksums embedded by
  v0.8.1 were resolved through the Go proxy and checksum database.

# v0.7 explicit request boundary and hardened HTTP kernel scorecard

Status: **accepted**

Target:

> From an empty directory, generate a PostgreSQL application whose ordinary Go
> request contracts decode JSON and browser forms, normalize deliberately,
> validate realistic typed input with reusable application rules, and return
> stable safe failures. Its explicit HTTP kernel bounds transport resources,
> correlates every request outcome, rejects unsafe cross-origin policy, and
> shares authentication throttles across processes without reflection,
> discovery, or hidden proxy trust.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Validation is application-extensible without reflection | passing | Applications implement an exported rule contract or use `RuleFunc`; rules are explicit values with no tag scanning, discovery, or framework-global registry, while format-6 callers retain their existing API |
| Real request states and composition are representable | passing | New APIs distinguish missing, explicit null, empty, and present values where policy requires it; numeric ranges, list size/uniqueness/each, nested-prefix merging, and conditional/cross-field custom rules have deterministic tests |
| Validation failures have a stable safe contract | passing | Each violation retains a field path, stable rule code, and human message without the rejected value; API output is machine-readable and browser helpers render escaped messages from the same result |
| Generated requests own both transports | passing | One application-owned request type explicitly decodes/normalizes JSON and URL-encoded form input, and JSON/browser controllers delegate to it instead of duplicating field extraction or validation policy |
| Transport failures are exact | passing | Wrong media type is 415, malformed or type-invalid input is 400, oversized JSON/form input is 413, semantic validation is 422, unknown JSON fields and repeated scalar form fields remain rejected, and causes never reach clients |
| Middleware configuration is validated and immutable | passing | Safe constructors reject invalid values at application startup, defensively copy caller collections, reject wildcard origins with credentials, and enforce requested preflight method/header policy with correct `Vary` behavior |
| Request lifecycle is correlated once | passing | Invalid or oversized inbound request IDs are replaced; a typed accessor exposes the accepted ID; success, HTTP error, cancellation, and panic each produce exactly one completion record with the same ID, method, matched route pattern, status, duration, and outcome |
| The generated HTTP server is bounded | passing | Typed defaults and environment overrides configure header/read/write/idle timeouts, handler deadline, and maximum header bytes; invalid values fail startup; custom `http.Server` and streaming routes remain explicit escape hatches |
| Browser and API error surfaces stay explicit | passing | Browser middleware failures render safe HTML while API routes retain the stable JSON envelope; no `Accept` negotiation or hidden controller switching is introduced |
| Authentication throttling is production-shaped | passing | Fresh PostgreSQL applications use an atomic shared store with database time, bounded pruning, hashed keys, restart persistence, and cross-process enforcement; the memory store remains an explicit local/test option |
| Proxy trust is explicit and spoof-resistant | passing | Only validated configured CIDRs can supply forwarded client addresses; untrusted peers and malformed/ambiguous chains fall back safely to the direct peer, with no automatic forwarding-header trust |
| Security policy stays inspectable | passing | Generated same-origin defaults include a compatible CSP and existing headers; HSTS is explicit production/TLS policy; every middleware call remains visible, removable, and replaceable in application code |
| Compatibility and full workflow pass twice | passing | Format-6 auth/CRUD/views/ORM/jobs fixtures retain their behavior; framework tests/vet/race and two fresh format-7 applications pass generated tests/vet/race/build plus two-process PostgreSQL request-boundary journeys without leaked schemas or processes |

## Baseline — 2026-09-05

- The accepted v0.6 worktree passes `go test ./... -count=1`; the slowest
  package completed in 58.721 seconds.
- Existing strengths include explicit middleware composition, standard
  `net/http` escape hatches, strict JSON media-type and unknown-field handling,
  bounded form parsing, duplicate scalar-form rejection, scoped method
  override, session-bound CSRF, Unicode-aware strings, and generated request
  structs shared at the validation-method level.
- `validation.Field` has an unexported method, so an application package cannot
  implement the advertised public extension interface. Only string and `int`
  fields exist; resource version checks already mutate the error map manually.
- `BindJSONLimit` converts `http.MaxBytesError` to HTTP 400 while the equivalent
  form boundary returns 413. JSON and browser field decoding/normalization are
  duplicated across controllers.
- `httpx.CORS` reflects every origin with credentials when `AllowedOrigins`
  contains `*` and `AllowCredentials` is true. Its caller-owned slices remain
  mutable after construction.
- Generated servers configure only `ReadHeaderTimeout`. Request IDs accept and
  reflect arbitrary inbound values, access logs omit correlation and route
  patterns, and panic paths do not produce the same completion record.
- Generated authentication throttling uses a process-local memory store. It is
  reset by restarts, multiplied by replicas, and cannot distinguish clients
  behind a trusted proxy without application-specific replacement code.
- Independent read-only audits found no P0 defect in the accepted auth/session
  contract itself. Account-wide revocation, password changes, transparent
  rehash, and absolute session lifetime form the next dedicated account-
  security milestone rather than being partially folded into this boundary.

## Accepted evidence — 2026-09-05

- Local `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet
  ./...`, and `git diff --check` pass. The PostgreSQL-only tests skip locally
  because this workstation has no available PostgreSQL service.
- A frozen, inspectable format-6 application containing authentication, owner-
  scoped CRUD, compiled views, typed ORM code, and a typed job passes tests,
  vet, builds, and the current compatible generators. The v0.7-only resource
  generator requires an explicit format upgrade instead of emitting
  incompatible source into older applications.
- Fresh format-7 scaffold and resource tests compile and pass, including exact
  JSON/form failures and the reusable application-owned `SafeText` rule.
- Public [CI run 33999322140](https://github.com/ShanilKoshitha/goforge/actions/runs/33999322140)
  passed formatting, the live PostgreSQL race suite, vet, the CLI build, and
  both isolated format-7 PostgreSQL generated-application workflows with
  `-count=2` in 4 minutes 26 seconds. The workflows include generated tests,
  vet, race builds, cross-process/restart throttling, browser/API CRUD, ORM,
  jobs, migrations, graceful shutdown, and cleanup.
- Lightweight unsigned tags `v0.7.0` and `v0.7.1` point to their immutable
  public commits. A fresh-cache `go run
  github.com/ShanilKoshitha/goforge/cmd/forge@v0.7.1` generated a format-7 app
  without `--replace`; that app passed `go test ./...`, `go mod verify`, `go
  vet ./...`, and `go build ./cmd/...` against the public v0.7.0 runtime.
- Independent final read-only reviews found no remaining P0, P1, or P2 code or
  distribution defect.

## Explicit non-goals for v0.7

- Reflection/tag validators, runtime validation discovery, database-backed
  `unique`/`exists` rules, localization, multipart uploads, OpenAPI/schema
  generation, or client-side validation generation.
- Password reset, email verification, password change, account-wide session
  revocation, MFA/WebAuthn, session inventory, signing-key rotation, or an auth
  event ledger. Those require the dedicated account-security and notification
  milestones.
- Middleware aliases, annotations, dependency injection, runtime ordering,
  tracing exporters, metrics, compression, general host allowlists, or
  automatic trust of any forwarding header.
- Schema-driven resource fields, API versioning, bulk CRUD, soft deletion, or
  speculative JSON conventions unrelated to the request-boundary proof.

# v0.6 durable PostgreSQL jobs scorecard

Status: **accepted**

Target:

> From an empty directory, generate a PostgreSQL application that can define a
> typed job, enqueue it atomically with domain writes, run it in a conventional
> project-owned worker, recover from crashes, retry predictably, and operate
> terminal failures. Delivery is explicitly at least once; every durable state
> transition is inspectable SQL and every runtime component is replaceable.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Typed jobs are explicit generated Go | passing | `forge make:job SendWelcome` atomically creates an application-owned typed payload/handler/test plus a deterministic generated registry; duplicate names and partial writes are rejected without runtime scanning or reflection |
| Dispatch composes with transactions | passing | The same API accepts `*sql.DB` and `*sql.Tx`; a job is invisible before commit, durable after commit, and absent after rollback or panic |
| Claims are concurrent and bounded | passing | PostgreSQL workers claim only available capacity in short `FOR UPDATE SKIP LOCKED` transactions, commit before handlers run, use database time, and respect configured queues and concurrency |
| Leases recover safely | passing | Heartbeats extend leases; killed workers are reclaimed; every mutation is fenced by job ID, worker ID, and lease generation; stale owners cannot acknowledge, retry, release, or fail a reclaimed job |
| Retry behavior is durable and finite | passing | Attempts, timeout, maximum attempts, and backoff are snapshotted at dispatch; returned errors, timeouts, panics, explicit permanent failures, and retry-after overrides have deterministic tests; exhausted crashed leases terminate |
| Failure operations are complete | passing | Terminal jobs move atomically to `failed_jobs`; `forge queue:failed`, `queue:retry`, and `queue:forget` delegate to application-owned code, omit payloads by default, and pass live PostgreSQL tests |
| Delays and active deduplication are precise | passing | One-off delay/at scheduling uses durable database timestamps; concurrent dispatch with the same explicit key creates one active row and reports enqueued versus duplicate without claiming exactly-once execution |
| Worker lifecycle is production-shaped | passing | A generated `cmd/worker` runs outside the source tree, stops claiming on shutdown, drains within a configured grace period, cancels afterward, retains heartbeats while draining, and leaves forced work recoverable |
| Observability is useful and safe | passing | Structured lifecycle events cover dispatch through completion/failure with stable IDs, queue, attempt, timing, and outcome; default logs never contain payloads and observer failures cannot corrupt durable state |
| Storage boundaries are defensive | passing | Payload/error/name/key sizes are bounded, malformed payloads fail terminally, unknown job versions remain unclaimed for rolling deploys, SQL values are parameterized, and queue state errors preserve causes |
| Escape hatches remain complete | passing | Public store/observer contracts, raw `database/sql`, transaction dispatch, an application-owned worker main/registry/migration, and inspectable claim/ack primitives allow replacement without framework startup magic |
| Existing framework contracts remain intact | passing | Format-5 views and format-4 ORM compatibility fixtures retain their behavior; fresh format-6 auth, validation, sessions, owner CRUD, ORM, and view checks pass |
| Full generated workflow passes twice | passing | Two independent fresh format-6 applications pass tests, vet, race/build checks, generated-artifact inspection, and isolated PostgreSQL job/browser/ORM journeys without leaked schemas or processes |

## Baseline — 2026-09-05

- The accepted v0.5 worktree passes `go test ./... -count=1`; the complete run
  finished successfully in 62.113 seconds for the slowest package.
- The repository contains no job runtime, PostgreSQL queue adapter, job
  generator, worker command, queue migration, or queue operations.
- Existing `orm.Executor` and `database.Transaction` seams already allow one
  typed dispatch path to compose with both `*sql.DB` and `*sql.Tx` without
  coupling jobs to ORM policy.
- Generated applications already own explicit `cmd/server`, `cmd/console`,
  database construction, migrations, and generated registries. The worker and
  queue operations must follow the same inspectable ownership model.
- PostgreSQL is the only promised durable backend for this milestone. Polling
  with indexed claims is the correctness baseline; notification-based wakeups
  are only an optional later optimization with polling retained as fallback.

## Local passing runs — 2026-09-05

- The final worktree passed `go test ./... -count=1`, `go vet ./...`, and
  `go test -race ./... -count=1`. The slowest framework race package completed
  in 70.450 seconds.
- Direct runtime and PostgreSQL adapter suites passed both without and with
  `GOFORGE_TEST_DATABASE_URL`, including real transaction visibility, lease
  fencing/reclaim, retry/failure transitions, active-key reuse, unknown-version
  filtering, schema constraints, and defensive scans.
- Independent adversarial review found no remaining P0, P1, or P2
  implementation or acceptance-evidence defect after the final generated-app
  workflow and CI gate were added.
- CI runs both generated PostgreSQL workflows twice. Each jobs workflow also
  runs the fresh application's complete tests, vet, and race suite before its
  live worker journey.

The final generated jobs journey built the CLI, created a fresh format-6
application, generated and inspected two typed jobs and the explicit registry,
and exercised direct and transactional dispatch, commit/error/panic rollback,
durable `Delay` and `At`, active deduplication, two competing external workers,
finite retries, safe failed-job output, retry/forget administration, a hard-
killed lease owner followed by successor reclaim, acknowledgement, graceful
shutdown, schema cleanup, and process reaping.

1. 2026-09-05 — final strengthened jobs journey passed in 22.943 seconds.
2. 2026-09-05 — immediate independent fresh jobs journey passed in 23.223 seconds.

The compatibility browser/ORM journey also passed twice without intervening
edits, in 29.096 and 29.735 seconds. Those fresh applications passed generated
tests, vet, builds, migrations, relationship execution, JSON and browser auth,
CSRF, validation, owner-scoped CRUD, contextual escaping, and clean shutdown.
The exact combined CI command then repeated both workflows twice and passed in
105.083 seconds.

The isolated PostgreSQL 18 acceptance cluster was stopped and its
`.tmp/v06-postgres` data directory removed after the final run. Port 55432 no
longer accepted connections; no host PostgreSQL service was modified.

## Explicit non-goals for v0.6

- Exactly-once execution or automatic idempotency for arbitrary external side
  effects. Lease fencing protects queue state; handlers remain responsible for
  business-level idempotency.
- Recurring cron schedules, time zones/DST, missed-run policy, scheduler leader
  election, and overlap policy. Those form the separately gated scheduler
  milestone; one-off delayed dispatch is included here.
- Chains, batches, workflow orchestration, a web dashboard, Redis/SQS parity,
  dynamic package discovery, runtime dependency injection, or hidden workers
  started by the HTTP application.
- Concurrent generator processes. Generation is atomic and rollback-safe per
  process, but projects must run generators serially until a project lock or
  compare-and-swap metadata protocol is introduced.

# v0.5 view authoring core scorecard

Status: **accepted**

Target:

> From an empty directory, generate a server-rendered PostgreSQL application
> whose first-party `.forge.html` language provides Blade/Twig-class composition,
> form ergonomics, and diagnostics while compiling deterministically to ordinary
> contextually escaped `html/template`. Production performs no source discovery
> or Forge-language interpretation, and applications retain complete standard-Go
> escape hatches.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| The language has a positioned grammar | passing | A lexer/parser with source spans replaces regex rewriting; nested directives, quoted arguments, balanced pipelines, escaped `@@`, comments, and mismatched closures have exact path/line/column tests |
| Components are complete compile-time composition | passing | Static components support required and literal-default props, named arguments, default/named slots, nesting, and strict missing/unknown/duplicate/cycle failures; expansion occurs before `html/template` contextual analysis and emits no runtime component machinery |
| Control flow and stacks are coherent | passing | Nested `@if`/`@elseif`/`@else`, `@for`/`@empty`, and `@with` compile standard Go-template pipelines; page-local `@push`/`@stack` ordering is deterministic and scoped constructs cannot leak across pages |
| Forms have explicit ergonomic state | passing | `@csrf`, `@method`, `@old`, and `@errors` compile to inspectable markup/actions over explicit `.Form` data; submitted empty values differ from absent values and every validation message remains escaped |
| Custom functions use one application registry | passing | An editable application-owned `template.FuncMap` is used by the project compiler and production renderer; string results remain escaped, missing functions fail before publication, and `template.HTML` remains conspicuous |
| Diagnostics survive composition | passing | Compile and render failures identify original source path/line/column plus page-to-layout/include/component/slot expansion context; buffered rendering never commits a partial response |
| Compilation is deterministic and checkable | passing | `forge views:compile` emits byte-identical gofmt'd source and mappings; `--check` is non-mutating and distinguishes current, stale, and invalid input; every failure preserves the byte-identical last-good artifact |
| Generated applications use the language | passing | Fresh auth/resource views and `forge make:component` exercise components, props, slots, stacks, control flow, explicit form state, and application functions instead of hand-expanding framework markup |
| Standard Go remains the escape hatch | passing | Generated output is inspectable canonical `html/template`; ordinary `{{...}}` pipelines remain legal, raw `.html` works through `view.Parse`, callers can use direct `html/template` and `Engine.Templates`, and production runs outside the source tree |
| Full generated workflow passes twice | passing | Two independent fresh format-5 applications pass tests/vet/build and isolated PostgreSQL browser journeys with adversarial escaping, diagnostics, stale checks, clean shutdown, and no leaked schemas/processes |

## Baseline — 2026-09-05

- v0.4 is accepted. Its fresh applications already compile `.forge.html` into a
  deterministic generated `views_gen.go`, parse only that artifact in
  production, use standard-library contextual escaping with `missingkey=error`,
  and buffer rendering before committing a response.
- The focused baseline passes: `go test ./view ./internal/cli -run
  'Forge|View|views' -count=1`.
- The baseline source language had layouts, named sections/yields, and includes
  with explicit data. Expressions and control actions are ordinary Go-template
  pipelines, and direct `view.Parse` remains available.
- The baseline compiler was a regex/text-rewrite pipeline. It could not safely
  represent nested components, slots, or control blocks and lost useful source
  provenance after composition.
- Baseline generated forms hand-wrote CSRF fields, method overrides, old-input
  lookups, and error loops. Managed compilation and production both passed a nil FuncMap,
  so application functions cannot use the complete managed workflow.
- Baseline `forge serve` performed a compile before startup and retained the last
  good artifact, but had no non-mutating `views:compile --check` mode.

## Local passing runs — 2026-09-05

- The final merged worktree passed `go test ./...`, `go vet ./...`, and
  `go test -race ./...` after the positioned compiler, source maps, format-5
  application compiler, and compatibility fixes landed.
- Focused adversarial tests cover multiline and Unicode source positions,
  synthetic-directive clamping, layout/include/component/slot expansion chains,
  contextual text/attribute/URL escaping, explicit `template.HTML`, strict
  props/slots/cycles, page-local stacks, submitted-empty form state, missing and
  corrupt artifact recovery, non-mutating stale checks, and rollback safety.
- A frozen format-4 compiler retains the v0.4 grammar and artifact shape. A real
  compatibility fixture passed compile/check/build/serve/last-good behavior
  while preserving text that only format 5 treats as directives.
- Independent final review found no remaining P0 or P1 implementation or
  test-evidence defect.

Each final PostgreSQL run built the CLI, generated a fresh format-5 application,
generated a component and Issue resource, ran view and ORM freshness checks,
inspected the mapped artifact for canonical output, proved an exact composed
source diagnostic and byte-identical last-good artifact, and passed generated
tests, vet, and builds. Each new isolated schema then passed relationship-rich
ORM execution, concurrent/idempotent and transactional migrations, JSON/browser
authentication, explicit-form validation, owner CRUD, contextual escaping,
optimistic conflict recovery, production startup outside the source tree, and
clean shutdown.

1. 2026-09-05 — final format-5 journey passed in 33.22 seconds.
2. 2026-09-05 — immediate independent fresh journey passed in 34.58 seconds.

The isolated PostgreSQL 18 acceptance cluster was stopped and its workspace
data directory removed after both runs. The host's pre-existing PostgreSQL
service was not modified.

## Explicit non-goals for v0.5

- A proprietary expression/filter language, dynamic component or include
  names, macros/compiler plugins, runtime source discovery, or production
  recompilation.
- Class-backed/stateful components, dependency injection inside templates,
  attribute bags/class merging, implicit request globals, or automatic
  model-to-form binding.
- Localization, asset bundling, browser live reload/HMR, client hydration,
  fragment caching, streaming, or templates authored by untrusted users.
- Automatic upgrades of pre-v1 format-4 generated applications. Their public
  runtime APIs and existing compiled artifacts remain compatibility evidence.

# v0.4 typed data mapper ORM scorecard

Status: **accepted**

Target:

> From an empty directory, generate a PostgreSQL application whose ordinary Go
> models support typed querying, partial changes, relationships, eager loading,
> transactions, locking, and stable persistence errors. Generated mapping and
> SQL remain deterministic and inspectable; every operation can compose with
> `database/sql` and handwritten repositories.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Typed mapping covers real field states | passing | Generated models and mappers round-trip required and zero values, nullable values and explicit NULL, database defaults/generated IDs, timestamps, and a `Scanner`/`Valuer` escape-hatch type without runtime reflection |
| Queries and mutations are complete and safe | passing | Typed create/find/filter/order/paginate/count/exists/partial-update/delete and bulk operations expose deterministic SQL/args; values are always parameters; updates/deletes require predicates or conspicuous `AllRows()` |
| Relationships are first-class and explicit | passing | Belongs-to, has-one, has-many, and many-to-many generation, eager loading, and attach/detach pass focused and live tests; no lazy I/O and each preload edge has a bounded constant query count |
| Transactions and concurrency compose | passing | The same generated store works with `*sql.DB` and `*sql.Tx`; cross-model commit/rollback/panic behavior, handwritten SQL in the same transaction, `FOR UPDATE`/`FOR SHARE`, and optimistic conflict handling pass against PostgreSQL |
| Persistence errors and cancellation are stable | passing | Not-found, unsafe mutation, stale data, unique, foreign-key, check, serialization, and deadlock errors preserve causes; deadlines cancel promptly and leave the pool reusable without leaking submitted values |
| Ownership and mass-assignment boundaries remain strong | passing | Generated create/change types protect IDs, owner IDs, timestamps, versions, and relationship ownership; CRUD, bulk operations, aggregates, and preloads cannot cross authenticated owners |
| Generation is deterministic and inspectable | passing | Format-4 `make:model`, relationship/resource generation, `orm:generate`, and `orm:generate --check` emit gofmt'd model/mapping/migration source atomically; stale or invalid declarations fail before writes |
| Escape hatches remain complete | passing | Callers can inspect `orm.Statement`, supply `*sql.DB`/`*sql.Tx`, execute raw SQL in the same transaction, and replace generated repositories without changing controllers |
| Existing framework behavior remains compatible | passing | Fresh v4 JSON/browser authentication, sessions, validation, CSRF, throttling, views, resource routes, pagination, migrations, and production lifecycle retain their v0.3 contracts |
| Full generated workflow passes twice | passing | A fresh relationship-rich application passes tests/vet/build and the isolated PostgreSQL ORM journey twice without regression or leaked schemas/processes |

## Baseline — 2026-09-05

- v0.3 browser applications are accepted; framework tests, vet, race tests,
  fresh generated applications, and two live PostgreSQL journeys pass.
- Current generated resources use secure owner-scoped handwritten repositories,
  not an ORM. Metadata records no fields, nullability, defaults, indexes, or
  relationships.
- Repositories hold `*sql.DB`, while transactions expose `*sql.Tx`; composed
  generated operations cannot currently share a transaction without rebuilding
  repositories.
- SQL values are parameterized, request types are explicit write allowlists,
  authenticated owners are controller-derived, and contexts already propagate.
  The ORM must preserve these strengths.

## Foundation evidence — 2026-09-05

- The reflection-free `orm` runtime now has typed table/column metadata,
  immutable query and mutation builders, explicit field states, structured
  persistence errors, transaction-required row locks, statement observation,
  and all four common relationship loaders. Its focused race tests and vet pass.
- The static model parser validates ordinary Go declarations, PostgreSQL names,
  exact foreign-key types, field states, and relationship metadata without
  runtime discovery. `forge orm:generate` and `--check` deterministically emit
  typed mappers, columns, stores, create inputs, and changesets while preserving
  the last good generated artifact on failure.
- `forge make:model` now creates a conventional application-owned model, paired
  plain SQL migration, and current ORM artifact as one rollback-safe generator
  operation. Fresh scaffolds own their User model, and `make:resource` adds an
  owner-related model plus an ORM-backed repository while retaining the public
  controller repository interface.
- The combined repository race suite and vet passed after the runtime slice.
  At that foundation checkpoint this was package-level evidence only; the
  generated-application PostgreSQL acceptance recorded below closes that gap.
- An isolated PostgreSQL 18 cluster then ran the expanded core ORM journey
  twice, with the second pass under the race detector. Both passes exercised
  real generated/default/NULL field behavior, typed single and bulk mutations,
  aggregates, optimistic success/stale/wrong-owner behavior, raw SQL composed
  inside commit and rollback transactions, `FOR UPDATE` and `FOR SHARE`
  blocking, all four relation loaders plus many-to-many attach/detach,
  classified constraint/serialization/deadlock errors, deadline cancellation
  with pool reuse, and eager-load query budgeting. The cluster is test-only and
  separate from the host's existing service. These passes proved the runtime
  SQL before the generated-application flow was closed below.
- An early fresh format-4 application completed the live PostgreSQL workflow in
  14.682 seconds. The final relationship-rich runs below supersede that partial
  checkpoint and include optimistic HTTP conflicts and all generated relation
  kinds.

## Local passing runs — 2026-09-05

The final framework verification passed `go test ./...`, `go vet ./...`, and
`go test -race ./...`. An independent closure review found no remaining P0 or
P1 implementation defect.

Each final run built the CLI, generated a fresh format-4 application and Issue
resource, added conventional Tenant, TenantProfile, Project, and Tag models,
regenerated and checked the inspectable ORM artifact, and passed generated-app
tests, vet, and builds. Against a new isolated PostgreSQL schema, each run then
proved concurrent/idempotent migrations, generated nullable belongs-to,
has-one, has-many, and many-to-many loaders, tenant-scoped preloads, exact query
budgets, attach/detach, transactional migration recovery, JSON/browser auth and
owner CRUD, optimistic conflict recovery, production startup, and clean process
shutdown.

1. 2026-09-05 — final relationship-rich journey passed in 21.97 seconds.
2. 2026-09-05 — immediate independent fresh journey passed in 22.50 seconds.

The isolated PostgreSQL 18 acceptance cluster was stopped and its workspace
data directory removed after both runs. The host's pre-existing PostgreSQL
service was not modified.

## Explicit non-goals for v0.4

- Runtime reflection, package scanning, Active Record `Save`, lazy loading,
  identity maps, implicit units of work, automatic startup migration, or schema
  diffing.
- Hidden validation/hooks, automatic tenant scopes, transparent caching,
  polymorphic relationships, composite primary keys, nested transactions, or
  multi-dialect parity.
- Rewriting the specialized session store or migration bookkeeping through the
  ORM; they remain deliberate raw-SQL examples.

# v0.3 browser applications scorecard

Status: **accepted**

Target:

> From an empty directory, produce a secure server-rendered application with
> HTML authentication and owner-scoped CRUD. GoForge templates compile into
> deterministic, inspectable `html/template` artifacts while retaining
> contextual escaping and ordinary Go escape hatches.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| GoForge view source compiles deterministically | passing | `.forge.html` layouts, sections, yields, and explicit includes compile to canonical `html/template`; invalid dependency graphs have focused tests |
| Generated view artifacts are inspectable | passing | Fresh scaffolds contain managed `views_gen.go`; generated production startup parses canonical output rather than the GoForge DSL |
| HTML authentication is secure end to end | passing | Fresh generated-app and live PostgreSQL tests cover registration, login/logout, CSRF, rotation, validation, sanitized input, redirects, one-request flash, API/browser boundary isolation, and bounded source/account throttling |
| Generated HTML CRUD is usable end to end | passing | Fresh generated-app and live PostgreSQL tests cover protected paginated index/create/show/edit/update/delete, bounded query parsing, method override, CSRF, owner assignment/isolation, and contextual escaping |
| Existing JSON behavior remains compatible | passing | Fresh v0.3 applications retain the unchanged JSON tests and run them first in the combined PostgreSQL journey; pre-v1 generated-project upgrades are not claimed |
| Browser session concurrency is safe | passing | Create/update/atomic-rotate persistence prevents a stale request from restoring a rotated session ID |
| Fresh generated application passes twice | passing | Two independent fresh v3 applications passed tests/vet/builds, followed by two isolated-schema PostgreSQL browser journeys without regression |

The first grammar is intentionally limited to inheritance, named sections,
yields, includes with explicit data, and standard Go template actions.
Components, slots, macros, asset pipelines, and the ORM remain separate later
milestones; they were not added after this milestone's acceptance gate passed.

## Local passing runs — 2026-09-05

The framework passed `go test ./...`, `go vet ./...`, and `go test -race ./...`.
Two applications generated independently from the freshly built v0.3 CLI then
each generated an `Issue` resource and passed `go test ./...`, `go vet ./...`,
and `go build ./cmd/...`. Inspection confirmed that production startup consumes
the generated `views_gen.go` artifact rather than the GoForge source grammar.

An isolated PostgreSQL 18 cluster was then used for the written end-to-end
journey. Both runs generated a fresh application/resource, applied migrations
concurrently, recovered from failed transactional DDL, exercised JSON and HTML
authentication plus owner-scoped CRUD, ran the production binary outside the
source directory, and cleaned their schemas and processes.

1. 2026-09-05 — live PostgreSQL journey passed in 12.871 seconds.
2. 2026-09-05 — immediate second journey passed in 12.740 seconds.

The temporary acceptance cluster was stopped and removed after both runs. The
host's pre-existing PostgreSQL service was not modified.

# v0.2 milestone scorecard

## Repository quality review — 2026-09-04

Current goal: review the whole repository for clean, idiomatic Go, understandable
structure, and concise useful comments; fix identified issues and verify emitted
applications. This is a quality pass on v0.2, not a feature milestone.

| Requirement | State | Evidence |
| --- | --- | --- |
| Review every runtime package | passing | Independent reviews covered HTTP/lifecycle, storage/password/session, and cache/config/validation/views; concrete defects have focused regressions |
| Separate source templates from generation logic | passing | Embedded scaffold/resource trees and one renderer replace the obsolete overlay and escaped source strings; fresh scaffold/resource tests pass |
| Use readable names, contracts, and comments | passing | Initialism handling, named repository parameters, concise concurrency/lifecycle contracts, and CONTRIBUTING.md |
| Verify the integrated runtime and generated app | passing | Full race/vet suite, fresh app tests/vet/builds, healthy generated Compose, and two PostgreSQL journeys in 13.859s and 13.807s |
| Record the assessment and remaining limitations | passing | CODE_QUALITY.md records findings, evidence, compatibility changes, and serial generator assumption |

Final independent closure review caught one remaining discarded migration-pair
cleanup error. It now joins the original failure; migration allocation/refusal
tests and CLI vet passed afterward. The temporary Compose database and volume
were removed after confirming zero leaked acceptance schemas.

The original v0.2 acceptance record below is retained as historical evidence.

Target:

> From an empty directory, produce a working authenticated PostgreSQL CRUD
> application in under ten minutes, with migrations, validation, HTML or JSON
> responses, tests, and production middleware.

Status values are **missing**, **partial**, or **passing**. Evidence must come
from the current worktree or a freshly generated application.

## Acceptance criteria

| Criterion | Baseline | Required evidence |
| --- | --- | --- |
| `forge new issueboard` creates a production-shaped PostgreSQL application | passing | Fresh scaffold contains database configuration, pinned pgx driver, JSON auth, database sessions, embedded migrations, Compose file, console, tests, and compiles |
| `forge make:resource Issue` creates a complete inspectable resource | passing | Model, owner-scoped PostgreSQL repository, validation, controller, migration, authenticated routes, and tests are generated; duplicate and successive-resource tests pass |
| `forge migrate` applies pending migrations safely | passing | Real PostgreSQL acceptance applies auth and resource migrations exactly once under two concurrent application processes; failed DDL rolls back and the same version reapplies |
| `forge serve` runs the generated application | passing | Real PostgreSQL acceptance launches the literal project command and observes readiness; the generated Unix entrypoint handles SIGTERM and lifecycle unit tests cover graceful/failed shutdown |
| Authentication is usable end to end | passing | Generated unit and real PostgreSQL tests cover registration, duplicate email, live logout/login, login session rotation, invalid credentials, protected routes, bounded expired-session pruning, and password hashing |
| Generated CRUD is usable end to end | passing | Real PostgreSQL acceptance covers authenticated create/show/update/delete, invalid input, unauthenticated access, and cross-owner isolation |
| Standard `net/http` compatibility is preserved | passing | `ResponseController` plus direct `Flusher`, `Hijacker`, and `Pusher` capability tests pass through logging middleware; unsupported capabilities are not falsely advertised |
| High-risk infrastructure is directly tested | passing | Lifecycle shutdown/failure, transaction commit/rollback/panic, failed migration, fake contention, and real PostgreSQL contention tests pass |
| Claims and CI match reality | passing | Dialect guarantees are precise; README workflow is exercised; CI provisions PostgreSQL and runs the generated-app acceptance twice |
| Milestone passes twice without regression | passing | Two consecutive isolated-schema PostgreSQL acceptance runs are recorded below |

## Baseline evidence — 2026-09-04

- Existing `go test ./...` passes and the scaffold compilation test is green.
- Package coverage: application 0%, database transaction helper 0%, migrations
  19.3%, HTTP 52.3%, CLI 67.4%.
- CLI exposes `new`, controller/request generation, and migration-file generation.
- Generated applications have no database construction, auth, project command,
  resource workflow, or integrated migration execution.
- The workspace is not a Git repository; release tagging cannot yet be verified.

## Explicitly deferred

- `forge rollback`, `forge routes`, wrapper commands for `forge test` and
  `forge build`
- background jobs and workers
- HTML authentication and resource views
- field-definition DSLs and additional SQL dialect guarantees
- release tagging and licensing, which require a Git repository and an explicit
  project licensing decision

## Passing runs

1. 2026-09-04 — final PostgreSQL 18 acceptance pass completed the literal
   four-command journey in 12.570 seconds.
2. 2026-09-04 — an immediate second isolated-schema pass completed in 13.210
   seconds and left no acceptance schemas behind.

Final verification also passed `go test -race ./...`, `go vet ./...`, and
`go build ./cmd/forge`. A fresh `issueboard` scaffold with an `Issue` resource
passed `go test ./...`, `go vet ./...`, and builds of both generated commands.
Independent architecture review reported no P0 or P1 blockers; its remaining
P2 findings were fixed before the final two runs above.
