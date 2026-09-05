package session_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/session"
)

type committedResponse struct{ *httptest.ResponseRecorder }

func (*committedResponse) Written() bool { return true }

type failingRotateStore struct {
	*session.MemoryStore
	err error
}

func (store *failingRotateStore) Rotate(context.Context, string, string, []byte, time.Time) error {
	return store.err
}

func TestManagerPersistsValuesAndExpiresFlash(t *testing.T) {
	manager, err := session.NewManager(session.NewMemoryStore(), []byte(strings.Repeat("s", 32)), session.Cookie{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := manager.Load(ctx, httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Put("user_id", int64(42)); err != nil {
		t.Fatal(err)
	}
	if err := first.Flash("notice", "welcome"); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	if err := manager.Save(ctx, response, first); err != nil {
		t.Fatal(err)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("unexpected cookie: %+v", cookies)
	}

	secondRequest := httptest.NewRequest("GET", "/", nil)
	secondRequest.AddCookie(cookies[0])
	second, err := manager.Load(ctx, secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	var userID int64
	if ok, err := second.Get("user_id", &userID); err != nil || !ok || userID != 42 {
		t.Fatalf("unexpected persisted value %d, %t, %v", userID, ok, err)
	}
	var notice string
	if ok, err := second.Get("notice", &notice); err != nil || !ok || notice != "welcome" {
		t.Fatalf("unexpected flash value %q, %t, %v", notice, ok, err)
	}
	response = httptest.NewRecorder()
	if err := manager.Save(ctx, response, second); err != nil {
		t.Fatal(err)
	}

	thirdRequest := httptest.NewRequest("GET", "/", nil)
	thirdRequest.AddCookie(response.Result().Cookies()[0])
	third, err := manager.Load(ctx, thirdRequest)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := third.Get("notice", &notice); err != nil || ok {
		t.Fatalf("flash survived more than one request: %t, %v", ok, err)
	}
}

func TestManagerRejectsTamperedCookie(t *testing.T) {
	manager, err := session.NewManager(session.NewMemoryStore(), []byte(strings.Repeat("s", 32)), session.Cookie{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, _ := manager.Load(ctx, httptest.NewRequest("GET", "/", nil))
	response := httptest.NewRecorder()
	if err := manager.Save(ctx, response, first); err != nil {
		t.Fatal(err)
	}
	cookie := response.Result().Cookies()[0]
	cookie.Value += "tampered"
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(cookie)
	second, err := manager.Load(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID() == first.ID() {
		t.Fatal("tampered cookie reused the existing session")
	}
}

func TestRotatedSessionCannotBeRecreatedByStaleRequest(t *testing.T) {
	store := session.NewMemoryStore()
	manager, err := session.NewManager(store, []byte(strings.Repeat("s", 32)), session.Cookie{UnsafeAllowHTTP: true}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	original, err := manager.Load(ctx, httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := original.Put("user_id", int64(42)); err != nil {
		t.Fatal(err)
	}
	initialResponse := httptest.NewRecorder()
	if err := manager.Save(ctx, initialResponse, original); err != nil {
		t.Fatal(err)
	}
	oldID := original.ID()
	cookie := initialResponse.Result().Cookies()[0]

	load := func() *session.Session {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.AddCookie(cookie)
		current, loadErr := manager.Load(ctx, request)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		return current
	}
	rotating := load()
	stale := load()
	if err := manager.Regenerate(ctx, rotating); err != nil {
		t.Fatal(err)
	}
	rotatedResponse := httptest.NewRecorder()
	if err := manager.Save(ctx, rotatedResponse, rotating); err != nil {
		t.Fatal(err)
	}
	if rotating.ID() == oldID {
		t.Fatal("session ID was not rotated")
	}
	if _, err := store.Get(ctx, oldID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("old session remains after rotation: %v", err)
	}

	staleResponse := httptest.NewRecorder()
	if err := manager.Save(ctx, staleResponse, stale); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("stale save error = %v, want ErrNotFound", err)
	}
	if len(staleResponse.Result().Cookies()) != 0 {
		t.Fatal("stale save wrote a session cookie")
	}
	if _, err := store.Get(ctx, oldID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("stale request recreated rotated session: %v", err)
	}
}

func TestFailedRotationPreservesPersistedSession(t *testing.T) {
	sentinel := errors.New("rotation unavailable")
	memory := session.NewMemoryStore()
	store := &failingRotateStore{MemoryStore: memory, err: sentinel}
	manager, err := session.NewManager(store, []byte(strings.Repeat("s", 32)), session.Cookie{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	current, err := manager.Load(ctx, httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := current.Put("user_id", int64(42)); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	if err := manager.Save(ctx, response, current); err != nil {
		t.Fatal(err)
	}
	oldID := current.ID()
	if err := manager.Regenerate(ctx, current); err != nil {
		t.Fatal(err)
	}
	failedResponse := httptest.NewRecorder()
	if err := manager.Save(ctx, failedResponse, current); !errors.Is(err, sentinel) {
		t.Fatalf("save error = %v, want rotation failure", err)
	}
	if len(failedResponse.Result().Cookies()) != 0 {
		t.Fatal("failed rotation wrote a replacement cookie")
	}
	if _, err := memory.Get(ctx, oldID); err != nil {
		t.Fatalf("failed rotation deleted persisted session: %v", err)
	}
}

func TestManagerRequiresStrongSecret(t *testing.T) {
	if _, err := session.NewManager(session.NewMemoryStore(), []byte("short"), session.Cookie{}, time.Hour); err == nil {
		t.Fatal("expected short secret to be rejected")
	}
}

func TestDestroyInvalidatesSessionObjectAndPreventsResurrection(t *testing.T) {
	store := session.NewMemoryStore()
	manager, err := session.NewManager(store, []byte(strings.Repeat("s", 32)), session.Cookie{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	current, err := manager.Load(ctx, httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	oldID := current.ID()
	if err := current.Put("user_id", 42); err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(ctx, httptest.NewRecorder(), current); err != nil {
		t.Fatal(err)
	}
	if err := manager.Regenerate(ctx, current); err != nil {
		t.Fatal(err)
	}
	if err := manager.Destroy(ctx, httptest.NewRecorder(), current); err != nil {
		t.Fatal(err)
	}
	if current.ID() != "" {
		t.Fatalf("destroyed session retained ID %q", current.ID())
	}
	if err := manager.Save(ctx, httptest.NewRecorder(), current); err == nil {
		t.Fatal("destroyed session was saved again")
	}
	if _, err := store.Get(ctx, oldID); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("destroyed session %q was resurrected: %v", oldID, err)
	}
}

func TestSaveRejectsCommittedResponseBeforeWritingStore(t *testing.T) {
	store := session.NewMemoryStore()
	manager, err := session.NewManager(store, []byte(strings.Repeat("s", 32)), session.Cookie{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	current, err := manager.Load(ctx, httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	response := &committedResponse{ResponseRecorder: httptest.NewRecorder()}
	if err := manager.Save(ctx, response, current); err == nil || !strings.Contains(err.Error(), "response header") {
		t.Fatalf("expected committed-response error, got %v", err)
	}
	if _, err := store.Get(ctx, current.ID()); !errors.Is(err, session.ErrNotFound) {
		t.Fatalf("session was stored despite committed response: %v", err)
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("session cookie was written after response commitment")
	}
}

func TestDestroyRejectsCommittedResponseBeforeDeletingStore(t *testing.T) {
	store := session.NewMemoryStore()
	manager, err := session.NewManager(store, []byte(strings.Repeat("s", 32)), session.Cookie{}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	current, err := manager.Load(ctx, httptest.NewRequest("GET", "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Save(ctx, httptest.NewRecorder(), current); err != nil {
		t.Fatal(err)
	}
	id := current.ID()
	response := &committedResponse{ResponseRecorder: httptest.NewRecorder()}
	if err := manager.Destroy(ctx, response, current); err == nil || !strings.Contains(err.Error(), "response header") {
		t.Fatalf("expected committed-response error, got %v", err)
	}
	if current.ID() != id {
		t.Fatal("failed destruction changed the session ID")
	}
	if _, err := store.Get(ctx, id); err != nil {
		t.Fatalf("session was removed despite committed response: %v", err)
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("session cookie was written after response commitment")
	}
}
