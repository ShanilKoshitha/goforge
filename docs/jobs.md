# Durable jobs

GoForge jobs are typed application code backed by PostgreSQL. Dispatchers,
handlers, registration, worker construction, migrations, and administration
remain ordinary Go and SQL in the generated application.

The delivery contract is **at least once**. Lease fencing prevents an old worker
from changing queue state after another worker reclaims the job, but it cannot
undo an external effect completed immediately before a crash. Handlers must use
an idempotent business key or another application-level idempotency strategy.

## Generate and define a job

```text
forge make:job SendWelcome
```

This exclusively creates `internal/jobs/send_welcome.go` and its test, records
the stable `send_welcome.v1` wire name in `.forge/jobs.json`, and regenerates an
explicit registry. The handler file is application-owned; the registry alone is
generated.

Edit the generated payload and policy directly:

```go
type SendWelcome struct {
    UserID int64 `json:"user_id"`
}

var SendWelcomeDefinition = job.MustDefine[SendWelcome](
    "send_welcome.v1",
    job.Policy{
        Queue:       "mail",
        MaxAttempts: 5,
        Timeout:     30 * time.Second,
        Backoff:     []time.Duration{time.Second, 5 * time.Second, 30 * time.Second},
    },
)
```

Wire names are versioned durable contracts. When an incompatible payload shape
is necessary, introduce a new name such as `send_welcome.v2` and keep the old
handler registered until its rows drain. Workers claim only names in their
explicit registry, so an older deployment leaves newer jobs untouched.

## Dispatch atomically

The generated `jobs.NewDispatcher` uses the application's `*sql.DB`. A typed
helper rejects the wrong payload type at compile time:

```go
result, err := jobs.DispatchSendWelcome(ctx, dispatcher, jobs.SendWelcome{
    UserID: user.ID,
})
```

Use `dispatcher.Using(tx)` to commit the domain mutation and job together:

```go
err := database.Transaction(ctx, db, nil, func(tx *sql.Tx) error {
    if _, err := tx.ExecContext(ctx, updateUserSQL, user.ID); err != nil {
        return err
    }
    _, err := jobs.DispatchSendWelcome(
        ctx,
        dispatcher.Using(tx),
        jobs.SendWelcome{UserID: user.ID},
    )
    return err
})
```

The enqueue is not visible before commit and disappears on rollback or panic.
No after-commit hook or ambient transaction is involved.

Per-dispatch options are explicit:

```go
jobs.DispatchSendWelcome(ctx, dispatcher, payload,
    job.Queue("mail"),
    job.Delay(10*time.Minute),
    job.Priority(10),
    job.Deduplicate(fmt.Sprintf("welcome:%d", payload.UserID)),
)
```

`job.At` is the absolute-time alternative to `job.Delay`. Active deduplication
applies only while a matching `(queue, versioned name, key)` row exists. The
returned `DispatchResult` distinguishes a new enqueue from a duplicate and
contains the durable ID. It is not an exactly-once guarantee and the key may be
used again after successful acknowledgement.

## Handle and retry

Handlers receive the typed payload and a context with the snapshotted timeout.
`job.ExecutionFromContext` exposes the durable job ID, name, queue, attempt, and
deduplication key without exposing the payload to observers.

- Returning an ordinary error schedules the next snapshotted backoff until the
  maximum attempt is reached.
- `job.RetryAfter(err, duration)` overrides the next delay for that failure.
- `job.Permanent(err)` moves the delivery directly to terminal failures.
- Panics are recovered and treated as failed attempts.
- Invalid stored JSON fails terminally as a malformed payload.

Go cannot forcibly terminate a goroutine. At timeout GoForge cancels the handler
context but retains its worker slot and lease heartbeat until the handler
returns. This prevents a healthy worker from overlapping another attempt merely
because application code ignored cancellation. During forced process shutdown,
the lease is left for fenced expiry and recovery.

## Run and operate workers

Run the application-owned worker:

```text
forge queue:work
```

The installed CLI delegates to `go run ./cmd/worker`; production can build and
run `cmd/worker` directly. The worker stops claiming on SIGTERM, continues
heartbeats while it drains, and cancels remaining handlers after the configured
grace period.

Generated worker settings are visible environment variables:

```text
JOB_QUEUES=default
JOB_CONCURRENCY=4
JOB_POLL_INTERVAL=1s
JOB_LEASE_DURATION=30s
JOB_HEARTBEAT_INTERVAL=10s
JOB_OPERATION_TIMEOUT=5s
JOB_SHUTDOWN_TIMEOUT=15s
```

Background configuration requires the database settings, not HTTP session
secrets. `cmd/worker` also sizes its ordinary `database/sql` pool with capacity
reserved for queue bookkeeping; applications can replace that visible policy.

Inspect and operate terminal failures through application-owned console code:

```text
forge queue:failed
forge queue:retry <id>
forge queue:retry --all
forge queue:forget <id>
```

Lists contain bounded summary metadata and escaped one-line diagnostics, never
payloads. The PostgreSQL adapter's explicit `FindFailed` method is the
conspicuous path for retrieving one payload. Forgetting all failures is not
supported because deletion is irreversible.

## Storage and observability

The generated paired migration creates `goforge_jobs` and
`goforge_failed_jobs`. Claims use the PostgreSQL clock and a short
`FOR UPDATE SKIP LOCKED` statement, then commit before handler code runs. Every
heartbeat, acknowledgement, retry, release, and terminal move matches the job
ID, lease owner, and monotonically increasing lease generation.

The default `job.SlogObserver` emits lifecycle metadata without payloads.
Application observers are delivered through a serialized bounded queue so a
blocked observer cannot hold a lease or defeat shutdown. `DroppedObserverEvents`
on the dispatcher and worker makes observer backpressure visible.

Replaceable `job.Store`, `job.AdminStore`, `job.Observer`, and
`job.FailedJobInspector` contracts are the runtime escape hatches. The
PostgreSQL adapter also exposes claim and fenced mutation primitives for custom
worker runtimes. Direct `database/sql`, custom stores, and custom workers can be
used without changing the HTTP framework.

Recurring schedules are intentionally separate. Cron parsing, time zones and
DST, missed-run policy, overlap locks, occurrence deduplication, and scheduler
leadership form the next operational contract; one-off delayed jobs are fully
supported here.
