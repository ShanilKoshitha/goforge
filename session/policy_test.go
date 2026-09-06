package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type sessionTestClock struct{ current time.Time }

func (clock *sessionTestClock) now() time.Time { return clock.current }

func (clock *sessionTestClock) advance(duration time.Duration) {
	clock.current = clock.current.Add(duration)
}

func policyManager(t *testing.T, clock *sessionTestClock, store *MemoryStore, policy Policy) *Manager {
	t.Helper()
	store.now = clock.now
	manager, err := NewManagerWithPolicy(
		store,
		[]byte(strings.Repeat("s", 32)),
		Cookie{UnsafeAllowHTTP: true},
		policy,
	)
	if err != nil {
		t.Fatal(err)
	}
	manager.now = clock.now
	return manager
}

func TestPolicyValidation(t *testing.T) {
	store := NewMemoryStore()
	secret := []byte(strings.Repeat("s", 32))
	tests := []struct {
		name   string
		policy Policy
	}{
		{name: "missing idle lifetime", policy: Policy{}},
		{name: "negative idle lifetime", policy: Policy{IdleLifetime: -time.Second}},
		{name: "negative absolute lifetime", policy: Policy{IdleLifetime: time.Hour, AbsoluteLifetime: -time.Second}},
		{name: "absolute shorter than idle", policy: Policy{IdleLifetime: 2 * time.Hour, AbsoluteLifetime: time.Hour}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewManagerWithPolicy(store, secret, Cookie{}, test.policy); err == nil {
				t.Fatalf("NewManagerWithPolicy(%+v) succeeded", test.policy)
			}
		})
	}
	if _, err := NewManagerWithPolicy(store, secret, Cookie{}, Policy{
		IdleLifetime: time.Hour, AbsoluteLifetime: time.Hour,
	}); err != nil {
		t.Fatalf("equal idle and absolute lifetimes should be valid: %v", err)
	}
}

func TestCookieMaxAgeNeverRoundsPastDeadline(t *testing.T) {
	if got := cookieMaxAge(1500 * time.Millisecond); got != 1 {
		t.Fatalf("1.5 second max-age = %d, want 1", got)
	}
	if got := cookieMaxAge(500 * time.Millisecond); got != -1 {
		t.Fatalf("sub-second max-age = %d, want early deletion", got)
	}
}

