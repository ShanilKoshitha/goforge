package schedule

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ShanilKoshitha/goforge/job"
)

// Event is payload-free durable scheduler lifecycle metadata.
type Event struct {
	Name        string
	Outcome     Outcome
	ScheduledAt time.Time
	JobID       job.ID
	At          time.Time
}

// Observer receives immutable schedule events after durable materialization
// returns successfully.
type Observer interface {
	Observe(context.Context, Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(context.Context, Event)

func (observer ObserverFunc) Observe(ctx context.Context, event Event) { observer(ctx, event) }

type slogObserver struct{ logger *slog.Logger }

// SlogObserver emits payload-free schedule decisions through structured logs.
func SlogObserver(logger *slog.Logger) Observer {
	if logger == nil {
		logger = slog.Default()
	}
	return slogObserver{logger: logger}
}

func (observer slogObserver) Observe(ctx context.Context, event Event) {
	observer.logger.InfoContext(ctx, "schedule "+string(event.Outcome),
		"schedule", event.Name, "scheduled_at", event.ScheduledAt,
		"job_id", event.JobID, "at", event.At)
}

const observerQueueCapacity = 256

type observedEvent struct {
	ctx   context.Context
	event Event
}

type eventEmitter struct {
	observer Observer

	mu      sync.Mutex
	pending []observedEvent
	running bool
	dropped atomic.Uint64
}

func newEventEmitter(observer Observer) *eventEmitter {
	if observer == nil {
		return nil
	}
	return &eventEmitter{observer: observer}
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
	emitter.mu.Unlock()
	go emitter.drain()
}

func (emitter *eventEmitter) drain() {
	for {
		emitter.mu.Lock()
		if len(emitter.pending) == 0 {
			emitter.running = false
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

func (emitter *eventEmitter) droppedEvents() uint64 {
	if emitter == nil {
		return 0
	}
	return emitter.dropped.Load()
}
