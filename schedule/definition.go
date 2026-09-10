package schedule

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/ShanilKoshitha/goforge/job"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*\.v[1-9][0-9]*$`)
var timeZonePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._+-]*(/[A-Za-z0-9._+-]+)+$`)

const (
	staticPayloadStrategy  = "static-json-v1"
	dynamicPayloadStrategy = "dynamic-factory-v1"
	overlapPolicy          = "forbid"
)

// Factory is an opaque typed schedule payload strategy.
type Factory[P any] struct {
	build      func(context.Context, Occurrence) (P, error)
	strategy   string
	staticJSON []byte
	err        error
}

// Static snapshots a payload as canonical encoding/json bytes. Later mutation
// of the supplied value cannot change dispatched schedule payloads.
func Static[P any](payload P) Factory[P] {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Factory[P]{err: fmt.Errorf("schedule: encode static payload: %w", err)}
	}
	var snapshot P
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return Factory[P]{err: fmt.Errorf("schedule: decode static payload snapshot: %w", err)}
	}
	reencoded, err := json.Marshal(snapshot)
	if err != nil || !bytes.Equal(encoded, reencoded) {
		return Factory[P]{err: fmt.Errorf("schedule: static payload must round-trip through canonical JSON")}
	}
	bytes := append([]byte(nil), encoded...)
	return Factory[P]{
		strategy:   staticPayloadStrategy,
		staticJSON: bytes,
		build: func(_ context.Context, _ Occurrence) (P, error) {
			var result P
			if err := json.Unmarshal(bytes, &result); err != nil {
				return result, fmt.Errorf("decode static payload: %w", err)
			}
			return result, nil
		},
	}
}

// Dynamic binds an occurrence-aware typed payload factory. Its implementation
// is governed by the schedule's versioned name rather than function identity.
func Dynamic[P any](factory func(Occurrence) (P, error)) Factory[P] {
	if factory == nil {
		return Factory[P]{err: fmt.Errorf("schedule: dynamic payload factory is required")}
	}
	return Factory[P]{
		build: func(_ context.Context, occurrence Occurrence) (P, error) {
			return factory(occurrence)
		},
		strategy: dynamicPayloadStrategy,
	}
}

// DynamicContext binds a cancellation-aware occurrence payload factory. Its
// implementation shares Dynamic's versioned strategy marker, so adopting it
// does not change an existing schedule definition's fingerprint.
func DynamicContext[P any](factory func(context.Context, Occurrence) (P, error)) Factory[P] {
	if factory == nil {
		return Factory[P]{err: fmt.Errorf("schedule: dynamic payload factory is required")}
	}
	return Factory[P]{build: factory, strategy: dynamicPayloadStrategy}
}

type definitionSettings struct {
	timeZone     string
	location     *time.Location
	misfireGrace time.Duration
}

// Option changes one schedule definition.
type Option func(*definitionSettings) error

// TimeZone evaluates civil cron fields in one explicit IANA location.
func TimeZone(name string) Option {
	return func(settings *definitionSettings) error {
		location, err := loadTimeZone(name)
		if err != nil {
			return err
		}
		settings.timeZone = name
		settings.location = location
		return nil
	}
}

// MisfireGrace sets the inclusive coalescing window.
func MisfireGrace(grace time.Duration) Option {
	return func(settings *definitionSettings) error {
		if grace < time.Minute || grace > 365*24*time.Hour || grace%time.Minute != 0 {
			return fmt.Errorf("schedule: misfire grace must be a whole minute between 1m and 365d")
		}
		settings.misfireGrace = grace
		return nil
	}
}

// Definition is an immutable recurring typed-job contract.
type Definition struct {
	name         string
	expression   cronExpression
	timeZone     string
	location     *time.Location
	misfireGrace time.Duration
	jobName      string
	policy       job.Policy
	fingerprint  string
	materialize  func(context.Context, job.Dispatcher, job.Executor, Occurrence) (job.DispatchResult, error)
}

// Define validates and binds a recurring typed-job definition.
func Define[P any](name, expression string, target job.Definition[P], factory Factory[P], options ...Option) (Definition, error) {
	if name != strings.TrimSpace(name) || len(name) == 0 || len(name) > MaxNameBytes || !namePattern.MatchString(name) {
		return Definition{}, fmt.Errorf("schedule: invalid versioned name %q", name)
	}
	if target.Name() == "" {
		return Definition{}, fmt.Errorf("schedule: valid job definition is required")
	}
	if factory.err != nil {
		return Definition{}, factory.err
	}
	if factory.build == nil || (factory.strategy != staticPayloadStrategy && factory.strategy != dynamicPayloadStrategy) {
		return Definition{}, fmt.Errorf("schedule: valid payload factory is required")
	}
	settings := definitionSettings{timeZone: "UTC", location: time.UTC, misfireGrace: DefaultMisfireGrace}
	for _, option := range options {
		if option == nil {
			return Definition{}, fmt.Errorf("schedule: nil definition option")
		}
		if err := option(&settings); err != nil {
			return Definition{}, err
		}
	}
	parsed, err := parseCron(expression, settings.location)
	if err != nil {
		return Definition{}, err
	}
	policy := target.Policy()
	fingerprint, err := definitionFingerprint(name, parsed.canonical, settings, target.Name(), policy, factory)
	if err != nil {
		return Definition{}, err
	}
	definition := Definition{
		name: name, expression: parsed, timeZone: settings.timeZone, location: settings.location,
		misfireGrace: settings.misfireGrace, jobName: target.Name(), policy: policy, fingerprint: fingerprint,
	}
	definition.materialize = func(ctx context.Context, dispatcher job.Dispatcher, executor job.Executor, occurrence Occurrence) (job.DispatchResult, error) {
		if err := ctx.Err(); err != nil {
			return job.DispatchResult{}, err
		}
		payload, err := factory.build(ctx, occurrence)
		if err != nil {
			return job.DispatchResult{}, fmt.Errorf("schedule: build payload for %s: %w", name, err)
		}
		if err := ctx.Err(); err != nil {
			return job.DispatchResult{}, err
		}
		return target.Dispatch(ctx, dispatcher.Using(executor), payload, job.Deduplicate("goforge:schedule:"+name))
	}
	return definition, nil
}

// MustDefine panics when a package-level definition is invalid.
func MustDefine[P any](name, expression string, target job.Definition[P], factory Factory[P], options ...Option) Definition {
	definition, err := Define(name, expression, target, factory, options...)
	if err != nil {
		panic(err)
	}
	return definition
}

func loadTimeZone(name string) (*time.Location, error) {
	if name != strings.TrimSpace(name) || name == "" || name == "Local" {
		return nil, fmt.Errorf("schedule: time zone must be UTC or an explicit IANA name")
	}
	if name == "UTC" {
		return time.UTC, nil
	}
	if len(name) > 128 || !timeZonePattern.MatchString(name) {
		return nil, fmt.Errorf("schedule: invalid IANA time zone %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == "." || part == ".." {
			return nil, fmt.Errorf("schedule: invalid IANA time zone %q", name)
		}
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("schedule: load IANA time zone %q: %w", name, err)
	}
	return location, nil
}

func definitionFingerprint[P any](name, expression string, settings definitionSettings, jobName string, policy job.Policy, factory Factory[P]) (string, error) {
	type input struct {
		Version         int             `json:"version"`
		Name            string          `json:"name"`
		Expression      string          `json:"expression"`
		TimeZone        string          `json:"time_zone"`
		MisfireGraceNS  int64           `json:"misfire_grace_ns"`
		JobName         string          `json:"job_name"`
		Queue           string          `json:"queue"`
		MaxAttempts     int             `json:"max_attempts"`
		TimeoutNS       int64           `json:"timeout_ns"`
		BackoffNS       []int64         `json:"backoff_ns"`
		PayloadStrategy string          `json:"payload_strategy"`
		StaticPayload   json.RawMessage `json:"static_payload,omitempty"`
		OverlapPolicy   string          `json:"overlap_policy"`
	}
	backoff := make([]int64, len(policy.Backoff))
	for index, duration := range policy.Backoff {
		backoff[index] = int64(duration)
	}
	encoded, err := json.Marshal(input{
		Version: 1, Name: name, Expression: expression, TimeZone: settings.timeZone,
		MisfireGraceNS: int64(settings.misfireGrace), JobName: jobName, Queue: policy.Queue,
		MaxAttempts: policy.MaxAttempts, TimeoutNS: int64(policy.Timeout), BackoffNS: backoff,
		PayloadStrategy: factory.strategy, StaticPayload: factory.staticJSON, OverlapPolicy: overlapPolicy,
	})
	if err != nil {
		return "", fmt.Errorf("schedule: encode definition fingerprint: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func (definition Definition) Name() string                { return definition.name }
func (definition Definition) Expression() string          { return definition.expression.canonical }
func (definition Definition) TimeZone() string            { return definition.timeZone }
func (definition Definition) MisfireGrace() time.Duration { return definition.misfireGrace }
func (definition Definition) JobName() string             { return definition.jobName }
func (definition Definition) Queue() string               { return definition.policy.Queue }
func (definition Definition) Fingerprint() string         { return definition.fingerprint }

// JobPolicy returns a defensive copy of the effective target-job policy.
func (definition Definition) JobPolicy() job.Policy {
	policy := definition.policy
	policy.Backoff = append([]time.Duration(nil), policy.Backoff...)
	return policy
}

// Next returns the next matching UTC occurrence strictly after after.
func (definition Definition) Next(after time.Time) time.Time {
	return definition.expression.next(after)
}

func (definition Definition) occurrence(at time.Time) (Occurrence, error) {
	if at.IsZero() || !at.Equal(at.Truncate(time.Minute)) {
		return Occurrence{}, fmt.Errorf("schedule: occurrence must be a non-zero whole minute")
	}
	return Occurrence{
		Schedule: definition.name, ScheduledAt: at.UTC(), TimeZone: definition.timeZone,
		location: definition.location,
	}, nil
}
