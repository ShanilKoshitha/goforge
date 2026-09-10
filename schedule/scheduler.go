package schedule

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/ShanilKoshitha/goforge/job"
)

// Config bounds scheduler polling and durable operations.
type Config struct {
	PollInterval     time.Duration
	OperationTimeout time.Duration
	Observer         Observer
}

// Scheduler materializes due definitions into the durable job queue.
type Scheduler struct {
	store      Store
	registry   *Registry
	dispatcher job.Dispatcher
	config     Config
	observer   *eventEmitter
}

func NewScheduler(store Store, registry *Registry, dispatcher job.Dispatcher, config Config) (*Scheduler, error) {
	if nilStore(store) {
		return nil, fmt.Errorf("schedule: store is required")
	}
	if registry == nil {
		return nil, fmt.Errorf("schedule: registry is required")
	}
	if config.Observer != nil && nilInterface(config.Observer) {
		return nil, fmt.Errorf("schedule: observer cannot be typed nil")
	}
	if err := dispatcher.Validate(); err != nil {
		return nil, fmt.Errorf("schedule: dispatcher is required: %w", err)
	}
	if config.PollInterval == 0 {
		config.PollInterval = DefaultPollInterval
	}
	if config.OperationTimeout == 0 {
		config.OperationTimeout = DefaultOperationTimeout
	}
	if config.PollInterval < time.Second || config.PollInterval > time.Minute {
		return nil, fmt.Errorf("schedule: poll interval must be between 1s and 1m")
	}
	if config.OperationTimeout < 100*time.Millisecond || config.OperationTimeout > time.Minute {
		return nil, fmt.Errorf("schedule: operation timeout must be between 100ms and 1m")
	}
	return &Scheduler{store: store, registry: registry, dispatcher: dispatcher, config: config, observer: newEventEmitter(config.Observer)}, nil
}

// DroppedObserverEvents reports events discarded because the configured
// observer did not keep up with the bounded serialized delivery queue.
func (scheduler *Scheduler) DroppedObserverEvents() uint64 {
	if scheduler == nil {
		return 0
	}
	return scheduler.observer.droppedEvents()
}

// RunOnce evaluates each registered definition once in stable name order.
func (scheduler *Scheduler) RunOnce(ctx context.Context) ([]Result, error) {
	if scheduler == nil {
		return nil, fmt.Errorf("schedule: scheduler is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	definitions := scheduler.registry.Definitions()
	if len(definitions) == 0 {
		return nil, nil
	}
	results := make([]Result, 0, len(definitions))
	for _, definition := range definitions {
		operationContext, cancel := context.WithTimeout(ctx, scheduler.config.OperationTimeout)
		result, err := scheduler.store.Materialize(operationContext, definition, func(callbackContext context.Context, executor job.Executor, raw Occurrence) (job.DispatchResult, error) {
			occurrence, occurrenceErr := definition.occurrence(raw.ScheduledAt)
			if occurrenceErr != nil {
				return job.DispatchResult{}, occurrenceErr
			}
			return definition.materialize(callbackContext, scheduler.dispatcher, executor, occurrence)
		})
		cancel()
		if err != nil {
			return results, fmt.Errorf("schedule: materialize %s: %w", definition.Name(), err)
		}
		if err := validateResult(definition, result); err != nil {
			return results, err
		}
		if result.Outcome != OutcomeNotDue {
			results = append(results, result)
			scheduler.observer.emit(ctx, Event{
				Name: result.Name, Outcome: result.Outcome, ScheduledAt: result.ScheduledAt,
				JobID: result.JobID, At: time.Now().UTC(),
			})
		}
	}
	return results, nil
}

// Run evaluates immediately and then polls until cancellation. Cancellation is
// a normal daemon shutdown.
func (scheduler *Scheduler) Run(ctx context.Context) error {
	if scheduler == nil {
		return fmt.Errorf("schedule: scheduler is required")
	}
	for {
		if _, err := scheduler.RunOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		timer := time.NewTimer(scheduler.config.PollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}

func nilStore(store Store) bool { return nilInterface(store) }

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func validateResult(definition Definition, result Result) error {
	if result.Name != definition.Name() {
		return fmt.Errorf("schedule: store returned result for %q while materializing %q", result.Name, definition.Name())
	}
	switch result.Outcome {
	case OutcomeNotDue, OutcomeMisfireSkipped, OutcomeContended:
		if !result.ScheduledAt.IsZero() || result.JobID != "" {
			return fmt.Errorf("schedule: %s result cannot contain an occurrence or job", result.Outcome)
		}
	case OutcomeEnqueued, OutcomeOverlapSuppressed:
		if result.JobID == "" || result.ScheduledAt.IsZero() ||
			!result.ScheduledAt.Equal(result.ScheduledAt.Truncate(time.Minute)) ||
			result.ScheduledAt.Location() != time.UTC {
			return fmt.Errorf("schedule: %s result requires a UTC whole-minute occurrence and job id", result.Outcome)
		}
	default:
		return fmt.Errorf("schedule: store returned invalid outcome %q", result.Outcome)
	}
	return nil
}
