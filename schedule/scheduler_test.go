package schedule_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/job"
	"github.com/ShanilKoshitha/goforge/schedule"
)

type inertExecutor struct{ label string }

func (inertExecutor) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}
func (inertExecutor) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, nil
}
func (inertExecutor) QueryRowContext(context.Context, string, ...any) *sql.Row { return nil }

type recordingJobStore struct {
	mu        sync.Mutex
	requests  []job.EnqueueRequest
	executors []job.Executor
	err       error
	duplicate bool
}

func (store *recordingJobStore) Enqueue(_ context.Context, executor job.Executor, request job.EnqueueRequest) (job.DispatchResult, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.err != nil {
		return job.DispatchResult{}, store.err
	}
	store.requests = append(store.requests, request)
	store.executors = append(store.executors, executor)
	return job.DispatchResult{ID: job.ID("00000000-0000-4000-8000-000000000001"), Enqueued: !store.duplicate}, nil
}

func (*recordingJobStore) Claim(context.Context, job.ClaimRequest) ([]job.Delivery, error) {
	return nil, nil
}
func (*recordingJobStore) Heartbeat(context.Context, job.Lease, time.Duration) (bool, error) {
	return false, nil
}
func (*recordingJobStore) Ack(context.Context, job.Lease) (bool, error)           { return false, nil }
func (*recordingJobStore) Retry(context.Context, job.RetryRequest) (bool, error)  { return false, nil }
func (*recordingJobStore) Release(context.Context, job.Lease) (bool, error)       { return false, nil }
func (*recordingJobStore) Fail(context.Context, job.FailureRequest) (bool, error) { return false, nil }
func (*recordingJobStore) FailExhausted(context.Context, []string, []string, int) ([]job.FailedJob, error) {
	return nil, nil
}

type callbackScheduleStore struct {
	mu            sync.Mutex
	at            time.Time
	executor      job.Executor
	names         []string
	calls         int
	err           error
	result        schedule.Result
	returnDirect  bool
	beforeCall    func(context.Context)
	afterDispatch func()
}

func (store *callbackScheduleStore) Materialize(ctx context.Context, definition schedule.Definition, dispatch schedule.MaterializeFunc) (schedule.Result, error) {
	store.mu.Lock()
	store.calls++
	store.names = append(store.names, definition.Name())
	before := store.beforeCall
	after := store.afterDispatch
	err := store.err
	result := store.result
	returnDirect := store.returnDirect
	at := store.at
	executor := store.executor
	store.mu.Unlock()
	if before != nil {
		before(ctx)
	}
	if err != nil {
		return schedule.Result{}, err
	}
	if returnDirect {
		return result, nil
	}
	if result.Outcome == schedule.OutcomeNotDue && at.IsZero() {
		return result, nil
	}
	if result.Outcome != "" && result.Outcome != schedule.OutcomeEnqueued && result.Outcome != schedule.OutcomeOverlapSuppressed {
		return result, nil
	}
	dispatched, err := dispatch(ctx, executor, schedule.Occurrence{ScheduledAt: at})
	if err != nil {
		return schedule.Result{}, err
	}
	if after != nil {
		after()
	}
	if result.Name == "" {
		result.Name = definition.Name()
	}
	if result.Outcome == "" {
		result.Outcome = schedule.OutcomeEnqueued
		if !dispatched.Enqueued {
			result.Outcome = schedule.OutcomeOverlapSuppressed
		}
	}
	result.ScheduledAt = at
	result.JobID = dispatched.ID
	return result, nil
}

func (*callbackScheduleStore) List(context.Context, []schedule.Definition) ([]schedule.Status, error) {
	return nil, nil
}

func newTestScheduler(t *testing.T, store schedule.Store, registry *schedule.Registry, jobs *recordingJobStore, config schedule.Config) *schedule.Scheduler {
	t.Helper()
	dispatcher, err := job.NewDispatcher(jobs, inertExecutor{label: "base"}, job.DispatcherConfig{})
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := schedule.NewScheduler(store, registry, dispatcher, config)
	if err != nil {
		t.Fatal(err)
	}
	return scheduler
}

