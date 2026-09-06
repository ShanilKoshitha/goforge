package postgres

// safeFailureMessage is deliberately an allowlist. The queue is commonly
// inspected from an operator console, so arbitrary handler text must never be
// persisted or returned from historical rows. Failure kind, job identity,
// attempts, and timestamps remain available as safe diagnostics.
func safeFailureMessage(kind, message string) string {
	switch kind {
	case "panic":
		return "handler panicked"
	case "permanent":
		return "handler reported a permanent failure"
	case "timeout":
		return "handler deadline exceeded"
	case "malformed_payload":
		return "job payload could not be decoded"
	case "lease_exhausted":
		return "lease expired after final attempt"
	case "error":
		if message == "handler requested a retry" {
			return message
		}
		return "handler returned an error"
	default:
		return "job attempt failed"
	}
}

func (store *Store) boundedSafeFailureMessage(kind, message string) string {
	message = safeFailureMessage(kind, message)
	if len(message) > store.maxError {
		return message[:store.maxError]
	}
	return message
}
