package job

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*\.v[1-9][0-9]*$`)
var queuePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

var defaultBackoff = []time.Duration{time.Second, 5 * time.Second, 30 * time.Second}

// Definition is a typed, versioned job contract.
type Definition[P any] struct {
	name   string
	policy Policy
}

// Define validates a typed job definition.
func Define[P any](name string, policy Policy) (Definition[P], error) {
	name = strings.TrimSpace(name)
	if len(name) == 0 || len(name) > MaxNameBytes || !namePattern.MatchString(name) {
		return Definition[P]{}, fmt.Errorf("job: invalid versioned name %q", name)
	}
	if policy.Queue == "" {
		policy.Queue = "default"
	}
	if err := validateQueue(policy.Queue); err != nil {
		return Definition[P]{}, err
	}
	if policy.MaxAttempts == 0 {
		policy.MaxAttempts = 3
	}
	if policy.MaxAttempts < 1 || policy.MaxAttempts > 1000 {
		return Definition[P]{}, fmt.Errorf("job: max attempts must be between 1 and 1000")
	}
	if policy.Timeout == 0 {
		policy.Timeout = 30 * time.Second
	}
	if policy.Timeout < time.Millisecond || policy.Timeout > 24*time.Hour {
		return Definition[P]{}, fmt.Errorf("job: timeout must be between 1ms and 24h")
	}
	if len(policy.Backoff) == 0 {
		policy.Backoff = append([]time.Duration(nil), defaultBackoff...)
	} else {
		policy.Backoff = append([]time.Duration(nil), policy.Backoff...)
	}
	if len(policy.Backoff) > 1000 {
		return Definition[P]{}, fmt.Errorf("job: backoff supports at most 1000 entries")
	}
	for _, delay := range policy.Backoff {
		if delay < 0 || delay > 30*24*time.Hour {
			return Definition[P]{}, fmt.Errorf("job: backoff must be between zero and 30 days")
		}
		if delay > 0 && delay < time.Millisecond {
			return Definition[P]{}, fmt.Errorf("job: non-zero backoff must be at least 1ms")
		}
	}
	return Definition[P]{name: name, policy: policy}, nil
}

// MustDefine panics when a package-level definition is invalid.
func MustDefine[P any](name string, policy Policy) Definition[P] {
	definition, err := Define[P](name, policy)
	if err != nil {
		panic(err)
	}
	return definition
}

// Name returns the stable wire name.
func (definition Definition[P]) Name() string { return definition.name }

// Policy returns a defensive copy of the default policy.
func (definition Definition[P]) Policy() Policy {
	policy := definition.policy
	policy.Backoff = append([]time.Duration(nil), policy.Backoff...)
	return policy
}

// Handler processes a decoded typed payload.
type Handler[P any] func(context.Context, P) error

func decodePayload[P any](payload []byte) (P, error) {
	var value P
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return value, err
	}
	return value, nil
}

func validateQueue(queue string) error {
	if len(queue) == 0 || len(queue) > MaxQueueBytes || !queuePattern.MatchString(queue) {
		return fmt.Errorf("job: invalid queue %q", queue)
	}
	return nil
}
