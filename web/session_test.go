package web_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/httpx"
	"github.com/ShanilKoshitha/goforge/session"
	"github.com/ShanilKoshitha/goforge/web"
)

type observedStore struct {
	*session.MemoryStore
	gets    int
	creates int
	updates int
	rotates int
	deletes int
	create  error
	update  error
	rotate  error
}

func newObservedStore() *observedStore {
	return &observedStore{MemoryStore: session.NewMemoryStore()}
}

func (store *observedStore) Get(ctx context.Context, id string) ([]byte, error) {
	store.gets++
	return store.MemoryStore.Get(ctx, id)
}

func (store *observedStore) Create(ctx context.Context, id string, value []byte, expiresAt time.Time) error {
	store.creates++
	if store.create != nil {
		return store.create
	}
	return store.MemoryStore.Create(ctx, id, value, expiresAt)
}

func (store *observedStore) Update(ctx context.Context, id string, value []byte, expiresAt time.Time) error {
	store.updates++
	if store.update != nil {
		return store.update
	}
	return store.MemoryStore.Update(ctx, id, value, expiresAt)
}

func (store *observedStore) Rotate(ctx context.Context, oldID, newID string, value []byte, expiresAt time.Time) error {
	store.rotates++
	if store.rotate != nil {
		return store.rotate
	}
	return store.MemoryStore.Rotate(ctx, oldID, newID, value, expiresAt)
}

func (store *observedStore) Delete(ctx context.Context, id string) error {
	store.deletes++
	return store.MemoryStore.Delete(ctx, id)
}

func managerFor(t *testing.T, store session.Store) *session.Manager {
	t.Helper()
	manager, err := session.NewManager(store, []byte(strings.Repeat("s", 32)), session.Cookie{UnsafeAllowHTTP: true}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func TestUntouchedAnonymousSessionIsNotPersisted(t *testing.T) {
	store := newObservedStore()
	router := httpx.NewRouter()
	router.Use(web.Sessions(managerFor(t, store)))
	router.GET("/", func(ctx *httpx.Context) error {
		return ctx.NoContent(http.StatusNoContent)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.Code)
	}
	if store.creates != 0 || store.updates != 0 || store.rotates != 0 {
		t.Fatalf("untouched session was persisted: create=%d update=%d rotate=%d", store.creates, store.updates, store.rotates)
	}
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("untouched session wrote %d cookie(s)", len(cookies))
	}
}

func TestFlashPersistsBeforeRedirectAndExpiresAfterDisplay(t *testing.T) {
	store := newObservedStore()
	manager := managerFor(t, store)
	router := httpx.NewRouter()
	router.Use(web.Sessions(manager))
	router.POST("/submit", func(ctx *httpx.Context) error {
		current, ok := web.Session(ctx)
		if !ok {
			t.Fatal("session middleware did not expose a session")
		}
		if err := current.Flash("notice", "saved"); err != nil {
			return err
		}
		return web.Redirect(ctx, "/result")
	})
	router.GET("/result", func(ctx *httpx.Context) error {
		current, ok := web.Session(ctx)
		if !ok {
			t.Fatal("session middleware did not expose a session")
		}
		var notice string
		present, err := current.Get("notice", &notice)
		if err != nil {
			return err
		}
		if !present {
			notice = "missing"
		}
		_, err = io.WriteString(ctx.Response, notice)
		return err
	})

	submit := httptest.NewRecorder()
	router.ServeHTTP(submit, httptest.NewRequest(http.MethodPost, "/submit", nil))
	if submit.Code != http.StatusSeeOther || submit.Header().Get("Location") != "/result" {
		t.Fatalf("redirect = %d %q", submit.Code, submit.Header().Get("Location"))
	}
	if got := submit.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("session response Cache-Control = %q, want no-store", got)
	}
	if store.creates != 1 {
		t.Fatalf("session creates = %d, want 1", store.creates)
	}
	cookie := submit.Result().Cookies()[0]

	visit := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/result", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if next := response.Result().Cookies(); len(next) == 1 {
			cookie = next[0]
		}
		return response
	}
	first := visit()
	if got := first.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("authenticated response Cache-Control = %q, want no-store", got)
	}
	if first.Body.String() != "saved" {
		t.Fatalf("first flash response = %q", first.Body.String())
	}
	second := visit()
	if second.Body.String() != "missing" {
		t.Fatalf("flash survived display request: %q", second.Body.String())
	}
	if store.updates != 2 {
		t.Fatalf("session updates = %d, want 2", store.updates)
	}
}

