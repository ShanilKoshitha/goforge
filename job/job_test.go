package job_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/job"
)

type inertExecutor struct{}

func (inertExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}
func (inertExecutor) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, nil
}
func (inertExecutor) QueryRowContext(context.Context, string, ...any) *sql.Row { return nil }

type fakeStore struct {
	mu sync.Mutex

	enqueueResult job.DispatchResult
	enqueued      []job.EnqueueRequest
	executors     []job.Executor
	deliveries    []job.Delivery
	claimLimits   []int
	claimNames    []string
	heartbeats    int
	heartbeatOwn  bool
	acks          []job.Lease
	retries       []job.RetryRequest
	releases      []job.Lease
	failures      []job.FailureRequest
	exhausted     int
	onAck         func()
	onRetry       func()
	onFail        func()
}

func (store *fakeStore) Enqueue(_ context.Context, executor job.Executor, request job.EnqueueRequest) (job.DispatchResult, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.enqueued = append(store.enqueued, request)
	store.executors = append(store.executors, executor)
	result := store.enqueueResult
	if result.ID == "" {
		result = job.DispatchResult{ID: request.ID, Enqueued: true}
	}
	return result, nil
}

func (store *fakeStore) Claim(_ context.Context, request job.ClaimRequest) ([]job.Delivery, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.claimLimits = append(store.claimLimits, request.Limit)
	store.claimNames = append([]string(nil), request.Names...)
	count := request.Limit
	if count > len(store.deliveries) {
		count = len(store.deliveries)
	}
	result := append([]job.Delivery(nil), store.deliveries[:count]...)
	store.deliveries = store.deliveries[count:]
	return result, nil
}

func (store *fakeStore) Heartbeat(_ context.Context, _ job.Lease, _ time.Duration) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.heartbeats++
	return store.heartbeatOwn, nil
}

func (store *fakeStore) Ack(_ context.Context, lease job.Lease) (bool, error) {
	store.mu.Lock()
	store.acks = append(store.acks, lease)
	callback := store.onAck
	store.mu.Unlock()
	if callback != nil {
		callback()
	}
	return true, nil
}

func (store *fakeStore) Retry(_ context.Context, request job.RetryRequest) (bool, error) {
	store.mu.Lock()
	store.retries = append(store.retries, request)
	callback := store.onRetry
	store.mu.Unlock()
	if callback != nil {
		callback()
	}
	return true, nil
}

func (store *fakeStore) Release(_ context.Context, lease job.Lease) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.releases = append(store.releases, lease)
	return true, nil
}

func (store *fakeStore) Fail(_ context.Context, request job.FailureRequest) (bool, error) {
	store.mu.Lock()
	store.failures = append(store.failures, request)
	callback := store.onFail
	store.mu.Unlock()
	if callback != nil {
		callback()
	}
	return true, nil
}

func (store *fakeStore) FailExhausted(context.Context, []string, []string, int) ([]job.FailedJob, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.exhausted++
	return nil, nil
}

type payload struct {
	Value string `json:"value"`
}

