package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExpiredReadPreservesConcurrentReplacement(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	if err := store.Put(ctx, "key", []byte("expired"), time.Second); err != nil {
		t.Fatal(err)
	}

	readExpired := make(chan struct{})
	resumeRead := make(chan struct{})
	store.now = func() time.Time {
		close(readExpired)
		<-resumeRead
		return now.Add(time.Second)
	}
	result := make(chan error, 1)
	go func() {
		_, err := store.Get(ctx, "key")
		result <- err
	}()
	<-readExpired
	if err := store.Put(ctx, "key", []byte("replacement"), 0); err != nil {
		t.Fatal(err)
	}
	close(resumeRead)
	if err := <-result; !errors.Is(err, ErrMiss) {
		t.Fatalf("expired read = %v, want cache miss", err)
	}
	value, err := store.Get(ctx, "key")
	if err != nil || string(value) != "replacement" {
		t.Fatalf("replacement = %q, %v; want replacement, nil", value, err)
	}
}

func TestMemoryExpiresAtDeadlineAndPrunes(t *testing.T) {
	store := NewMemory()
	ctx := context.Background()
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	for _, key := range []string{"read", "prune"} {
		if err := store.Put(ctx, key, []byte("value"), time.Second); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(time.Second)
	if _, err := store.Get(ctx, "read"); !errors.Is(err, ErrMiss) {
		t.Fatalf("read at expiration = %v, want cache miss", err)
	}
	if removed := store.Prune(); removed != 1 {
		t.Fatalf("pruned %d entries, want 1", removed)
	}
}
