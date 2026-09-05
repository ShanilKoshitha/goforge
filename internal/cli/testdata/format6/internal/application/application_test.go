package application_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShanilKoshitha/goforge/httpx"
	"github.com/ShanilKoshitha/goforge/security/ratelimit"
	"github.com/ShanilKoshitha/goforge/session"

	"example.com/format6/internal/application"
	"example.com/format6/internal/config"
)

func TestHealth(t *testing.T) {
	app, err := application.New(config.Config{
		Address: ":8080", Environment: "test", SessionSecret: strings.Repeat("s", 32),
	}, application.Dependencies{Sessions: session.NewMemoryStore(), RateLimits: ratelimit.NewMemoryStore(100)})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}
	if response.Header().Get("X-Request-ID") == "" || response.Header().Get("X-Content-Type-Options") != "nosniff" ||
		response.Header().Get("Content-Security-Policy") == "" || response.Header().Get("Strict-Transport-Security") != "" {
		t.Fatal("production middleware headers are missing")
	}
}

func TestBrowserAndAPIErrorsKeepExplicitSurfaces(t *testing.T) {
	app, err := application.New(config.Config{
		Address: ":8080", Environment: "test", SessionSecret: strings.Repeat("s", 32),
	}, application.Dependencies{Sessions: session.NewMemoryStore(), RateLimits: ratelimit.NewMemoryStore(100)})
	if err != nil {
		t.Fatal(err)
	}
	failure := func(*httpx.Context) error {
		return httpx.NewHTTPError(http.StatusForbidden, `<unsafe & denied>`)
	}
	app.Router.GET("/app/failure", failure)
	app.Router.GET("/api/failure", failure)

	browser := httptest.NewRecorder()
	app.Handler().ServeHTTP(browser, httptest.NewRequest(http.MethodGet, "/app/failure", nil))
	if browser.Code != http.StatusForbidden || !strings.HasPrefix(browser.Header().Get("Content-Type"), "text/html") ||
		strings.Contains(browser.Body.String(), `<unsafe & denied>`) || !strings.Contains(browser.Body.String(), `&lt;unsafe &amp; denied&gt;`) {
		t.Fatalf("browser error surface: status=%d headers=%v body=%s", browser.Code, browser.Header(), browser.Body.String())
	}

	api := httptest.NewRecorder()
	app.Handler().ServeHTTP(api, httptest.NewRequest(http.MethodGet, "/api/failure", nil))
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(api.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if api.Code != http.StatusForbidden || !strings.HasPrefix(api.Header().Get("Content-Type"), "application/json") ||
		payload.Error.Message != `<unsafe & denied>` {
		t.Fatalf("API error surface: status=%d headers=%v body=%s", api.Code, api.Header(), api.Body.String())
	}
}
