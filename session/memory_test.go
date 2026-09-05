package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExpiredGetCannotDeleteConcurrentRefresh(t *testing.T) {
	store := NewMemoryStore()
	store.entries["session"] = memoryEntry{value: []byte("expired"), expiresAt: time.Unix(1, 0)}
	nowEntered := make(chan struct{})
	allowExpiryCheck := make(chan struct{})
	store.now = func() time.Time {
		close(nowEntered)
		<-allowExpiryCheck
		return time.Unix(2, 0)
	}

	getDone := make(chan error, 1)
	go func() {
		_, err := store.Get(context.Background(), "session")
		getDone <- err
	}()
	<-nowEntered
	if store.mu.TryLock() {
		store.mu.Unlock()
		close(allowExpiryCheck)
		t.Fatal("Get released the store lock between reading and deleting an expired entry")
	}

	putDone := make(chan struct{})
	go func() {
		_ = store.Create(context.Background(), "session", []byte("fresh"), time.Unix(10, 0))
		close(putDone)
	}()
	close(allowExpiryCheck)
	if err := <-getDone; !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired Get returned %v", err)
	}
	<-putDone
	store.now = func() time.Time { return time.Unix(3, 0) }
	value, err := store.Get(context.Background(), "session")
	if err != nil || string(value) != "fresh" {
		t.Fatalf("concurrent refresh was lost: value=%q err=%v", value, err)
	}
}

func TestMemoryStoreEnforcesExplicitWriteSemantics(t *testing.T) {
	store := NewMemoryStore()
	store.now = func() time.Time { return time.Unix(1, 0) }
	ctx := context.Background()
	expiresAt := time.Unix(10, 0)
	if err := store.Create(ctx, "old", []byte("original"), expiresAt); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, "old", []byte("replacement"), expiresAt); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate Create error = %v, want ErrExists", err)
	}
	if err := store.Update(ctx, "missing", []byte("value"), expiresAt); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Update error = %v, want ErrNotFound", err)
	}
	if err := store.Create(ctx, "occupied", []byte("occupied"), expiresAt); err != nil {
		t.Fatal(err)
	}
	if err := store.Rotate(ctx, "old", "occupied", []byte("rotated"), expiresAt); !errors.Is(err, ErrExists) {
		t.Fatalf("conflicting Rotate error = %v, want ErrExists", err)
	}
	if value, err := store.Get(ctx, "old"); err != nil || string(value) != "original" {
		t.Fatalf("failed rotation changed old session: value=%q err=%v", value, err)
	}
	if err := store.Delete(ctx, "occupied"); err != nil {
		t.Fatal(err)
	}
	if err := store.Rotate(ctx, "old", "new", []byte("rotated"), expiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "old"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rotated old ID still exists: %v", err)
	}
	if value, err := store.Get(ctx, "new"); err != nil || string(value) != "rotated" {
		t.Fatalf("rotated session missing: value=%q err=%v", value, err)
	}
}