func TestRunOnceUsesSortedDefinitionsTransactionExecutorAndCanonicalOccurrence(t *testing.T) {
	registry := schedule.NewRegistry()
	var got schedule.Occurrence
	second := scheduleDefinition(t, "tests.second.v1", "* * * * *", schedule.Static(testPayload{Value: "second"}))
	first := scheduleDefinition(t, "tests.first.v1", "* * * * *", schedule.Dynamic(func(occurrence schedule.Occurrence) (testPayload, error) {
		got = occurrence
		return testPayload{Value: occurrence.LocalTime().Format("15:04 MST")}, nil
	}), schedule.TimeZone("America/Halifax"))
	schedule.MustRegister(registry, second)
	schedule.MustRegister(registry, first)
	at := time.Date(2026, time.September, 9, 12, 34, 0, 0, time.UTC)
	tx := inertExecutor{label: "transaction"}
	store := &callbackScheduleStore{at: at, executor: tx}
	jobs := &recordingJobStore{}
	scheduler := newTestScheduler(t, store, registry, jobs, schedule.Config{})
	results, err := scheduler.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Name != "tests.first.v1" || results[1].Name != "tests.second.v1" {
		t.Fatalf("results = %+v", results)
	}
	if got.Schedule != "tests.first.v1" || got.TimeZone != "America/Halifax" || !got.ScheduledAt.Equal(at) || got.ScheduledAt.Location() != time.UTC {
		t.Fatalf("occurrence = %+v", got)
	}
	if local := got.LocalTime(); local.Hour() != 9 {
		t.Fatalf("local occurrence = %s", local)
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if len(jobs.executors) != 2 || jobs.executors[0] != tx || jobs.executors[1] != tx {
		t.Fatalf("dispatch executors = %v", jobs.executors)
	}
	if jobs.requests[0].DedupKey != "goforge:schedule:tests.first.v1" || jobs.requests[1].DedupKey != "goforge:schedule:tests.second.v1" {
		t.Fatalf("deduplication keys = %q, %q", jobs.requests[0].DedupKey, jobs.requests[1].DedupKey)
	}
}

func TestStaticSnapshotsMutablePayloadForFingerprintAndDispatch(t *testing.T) {
	type mutablePayload struct {
		Values map[string][]int `json:"values"`
	}
	target := job.MustDefine[mutablePayload]("tests.mutable.v1", job.Policy{})
	original := mutablePayload{Values: map[string][]int{"numbers": {1, 2}}}
	factory := schedule.Static(original)
	original.Values["numbers"][0] = 99
	original.Values["new"] = []int{3}
	definition := schedule.MustDefine("tests.mutable.v1", "* * * * *", target, factory)
	wantDefinition := schedule.MustDefine("tests.mutable.v1", "* * * * *", target, schedule.Static(mutablePayload{Values: map[string][]int{"numbers": {1, 2}}}))
	if definition.Fingerprint() != wantDefinition.Fingerprint() {
		t.Fatal("mutation after Static changed the fingerprint snapshot")
	}
	original.Values["numbers"][1] = 88
	registry := schedule.NewRegistry()
	schedule.MustRegister(registry, definition)
	jobs := &recordingJobStore{}
	store := &callbackScheduleStore{
		at: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC), executor: inertExecutor{label: "tx"},
	}
	if _, err := newTestScheduler(t, store, registry, jobs, schedule.Config{}).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	var dispatched mutablePayload
	if err := json.Unmarshal(jobs.requests[0].Payload, &dispatched); err != nil {
		t.Fatal(err)
	}
	if len(dispatched.Values) != 1 || len(dispatched.Values["numbers"]) != 2 || dispatched.Values["numbers"][0] != 1 || dispatched.Values["numbers"][1] != 2 {
		t.Fatalf("dispatched payload = %+v", dispatched)
	}
}

