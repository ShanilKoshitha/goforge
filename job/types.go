// Package job provides typed, explicit durable job dispatch and worker lifecycle.
package job

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

const (
	DefaultMaxPayloadBytes = 256 << 10
	DefaultMaxErrorBytes   = 8 << 10
	MaxNameBytes           = 128
	MaxQueueBytes          = 64
	MaxDedupKeyBytes       = 256
)

var (
	ErrLeaseLost       = errors.New("job: lease lost")
	ErrDuplicate       = errors.New("job: duplicate registration")
	ErrPayloadTooLarge = errors.New("job: payload too large")
	// ErrHandlerCancellationTimeout means a handler did not return within the
	// worker's bounded cancellation window. The worker stops without mutating
	// the delivery so its lease can expire and another process can recover it.
	ErrHandlerCancellationTimeout = errors.New("job: handler did not stop after cancellation")
)

// ID is a stable durable job identifier.
type ID string

// Executor is implemented by *sql.DB, *sql.Tx, and orm.Executor.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// ValidateExecutor rejects nil and typed-nil database executors. Custom stores
// can use it before invoking the database/sql-shaped escape hatch.
func ValidateExecutor(executor Executor) error {
	if nilValue(executor) {
		return fmt.Errorf("job: executor is required")
	}
	return nil
}

