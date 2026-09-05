package ratelimit

import (
	"context"
	"sync"
	"time"
)

const defaultCapacity = 10_000

type memoryEntry struct {
	count   int
	resetAt time.Time
}

// MemoryStore is a bounded, concurrency-safe fixed-window store. When full it
// removes expired entries; if capacity is still exhausted, unseen keys are
// denied until the earliest active window resets instead of evicting a live
// key and allowing a key-spray bypass.
type MemoryStore struct {
	mu       sync.Mutex
	entries  map[string]memoryEntry
	capacity int
}

func NewMemoryStore(capacity int) *MemoryStore {
	if capacity <= 0 {
		capacity = defaultCapacity
	}
	return &MemoryStore{entries: make(map[string]memoryEntry), capacity: capacity}
}

var _ Store = (*MemoryStore)(nil)

func (store *MemoryStore) Take(ctx context.Context, key string, policy Policy, now time.Time) (Decision, error) {
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()

	if entry, exists := store.entries[key]; exists {
		if !now.Before(entry.resetAt) {
			entry = memoryEntry{count: 1, resetAt: now.Add(policy.Window)}
			store.entries[key] = entry
			return Decision{Allowed: true}, nil
		}
		if entry.count >= policy.Limit {
			return Decision{RetryAfter: entry.resetAt.Sub(now)}, nil
		}
		entry.count++
		store.entries[key] = entry
		return Decision{Allowed: true}, nil
	}

	if len(store.entries) >= store.capacity {
		earliest := time.Time{}
		for candidate, entry := range store.entries {
			if !now.Before(entry.resetAt) {
				delete(store.entries, candidate)
				continue
			}
			if earliest.IsZero() || entry.resetAt.Before(earliest) {
				earliest = entry.resetAt
			}
		}
		if len(store.entries) >= store.capacity {
			retry := policy.Window
			if !earliest.IsZero() {
				retry = earliest.Sub(now)
			}
			return Decision{RetryAfter: retry}, nil
		}
	}

	store.entries[key] = memoryEntry{count: 1, resetAt: now.Add(policy.Window)}
	return Decision{Allowed: true}, nil
}

func (store *MemoryStore) Reset(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	delete(store.entries, key)
	store.mu.Unlock()
	return nil
}
