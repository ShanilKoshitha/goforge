package job

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// WorkerConfig controls bounded claiming, leases, and graceful shutdown.
type WorkerConfig struct {
	Queues              []string
	Concurrency         int
	PollInterval        time.Duration
	LeaseDuration       time.Duration
	HeartbeatInterval   time.Duration
	OperationTimeout    time.Duration
	CancellationTimeout time.Duration
	ShutdownTimeout     time.Duration
	WorkerID            string
	Observer            Observer
	MaxErrorBytes       int
}

// Worker executes explicitly registered durable jobs.
type Worker struct {
	store    Store
	registry *Registry
	config   WorkerConfig
	observer *eventEmitter
}

// NewWorker validates a worker and applies production-shaped defaults.
func NewWorker(store Store, registry *Registry, config WorkerConfig) (*Worker, error) {
	if nilValue(store) {
		return nil, fmt.Errorf("job: store is required")
	}
	if registry == nil {
		return nil, fmt.Errorf("job: registry is required")
	}
	if len(config.Queues) == 0 {
		config.Queues = []string{"default"}
	} else {
		config.Queues = append([]string(nil), config.Queues...)
	}
	seen := make(map[string]struct{}, len(config.Queues))
	for _, queue := range config.Queues {
		if err := validateQueue(queue); err != nil {
			return nil, err
		}
		if _, exists := seen[queue]; exists {
			return nil, fmt.Errorf("job: duplicate worker queue %q", queue)
		}
		seen[queue] = struct{}{}
	}
	if config.Concurrency == 0 {
		config.Concurrency = 4
	}
	if config.Concurrency < 1 || config.Concurrency > 10000 {
		return nil, fmt.Errorf("job: concurrency must be between 1 and 10000")
	}
	if config.PollInterval == 0 {
		config.PollInterval = time.Second
	}
	if config.PollInterval < time.Millisecond {
		return nil, fmt.Errorf("job: poll interval must be at least 1ms")
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = time.Minute
	}
	if config.LeaseDuration < 100*time.Millisecond {
		return nil, fmt.Errorf("job: lease duration must be at least 100ms")
	}
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = config.LeaseDuration / 3
	}
	if config.HeartbeatInterval < time.Millisecond || config.HeartbeatInterval >= config.LeaseDuration {
		return nil, fmt.Errorf("job: heartbeat interval must be positive and shorter than the lease")
	}
	if config.OperationTimeout == 0 {
		config.OperationTimeout = 5 * time.Second
		if halfLease := config.LeaseDuration / 2; halfLease < config.OperationTimeout {
			config.OperationTimeout = halfLease
		}
	}
	if config.OperationTimeout < time.Millisecond || config.OperationTimeout >= config.LeaseDuration {
		return nil, fmt.Errorf("job: operation timeout must be positive and shorter than the lease")
	}
	if config.CancellationTimeout == 0 {
		config.CancellationTimeout = config.OperationTimeout
	}
	if config.CancellationTimeout < time.Millisecond {
		return nil, fmt.Errorf("job: cancellation timeout must be at least 1ms")
	}
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = 30 * time.Second
	}
	if config.ShutdownTimeout < 0 {
		return nil, fmt.Errorf("job: shutdown timeout cannot be negative")
	}
	if config.MaxErrorBytes == 0 {
		config.MaxErrorBytes = DefaultMaxErrorBytes
	}
	if config.MaxErrorBytes < 1 {
		return nil, fmt.Errorf("job: max error bytes must be positive")
	}
	if config.WorkerID == "" {
		id, err := newID()
		if err != nil {
			return nil, err
		}
		config.WorkerID = string(id)
	}
	if len(config.WorkerID) > 128 || strings.TrimSpace(config.WorkerID) == "" {
		return nil, fmt.Errorf("job: invalid worker id")
	}
	return &Worker{store: store, registry: registry, config: config, observer: newEventEmitter(config.Observer)}, nil
}