func definition(t *testing.T, policy job.Policy) job.Definition[payload] {
	t.Helper()
	definition, err := job.Define[payload]("tests.payload.v1", policy)
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func TestTypedDispatchUsesExplicitExecutorAndSnapshotsOptions(t *testing.T) {
	store := &fakeStore{}
	first := inertExecutor{}
	dispatcher, err := job.NewDispatcher(store, first, job.DispatcherConfig{})
	if err != nil {
		t.Fatal(err)
	}
	second := inertExecutor{}
	definition := definition(t, job.Policy{Queue: "mail", MaxAttempts: 5, Timeout: 2 * time.Second, Backoff: []time.Duration{3 * time.Second}})
	result, err := definition.Dispatch(context.Background(), dispatcher.Using(second), payload{Value: "hello"}, job.Queue("urgent"), job.Delay(time.Minute), job.Deduplicate("user:42"), job.Priority(7))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Enqueued || result.ID == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	request := store.enqueued[0]
	if request.Queue != "urgent" || request.Delay != time.Minute || request.DedupKey != "user:42" || request.Priority != 7 {
		t.Fatalf("dispatch options not preserved: %+v", request)
	}
	if request.MaxAttempts != 5 || request.Timeout != 2*time.Second || len(request.Backoff) != 1 || request.Backoff[0] != 3*time.Second {
		t.Fatalf("policy not snapshotted: %+v", request)
	}
	if store.executors[0] != second {
		t.Fatal("Using executor was not passed to the store")
	}
}

func TestDispatchDeduplicationAndDefensiveBoundaries(t *testing.T) {
	store := &fakeStore{enqueueResult: job.DispatchResult{ID: "existing", Enqueued: false}}
	dispatcher, err := job.NewDispatcher(store, inertExecutor{}, job.DispatcherConfig{MaxPayloadBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	result, err := definition(t, job.Policy{}).Dispatch(context.Background(), dispatcher, payload{Value: ""}, job.Deduplicate("same"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Enqueued || result.ID != "existing" {
		t.Fatalf("unexpected duplicate result: %+v", result)
	}
	_, err = definition(t, job.Policy{}).Dispatch(context.Background(), dispatcher, payload{Value: "too large"})
	if !errors.Is(err, job.ErrPayloadTooLarge) {
		t.Fatalf("expected payload limit, got %v", err)
	}
	_, err = definition(t, job.Policy{}).Dispatch(context.Background(), dispatcher, payload{}, job.Delay(0), job.At(time.Now()))
	if err == nil {
		t.Fatal("expected mutually exclusive delay/at error")
	}
}

func TestRegistryIsExplicitVersionedAndRejectsDuplicates(t *testing.T) {
	if _, err := job.Define[payload]("unversioned", job.Policy{}); err == nil {
		t.Fatal("expected versioned-name error")
	}
	registry := job.NewRegistry()
	definition := definition(t, job.Policy{})
	if err := job.Register(registry, definition, func(context.Context, payload) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := job.Register(registry, definition, func(context.Context, payload) error { return nil }); !errors.Is(err, job.ErrDuplicate) {
		t.Fatalf("expected duplicate registration, got %v", err)
	}
	if names := registry.Names(); len(names) != 1 || names[0] != definition.Name() {
		t.Fatalf("unexpected names: %v", names)
	}
}

func TestObserverPanicCannotCorruptDispatch(t *testing.T) {
	store := &fakeStore{}
	dispatcher, err := job.NewDispatcher(store, inertExecutor{}, job.DispatcherConfig{Observer: job.ObserverFunc(func(context.Context, job.Event) { panic("observer") })})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := definition(t, job.Policy{}).Dispatch(context.Background(), dispatcher, payload{}); err != nil {
		t.Fatalf("observer panic escaped: %v", err)
	}
}

func TestBlockingObserverCannotBlockDispatchOrWorkerLifecycle(t *testing.T) {
	t.Run("dispatch", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		store := &fakeStore{}
		dispatcher, err := job.NewDispatcher(store, inertExecutor{}, job.DispatcherConfig{Observer: job.ObserverFunc(func(context.Context, job.Event) {
			once.Do(func() { close(started) })
			<-release
		})})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := definition(t, job.Policy{}).Dispatch(context.Background(), dispatcher, payload{}); err != nil {
			t.Fatal(err)
		}
		<-started
		done := make(chan error, 1)
		go func() {
			_, err := definition(t, job.Policy{}).Dispatch(context.Background(), dispatcher, payload{})
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(50 * time.Millisecond):
			t.Fatal("blocked observer stalled dispatch")
		}
		for range 300 {
			if _, err := definition(t, job.Policy{}).Dispatch(context.Background(), dispatcher, payload{}); err != nil {
				t.Fatal(err)
			}
		}
		if dispatcher.DroppedObserverEvents() == 0 {
			t.Fatal("observer backpressure was not observable")
		}
		close(release)
	})

	t.Run("worker", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		started := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		store := &fakeStore{heartbeatOwn: true, deliveries: []job.Delivery{delivery("a", 3)}}
		store.onAck = cancel
		observer := job.ObserverFunc(func(context.Context, job.Event) {
			once.Do(func() { close(started) })
			<-release
		})
		worker := newWorker(t, store, func(context.Context, payload) error { return nil }, job.WorkerConfig{Observer: observer, OperationTimeout: 5 * time.Millisecond})
		done := make(chan error, 1)
		go func() { done <- worker.Run(ctx) }()
		<-started
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("blocked observer stalled worker shutdown")
		}
		close(release)
	})
}

func TestEmptyRegistryWorkerIdlesUntilCancellation(t *testing.T) {
	store := &fakeStore{}
	worker, err := job.NewWorker(store, job.NewRegistry(), job.WorkerConfig{
		WorkerID: "empty", PollInterval: time.Millisecond,
		LeaseDuration: 100 * time.Millisecond, HeartbeatInterval: 10 * time.Millisecond,
		ShutdownTimeout: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.claimLimits) != 0 || store.exhausted != 0 {
		t.Fatal("empty registry worker touched durable queue state")
	}
}

func delivery(id string, maxAttempts int) job.Delivery {
	return job.Delivery{
		ID: job.ID(id), Queue: "default", Name: "tests.payload.v1", Payload: []byte(`{"value":"ok"}`),
		Attempt: 1, MaxAttempts: maxAttempts, Timeout: time.Second, Backoff: []time.Duration{10 * time.Millisecond},
		Lease: job.Lease{JobID: job.ID(id), WorkerID: "worker", Generation: 1},
	}
}

func newWorker(t *testing.T, store *fakeStore, handler job.Handler[payload], config job.WorkerConfig) *job.Worker {
	t.Helper()
	registry := job.NewRegistry()
	if err := job.Register(registry, definition(t, job.Policy{}), handler); err != nil {
		t.Fatal(err)
	}
	config.WorkerID = "worker"
	if config.PollInterval == 0 {
		config.PollInterval = time.Millisecond
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = 100 * time.Millisecond
	}
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 10 * time.Millisecond
	}
	if config.ShutdownTimeout == 0 {
		config.ShutdownTimeout = 100 * time.Millisecond
	}
	worker, err := job.NewWorker(store, registry, config)
	if err != nil {
		t.Fatal(err)
	}
	return worker
}

func TestWorkerClaimsOnlyCapacityAndFiltersKnownNames(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &fakeStore{heartbeatOwn: true}
	for index := 0; index < 5; index++ {
		store.deliveries = append(store.deliveries, delivery(string(rune('a'+index)), 3))
	}
	store.onAck = func() {
		store.mu.Lock()
		count := len(store.acks)
		store.mu.Unlock()
		if count == 5 {
			cancel()
		}
	}
	var active atomic.Int32
	var maximum atomic.Int32
	handler := func(context.Context, payload) error {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(4 * time.Millisecond)
		active.Add(-1)
		return nil
	}
	worker := newWorker(t, store, handler, job.WorkerConfig{Concurrency: 2})
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if maximum.Load() > 2 {
		t.Fatalf("worker exceeded concurrency: %d", maximum.Load())
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, limit := range store.claimLimits {
		if limit > 2 {
			t.Fatalf("claim exceeded capacity: %d", limit)
		}
	}
	if len(store.claimNames) != 1 || store.claimNames[0] != "tests.payload.v1" {
		t.Fatalf("claim did not filter registry names: %v", store.claimNames)
	}
	if store.exhausted == 0 {
		t.Fatal("worker never checked exhausted leases")
	}
}

func TestWorkerRetryPermanentPanicAndMalformedPayload(t *testing.T) {
	tests := []struct {
		name        string
		handler     job.Handler[payload]
		mutate      func(*job.Delivery)
		wantRetry   bool
		wantKind    string
		wantMessage string
		wantDelay   time.Duration
	}{
		{name: "error", handler: func(context.Context, payload) error { return errors.New("password=secret-value") }, wantRetry: true, wantKind: "error", wantMessage: "handler returned an error", wantDelay: 10 * time.Millisecond},
		{name: "retry after", handler: func(context.Context, payload) error {
			return job.RetryAfter(errors.New("secret retry detail"), 17*time.Millisecond)
		}, wantRetry: true, wantKind: "error", wantMessage: "handler requested a retry", wantDelay: 17 * time.Millisecond},
		{name: "permanent", handler: func(context.Context, payload) error { return job.Permanent(errors.New("secret permanent detail")) }, wantKind: "permanent", wantMessage: "handler reported a permanent failure"},
		{name: "panic", handler: func(context.Context, payload) error { panic("secret panic detail") }, wantRetry: true, wantKind: "panic", wantMessage: "handler panicked", wantDelay: 10 * time.Millisecond},
		{name: "malformed", handler: func(context.Context, payload) error { return nil }, mutate: func(delivery *job.Delivery) { delivery.Payload = []byte(`{"unknown":true}`) }, wantKind: "malformed_payload", wantMessage: "job payload could not be decoded"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &fakeStore{heartbeatOwn: true}
			item := delivery("a", 3)
			if test.mutate != nil {
				test.mutate(&item)
			}
			store.deliveries = []job.Delivery{item}
			store.onRetry = cancel
			store.onFail = cancel
			worker := newWorker(t, store, test.handler, job.WorkerConfig{})
			if err := worker.Run(ctx); err != nil {
				t.Fatal(err)
			}
			store.mu.Lock()
			defer store.mu.Unlock()
			if test.wantRetry {
				if len(store.retries) != 1 || store.retries[0].ErrorKind != test.wantKind || store.retries[0].Error != test.wantMessage || store.retries[0].Delay != test.wantDelay {
					t.Fatalf("unexpected retry: %+v", store.retries)
				}
			} else if len(store.failures) != 1 || store.failures[0].Kind != test.wantKind || store.failures[0].Error != test.wantMessage {
				t.Fatalf("unexpected failure: %+v", store.failures)
			}
		})
	}
}

func TestTimedOutUncooperativeHandlerStopsWorkerAndHeartbeatWithinBound(t *testing.T) {
	store := &fakeStore{heartbeatOwn: true}
	item := delivery("a", 3)
	item.Timeout = 8 * time.Millisecond
	store.deliveries = []job.Delivery{item}
	release := make(chan struct{})
	defer close(release)
	started := time.Now()
	worker := newWorker(t, store, func(context.Context, payload) error {
		<-release // deliberately ignores cancellation
		return nil
	}, job.WorkerConfig{Concurrency: 1, HeartbeatInterval: 3 * time.Millisecond, CancellationTimeout: 12 * time.Millisecond})
	err := worker.Run(context.Background())
	if !errors.Is(err, job.ErrHandlerCancellationTimeout) {
		t.Fatalf("worker error = %v, want cancellation timeout", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("uncooperative handler held worker beyond bound: %s", elapsed)
	}
	store.mu.Lock()
	heartbeats := store.heartbeats
	mutations := len(store.acks) + len(store.retries) + len(store.failures) + len(store.releases)
	store.mu.Unlock()
	if mutations != 0 {
		t.Fatal("abandoned handler mutated its fenced delivery")
	}
	time.Sleep(15 * time.Millisecond)
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.heartbeats != heartbeats {
		t.Fatalf("worker kept heartbeating after cancellation bound: %d -> %d", heartbeats, store.heartbeats)
	}
}

func TestHandlerReturningNilAfterDeadlineCannotBeAcknowledged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &fakeStore{heartbeatOwn: true}
	item := delivery("a", 3)
	item.Timeout = 8 * time.Millisecond
	store.deliveries = []job.Delivery{item}
	store.onRetry = cancel
	worker := newWorker(t, store, func(ctx context.Context, _ payload) error {
		<-ctx.Done()
		return nil
	}, job.WorkerConfig{HeartbeatInterval: 2 * time.Millisecond})
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.acks) != 0 || len(store.retries) != 1 || store.retries[0].ErrorKind != "timeout" {
		t.Fatalf("deadline result was not classified as a timeout: acks=%v retries=%+v", store.acks, store.retries)
	}
}

func TestLostLeaseCancelsHandlerWithoutMutation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &fakeStore{deliveries: []job.Delivery{delivery("a", 3)}, heartbeatOwn: false}
	observer := job.ObserverFunc(func(_ context.Context, event job.Event) {
		if event.Kind == job.EventLeaseLost {
			cancel()
		}
	})
	worker := newWorker(t, store, func(ctx context.Context, _ payload) error {
		<-ctx.Done()
		return ctx.Err()
	}, job.WorkerConfig{Observer: observer, HeartbeatInterval: 5 * time.Millisecond})
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.acks)+len(store.retries)+len(store.failures)+len(store.releases) != 0 {
		t.Fatal("lost owner mutated the job")
	}
}

