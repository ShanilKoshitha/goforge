# Read-only application diagnosis

This guide describes format-17 applications from the v0.23.1 adoption candidate.
The v0.23.0 runtime is public and verified; complete adoption qualification and
public CLI distribution remain pending. Older public CLIs retain their released
scaffold contracts. See [acceptance evidence](SCORECARD.md).

From the application root:

```sh
forge doctor
forge routes:list
```

`doctor` reports HTTP, worker/mail, scheduler, and database configuration;
application construction; PostgreSQL reachability; migration integrity; generated
ORM freshness; compiled Forge-view freshness; and embedded asset validity. A
failed check does not prevent independent checks from running. A dependent check
is marked skipped rather than healthy. Failed or skipped checks exit nonzero.

No repair is implicit. After reviewing the report, run the specific existing
command deliberately: `forge migrate`, `forge orm:generate`, `forge views:compile`,
or `forge assets:check`. Migration drift requires investigating the applied
history and its reviewed source; do not erase the evidence by rebaselining it.
An uninitialized or legacy ledger requires an explicit operational migration
decision. Diagnosis uses read-only migration `Verify`, not operational `Status`.

`routes:list` prints quoted method, path, and optional name from the actual
production router, including generated and handwritten registrations. It does
not infer authorization or middleware policy. PostgreSQL can be offline, but HTTP
configuration and application construction must be valid. It does not run
handlers, bind a server, connect to SMTP, claim jobs, or start the scheduler.

## Output and boundaries

Diagnostic output contains fixed explanations and trusted configuration keys,
not supplied values, raw driver/compiler errors, SQL, or panic stacks. A compile
failure is reported as a failed application check, never an empty successful
inventory. Use ordinary Go commands deliberately when low-level compiler output
is needed; that output is not the CLI's sanitized diagnostic report.

The CLI runs subprocesses with readonly modules, `GOWORK=off`, bounded captured
output, deadlines, process-tree cancellation, and external shared Go caches and
temporary builds. Application-local cache settings are not inherited. It does
not change `.env`, source, generated files, `go.mod`, `go.sum`, migration records,
or domain data, and does not publish generator locks or build artifacts.

Guarded diagnostics also ignore process and persisted `GOFLAGS`: ordinary linker
profiling flags can otherwise create files inside the application even when its
constructors are side-effect-free. Other persisted Go settings, including private
module/proxy configuration, remain available; the CLI does not edit the user's
Go environment file. Applications requiring custom build flags can use the direct
Go escape hatch with deliberate external output paths.

This is a point-in-time inspection, not a deployment readiness guarantee, a
security audit, an SMTP delivery test, or a sandbox for application code.
Constructors and custom checks are trusted ordinary Go and must remain free of
side effects.

## Inspectable application ownership

`cmd/diagnostics` owns the command entry point and versioned JSON report.
`internal/diagnostics` owns explicit check callbacks and production construction.
Edit or replace those functions using ordinary Go; there is no reflection,
runtime discovery, duplicated route manifest, or source-code parser.

Direct execution is an escape hatch:

```sh
go run -mod=readonly ./cmd/diagnostics doctor
go run -mod=readonly ./cmd/diagnostics routes:list
```

The Go tool runs before application code. When requiring the same build-tool
nonmutation boundary for direct execution, explicitly use `GOWORK=off` and
external `GOCACHE`, `GOMODCACHE`, and `GOTMPDIR` locations. Direct compiler output
has normal Go behavior; application diagnostic JSON retains safe categories.
The direct caller also owns `GOFLAGS` and any profiling or other output targets;
readonly modules alone do not prevent linker profiling files.

Older application formats receive an unsupported-command explanation. Running
these commands never injects diagnostic source or upgrades existing applications.