// Execution describes the current attempt without exposing its payload.
type Execution struct {
	ID          ID
	Name        string
	Queue       string
	Attempt     int
	MaxAttempts int
	DedupKey    string
}

type executionContextKey struct{}

// ExecutionFromContext returns metadata for the current handler invocation.
func ExecutionFromContext(ctx context.Context) (Execution, bool) {
	execution, ok := ctx.Value(executionContextKey{}).(Execution)
	return execution, ok
}

// DroppedObserverEvents reports lifecycle events discarded because the
// configured observer did not keep up with the bounded delivery queue.
func (worker *Worker) DroppedObserverEvents() uint64 {
	if worker == nil {
		return 0
	}
	return worker.observer.droppedEvents()
}

// Run claims until cancelled or a durable store operation fails.
func (worker *Worker) Run(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("job: nil worker context")
	}
	started := time.Now()
	worker.observer.emit(ctx, Event{Kind: EventWorkerStarted, WorkerID: worker.config.WorkerID, At: started})

	base, hardCancel := context.WithCancel(context.WithoutCancel(ctx))
	defer hardCancel()
	slots := make(chan struct{}, worker.config.Concurrency)
	var active sync.WaitGroup
	processErrors := make(chan error, worker.config.Concurrency)
	names := worker.registry.Names()

	var runErr error
	for runErr == nil {
		select {
		case <-ctx.Done():
			runErr = ctx.Err()
			continue
		case err := <-processErrors:
			runErr = err
			continue
		default:
		}

		free := cap(slots) - len(slots)
		if free > 0 && len(names) > 0 {
			operationContext, operationCancel := worker.operationContext(ctx)
			exhausted, err := worker.store.FailExhausted(operationContext, worker.config.Queues, names, free)
			operationCancel()
			if err != nil {
				runErr = wrapStore("fail exhausted leases", err)
				continue
			}
			for _, failed := range exhausted {
				worker.observer.emit(ctx, Event{
					Kind: EventFailed, JobID: failed.ID, Name: failed.Name, Queue: failed.Queue,
					WorkerID: worker.config.WorkerID, Attempt: failed.Attempts,
					Outcome: "lease_exhausted", At: time.Now(),
				})
			}
			operationContext, operationCancel = worker.operationContext(ctx)
			deliveries, err := worker.store.Claim(operationContext, ClaimRequest{
				Queues: worker.config.Queues, Names: names, Limit: free,
				WorkerID: worker.config.WorkerID, LeaseDuration: worker.config.LeaseDuration,
			})
			operationCancel()
			if err != nil {
				runErr = wrapStore("claim", err)
				continue
			}
			for _, delivery := range deliveries {
				slots <- struct{}{}
				active.Add(1)
				worker.observer.emit(ctx, eventFor(EventClaimed, worker.config.WorkerID, delivery))
				go func(delivery Delivery) {
					defer func() { <-slots; active.Done() }()
					if err := worker.process(base, delivery); err != nil {
						select {
						case processErrors <- err:
						default:
						}
					}
				}(delivery)
			}
			if len(deliveries) > 0 {
				continue
			}
		}

		timer := time.NewTimer(worker.config.PollInterval)
		select {
		case <-ctx.Done():
			runErr = ctx.Err()
		case err := <-processErrors:
			runErr = err
		case <-timer.C:
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}

	worker.observer.emit(context.Background(), Event{Kind: EventWorkerStopping, WorkerID: worker.config.WorkerID, At: time.Now()})
	drained := make(chan struct{})
	go func() { active.Wait(); close(drained) }()
	timer := time.NewTimer(worker.config.ShutdownTimeout)
	select {
	case <-drained:
		if !timer.Stop() {
			<-timer.C
		}
	case <-timer.C:
		hardCancel()
	}
	worker.observer.emit(context.Background(), Event{Kind: EventWorkerStopped, WorkerID: worker.config.WorkerID, Duration: time.Since(started), At: time.Now()})
	flushTimeout := min(worker.config.OperationTimeout, 100*time.Millisecond)
	flushContext, flushCancel := context.WithTimeout(context.Background(), flushTimeout)
	worker.observer.flush(flushContext)
	flushCancel()
	if ctx.Err() != nil && errors.Is(runErr, ctx.Err()) {
		return nil
	}
	return runErr
}