func TestLostLeaseRetainsCapacityUntilUncooperativeHandlerReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &fakeStore{heartbeatOwn: false, deliveries: []job.Delivery{delivery("a", 3), delivery("b", 3)}}
	var active atomic.Int32
	var maximum atomic.Int32
	var lost atomic.Int32
	observer := job.ObserverFunc(func(_ context.Context, event job.Event) {
		if event.Kind == job.EventLeaseLost && lost.Add(1) == 2 {
			cancel()
		}
	})
	worker := newWorker(t, store, func(context.Context, payload) error {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(25 * time.Millisecond) // deliberately ignores cancellation
		active.Add(-1)
		return nil
	}, job.WorkerConfig{Concurrency: 1, HeartbeatInterval: 2 * time.Millisecond, Observer: observer})
	if err := worker.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if maximum.Load() != 1 {
		t.Fatalf("lost leases released local capacity early; maximum handlers=%d", maximum.Load())
	}
}

type blockingAckStore struct{ *fakeStore }

func (store blockingAckStore) Ack(ctx context.Context, _ job.Lease) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

type blockingHeartbeatStore struct{ *fakeStore }

func (store blockingHeartbeatStore) Heartbeat(ctx context.Context, _ job.Lease, _ time.Duration) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

type blockingClaimStore struct{ *fakeStore }

