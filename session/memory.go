package session

import (
	"context"
	"sync"
	"time"
)

type memoryEntry struct {
	value     []byte
	expiresAt time.Time
}

// MemoryStore is an in-process store intended for development and tests.
// Entries are removed on expired reads; restarting the process loses sessions.
type MemoryStore struct {
	mu      sync.Mutex
	entries map[string]memoryEntry
	now     func() time.Time
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{entries: make(map[string]memoryEntry), now: time.Now}
}

func (store *MemoryStore) Get(_ context.Context, id string) ([]byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	entry, ok := store.entries[id]
	if !ok {
		return nil, ErrNotFound
	}
	if !store.now().Before(entry.expiresAt) {
		delete(store.entries, id)
		return nil, ErrNotFound
	}
	return append([]byte(nil), entry.value...), nil
}

func (store *MemoryStore) Create(_ context.Context, id string, value []byte, expiresAt time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if entry, exists := store.entries[id]; exists {
		if store.now().Before(entry.expiresAt) {
			return ErrExists
		}
		delete(store.entries, id)
	}
	store.entries[id] = memoryEntry{value: append([]byte(nil), value...), expiresAt: expiresAt}
	return nil
}

func (store *MemoryStore) Update(_ context.Context, id string, value []byte, expiresAt time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	entry, exists := store.entries[id]
	if !exists || !store.now().Before(entry.expiresAt) {
		if exists {
			delete(store.entries, id)
		}
		return ErrNotFound
	}
	store.entries[id] = memoryEntry{value: append([]byte(nil), value...), expiresAt: expiresAt}
	return nil
}

func (store *MemoryStore) Rotate(_ context.Context, oldID, newID string, value []byte, expiresAt time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	oldEntry, exists := store.entries[oldID]
	if !exists || !store.now().Before(oldEntry.expiresAt) {
		if exists {
			delete(store.entries, oldID)
		}
		return ErrNotFound
	}
	if newEntry, exists := store.entries[newID]; exists {
		if store.now().Before(newEntry.expiresAt) {
			return ErrExists
		}
		delete(store.entries, newID)
	}
	store.entries[newID] = memoryEntry{value: append([]byte(nil), value...), expiresAt: expiresAt}
	delete(store.entries, oldID)
	return nil
}

func (store *MemoryStore) Delete(_ context.Context, id string) error {
	store.mu.Lock()
	delete(store.entries, id)
	store.mu.Unlock()
	return nil
}
