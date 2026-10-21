# v0.23 read-only diagnosis and application inventory

Status: **accepted** — v0.23.0 runtime and v0.23.1 format-17 adoption published; public distribution verified — 2026-10-21.

## Runnable baseline

The starting release is public v0.22.1: format 16, runtime v0.22.0. Framework
configuration, migration, and HTTP-router tests pass (0.484s, 0.529s, 0.771s).
Fresh-scaffold/workflow baseline tests pass (31.539s). The retained public,
no-replace `example.com/releasedscale` application's configuration, application,
and route tests pass (0.581s, 2.018s, 1.027s); its ordinary Go view and asset
checks pass with `-mod=readonly`.

At baseline there was no `forge doctor` or `forge routes:list`. Artifact checks
stopped on the first failure. Reader conversion errors and driver errors could
include supplied values. Migration `Verify` reads without upgrading the ledger; `Status` may
create/alter/backfill it. `Router.Routes()` already exposes actual registration.

## Contract

`forge doctor` reports all independent checks, with stable check identifiers,
pass/fail/skipped states, safe explanations, and explicit next actions. It does
not fix anything. Application-owned ordinary Go owns configuration, database,
and construction checks; the CLI orchestrates existing ORM/view/asset checks.

`forge routes:list` prints method, path, and optional name from the same
application construction/registration used by the server. It does not parse Go
source or maintain a second route manifest, invoke handlers, or start a server.
It needs valid HTTP configuration but no reachable database. Constructor
failures are reported safely rather than replaced with a fictional route list.

The CLI supplies the full guarded orchestration: readonly modules, external
shared caches/temporary builds, disabled workspaces, bounded process trees, and
safe subprocess summaries. Direct `go run -mod=readonly ./cmd/diagnostics ...`
is an ordinary escape hatch: the Go tool runs before application code, so the
caller must select external caches and `GOWORK=off` when requiring the same
build-tool nonmutation boundary, and deliberately control Go flags and output
targets. Direct compiler output retains ordinary Go
behavior; application diagnostic reports themselves never echo raw errors.

Fresh applications introduce format 17 only after the new runtime APIs
are publicly available with verified checksums. Existing applications are not
rewritten; older formats receive an explicit unsupported-command explanation.
Runtime-first preparation retained format 16 while explicit candidate fixtures
qualified the new scaffold. Runtime v0.23.0 is now published and publicly
verified before v0.23.1 adopted format 17 with its exact public sums. Adoption
passed two complete independently audited final-content matrices and a fresh
public installer/no-replace application proof. Runtime publication alone was
not acceptance. Final evidence and retained failures are in the
[scorecard](SCORECARD.md).

## Measurable acceptance criteria

1. **Complete fresh workflow.** A fresh supported app runs both commands through
   the CLI and directly through inspectable application-owned Go. A healthy
   migrated app passes doctor. Nonhealthy/unsupported/invalid invocations exit
   nonzero. Every independent check still runs when another fails; dependent
   checks are explicitly skipped, never represented as healthy.
2. **Useful safe diagnosis.** Check HTTP, worker/mail, scheduler, and database
   configuration, application construction, PostgreSQL availability, migration
   integrity, ORM freshness, compiled-view freshness, and embedded assets.
   Invalid configuration identifies trusted keys/categories and correction
   guidance without printing values. Driver/compiler/application errors are not
   forwarded raw. Secret-shaped malformed values, DSN credentials, SMTP
   passwords, session/outbox secrets, SQL, and panic stacks do not appear in
   diagnostic output, including subprocess failure paths.
3. **Read-only migration states.** Real PostgreSQL coverage distinguishes a
   healthy ledger, pending migrations, applied migration drift (changed checksum,
   renamed or missing source), and an uninitialized/legacy ledger requiring
   deliberate operational action. An incomplete, empty, or inconsistently named
   pair is a distinct invalid-source failure before ledger inspection; do not
   claim applied drift without evidence from the ledger. Reuse authoritative
   `Verify` logic with typed error categories; no string matching or duplicate
   checksum implementation.
   Diagnosis never calls `Up`, `Down`, or operational `Status`.
4. **Nonmutation and bounds.** Snapshot application files/directories and
   database schema/ledger before and after healthy and failed commands. Include
   `.env`, manifest, registry, `go.mod`, `go.sum`, generated files, and absent
   generator-lock/build-output paths. Go subprocesses use `-mod=readonly` and
   `GOWORK=off`; conventional shared user caches and temporary Go build files
   outside the project are allowed. Diagnostic subprocesses explicitly select
   external caches rather than inheriting app-local Go cache settings.
   Suppress both process and persisted `GOFLAGS`, including linker-profile and
   alternate-module/overlay flags, without disabling unrelated persisted private
   module/proxy configuration or changing the caller's environment file.
   No source/lock/build publication, migration bookkeeping/domain writes, SMTP
   connection, server bind, job claim, or scheduler startup occurs. DB checks
   have finite deadlines; cancellation stops descendants through the existing
   process-tree boundary. Application-owned custom code remains trusted code,
   not a sandbox; document that constructors/checks must remain side-effect-free.
5. **Independent artifact failures.** Dirty ORM, dirty Forge source, and invalid
   assets are detected individually and together without fixing files. A
   compile failure is a failed check, not an empty successful report. Repair
   guidance names explicit existing commands; drift is never advised away by
   automatic rebaselining.
6. **Actual route inventory and escape hatches.** Inventory includes built-in,
   generated resource, and handwritten named routes. An explicit registration
   edit changes the printed inventory, which matches the production router.
   PostgreSQL may be offline. Default constructors perform no DB/network work.
   Tests replace a diagnostic check using ordinary application Go and retain
   the rest of the workflow; no reflection or hidden discovery is introduced.
7. **Compatibility and public distribution.** Retain all historical generator
   contracts, format-16 output, and current runtime pins until deliberate
   adoption. New commands never inject support into an existing project.
   Publish/verify immutable runtime APIs before pinning a new scaffold, then
   verify public CLI installation and a no-replace application with exact sums.
8. **Qualification.** Framework and fresh generated-app tests, race checks,
   vet/build, real PostgreSQL nonmutation/failure journeys, supported Windows
   and Linux environments, independent adversarial review, and two complete
   final-content passes are required. Narrow baseline checks are not milestone
   acceptance. Record failures and actual execution/skips transparently.

## Deferred scope

No auto-fix, migration operations UI, schema diffing, dependency installation,
route-source parser, middleware/auth-policy inference, external SMTP probe,
background-service readiness orchestration, or v1 adoption tool. Operational
migration commands remain the later v0.25 workflow. This milestone reports
evidence; it does not promise that every custom application behavior is safe.
