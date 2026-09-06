package job

import (
	"errors"
	"fmt"
	"time"
)

type permanentError struct{ err error }

func (err permanentError) Error() string { return err.err.Error() }
func (err permanentError) Unwrap() error { return err.err }

// Permanent marks an error as terminal without further attempts.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

type retryAfterError struct {
	err   error
	delay time.Duration
}

// panicError intentionally retains neither the recovered value nor a stack.
// Handler failures are durable and administrator-visible, so persisting either
// could disclose credentials or implementation details.
type panicError struct{}

func (panicError) Error() string { return "handler panicked" }

func (err retryAfterError) Error() string { return err.err.Error() }
func (err retryAfterError) Unwrap() error { return err.err }

// RetryAfter overrides the snapshotted backoff for one failure.
func RetryAfter(err error, delay time.Duration) error {
	if err == nil {
		return nil
	}
	if delay < 0 || delay > 30*24*time.Hour {
		return permanentError{err: fmt.Errorf("invalid retry-after %s: %w", delay, err)}
	}
	if delay > 0 && delay < time.Millisecond {
		return permanentError{err: fmt.Errorf("invalid retry-after %s: %w", delay, err)}
	}
	return retryAfterError{err: err, delay: delay}
}

func classifyError(err error) (kind string, message string, delay *time.Duration, permanent bool) {
	var panicked panicError
	if errors.As(err, &panicked) {
		return "panic", "handler panicked", nil, false
	}
	var terminal permanentError
	if errors.As(err, &terminal) {
		return "permanent", "handler reported a permanent failure", nil, true
	}
	var retry retryAfterError
	if errors.As(err, &retry) {
		value := retry.delay
		return "error", "handler requested a retry", &value, false
	}
	return "error", "handler returned an error", nil, false
}