func TestRunOnceFiltersOnlyNotDueAndPreservesMeaningfulOutcomes(t *testing.T) {
	registry := schedule.NewRegistry()
	schedule.MustRegister(registry, scheduleDefinition(t, "tests.one.v1", "* * * * *", schedule.Static(testPayload{})))
	jobs := &recordingJobStore{}
	store := &callbackScheduleStore{result: schedule.Result{Name: "tests.one.v1", Outcome: schedule.OutcomeNotDue}}
	results, err := newTestScheduler(t, store, registry, jobs, schedule.Config{}).RunOnce(context.Background())
	if err != nil || len(results) != 0 {
		t.Fatalf("not-due results = %+v, err=%v", results, err)
	}
	store.at = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	store.result = schedule.Result{Name: "tests.one.v1", Outcome: schedule.OutcomeContended}
	results, err = newTestScheduler(t, store, registry, jobs, schedule.Config{}).RunOnce(context.Background())
	if err != nil || len(results) != 1 || results[0].Outcome != schedule.OutcomeContended {
		t.Fatalf("contended results = %+v, err=%v", results, err)
	}
}

func TestRunOnceReportsActiveJobAsOverlapSuppressed(t *testing.T) {
	registry := schedule.NewRegistry()
	schedule.MustRegister(registry, scheduleDefinition(t, "tests.overlap.v1", "* * * * *", schedule.Static(testPayload{})))
	jobs := &recordingJobStore{duplicate: true}
	store := &callbackScheduleStore{at: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC), executor: inertExecutor{}}
	results, err := newTestScheduler(t, store, registry, jobs, schedule.Config{}).RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Outcome != schedule.OutcomeOverlapSuppressed || results[0].JobID == "" {
		t.Fatalf("results = %+v", results)
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if len(jobs.requests) != 1 || jobs.requests[0].DedupKey != "goforge:schedule:tests.overlap.v1" {
		t.Fatalf("requests = %+v", jobs.requests)
	}
}

