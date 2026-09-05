package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/httpx"
)

func TestLimiterDeniesAtLimitExpiresAndResets(t *testing.T) {
	store := NewMemoryStore(10)
	limiter, err := New(store, Policy{Limit: 2, Window: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	limiter.now = func() time.Time { return now }
	for range 2 {
		decision, err := limiter.Take(context.Background(), "account")
		if err != nil || !decision.Allowed {
			t.Fatalf("allowed attempt: decision=%+v err=%v", decision, err)
		}
	}
	decision, err := limiter.Take(context.Background(), "account")
	if err != nil || decision.Allowed || decision.RetryAfter != time.Minute {
		t.Fatalf("limited attempt: decision=%+v err=%v", decision, err)
	}
	if err := limiter.Reset(context.Background(), "account"); err != nil {
		t.Fatal(err)
	}
	decision, err = limiter.Take(context.Background(), "account")
	if err != nil || !decision.Allowed {
		t.Fatalf("reset attempt: decision=%+v err=%v", decision, err)
	}
	now = now.Add(2 * time.Minute)
	decision, err = limiter.Take(context.Background(), "account")
	if err != nil || !decision.Allowed {
		t.Fatalf("expired attempt: decision=%+v err=%v", decision, err)
	}
}

func TestMemoryStoreRemainsBoundedAndFailsClosed(t *testing.T) {
	store := NewMemoryStore(2)
	policy := Policy{Limit: 2, Window: time.Minute}
	now := time.Unix(100, 0)
	for _, key := range []string{"first", "second"} {
		decision, err := store.Take(context.Background(), key, policy, now)
		if err != nil || !decision.Allowed {
			t.Fatalf("seed %q: decision=%+v err=%v", key, decision, err)
		}
	}
	decision, err := store.Take(context.Background(), "third", policy, now)
	if err != nil || decision.Allowed || len(store.entries) != 2 {
		t.Fatalf("capacity decision=%+v entries=%d err=%v", decision, len(store.entries), err)
	}
	decision, err = store.Take(context.Background(), "third", policy, now.Add(time.Minute))
	if err != nil || !decision.Allowed || len(store.entries) > 2 {
		t.Fatalf("expired capacity decision=%+v entries=%d err=%v", decision, len(store.entries), err)
	}
}

func TestMemoryStoreAtomicallyLimitsConcurrentAttempts(t *testing.T) {
	store := NewMemoryStore(10)
	policy := Policy{Limit: 5, Window: time.Minute}
	now := time.Unix(100, 0)
	var allowed atomic.Int64
	var group sync.WaitGroup
	for range 50 {
		group.Add(1)
		go func() {
			defer group.Done()
			decision, err := store.Take(context.Background(), "shared", policy, now)
			if err != nil {
				t.Errorf("Take: %v", err)
				return
			}
			if decision.Allowed {
				allowed.Add(1)
			}
		}()
	}
	group.Wait()
	if allowed.Load() != int64(policy.Limit) {
		t.Fatalf("allowed = %d, want %d", allowed.Load(), policy.Limit)
	}
}

func TestMiddlewareReturnsGeneric429AndRetryAfter(t *testing.T) {
	limiter, err := New(NewMemoryStore(10), Policy{Limit: 1, Window: 1500 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	limiter.now = func() time.Time { return time.Unix(100, 0) }
	router := httpx.NewRouter()
	router.Use(Middleware(limiter, func(*httpx.Context) string { return Key("login", "peer") }))
	router.POST("/login", func(ctx *httpx.Context) error { return ctx.NoContent(http.StatusNoContent) })
	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/login", nil))
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/login", nil))
	if first.Code != http.StatusNoContent || second.Code != http.StatusTooManyRequests {
		t.Fatalf("statuses = %d, %d", first.Code, second.Code)
	}
	if second.Header().Get("Retry-After") != strconv.Itoa(2) {
		t.Fatalf("Retry-After = %q, want 2", second.Header().Get("Retry-After"))
	}
	if body := second.Body.String(); body == "" || containsAny(body, "peer", "login", "1") {
		t.Fatalf("429 body disclosed limiter state: %q", body)
	}
}

func TestKeyAndRemoteAddressAvoidRawIdentifiers(t *testing.T) {
	key := Key("login", "Ada@Example.com", "192.0.2.1")
	if len(key) != sha256HexLength || containsAny(key, "Ada", "Example", "192") {
		t.Fatalf("unsafe key %q", key)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "[2001:db8::1]:4321"
	if got := RemoteAddress(request); got != "2001:db8::1" {
		t.Fatalf("remote address = %q", got)
	}
}

const sha256HexLength = 64

func containsAny(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}
