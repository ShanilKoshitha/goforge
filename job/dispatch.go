package job

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// DispatcherConfig controls encoded dispatch boundaries and observation.
type DispatcherConfig struct {
	Observer        Observer
	MaxPayloadBytes int
}

// Dispatcher dispatches through a replaceable store and explicit executor.
type Dispatcher struct {
	store           Store
	executor        Executor
	observer        *eventEmitter
	maxPayloadBytes int
}

// NewDispatcher validates a dispatcher.
func NewDispatcher(store Store, executor Executor, config DispatcherConfig) (Dispatcher, error) {
	if nilValue(store) {
		return Dispatcher{}, fmt.Errorf("job: store is required")
	}
	if err := ValidateExecutor(executor); err != nil {
		return Dispatcher{}, err
	}
	if config.MaxPayloadBytes == 0 {
		config.MaxPayloadBytes = DefaultMaxPayloadBytes
	}
	if config.MaxPayloadBytes < 1 {
		return Dispatcher{}, fmt.Errorf("job: max payload bytes must be positive")
	}
	return Dispatcher{store: store, executor: executor, observer: newEventEmitter(config.Observer), maxPayloadBytes: config.MaxPayloadBytes}, nil
}

// Using returns a dispatcher whose next calls use the supplied DB or transaction.
func (dispatcher Dispatcher) Using(executor Executor) Dispatcher {
	dispatcher.executor = executor
	return dispatcher
}

// DroppedObserverEvents reports lifecycle events discarded because the
// configured observer did not keep up with the bounded delivery queue.
func (dispatcher Dispatcher) DroppedObserverEvents() uint64 {
	return dispatcher.observer.droppedEvents()
}

type dispatchSettings struct {
	queue    string
	delay    time.Duration
	delaySet bool
	at       *time.Time
	dedupKey string
	priority int
}

// DispatchOption changes one dispatch without mutating the definition.
type DispatchOption func(*dispatchSettings) error

// Queue selects a validated queue.
func Queue(name string) DispatchOption {
	return func(settings *dispatchSettings) error {
		if err := validateQueue(name); err != nil {
			return err
		}
		settings.queue = name
		return nil
	}
}

// Delay makes a job eligible after a database-clock-relative delay.
func Delay(delay time.Duration) DispatchOption {
	return func(settings *dispatchSettings) error {
		if delay < 0 || delay > 365*24*time.Hour {
			return fmt.Errorf("job: delay must be between zero and one year")
		}
		if delay > 0 && delay < time.Millisecond {
			return fmt.Errorf("job: non-zero delay must be at least 1ms")
		}
		if settings.at != nil {
			return fmt.Errorf("job: delay and at are mutually exclusive")
		}
		settings.delay = delay
		settings.delaySet = true
		return nil
	}
}

// At makes a job eligible at an absolute timestamp.
func At(at time.Time) DispatchOption {
	return func(settings *dispatchSettings) error {
		if at.IsZero() {
			return fmt.Errorf("job: at timestamp is required")
		}
		if settings.delaySet {
			return fmt.Errorf("job: delay and at are mutually exclusive")
		}
		copy := at
		settings.at = &copy
		return nil
	}
}

// Deduplicate suppresses another active job with the same queue, name, and key.
func Deduplicate(key string) DispatchOption {
	return func(settings *dispatchSettings) error {
		if len(key) == 0 || len(key) > MaxDedupKeyBytes {
			return fmt.Errorf("job: deduplication key must be between 1 and %d bytes", MaxDedupKeyBytes)
		}
		settings.dedupKey = key
		return nil
	}
}

// Priority selects a signed small-integer priority.
func Priority(priority int) DispatchOption {
	return func(settings *dispatchSettings) error {
		if priority < -32768 || priority > 32767 {
			return fmt.Errorf("job: priority is outside smallint range")
		}
		settings.priority = priority
		return nil
	}
}

// Dispatch encodes and stores a typed payload.
func (definition Definition[P]) Dispatch(ctx context.Context, dispatcher Dispatcher, payload P, options ...DispatchOption) (DispatchResult, error) {
	if definition.name == "" {
		return DispatchResult{}, fmt.Errorf("job: valid definition is required")
	}
	if dispatcher.store == nil || ValidateExecutor(dispatcher.executor) != nil {
		return DispatchResult{}, fmt.Errorf("job: valid dispatcher is required")
	}
	settings := dispatchSettings{queue: definition.policy.Queue}
	for _, option := range options {
		if option == nil {
			return DispatchResult{}, fmt.Errorf("job: nil dispatch option")
		}
		if err := option(&settings); err != nil {
			return DispatchResult{}, err
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("job: encode %s: %w", definition.name, err)
	}
	if len(encoded) > dispatcher.maxPayloadBytes {
		return DispatchResult{}, fmt.Errorf("%w: %s is %d bytes; limit is %d", ErrPayloadTooLarge, definition.name, len(encoded), dispatcher.maxPayloadBytes)
	}
	id, err := newID()
	if err != nil {
		return DispatchResult{}, err
	}
	request := EnqueueRequest{
		ID: id, Queue: settings.queue, Name: definition.name, Payload: encoded,
		Priority: settings.priority, Delay: settings.delay, At: settings.at,
		MaxAttempts: definition.policy.MaxAttempts, Timeout: definition.policy.Timeout,
		Backoff: append([]time.Duration(nil), definition.policy.Backoff...), DedupKey: settings.dedupKey,
	}
	result, err := dispatcher.store.Enqueue(ctx, dispatcher.executor, request)
	if err != nil {
		return DispatchResult{}, wrapStore("dispatch "+definition.name, err)
	}
	kind := EventDuplicate
	if result.Enqueued {
		kind = EventDispatched
	}
	dispatcher.observer.emit(ctx, Event{Kind: kind, JobID: result.ID, Name: definition.name, Queue: settings.queue, At: time.Now()})
	return result, nil
}

func newID() (ID, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("job: generate id: %w", err)
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	var encoded [36]byte
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])
	return ID(encoded[:]), nil
}
