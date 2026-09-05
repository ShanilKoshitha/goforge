// Package cache defines a deliberately small cache contract and an in-process
// implementation. Distributed stores can implement the same four methods.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrMiss = errors.New("cache miss")

type Store interface {
	Get(context.Context, string) ([]byte, error)
	Put(context.Context, string, []byte, time.Duration) error
	Forget(context.Context, string) error
	Flush(context.Context) error
}

type entry struct {
	value     []byte
	expiresAt time.Time
}

type Memory struct {
	mu      sync.RWMutex
	entries map[string]entry
	now     func() time.Time
}

func NewMemory() *Memory {
	return &Memory{entries: make(map[string]entry), now: time.Now}
}

func (memory *Memory) Get(_ context.Context, key string) ([]byte, error) {
	memory.mu.RLock()
	entry, ok := memory.entries[key]
	memory.mu.RUnlock()
	if !ok {
		return nil, ErrMiss
	}
	if !entry.expiresAt.IsZero() && !memory.now().Before(entry.expiresAt) {
		memory.mu.Lock()
		// A writer may have replaced the expired entry since the read lock was released.
		if current, exists := memory.entries[key]; exists && !current.expiresAt.IsZero() && !memory.now().Before(current.expiresAt) {
			delete(memory.entries, key)
		}
		memory.mu.Unlock()
		return nil, ErrMiss
	}
	return append([]byte(nil), entry.value...), nil
}

func (memory *Memory) Put(_ context.Context, key string, value []byte, lifetime time.Duration) error {
	item := entry{value: append([]byte(nil), value...)}
	if lifetime > 0 {
		item.expiresAt = memory.now().Add(lifetime)
	}
	memory.mu.Lock()
	memory.entries[key] = item
	memory.mu.Unlock()
	return nil
}

func (memory *Memory) Forget(_ context.Context, key string) error {
	memory.mu.Lock()
	delete(memory.entries, key)
	memory.mu.Unlock()
	return nil
}

func (memory *Memory) Flush(_ context.Context) error {
	memory.mu.Lock()
	memory.entries = make(map[string]entry)
	memory.mu.Unlock()
	return nil
}

// Prune removes expired entries and returns the number removed.
func (memory *Memory) Prune() int {
	now := memory.now()
	removed := 0
	memory.mu.Lock()
	for key, entry := range memory.entries {
		if !entry.expiresAt.IsZero() && !now.Before(entry.expiresAt) {
			delete(memory.entries, key)
			removed++
		}
	}
	memory.mu.Unlock()
	return removed
}

func GetJSON[T any](ctx context.Context, store Store, key string) (T, error) {
	var value T
	encoded, err := store.Get(ctx, key)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal(encoded, &value); err != nil {
		return value, fmt.Errorf("decode cached %q: %w", key, err)
	}
	return value, nil
}

func PutJSON[T any](ctx context.Context, store Store, key string, value T, lifetime time.Duration) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode cached %q: %w", key, err)
	}
	return store.Put(ctx, key, encoded, lifetime)
}

func RememberJSON[T any](ctx context.Context, store Store, key string, lifetime time.Duration, load func(context.Context) (T, error)) (T, error) {
	value, err := GetJSON[T](ctx, store, key)
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, ErrMiss) {
		var zero T
		return zero, err
	}
	value, err = load(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	if err := PutJSON(ctx, store, key, value, lifetime); err != nil {
		var zero T
		return zero, err
	}
	return value, nil
}