func (worker *Worker) process(base context.Context, delivery Delivery) error {
	handler, ok := worker.registry.lookup(delivery.Name)
	if !ok {
		operationContext, cancel := worker.operationContext(base)
		defer cancel()
		owned, err := worker.store.Release(operationContext, delivery.Lease)
		if err != nil {
			return wrapStore("release unknown job", err)
		}
		if !owned {
			worker.leaseLost(delivery)
		}
		return nil
	}
	payload, err := handler.decode(delivery.Payload)
	if err != nil {
		return worker.fail(base, delivery, "malformed_payload", "job payload could not be decoded")
	}

	execution := Execution{ID: delivery.ID, Name: delivery.Name, Queue: delivery.Queue, Attempt: delivery.Attempt, MaxAttempts: delivery.MaxAttempts, DedupKey: delivery.DedupKey}
	handlerContext := context.WithValue(base, executionContextKey{}, execution)
	timeout := delivery.Timeout
	if timeout <= 0 {
		timeout = handler.policy.Timeout
	}
	handlerContext, cancel := context.WithTimeout(handlerContext, timeout)
	defer cancel()
	started := time.Now()
	worker.observer.emit(handlerContext, eventFor(EventStarted, worker.config.WorkerID, delivery))

	result := make(chan error, 1)
	go func() {
		var handlerErr error
		defer func() {
			if recovered := recover(); recovered != nil {
				handlerErr = panicError{}
			}
			result <- handlerErr
		}()
		handlerErr = handler.handle(handlerContext, payload)
	}()

	ticker := time.NewTicker(worker.config.HeartbeatInterval)
	defer ticker.Stop()
	handlerDone := handlerContext.Done()
	var cancellationTimer *time.Timer
	var cancellationDone <-chan time.Time
	timedOut := false
	defer func() {
		if cancellationTimer != nil {
			cancellationTimer.Stop()
		}
	}()
	for {
		select {
		case handlerErr := <-result:
			if timedOut || errors.Is(handlerContext.Err(), context.DeadlineExceeded) {
				handlerErr = context.DeadlineExceeded
			}
			return worker.complete(base, delivery, handlerErr, time.Since(started))
		case <-ticker.C:
			heartbeatContext, heartbeatCancel := worker.operationContext(base)
			owned, heartbeatErr := worker.store.Heartbeat(heartbeatContext, delivery.Lease, worker.config.LeaseDuration)
			heartbeatCancel()
			if heartbeatErr != nil {
				cancel()
				return wrapStore("heartbeat", heartbeatErr)
			}
			if !owned {
				cancel()
				worker.leaseLost(delivery)
				return worker.awaitCancellation(base, result, delivery, "lease_lost")
			}
		case <-handlerDone:
			if errors.Is(handlerContext.Err(), context.DeadlineExceeded) {
				// Keep the delivery fenced for a bounded cancellation window. If the
				// handler still does not return, stop this worker and leave the lease
				// to expire rather than heartbeating an uncooperative handler forever.
				timedOut = true
				handlerDone = nil
				cancellationTimer = time.NewTimer(worker.config.CancellationTimeout)
				cancellationDone = cancellationTimer.C
				continue
			}
			// Forced shutdown or a lost parent leaves the fenced lease to expire.
			return nil
		case <-cancellationDone:
			worker.abandoned(delivery, "cancellation_timeout")
			return fmt.Errorf("%w after %s", ErrHandlerCancellationTimeout, worker.config.CancellationTimeout)
		case <-base.Done():
			return nil
		}
	}
}