func TestPersistenceFailureDiscardsBufferedResponse(t *testing.T) {
	sentinel := errors.New("store unavailable")
	store := newObservedStore()
	store.create = sentinel
	router := httpx.NewRouter()
	router.Use(web.Sessions(managerFor(t, store)))
	router.POST("/submit", func(ctx *httpx.Context) error {
		current, ok := web.Session(ctx)
		if !ok {
			t.Fatal("session middleware did not expose a session")
		}
		if err := current.Put("value", "secret"); err != nil {
			return err
		}
		ctx.Response.Header().Set("X-Buffered", "discard me")
		if _, err := io.WriteString(ctx.Response, "redirect body"); err != nil {
			return err
		}
		return web.Redirect(ctx, "/committed-too-early")
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/submit", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	if response.Header().Get("Location") != "" || response.Header().Get("X-Buffered") != "" {
		t.Fatalf("buffered headers leaked: %v", response.Header())
	}
	if strings.Contains(response.Body.String(), "redirect body") {
		t.Fatalf("buffered body leaked: %q", response.Body.String())
	}
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("failed persistence wrote %d cookie(s)", len(cookies))
	}
}

func TestSessionLoadsOnceAndRotatesBeforeCommit(t *testing.T) {
	store := newObservedStore()
	manager := managerFor(t, store)
	current, err := manager.Load(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	seed := httptest.NewRecorder()
	if err := manager.Save(context.Background(), seed, current); err != nil {
		t.Fatal(err)
	}
	cookie := seed.Result().Cookies()[0]
	store.gets, store.creates = 0, 0

	router := httpx.NewRouter()
	router.Use(web.Sessions(manager))
	router.POST("/rotate", func(ctx *httpx.Context) error {
		first, ok := web.Session(ctx)
		if !ok {
			t.Fatal("session middleware did not expose a session")
		}
		second, _ := web.Session(ctx)
		if first != second {
			t.Fatal("multiple session instances were exposed")
		}
		if err := web.Regenerate(ctx); err != nil {
			return err
		}
		return ctx.NoContent(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/rotate", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.Code)
	}
	if store.gets != 1 || store.rotates != 1 || store.updates != 0 || store.creates != 0 {
		t.Fatalf("operations: get=%d create=%d update=%d rotate=%d", store.gets, store.creates, store.updates, store.rotates)
	}
	if next := response.Result().Cookies(); len(next) != 1 || next[0].Value == cookie.Value {
		t.Fatal("rotation did not commit a replacement cookie")
	}
}

func TestDestroyCommitsExpiredCookieWithoutResaving(t *testing.T) {
	store := newObservedStore()
	manager := managerFor(t, store)
	current, err := manager.Load(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	seed := httptest.NewRecorder()
	if err := manager.Save(context.Background(), seed, current); err != nil {
		t.Fatal(err)
	}
	cookie := seed.Result().Cookies()[0]
	store.gets, store.creates = 0, 0

	router := httpx.NewRouter()
	router.Use(web.Sessions(manager))
	router.POST("/logout", func(ctx *httpx.Context) error {
		if err := web.Destroy(ctx); err != nil {
			return err
		}
		return web.Redirect(ctx, "/")
	})
	request := httptest.NewRequest(http.MethodPost, "/logout", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("logout response Cache-Control = %q, want no-store", got)
	}
	if store.gets != 1 || store.deletes != 1 || store.updates != 0 || store.rotates != 0 {
		t.Fatalf("operations: get=%d delete=%d update=%d rotate=%d", store.gets, store.deletes, store.updates, store.rotates)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatalf("logout cookie = %+v, want one expired cookie", cookies)
	}
}

func TestRedirectRequiresAbsoluteApplicationPath(t *testing.T) {
	for _, location := range []string{"https://evil.example/path", "//evil.example/path", `/\\evil.example/path`, "relative/path"} {
		t.Run(location, func(t *testing.T) {
			ctx := &httpx.Context{Response: httptest.NewRecorder(), Request: httptest.NewRequest(http.MethodPost, "/", nil)}
			if err := web.Redirect(ctx, location); err == nil {
				t.Fatalf("Redirect(%q) succeeded", location)
			}
		})
	}
	response := httptest.NewRecorder()
	ctx := &httpx.Context{Response: response, Request: httptest.NewRequest(http.MethodPost, "/", nil)}
	if err := web.Redirect(ctx, "/app/issues?notice=saved"); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/app/issues?notice=saved" {
		t.Fatalf("local redirect = %d %q", response.Code, response.Header().Get("Location"))
	}
}

func TestExternalRedirectIsExplicitAndHTTPOnly(t *testing.T) {
	for _, location := range []string{"/local", "javascript:alert(1)", "mailto:user@example.com"} {
		ctx := &httpx.Context{Response: httptest.NewRecorder(), Request: httptest.NewRequest(http.MethodPost, "/", nil)}
		if err := web.ExternalRedirect(ctx, location); err == nil {
			t.Fatalf("ExternalRedirect(%q) succeeded", location)
		}
	}
	response := httptest.NewRecorder()
	ctx := &httpx.Context{Response: response, Request: httptest.NewRequest(http.MethodPost, "/", nil)}
	if err := web.ExternalRedirect(ctx, "https://example.com/complete"); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "https://example.com/complete" {
		t.Fatalf("external redirect = %d %q", response.Code, response.Header().Get("Location"))
	}
}

func TestPanicRecoveryUsesOriginalResponse(t *testing.T) {
	router := httpx.NewRouter()
	router.Use(httpx.Recover(nil), web.Sessions(managerFor(t, session.NewMemoryStore())))
	router.GET("/", func(ctx *httpx.Context) error {
		ctx.Response.Header().Set("X-Buffered", "discard me")
		_, _ = io.WriteString(ctx.Response, "partial")
		panic("boom")
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	if response.Header().Get("X-Buffered") != "" || strings.Contains(response.Body.String(), "partial") {
		t.Fatalf("panic leaked buffered response: headers=%v body=%q", response.Header(), response.Body.String())
	}
}

func TestBufferedRoutesDoNotClaimStreamingSupport(t *testing.T) {
	router := httpx.NewRouter()
	router.Use(web.Sessions(managerFor(t, session.NewMemoryStore())))
	router.GET("/", func(ctx *httpx.Context) error {
		if _, ok := ctx.Response.(http.Flusher); ok {
			t.Fatal("buffered response unexpectedly exposes http.Flusher")
		}
		return ctx.NoContent(http.StatusNoContent)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.Code)
	}
}

func TestBufferedResponseLimitDiscardsResponseAndSessionMutation(t *testing.T) {
	store := newObservedStore()
	router := httpx.NewRouter()
	router.Use(web.SessionsLimit(managerFor(t, store), 4))
	router.GET("/", func(ctx *httpx.Context) error {
		current, _ := web.Session(ctx)
		if err := current.Put("notice", "not committed"); err != nil {
			return err
		}
		_, _ = io.WriteString(ctx.Response, "12345") // Prove ignored write errors are still caught at commit.
		return nil
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	if response.Body.String() == "12345" {
		t.Fatal("oversized buffered body was committed")
	}
	if store.creates != 0 || store.updates != 0 || store.rotates != 0 {
		t.Fatalf("oversized response persisted session: create=%d update=%d rotate=%d", store.creates, store.updates, store.rotates)
	}
}