func (store blockingClaimStore) Claim(ctx context.Context, _ job.ClaimRequest) ([]job.Delivery, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestClaimIsBoundedByOperationTimeout(t *testing.T) {
	store := &fakeStore{}
	registry := job.NewRegistry()
	if err := job.Register(registry, definition(t, job.Policy{}), func(context.Context, payload) error { return nil }); err != nil {
		t.Fatal(err)
	}
	worker, err := job.NewWorker(blockingClaimStore{store}, registry, job.WorkerConfig{
		WorkerID: "worker", Concurrency: 1, PollInterval: time.Millisecond,
		LeaseDuration: 100 * time.Millisecond, HeartbeatInterval: 10 * time.Millisecond,
		OperationTimeout: 5 * time.Millisecond, ShutdownTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = worker.Run(context.Background())
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded claim failure, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("claim exceeded its operation bound: %s", elapsed)
	}
}

func TestHeartbeatIsBoundedByOperationTimeout(t *testing.T) {
	store := &fakeStore{deliveries: []job.Delivery{delivery("a", 3)}}
	registry := job.NewRegistry()
	if err := job.Register(registry, definition(t, job.Policy{}), func(ctx context.Context, _ payload) error {
		<-ctx.Done()
		return ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	worker, err := job.NewWorker(blockingHeartbeatStore{store}, registry, job.WorkerConfig{
		WorkerID: "worker", Concurrency: 1, PollInterval: time.Millisecond,
		LeaseDuration: 100 * time.Millisecond, HeartbeatInterval: 2 * time.Millisecond,
		OperationTimeout: 5 * time.Millisecond, ShutdownTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = worker.Run(context.Background())
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded heartbeat failure, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("heartbeat exceeded its operation bound: %s", elapsed)
	}
}

func TestFinalizationIsBoundedByOperationTimeout(t *testing.T) {
	store := &fakeStore{heartbeatOwn: true, deliveries: []job.Delivery{delivery("a", 3)}}
	registry := job.NewRegistry()
	if err := job.Register(registry, definition(t, job.Policy{}), func(context.Context, payload) error { return nil }); err != nil {
		t.Fatal(err)
	}
	worker, err := job.NewWorker(blockingAckStore{store}, registry, job.WorkerConfig{
		WorkerID: "worker", Concurrency: 1, PollInterval: time.Millisecond,
		LeaseDuration: 100 * time.Millisecond, HeartbeatInterval: 10 * time.Millisecond,
		OperationTimeout: 5 * time.Millisecond, ShutdownTimeout: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	err = worker.Run(context.Background())
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded acknowledgement failure, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("worker finalization exceeded its operation bound: %s", elapsed)
	}
}

func TestGracefulShutdownDrainsThenHardCancellationLeavesLease(t *testing.T) {
	t.Run("drain", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		store := &fakeStore{deliveries: []job.Delivery{delivery("a", 3)}, heartbeatOwn: true}
		started := make(chan struct{})
		release := make(chan struct{})
		worker := newWorker(t, store, func(context.Context, payload) error {
			close(started)
			<-release
			return nil
		}, job.WorkerConfig{ShutdownTimeout: 100 * time.Millisecond})
		done := make(chan error, 1)
		go func() { done <- worker.Run(ctx) }()
		<-started
		cancel()
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		if len(store.acks) != 1 {
			t.Fatalf("drained job was not acknowledged: %v", store.acks)
		}
	})

	t.Run("hard cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		store := &fakeStore{deliveries: []job.Delivery{delivery("a", 3)}, heartbeatOwn: true}
		started := make(chan struct{})
		release := make(chan struct{})
		worker := newWorker(t, store, func(context.Context, payload) error {
			close(started)
			<-release
			return nil
		}, job.WorkerConfig{ShutdownTimeout: 15 * time.Millisecond, HeartbeatInterval: 5 * time.Millisecond})
		done := make(chan error, 1)
		go func() { done <- worker.Run(ctx) }()
		<-started
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		store.mu.Lock()
		mutations := len(store.acks) + len(store.retries) + len(store.failures) + len(store.releases)
		store.mu.Unlock()
		if mutations != 0 {
			t.Fatal("forced work should remain recoverable by lease expiry")
		}
		close(release)
	})
}