func (worker *Worker) awaitCancellation(base context.Context, result <-chan error, delivery Delivery, outcome string) error {
	timer := time.NewTimer(worker.config.CancellationTimeout)
	defer timer.Stop()
	select {
	case <-result:
		return nil
	case <-base.Done():
		return nil
	case <-timer.C:
		worker.abandoned(delivery, outcome+"_cancellation_timeout")
		return fmt.Errorf("%w after %s", ErrHandlerCancellationTimeout, worker.config.CancellationTimeout)
	}
}

func (worker *Worker) complete(ctx context.Context, delivery Delivery, handlerErr error, duration time.Duration) error {
	if handlerErr == nil {
		operationContext, cancel := worker.operationContext(ctx)
		defer cancel()
		owned, err := worker.store.Ack(operationContext, delivery.Lease)
		if err != nil {
			return wrapStore("acknowledge", err)
		}
		if !owned {
			worker.leaseLost(delivery)
			return nil
		}
		event := eventFor(EventSucceeded, worker.config.WorkerID, delivery)
		event.Duration = duration
		worker.observer.emit(context.Background(), event)
		return nil
	}

	kind, message, retryOverride, permanent := classifyError(handlerErr)
	if errors.Is(handlerErr, context.DeadlineExceeded) {
		kind = "timeout"
		message = "handler deadline exceeded"
	}
	message = truncateUTF8(message, worker.config.MaxErrorBytes)
	if permanent || delivery.Attempt >= delivery.MaxAttempts {
		return worker.fail(ctx, delivery, kind, message)
	}
	delay := retryDelay(delivery.Backoff, delivery.Attempt)
	if retryOverride != nil {
		delay = *retryOverride
	}
	operationContext, cancel := worker.operationContext(ctx)
	defer cancel()
	owned, err := worker.store.Retry(operationContext, RetryRequest{Lease: delivery.Lease, Delay: delay, ErrorKind: kind, Error: message})
	if err != nil {
		return wrapStore("schedule retry", err)
	}
	if !owned {
		worker.leaseLost(delivery)
		return nil
	}
	event := eventFor(EventRetryScheduled, worker.config.WorkerID, delivery)
	event.Duration = duration
	event.RetryAfter = delay
	event.Outcome = kind
	worker.observer.emit(context.Background(), event)
	return nil
}

func (worker *Worker) fail(ctx context.Context, delivery Delivery, kind, message string) error {
	message = truncateUTF8(message, worker.config.MaxErrorBytes)
	operationContext, cancel := worker.operationContext(ctx)
	defer cancel()
	owned, err := worker.store.Fail(operationContext, FailureRequest{Lease: delivery.Lease, Kind: kind, Error: message})
	if err != nil {
		return wrapStore("fail terminal job", err)
	}
	if !owned {
		worker.leaseLost(delivery)
		return nil
	}
	event := eventFor(EventFailed, worker.config.WorkerID, delivery)
	event.Outcome = kind
	worker.observer.emit(context.Background(), event)
	return nil
}

func (worker *Worker) leaseLost(delivery Delivery) {
	worker.observer.emit(context.Background(), eventFor(EventLeaseLost, worker.config.WorkerID, delivery))
}

func (worker *Worker) abandoned(delivery Delivery, outcome string) {
	event := eventFor(EventAbandoned, worker.config.WorkerID, delivery)
	event.Outcome = outcome
	worker.observer.emit(context.Background(), event)
}

func (worker *Worker) operationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, worker.config.OperationTimeout)
}

func retryDelay(backoff []time.Duration, attempt int) time.Duration {
	if len(backoff) == 0 {
		backoff = defaultBackoff
	}
	index := attempt - 1
	if index < 0 {
		index = 0
	}
	if index >= len(backoff) {
		index = len(backoff) - 1
	}
	return backoff[index]
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func eventFor(kind EventKind, workerID string, delivery Delivery) Event {
	return Event{Kind: kind, JobID: delivery.ID, Name: delivery.Name, Queue: delivery.Queue, WorkerID: workerID, Attempt: delivery.Attempt, At: time.Now()}
}