func nilValue(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// Policy is snapshotted into each dispatched job.
type Policy struct {
	Queue       string
	MaxAttempts int
	Timeout     time.Duration
	Backoff     []time.Duration
}

// DispatchResult distinguishes a newly inserted job from active deduplication.
type DispatchResult struct {
	ID       ID
	Enqueued bool
}

// EnqueueRequest is the validated, encoded store boundary.
type EnqueueRequest struct {
	ID          ID
	Queue       string
	Name        string
	Payload     json.RawMessage
	Priority    int
	Delay       time.Duration
	At          *time.Time
	MaxAttempts int
	Timeout     time.Duration
	Backoff     []time.Duration
	DedupKey    string
}

// ClaimRequest describes one capacity-bounded claim.
type ClaimRequest struct {
	Queues        []string
	Names         []string
	Limit         int
	WorkerID      string
	LeaseDuration time.Duration
}

// Lease fences all mutations after a claim.
type Lease struct {
	JobID      ID
	WorkerID   string
	Generation int64
}

// Delivery is one claimed job with snapshotted execution policy.
type Delivery struct {
	ID           ID
	Queue        string
	Name         string
	Payload      json.RawMessage
	Priority     int
	Attempt      int
	MaxAttempts  int
	Timeout      time.Duration
	Backoff      []time.Duration
	DedupKey     string
	AvailableAt  time.Time
	LeasedAt     time.Time
	LeaseExpires time.Time
	Lease        Lease
}

// RetryRequest releases a fenced delivery after an execution failure.
type RetryRequest struct {
	Lease     Lease
	Delay     time.Duration
	ErrorKind string
	Error     string
}

// FailureRequest atomically moves a fenced delivery to terminal failures.
type FailureRequest struct {
	Lease Lease
	Kind  string
	Error string
}

// Store is the replaceable durable worker and dispatch contract.
type Store interface {
	Enqueue(context.Context, Executor, EnqueueRequest) (DispatchResult, error)
	Claim(context.Context, ClaimRequest) ([]Delivery, error)
	Heartbeat(context.Context, Lease, time.Duration) (bool, error)
	Ack(context.Context, Lease) (bool, error)
	Retry(context.Context, RetryRequest) (bool, error)
	Release(context.Context, Lease) (bool, error)
	Fail(context.Context, FailureRequest) (bool, error)
	FailExhausted(context.Context, []string, []string, int) ([]FailedJob, error)
}

// FailedJob is safe administration metadata and deliberately omits its payload.
type FailedJob struct {
	ID             ID
	Queue          string
	Name           string
	Priority       int
	Attempts       int
	MaxAttempts    int
	Timeout        time.Duration
	Backoff        []time.Duration
	DedupKey       string
	FailureKind    string
	FailureMessage string
	CreatedAt      time.Time
	FailedAt       time.Time
}

// FailedJobDetail exposes one terminal payload through an explicit lookup.
type FailedJobDetail struct {
	FailedJob
	Payload json.RawMessage
}

// AdminStore exposes terminal failure operations separately from workers.
type AdminStore interface {
	ListFailed(context.Context, int) ([]FailedJob, error)
	RetryFailed(context.Context, ID) (bool, error)
	ForgetFailed(context.Context, ID) (bool, error)
}

// FailedJobInspector is the conspicuous payload-bearing administration path.
type FailedJobInspector interface {
	FindFailed(context.Context, ID) (FailedJobDetail, bool, error)
}

// EventKind identifies a lifecycle event.
type EventKind string

const (
	EventDispatched     EventKind = "dispatched"
	EventDuplicate      EventKind = "duplicate"
	EventClaimed        EventKind = "claimed"
	EventStarted        EventKind = "started"
	EventSucceeded      EventKind = "succeeded"
	EventRetryScheduled EventKind = "retry_scheduled"
	EventFailed         EventKind = "failed"
	EventLeaseLost      EventKind = "lease_lost"
	EventAbandoned      EventKind = "abandoned"
	EventWorkerStarted  EventKind = "worker_started"
	EventWorkerStopping EventKind = "worker_stopping"
	EventWorkerStopped  EventKind = "worker_stopped"
)

// Event deliberately excludes payloads.
type Event struct {
	Kind       EventKind
	JobID      ID
	Name       string
	Queue      string
	WorkerID   string
	Attempt    int
	Duration   time.Duration
	RetryAfter time.Duration
	Outcome    string
	At         time.Time
}

// Observer receives immutable job lifecycle events.
type Observer interface{ Observe(context.Context, Event) }

// ObserverFunc adapts a function into an Observer.
type ObserverFunc func(context.Context, Event)

func (observer ObserverFunc) Observe(ctx context.Context, event Event) { observer(ctx, event) }

const observerQueueCapacity = 256

type observedEvent struct {
	ctx   context.Context
	event Event
}

// eventEmitter keeps application observers off queue-state and lease-critical
// paths. One short-lived drain goroutine preserves delivered-event order; a
// blocked observer can retain at most one goroutine and a bounded event queue.
type eventEmitter struct {
	observer Observer

	mu      sync.Mutex
	pending []observedEvent
	running bool
	idle    chan struct{}
	dropped atomic.Uint64
}

func newEventEmitter(observer Observer) *eventEmitter {
	if observer == nil {
		return nil
	}
	idle := make(chan struct{})
	close(idle)
	return &eventEmitter{observer: observer, idle: idle}
}

func (emitter *eventEmitter) emit(ctx context.Context, event Event) {
	if emitter == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	emitter.mu.Lock()
	if len(emitter.pending) >= observerQueueCapacity {
		emitter.dropped.Add(1)
		emitter.mu.Unlock()
		return
	}
	emitter.pending = append(emitter.pending, observedEvent{ctx: ctx, event: event})
	if emitter.running {
		emitter.mu.Unlock()
		return
	}
	emitter.running = true
	emitter.idle = make(chan struct{})
	emitter.mu.Unlock()
	go emitter.drain()
}

func (emitter *eventEmitter) drain() {
	for {
		emitter.mu.Lock()
		if len(emitter.pending) == 0 {
			emitter.running = false
			close(emitter.idle)
			emitter.mu.Unlock()
			return
		}
		next := emitter.pending[0]
		emitter.pending[0] = observedEvent{}
		emitter.pending = emitter.pending[1:]
		emitter.mu.Unlock()

		func() {
			defer func() { _ = recover() }()
			emitter.observer.Observe(next.ctx, next.event)
		}()
	}
}

func (emitter *eventEmitter) flush(ctx context.Context) bool {
	if emitter == nil {
		return true
	}
	emitter.mu.Lock()
	idle := emitter.idle
	emitter.mu.Unlock()
	select {
	case <-idle:
		return true
	case <-ctx.Done():
		return false
	}
}

func (emitter *eventEmitter) droppedEvents() uint64 {
	if emitter == nil {
		return 0
	}
	return emitter.dropped.Load()
}

type slogObserver struct{ logger *slog.Logger }

// SlogObserver logs lifecycle metadata without payloads.
func SlogObserver(logger *slog.Logger) Observer {
	if logger == nil {
		logger = slog.Default()
	}
	return slogObserver{logger: logger}
}

func (observer slogObserver) Observe(ctx context.Context, event Event) {
	observer.logger.InfoContext(ctx, "job "+string(event.Kind),
		"job_id", event.JobID, "job", event.Name, "queue", event.Queue,
		"worker_id", event.WorkerID, "attempt", event.Attempt,
		"duration", event.Duration, "retry_after", event.RetryAfter,
		"outcome", event.Outcome)
}

func wrapStore(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("job: %s: %w", operation, err)
}