func TestPolicyCapsSlidingExpiryAndRotationAtDurableAbsoluteDeadline(t *testing.T) {
	ctx := context.Background()
	clock := &sessionTestClock{current: time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)}
	store := NewMemoryStore()
	manager := policyManager(t, clock, store, Policy{
		IdleLifetime: 2 * time.Hour, AbsoluteLifetime: 3 * time.Hour,
	})

	current, err := manager.Load(ctx, httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := current.Put("user_id", int64(42)); err != nil {
		t.Fatal(err)
	}
	firstResponse := httptest.NewRecorder()
	if err := manager.Save(ctx, firstResponse, current); err != nil {
		t.Fatal(err)
	}
	firstCookie := firstResponse.Result().Cookies()[0]
	if !firstCookie.Expires.Equal(clock.current.Add(2*time.Hour)) || firstCookie.MaxAge != 2*60*60 {
		t.Fatalf("initial cookie expiry = %v (max-age %d)", firstCookie.Expires, firstCookie.MaxAge)
	}
	stored := store.entries[current.ID()]
	var encoded payload
	if err := json.Unmarshal(stored.value, &encoded); err != nil {
		t.Fatal(err)
	}
	if !encoded.IssuedAt.Equal(clock.current) {
		t.Fatalf("stored issuance = %v, want %v", encoded.IssuedAt, clock.current)
	}

	clock.advance(90 * time.Minute)
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(firstCookie)
	rotated, err := manager.Load(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	oldID := rotated.ID()
	if err := manager.Regenerate(ctx, rotated); err != nil {
		t.Fatal(err)
	}
	rotatedResponse := httptest.NewRecorder()
	if err := manager.Save(ctx, rotatedResponse, rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.ID() == oldID {
		t.Fatal("rotation did not replace the session ID")
	}
	rotatedCookie := rotatedResponse.Result().Cookies()[0]
	absoluteDeadline := time.Date(2026, time.September, 5, 15, 0, 0, 0, time.UTC)
	if !rotatedCookie.Expires.Equal(absoluteDeadline) || rotatedCookie.MaxAge != 90*60 {
		t.Fatalf("rotated cookie expiry = %v (max-age %d), want absolute deadline", rotatedCookie.Expires, rotatedCookie.MaxAge)
	}
	if !store.entries[rotated.ID()].expiresAt.Equal(absoluteDeadline) {
		t.Fatalf("store expiry = %v, want %v", store.entries[rotated.ID()].expiresAt, absoluteDeadline)
	}
	if err := json.Unmarshal(store.entries[rotated.ID()].value, &encoded); err != nil {
		t.Fatal(err)
	}
	if !encoded.IssuedAt.Equal(time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("rotation changed durable issuance to %v", encoded.IssuedAt)
	}

	// Prove the payload deadline is authoritative even if a store adapter were
	// to retain the row beyond the expiry supplied by Manager.Save.
	entry := store.entries[rotated.ID()]
	entry.expiresAt = absoluteDeadline.Add(24 * time.Hour)
	store.entries[rotated.ID()] = entry
	clock.advance(90 * time.Minute)
	expiredRequest := httptest.NewRequest("GET", "/", nil)
	expiredRequest.AddCookie(rotatedCookie)
	fresh, err := manager.Load(ctx, expiredRequest)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ID() == rotated.ID() {
		t.Fatal("absolutely expired session was loaded")
	}
	if _, exists := store.entries[rotated.ID()]; exists {
		t.Fatal("absolutely expired session was not removed from the store")
	}
}

func TestSaveAtAbsoluteDeadlineDestroysPersistedSession(t *testing.T) {
	ctx := context.Background()
	clock := &sessionTestClock{current: time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)}
	store := NewMemoryStore()
	manager := policyManager(t, clock, store, Policy{
		IdleLifetime: time.Hour, AbsoluteLifetime: 2 * time.Hour,
	})
	current, err := manager.Load(ctx, httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	if err := manager.Save(ctx, response, current); err != nil {
		t.Fatal(err)
	}
	persistedID := current.ID()

	entry := store.entries[persistedID]
	entry.expiresAt = clock.current.Add(24 * time.Hour)
	store.entries[persistedID] = entry
	clock.advance(2 * time.Hour)
	expiredResponse := httptest.NewRecorder()
	if err := manager.Save(ctx, expiredResponse, current); err != nil {
		t.Fatalf("expiry should be a successful destruction, got %v", err)
	}
	if current.ID() != "" {
		t.Fatalf("expired session retained ID %q", current.ID())
	}
	if _, exists := store.entries[persistedID]; exists {
		t.Fatal("expired session remained persisted")
	}
	cookies := expiredResponse.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 || cookies[0].Value != "" {
		t.Fatalf("expiry cookie = %+v, want one deletion", cookies)
	}
}

func TestManagerLastCookieWriteReplacesItsCookieAndPreservesOthers(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	manager, err := NewManager(
		store,
		[]byte(strings.Repeat("s", 32)),
		Cookie{UnsafeAllowHTTP: true},
		time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err := manager.Load(ctx, httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	http.SetCookie(response, &http.Cookie{Name: "preference", Value: "dark", Path: "/"})
	if err := manager.Save(ctx, response, current); err != nil {
		t.Fatal(err)
	}
	firstSessionCookie := namedCookie(t, response.Header().Values("Set-Cookie"), manager.cookie.Name)

	if err := manager.Regenerate(ctx, current); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(ctx, response, current); err != nil {
		t.Fatal(err)
	}
	afterRotation := parsedCookies(t, response.Header().Values("Set-Cookie"))
	if len(afterRotation) != 2 {
		t.Fatalf("cookies after rotation = %+v, want one application and one session cookie", afterRotation)
	}
	rotatedCookie := namedCookie(t, response.Header().Values("Set-Cookie"), manager.cookie.Name)
	if rotatedCookie.Value == firstSessionCookie.Value {
		t.Fatal("later save retained the earlier session cookie")
	}
	if preference := namedCookie(t, response.Header().Values("Set-Cookie"), "preference"); preference.Value != "dark" {
		t.Fatalf("unrelated cookie = %+v", preference)
	}

	if err := manager.Destroy(ctx, response, current); err != nil {
		t.Fatal(err)
	}
	finalCookies := parsedCookies(t, response.Header().Values("Set-Cookie"))
	if len(finalCookies) != 2 {
		t.Fatalf("final cookies = %+v, want one application and one session cookie", finalCookies)
	}
	finalSessionCookie := namedCookie(t, response.Header().Values("Set-Cookie"), manager.cookie.Name)
	if finalSessionCookie.Value != "" || finalSessionCookie.MaxAge != -1 {
		t.Fatalf("final session cookie = %+v, want deletion", finalSessionCookie)
	}
	if preference := namedCookie(t, response.Header().Values("Set-Cookie"), "preference"); preference.Value != "dark" {
		t.Fatalf("destroy changed unrelated cookie = %+v", preference)
	}
}

func TestRotationSurvivesConcurrentInvalidation(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	manager, err := NewManager(
		store, []byte(strings.Repeat("s", 32)), Cookie{UnsafeAllowHTTP: true}, time.Hour,
	)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := manager.Load(ctx, httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := seed.Put("user_id", int64(42)); err != nil {
		t.Fatal(err)
	}
	seedResponse := httptest.NewRecorder()
	if err := manager.Save(ctx, seedResponse, seed); err != nil {
		t.Fatal(err)
	}
	cookie := seedResponse.Result().Cookies()[0]
	load := func() *Session {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.AddCookie(cookie)
		current, loadErr := manager.Load(ctx, request)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		return current
	}
	winner, stale := load(), load()
	if err := manager.RegenerateOrCreate(ctx, winner); err != nil {
		t.Fatal(err)
	}
	staleResponse := httptest.NewRecorder()
	if err := manager.Invalidate(ctx, staleResponse, stale); err != nil {
		t.Fatal(err)
	}
	if cookies := staleResponse.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("stale invalidation wrote a cookie: %+v", cookies)
	}
	winnerResponse := httptest.NewRecorder()
	if err := manager.Save(ctx, winnerResponse, winner); err != nil {
		t.Fatalf("winning rotation after invalidation: %v", err)
	}
	nextCookies := winnerResponse.Result().Cookies()
	if len(nextCookies) != 1 || nextCookies[0].Value == cookie.Value {
		t.Fatalf("winning rotation cookie = %+v", nextCookies)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(nextCookies[0])
	loaded, err := manager.Load(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	var userID int64
	if present, err := loaded.Get("user_id", &userID); err != nil || !present || userID != 42 {
		t.Fatalf("winning identity = %d, present=%t, err=%v", userID, present, err)
	}
}

func parsedCookies(t *testing.T, headers []string) []*http.Cookie {
	t.Helper()
	cookies := make([]*http.Cookie, 0, len(headers))
	for _, header := range headers {
		cookie, err := http.ParseSetCookie(header)
		if err != nil {
			t.Fatalf("parse Set-Cookie %q: %v", header, err)
		}
		cookies = append(cookies, cookie)
	}
	return cookies
}

func namedCookie(t *testing.T, headers []string, name string) *http.Cookie {
	t.Helper()
	var found *http.Cookie
	for _, cookie := range parsedCookies(t, headers) {
		if cookie.Name != name {
			continue
		}
		if found != nil {
			t.Fatalf("multiple %q cookies: %v", name, headers)
		}
		found = cookie
	}
	if found == nil {
		t.Fatalf("cookie %q missing from %v", name, headers)
	}
	return found
}

func TestAbsolutePolicyRejectsLegacyPayloadWithoutIssuance(t *testing.T) {
	ctx := context.Background()
	clock := &sessionTestClock{current: time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)}
	store := NewMemoryStore()
	manager := policyManager(t, clock, store, Policy{
		IdleLifetime: time.Hour, AbsoluteLifetime: 2 * time.Hour,
	})
	id, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(payload{Values: map[string]json.RawMessage{
		"user_id": json.RawMessage("42"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create(ctx, id, legacy, clock.current.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(&http.Cookie{Name: manager.cookie.Name, Value: manager.sign(id)})
	loaded, err := manager.Load(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID() == id {
		t.Fatal("absolute policy trusted a legacy session without issuance")
	}
	if _, exists := store.entries[id]; exists {
		t.Fatal("legacy session was not removed")
	}
}