func TestRunOncePreservesStoreFactoryAndDispatchErrors(t *testing.T) {
	sentinel := errors.New("sentinel")
	tests := []struct {
		name       string
		definition schedule.Definition
		storeErr   error
		jobErr     error
	}{
		{name: "store", definition: scheduleDefinition(t, "tests.store.v1", "* * * * *", schedule.Static(testPayload{})), storeErr: sentinel},
		{name: "factory", definition: scheduleDefinition(t, "tests.factory.v1", "* * * * *", schedule.Dynamic(func(schedule.Occurrence) (testPayload, error) { return testPayload{}, sentinel }))},
		{name: "dispatch", definition: scheduleDefinition(t, "tests.dispatch.v1", "* * * * *", schedule.Static(testPayload{})), jobErr: sentinel},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry := schedule.NewRegistry()
			schedule.MustRegister(registry, test.definition)
			jobs := &recordingJobStore{err: test.jobErr}
			store := &callbackScheduleStore{at: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC), executor: inertExecutor{}, err: test.storeErr}
			_, err := newTestScheduler(t, store, registry, jobs, schedule.Config{}).RunOnce(context.Background())
			if !errors.Is(err, sentinel) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRunOnceRejectsInvalidOccurrenceWithoutDispatch(t *testing.T) {
	registry := schedule.NewRegistry()
	schedule.MustRegister(registry, scheduleDefinition(t, "tests.invalid.v1", "* * * * *", schedule.Static(testPayload{})))
	jobs := &recordingJobStore{}
	store := &callbackScheduleStore{at: time.Date(2026, 9, 9, 12, 0, 1, 0, time.UTC), executor: inertExecutor{}}
	_, err := newTestScheduler(t, store, registry, jobs, schedule.Config{}).RunOnce(context.Background())
	if err == nil {
		t.Fatal("expected invalid occurrence error")
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if len(jobs.requests) != 0 {
		t.Fatal("invalid occurrence reached job store")
	}
}

func TestEmptyRegistryDoesNotAccessStore(t *testing.T) {
	store := &callbackScheduleStore{err: errors.New("must not be called")}
	results, err := newTestScheduler(t, store, schedule.NewRegistry(), &recordingJobStore{}, schedule.Config{}).RunOnce(context.Background())
	if err != nil || len(results) != 0 || store.calls != 0 {
		t.Fatalf("results=%v err=%v calls=%d", results, err, store.calls)
	}
}

func TestSchedulerValidatesDependenciesAndBounds(t *testing.T) {
	jobStore := &recordingJobStore{}
	dispatcher, err := job.NewDispatcher(jobStore, inertExecutor{}, job.DispatcherConfig{})
	if err != nil {
		t.Fatal(err)
	}
	var typedNil *callbackScheduleStore
	tests := []struct {
		name       string
		store      schedule.Store
		registry   *schedule.Registry
		dispatcher job.Dispatcher
		config     schedule.Config
	}{
		{name: "nil store", registry: schedule.NewRegistry(), dispatcher: dispatcher},
		{name: "typed nil store", store: typedNil, registry: schedule.NewRegistry(), dispatcher: dispatcher},
		{name: "nil registry", store: &callbackScheduleStore{}, dispatcher: dispatcher},
		{name: "zero dispatcher", store: &callbackScheduleStore{}, registry: schedule.NewRegistry()},
		{name: "typed nil observer", store: &callbackScheduleStore{}, registry: schedule.NewRegistry(), dispatcher: dispatcher, config: schedule.Config{Observer: schedule.ObserverFunc(nil)}},
		{name: "short poll", store: &callbackScheduleStore{}, registry: schedule.NewRegistry(), dispatcher: dispatcher, config: schedule.Config{PollInterval: time.Millisecond}},
		{name: "long poll", store: &callbackScheduleStore{}, registry: schedule.NewRegistry(), dispatcher: dispatcher, config: schedule.Config{PollInterval: 2 * time.Minute}},
		{name: "short operation", store: &callbackScheduleStore{}, registry: schedule.NewRegistry(), dispatcher: dispatcher, config: schedule.Config{OperationTimeout: time.Millisecond}},
		{name: "long operation", store: &callbackScheduleStore{}, registry: schedule.NewRegistry(), dispatcher: dispatcher, config: schedule.Config{OperationTimeout: 2 * time.Minute}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := schedule.NewScheduler(test.store, test.registry, test.dispatcher, test.config); err == nil {
				t.Fatal("expected constructor error")
			}
		})
	}
}

func TestRunOnceRejectsMalformedStoreResults(t *testing.T) {
	registry := schedule.NewRegistry()
	schedule.MustRegister(registry, scheduleDefinition(t, "tests.result.v1", "* * * * *", schedule.Static(testPayload{})))
	minute := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	tests := []schedule.Result{
		{Name: "wrong.v1", Outcome: schedule.OutcomeNotDue},
		{Name: "tests.result.v1", Outcome: "invented"},
		{Name: "tests.result.v1", Outcome: schedule.OutcomeNotDue, ScheduledAt: minute},
		{Name: "tests.result.v1", Outcome: schedule.OutcomeContended, JobID: "job"},
		{Name: "tests.result.v1", Outcome: schedule.OutcomeMisfireSkipped, ScheduledAt: minute},
		{Name: "tests.result.v1", Outcome: schedule.OutcomeEnqueued},
		{Name: "tests.result.v1", Outcome: schedule.OutcomeEnqueued, ScheduledAt: minute, JobID: ""},
		{Name: "tests.result.v1", Outcome: schedule.OutcomeEnqueued, ScheduledAt: minute.Add(time.Second), JobID: "job"},
		{Name: "tests.result.v1", Outcome: schedule.OutcomeEnqueued, ScheduledAt: time.Date(2026, 9, 9, 8, 0, 0, 0, time.FixedZone("EDT", -4*60*60)), JobID: "job"},
	}
	for index, result := range tests {
		t.Run(string(rune('a'+index)), func(t *testing.T) {
			store := &callbackScheduleStore{returnDirect: true, result: result}
			if _, err := newTestScheduler(t, store, registry, &recordingJobStore{}, schedule.Config{}).RunOnce(context.Background()); err == nil {
				t.Fatalf("accepted malformed result: %+v", result)
			}
		})
	}
	valid := []schedule.Result{
		{Name: "tests.result.v1", Outcome: schedule.OutcomeNotDue},
		{Name: "tests.result.v1", Outcome: schedule.OutcomeContended},
		{Name: "tests.result.v1", Outcome: schedule.OutcomeMisfireSkipped},
		{Name: "tests.result.v1", Outcome: schedule.OutcomeEnqueued, ScheduledAt: minute, JobID: "job"},
		{Name: "tests.result.v1", Outcome: schedule.OutcomeOverlapSuppressed, ScheduledAt: minute, JobID: "job"},
	}
	for _, result := range valid {
		store := &callbackScheduleStore{returnDirect: true, result: result}
		if _, err := newTestScheduler(t, store, registry, &recordingJobStore{}, schedule.Config{}).RunOnce(context.Background()); err != nil {
			t.Fatalf("rejected valid result %+v: %v", result, err)
		}
	}
}

func TestRunOnceBoundsStoreOperation(t *testing.T) {
	registry := schedule.NewRegistry()
	schedule.MustRegister(registry, scheduleDefinition(t, "tests.timeout.v1", "* * * * *", schedule.Static(testPayload{})))
	store := &callbackScheduleStore{beforeCall: func(ctx context.Context) { <-ctx.Done() }, err: context.DeadlineExceeded}
	started := time.Now()
	_, err := newTestScheduler(t, store, registry, &recordingJobStore{}, schedule.Config{OperationTimeout: 100 * time.Millisecond}).RunOnce(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("operation exceeded bound: %s", elapsed)
	}
}

func TestRunOnceBoundsContextAwareFactoryAndSkipsDispatch(t *testing.T) {
	registry := schedule.NewRegistry()
	var received context.Context
	schedule.MustRegister(registry, scheduleDefinition(t, "tests.factory-timeout.v1", "* * * * *", schedule.DynamicContext(func(ctx context.Context, _ schedule.Occurrence) (testPayload, error) {
		received = ctx
		<-ctx.Done()
		return testPayload{}, ctx.Err()
	})))
	jobs := &recordingJobStore{}
	store := &callbackScheduleStore{
		at:       time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC),
		executor: inertExecutor{},
	}
	started := time.Now()
	_, err := newTestScheduler(t, store, registry, jobs, schedule.Config{OperationTimeout: 100 * time.Millisecond}).RunOnce(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
	if received == nil || !errors.Is(received.Err(), context.DeadlineExceeded) {
		t.Fatalf("factory context error = %v", received)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("factory operation exceeded bound: %s", elapsed)
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if len(jobs.requests) != 0 {
		t.Fatalf("timed-out factory dispatched %d jobs", len(jobs.requests))
	}
}

func TestRunOnceChecksCancellationBeforeFactory(t *testing.T) {
	registry := schedule.NewRegistry()
	called := false
	schedule.MustRegister(registry, scheduleDefinition(t, "tests.factory-pre-cancel.v1", "* * * * *", schedule.DynamicContext(func(context.Context, schedule.Occurrence) (testPayload, error) {
		called = true
		return testPayload{Value: "must not build"}, nil
	})))
	operationContext, cancelOperation := context.WithCancel(context.Background())
	jobs := &recordingJobStore{}
	store := &callbackScheduleStore{
		at:         time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC),
		executor:   inertExecutor{},
		beforeCall: func(context.Context) { cancelOperation() },
	}
	_, err := newTestScheduler(t, store, registry, jobs, schedule.Config{}).RunOnce(operationContext)
	cancelOperation()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if called {
		t.Fatal("canceled operation invoked payload factory")
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if len(jobs.requests) != 0 {
		t.Fatalf("canceled operation dispatched %d jobs", len(jobs.requests))
	}
}

func TestRunOnceChecksCancellationAfterFactoryBeforeDispatch(t *testing.T) {
	registry := schedule.NewRegistry()
	operationContext, cancelOperation := context.WithCancel(context.Background())
	schedule.MustRegister(registry, scheduleDefinition(t, "tests.factory-cancel.v1", "* * * * *", schedule.DynamicContext(func(context.Context, schedule.Occurrence) (testPayload, error) {
		cancelOperation()
		return testPayload{Value: "must not dispatch"}, nil
	})))
	jobs := &recordingJobStore{}
	store := &callbackScheduleStore{
		at:       time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC),
		executor: inertExecutor{},
	}
	_, err := newTestScheduler(t, store, registry, jobs, schedule.Config{}).RunOnce(operationContext)
	cancelOperation()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if len(jobs.requests) != 0 {
		t.Fatalf("canceled factory dispatched %d jobs", len(jobs.requests))
	}
}

func TestRunExecutesImmediatelyAndCancellationIsNormal(t *testing.T) {
	registry := schedule.NewRegistry()
	schedule.MustRegister(registry, scheduleDefinition(t, "tests.run.v1", "* * * * *", schedule.Static(testPayload{})))
	started := make(chan struct{})
	release := make(chan struct{})
	store := &callbackScheduleStore{beforeCall: func(context.Context) {
		close(started)
		<-release // Deliberately ignores cancellation.
	}, err: errors.New("alien error after cancellation")}
	scheduler := newTestScheduler(t, store, registry, &recordingJobStore{}, schedule.Config{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx) }()
	<-started
	cancel()
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancellation returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop")
	}
	if store.calls != 1 {
		t.Fatalf("immediate calls = %d", store.calls)
	}
}

