# Durable recurring schedules

GoForge schedules are explicit Go definitions that materialize typed jobs into
the existing PostgreSQL queue. The scheduler never runs handlers itself. An
ordinary queue worker remains the only process that executes application
effects.

The delivery contract remains **at least once**. Schedule coordination prevents
two active materializations for one definition, but handlers still need an
application idempotency key for external effects that cannot participate in the
database transaction.

## Define schedules in application code

A schedule binds a versioned schedule name and five-field cron expression to an
existing typed job definition:

```go
var RebuildSearchNightly = schedule.MustDefine(
	"search.rebuild-nightly.v1",
	"0 2 * * *",
	RebuildSearchIndexDefinition,
	schedule.Static(RebuildSearchIndex{Scope: "all"}),
	schedule.TimeZone("America/Halifax"),
	schedule.MisfireGrace(15*time.Minute),
)
```

`schedule.Static` snapshots the payload as canonical JSON when the definition
is created. Mutating the original map, slice, or pointer later cannot change
the scheduled payload or its fingerprint.

Use an explicit dynamic factory when the payload depends on the occurrence:

```go
var BuildDailyReport = schedule.MustDefine(
	"reports.daily.v1",
	"0 6 * * *",
	BuildReportDefinition,
	schedule.Dynamic(func(run schedule.Occurrence) (BuildReport, error) {
		return BuildReport{ScheduledAt: run.ScheduledAt}, nil
	}),
	schedule.TimeZone("America/Halifax"),
)
```

Dynamic function identity cannot be hashed reliably. Its implementation is
therefore part of the versioned schedule-name contract. Change the name to
`.v2` when the factory's behavior changes incompatibly.

Registration is an ordinary, explicit application function:

```go
registry := schedule.NewRegistry()
schedule.MustRegister(registry, RebuildSearchNightly)
schedule.MustRegister(registry, BuildDailyReport)
```

There is no package scan, annotation, reflection-driven discovery, or
database-authored application schema. `Registry.Definitions` and
`Registry.Names` return
stable name order for tests and operational output.

Fresh format-11 applications provide the application-owned registration point
at `internal/schedules/registry.go`. A definition's typed job must also remain
in `internal/jobs/registry_gen.go`, because the scheduler only materializes the
job and the separate worker registry decides which job types may be claimed.

After applying the generated migration, the opinionated commands are:

```sh
forge schedule:list # inspect definitions and durable cursor state
forge schedule:run  # evaluate once and exit
forge schedule:work # evaluate immediately, then poll until signalled
forge queue:work    # execute the materialized jobs in a separate process
```

Their exact direct-Go escape hatches are `go run ./cmd/console schedule:list`,
`go run ./cmd/scheduler --once`, `go run ./cmd/scheduler`, and
`go run ./cmd/worker`.

## Run the scheduler directly

The application owns its database, queue adapter, dispatcher, registry, and
process lifecycle:

```go
queueStore, err := jobpostgres.New(db)
if err != nil {
	return err
}
dispatcher, err := job.NewDispatcher(queueStore, db, job.DispatcherConfig{})
if err != nil {
	return err
}
scheduleStore, err := schedulepostgres.New(db)
if err != nil {
	return err
}
scheduler, err := schedule.NewScheduler(
	scheduleStore,
	registry,
	dispatcher,
	schedule.Config{
		PollInterval:     15 * time.Second,
		OperationTimeout: 5 * time.Second,
		Observer:         schedule.SlogObserver(logger),
	},
)
if err != nil {
	return err
}
return scheduler.Run(ctx)
```

`Run` evaluates immediately and then polls until its context is cancelled.
Cancellation is a normal shutdown. `RunOnce` returns deterministic result data
for a one-shot command or test. A blocking observer cannot block database work:
events are payload-free, serialized through a bounded queue, emitted only after
the store returns successfully, and expose dropped-event counts through
`DroppedObserverEvents`.

Applications install `schedule/postgres.Schema` through an ordinary migration;
the framework does not mutate schema during process startup. `DropSchema` is
provided for isolated test cleanup, and `WithTable` supports an explicitly
named application-owned table.

## Cron and civil-time contract

Expressions contain exactly five fields: minute, hour, day of month, month, and
day of week. They support numbers, lists, inclusive ranges, steps,
case-insensitive month and weekday names, and Sunday as `0` or `7`.

Seconds, years, macros, embedded `CRON_TZ`, `?`, `L`, `W`, `#`, zero steps, and
wrapping ranges are rejected. When both day-of-month and day-of-week are
restricted, traditional cron OR semantics apply.

The default zone is `UTC`. Other zones must be explicit IANA names. GoForge
bundles the time-zone database so behavior does not depend on the host image.
Nonexistent spring-forward minutes do not run. A fall-back repeated minute is
two distinct UTC occurrences. `Definition.Next` is exclusive and always
returns a whole UTC minute.

## Durable behavior

PostgreSQL time decides whether work is due. Each versioned schedule owns one
cursor row. Competing scheduler processes take a short row lock with
`FOR UPDATE SKIP LOCKED`; unrelated definitions can progress independently
without a global leader.

For an existing definition after downtime, missed occurrences coalesce to the
latest match inside the inclusive grace window. If no match remains eligible,
the cursor advances and records `misfire_skipped` without dispatching. A newly
observed definition never replays older history: it may materialize only the
current matching minute, otherwise it starts at its next future occurrence.

Dispatch uses `Dispatcher.Using(tx)`, and the queue insert and cursor advance
commit in the same transaction. Encoding failure, cancellation, enqueue error,
state conflict, panic, connection loss, or commit failure leaves neither half
committed.

The v0.14 overlap policy is deliberately conservative. Every occurrence uses
the stable active-job key:

```text
goforge:schedule:<versioned-schedule-name>
```

A queued, leased, or retrying job suppresses a later occurrence. The scheduler
records `overlap_suppressed` and advances the cursor. Once the prior delivery
leaves the active queue, a future occurrence can enqueue normally.

## Definition drift and inspection

The durable row stores inspectable timing and job metadata plus a SHA-256
fingerprint covering the schedule name, normalized cron expression, zone,
grace, target job name and effective policy, queue, payload strategy, static
payload bytes when present, and overlap policy.

A same-name mismatch returns `schedule.ErrDefinitionConflict`; it does not
dispatch or rewrite durable state. Introduce a new versioned schedule name for
an intentional behavior change. Rows absent from the explicit registry remain
inspectable but inert.

Per-row coordination makes overlapping scheduler processes safe only when they
run the same registry. A purely additive change may roll normally: old
processes cannot see the new definition, and new processes coordinate its row.
Before removing or replacing a definition, stop every scheduler running the old
registry, deploy the changed registry, and then start the new schedulers. HTTP
servers and queue workers may continue running. Do not run old `.v1` and new
`.v2` registries together: their distinct rows and deduplication keys represent
distinct schedules and can both enqueue. v0.14 does not pretend to provide a
durable mixed-registry retirement protocol.

`schedule/postgres.Store.List` reports code definitions in stable order and is
read-only. It distinguishes definitions with no durable row, returns cursor and
audit timestamps for initialized rows, and also fails closed on drift.

## v0.14 release boundary

The lightweight unsigned v0.14.0 runtime tag first fixed the public schedule
packages while fresh projects remained on format 10 and the existing v0.13.0
framework pin. The v0.14.1 CLI then moves fresh applications to format 11 with
application-owned scheduler wiring, migration, registry, configuration, and
`forge schedule:*` commands while pinning that immutable v0.14.0 dependency.

This two-stage release prevents `main` from ever generating imports that are
not available from the scaffold's public module dependency.
