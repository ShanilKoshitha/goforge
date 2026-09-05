package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/cache"
)

func TestMemoryCopiesValues(t *testing.T) {
	store := cache.NewMemory()
	ctx := context.Background()
	value := []byte("first")
	if err := store.Put(ctx, "key", value, 0); err != nil {
		t.Fatal(err)
	}
	value[0] = 'x'
	got, err := store.Get(ctx, "key")
	if err != nil || string(got) != "first" {
		t.Fatalf("got %q, %v", got, err)
	}
	got[0] = 'x'
	again, _ := store.Get(ctx, "key")
	if string(again) != "first" {
		t.Fatal("Get exposed the cache's internal byte slice")
	}
}

func TestRememberJSONLoadsOnce(t *testing.T) {
	type value struct{ Name string }
	store := cache.NewMemory()
	loads := 0
	load := func(context.Context) (value, error) {
		loads++
		return value{Name: "Ada"}, nil
	}
	for range 2 {
		got, err := cache.RememberJSON(context.Background(), store, "user:1", time.Minute, load)
		if err != nil || got.Name != "Ada" {
			t.Fatalf("got %+v, %v", got, err)
		}
	}
	if loads != 1 {
		t.Fatalf("expected one load, got %d", loads)
	}
}
