# Repository quality assessment

Reviewed on 2026-09-04 against the current repository, including every runtime
package, CLI generators, generated application source, tests, documentation, and
CI configuration. Go's own conventions are the reference; the repository was
accessible, so a Goravel fallback was unnecessary.

The foundation is sound: packages have clear responsibilities, dependencies are
explicit, HTTP and SQL use standard-library boundaries, and application code is
inspectable. The main maintainability weakness was the generator implementation:
large escaped strings, duplicated obsolete scaffolding, and mixed rendering and
file-publication logic. The review corrected this and the concrete defects below.

## Findings and changes

| Area | Finding | Result |
| --- | --- | --- |
| Generator structure | Source was embedded in large Go strings and an obsolete scaffold overlay | Templates now mirror the output tree; a shared renderer formats Go and checks substitutions |
| Generator boundaries | Rendering, metadata allocation, project parsing, and file writes were interleaved | Separate focused files; write/close errors and rollback failures are preserved |
| Names and output | Initialisms split into single letters; resource imports and declarations could collide | Idiomatic initialisms and lowercase package names; explicit aliases; reserved names rejected before writes |
| Scaffold configuration | Unquoted paths/names and a PostgreSQL 18 volume at the old data path | Quoted replacements/YAML names, formatted output, correct versioned-volume parent |
| HTTP | Response commit tracking depended on Logger, allowing late errors to append JSON | Tracking now belongs to request context and works through Router and Standard |
| HTTP interoperability | Rejected routes left metadata behind; abort/101/OPTIONS behavior was inconsistent | Registration bookkeeping follows mux validation; protocol and CORS regressions cover fixes |
| JSON authentication | JSON binding accepted form-compatible media types | Explicit application/json requirement with 415 rejection before decoding |
| Auth validation | Names were validated before trimming; login omitted the password upper bound | Validation matches stored name semantics and registration's password limit |
| Sessions/passwords | Destroy accepted committed responses; oversized hash work factors could produce unverifiable hashes | Consistent response guards, contextual errors, and bounded configuration |
| Cache | Expired reads could delete a newly replaced entry | Expiry rechecked under the write lock; deterministic concurrency regression |
| Config/validation/views | Dense builders, mutable OneOf input, sparse contract documentation | Readable methods, defensive copying, and concise precedence/Unicode/concurrency comments |

HTTP response-writer capability code is separate from middleware. Comments focus
on ownership, deadlines, concurrency, and ordering rather than narrating ordinary
code. Generated repository interfaces name their parameters so owner IDs, record
IDs, and adjacent strings can be distinguished at a glance.

## Verification

- Full `go test -race ./...` and `go vet ./...` passed.
- CLI build passed. Fresh generated applications compiled and tested with `Issue`,
  `Category`, `APIClient`, `SQL`, `Auth`, and `Context` resources in regression tests.
- The inspected `tmp/quality-review` application with Issue/APIClient passed its
  own tests, vet, and builds of both server and console.
- The generated PostgreSQL 18 Compose service reached healthy state. Its named
  volume mounts at `/var/lib/postgresql`, containing the versioned data directory.
- Two consecutive live PostgreSQL workflows passed in 13.859 and 13.807 seconds,
  including registration/login/logout, owner-scoped CRUD, migration locking,
  rollback recovery, expired-session pruning, and isolated-schema cleanup.
- Focused regressions cover formatting/dotfiles, preserved HTML expressions,
  quoted paths, unique secrets, naming collisions, and rejected non-JSON input.

Live acceptance ran on Windows. The Unix SIGTERM assertion remains in the Linux
CI workflow; this review does not claim a remote CI run. The initial local port
5432 attempt reached another PostgreSQL service; the isolated review service was
moved to 15432 and both final runs passed there.

## Compatibility and operating assumptions

JSON callers must now send `Content-Type: application/json`. Newly generated
compound resource package names use lowercase without underscores; existing
resource metadata keeps its recorded paths. Existing generated applications are
not rewritten by this template change.

Run generators serially within a project. Rollback protects failed generation,
but simultaneous generator processes are not serialized. Migration execution is
separately serialized with PostgreSQL advisory locks. These are different kinds
of operations with different guarantees.

The conventions for subsequent work are in [CONTRIBUTING.md](../CONTRIBUTING.md).
The quality pass adds no service container, ORM, route discovery, or framework
runtime dependency.