func TestObserverEmitsOnlyAfterMaterializeReturns(t *testing.T) {
	registry := schedule.NewRegistry()
	schedule.MustRegister(registry, scheduleDefinition(t, "tests.observe.v1", "* * * * *", schedule.Static(testPayload{})))
	dispatched := make(chan struct{})
	release := make(chan struct{})
	events := make(chan schedule.Event, 1)
	store := &callbackScheduleStore{
		at: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC), executor: inertExecutor{},
		afterDispatch: func() {
			close(dispatched)
			<-release
		},
	}
	dispatcher, err := job.NewDispatcher(&recordingJobStore{}, inertExecutor{}, job.DispatcherConfig{})
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := schedule.NewScheduler(store, registry, dispatcher, schedule.Config{Observer: schedule.ObserverFunc(func(_ context.Context, event schedule.Event) {
		events <- event
	})})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := scheduler.RunOnce(context.Background())
		done <- err
	}()
	<-dispatched
	select {
	case event := <-events:
		t.Fatalf("observer ran before materialization returned: %+v", event)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Name != "tests.observe.v1" || event.Outcome != schedule.OutcomeEnqueued || event.JobID == "" || event.At.Location() != time.UTC {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("observer did not receive committed result")
	}
}

func TestBlockingObserverIsSerializedBoundedAndNonBlocking(t *testing.T) {
	registry := schedule.NewRegistry()
	schedule.MustRegister(registry, scheduleDefinition(t, "tests.blocking.v1", "* * * * *", schedule.Static(testPayload{})))
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var active atomic.Int32
	var maximum atomic.Int32
	observer := schedule.ObserverFunc(func(context.Context, schedule.Event) {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		once.Do(func() { close(started) })
		<-release
		active.Add(-1)
	})
	store := &callbackScheduleStore{at: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC), executor: inertExecutor{}}
	dispatcher, err := job.NewDispatcher(&recordingJobStore{}, inertExecutor{}, job.DispatcherConfig{})
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := schedule.NewScheduler(store, registry, dispatcher, schedule.Config{Observer: observer})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-started
	startedAt := time.Now()
	for range 300 {
		if _, err := scheduler.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("blocked observer stalled materialization: %s", elapsed)
	}
	if scheduler.DroppedObserverEvents() == 0 {
		t.Fatal("observer backpressure was not observable")
	}
	if maximum.Load() != 1 {
		t.Fatalf("observer executions were not serialized: %d", maximum.Load())
	}
	close(release)
}

func TestObserverPanicCannotFailMaterialization(t *testing.T) {
	registry := schedule.NewRegistry()
	schedule.MustRegister(registry, scheduleDefinition(t, "tests.panic.v1", "* * * * *", schedule.Static(testPayload{})))
	store := &callbackScheduleStore{at: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC), executor: inertExecutor{}}
	dispatcher, err := job.NewDispatcher(&recordingJobStore{}, inertExecutor{}, job.DispatcherConfig{})
	if err != nil {
		t.Fatal(err)
	}
	scheduler, err := schedule.NewScheduler(store, registry, dispatcher, schedule.Config{Observer: schedule.ObserverFunc(func(context.Context, schedule.Event) {
		panic("observer")
	})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scheduler.RunOnce(context.Background()); err != nil {
		t.Fatalf("observer panic escaped: %v", err)
	}
}
