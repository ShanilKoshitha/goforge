# Contributing

GoForge follows [Effective Go](https://go.dev/doc/effective_go),
[Go Code Review Comments](https://go.dev/wiki/CodeReviewComments), and
[Go Doc Comments](https://go.dev/doc/comment). Use judgment: a useful contract
comment is better than repeating a function's name in a sentence.

## Code and structure

- Keep packages focused on one concern. Runtime packages must work without the CLI.
- Use explicit constructors and small interfaces at dependency boundaries. Name
  interface parameters when adjacent values have the same type.
- Use normal Go names and initialisms (`userID`, `HTTPClient`). Package names are
  short, lowercase words; SQL tables and filenames may use underscores.
- Return early on errors. Add context with `%w` when it helps locate a failure;
  preserve rollback errors with `errors.Join`. Never silently ignore a failed
  write or hide an error that changes the operation's result.
- Use comments for public contracts, ownership, concurrency, ordering, and
  non-obvious decisions. Avoid narrating straightforward assignments or getters.
- Format source with `gofmt`. Keep tests alongside the code they exercise, using
  table cases where behavior is parallel. Prefer controlled clocks and explicit
  synchronization to timing-dependent sleeps.

## Generator changes

Edit source templates under `internal/cli/templates/scaffold` or `resource`.
Templates mirror the generated layout and use `[[.Field]]` substitutions so
ordinary Go HTML templates retain their `{{...}}` expressions. All emitted Go
passes through `go/format` before files are published. Include dotfile templates
when adding a scaffold file.

Application code is written once. Only the resource registry and its metadata
are generator-owned. Keep SQL, dependencies, validation, and route registration
visible in the generated application. Inspect a freshly generated project after
changing templates; a passing framework test alone does not prove the output.
Run generators serially in a project; file rollback is not a concurrency lock.

## Verification

```sh
go test -race ./...
go vet ./...
go build -o bin/forge ./cmd/forge
```

CLI tests create fresh applications, test their generated packages, and check
generator refusal and naming behavior. For the live PostgreSQL workflow, set
`GOFORGE_TEST_DATABASE_URL` to a disposable PostgreSQL database whose role can
create schemas, then run:

```sh
go test ./internal/cli -run TestGeneratedPostgresWorkflow -count=2 -v
```

The test uses isolated schemas and removes them. Unix additionally checks
SIGTERM shutdown; Windows cleans up the spawned process tree. CI runs the Unix
path with PostgreSQL 18. Record substantive decisions in `docs/DECISIONS.md` and
review evidence in `docs/SCORECARD.md`.
