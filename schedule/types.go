// Package schedule provides explicit recurring job definitions and scheduler lifecycle.
package schedule

import (
	"context"
	"errors"
	"time"

	"github.com/ShanilKoshitha/goforge/job"
)

const (
	DefaultMisfireGrace     = 15 * time.Minute
	DefaultPollInterval     = 15 * time.Second
	DefaultOperationTimeout = 5 * time.Second
	MaxNameBytes            = 128
)

var ErrDefinitionConflict = errors.New("schedule: durable definition conflicts with code")

// Outcome describes one durable schedule decision.
type Outcome string

const (
	OutcomeNotDue            Outcome = "not_due"
	OutcomeEnqueued          Outcome = "enqueued"
	OutcomeOverlapSuppressed Outcome = "overlap_suppressed"
	OutcomeMisfireSkipped    Outcome = "misfire_skipped"
	OutcomeContended         Outcome = "contended"
)

// Occurrence identifies one cron minute. ScheduledAt is always UTC.
type Occurrence struct {
	Schedule    string
	ScheduledAt time.Time
	TimeZone    string

	location *time.Location
}

// LocalTime returns the occurrence in its declared location.
func (occurrence Occurrence) LocalTime() time.Time {
	location := occurrence.location
	if location == nil {
		location = time.UTC
	}
	return occurrence.ScheduledAt.In(location)
}

// Result describes one scheduler decision. A zero ScheduledAt means that no
// concrete occurrence was selected.
type Result struct {
	Name        string
	Outcome     Outcome
	ScheduledAt time.Time
	JobID       job.ID
}

// Status combines the current code-owned definition with its durable state.
// Nil timestamps mean that the scheduler has not recorded that transition.
type Status struct {
	Name             string
	Expression       string
	TimeZone         string
	JobName          string
	Queue            string
	MisfireGrace     time.Duration
	Fingerprint      string
	NextRunAt        time.Time
	LastEvaluatedAt  *time.Time
	LastCursorAt     *time.Time
	LastOccurrenceAt *time.Time
	LastJobID        job.ID
	LastOutcome      Outcome
	CreatedAt        *time.Time
	UpdatedAt        *time.Time
}

// MaterializeFunc dispatches the selected occurrence through the executor for
// the store's current transaction.
type MaterializeFunc func(context.Context, job.Executor, Occurrence) (job.DispatchResult, error)

// Store is the replaceable durable coordination boundary. Materialize must
// invoke dispatch and advance its cursor in one atomic transaction.
type Store interface {
	Materialize(context.Context, Definition, MaterializeFunc) (Result, error)
	List(context.Context, []Definition) ([]Status, error)
}
