package httpx_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShanilKoshitha/goforge/httpx"
)

func TestRouterDispatchesAndReadsPathParameters(t *testing.T) {
	router := httpx.NewRouter()
	router.GET("/users/{id}", func(ctx *httpx.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"id": ctx.Param("id")})
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if got := response.Body.String(); got != "{\"id\":\"42\"}\n" {
		t.Fatalf("unexpected body %s", got)
	}
}

func TestRouterRendersStructuredErrors(t *testing.T) {
	router := httpx.NewRouter()
	router.POST("/users", func(ctx *httpx.Context) error {
		return httpx.NewHTTPError(http.StatusUnprocessableEntity, "validation failed").WithDetails(map[string][]string{"email": {"is required"}})
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/users", nil))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), "validation failed") || !strings.Contains(response.Body.String(), "email") {
		t.Fatalf("unexpected error body %s", response.Body.String())
	}
}

func TestBindJSONRejectsUnknownFieldsAndMultipleValues(t *testing.T) {
	tests := []string{
		`{"name":"Ada","admin":true}`,
		`{"name":"Ada"} {"name":"Grace"}`,
	}
	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			ctxHandler := httpx.Standard(func(ctx *httpx.Context) error {
				var request struct {
					Name string `json:"name"`
				}
				return ctx.BindJSON(&request)
			})
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			ctxHandler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", response.Code)
			}
		})
	}
}

func TestMiddlewareOrderIsExplicit(t *testing.T) {
	var order []string
	middleware := func(name string) httpx.Middleware {
		return func(next httpx.Handler) httpx.Handler {
			return func(ctx *httpx.Context) error {
				order = append(order, name+" before")
				err := next(ctx)
				order = append(order, name+" after")
				return err
			}
		}
	}
	router := httpx.NewRouter()
	router.Use(middleware("outer"), middleware("inner"))
	router.GET("/", func(ctx *httpx.Context) error {
		order = append(order, "handler")
		return ctx.NoContent(http.StatusNoContent)
	})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	want := "outer before,inner before,handler,inner after,outer after"
	if got := strings.Join(order, ","); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRecoverConvertsPanicToError(t *testing.T) {
	router := httpx.NewRouter()
	router.Use(httpx.Recover(nil))
	router.GET("/", func(ctx *httpx.Context) error { panic(errors.New("boom")) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", response.Code)
	}
}

func TestGlobalMiddlewareHandlesPreflightWithoutAnOptionsRoute(t *testing.T) {
	router := httpx.NewRouter()
	router.Use(httpx.CORS(httpx.CORSConfig{AllowedOrigins: []string{"https://example.com"}}))
	router.GET("/users", func(ctx *httpx.Context) error { return ctx.NoContent(http.StatusOK) })
	request := httptest.NewRequest(http.MethodOptions, "/users", nil)
	request.Header.Set("Origin", "https://example.com")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("unexpected allowed origin %q", got)
	}
}

func TestNamedRouteBuildsEscapedURL(t *testing.T) {
	router := httpx.NewRouter()
	router.Named("users.show", http.MethodGet, "/users/{id}", func(ctx *httpx.Context) error { return nil })
	path, err := router.URL("users.show", map[string]string{"id": "Ada Lovelace"})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/users/Ada%20Lovelace" {
		t.Fatalf("unexpected path %q", path)
	}
	if _, err := router.URL("users.show", nil); err == nil {
		t.Fatal("expected a missing parameter error")
	}
}

func TestCORSLeavesOrdinaryOptionsRequestsToTheHandler(t *testing.T) {
	router := httpx.NewRouter()
	router.Use(httpx.CORS(httpx.CORSConfig{AllowedOrigins: []string{"https://example.com"}}))
	router.Handle(http.MethodOptions, "/users", func(ctx *httpx.Context) error {
		return ctx.JSON(http.StatusOK, map[string]string{"method": "OPTIONS"})
	})
	request := httptest.NewRequest(http.MethodOptions, "/users", nil)
	request.Header.Set("Origin", "https://example.com")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "{\"method\":\"OPTIONS\"}\n" {
		t.Fatalf("ordinary OPTIONS request did not reach its handler: %d %q", response.Code, response.Body.String())
	}
}

func TestCORSVariesAllOriginDependentResponses(t *testing.T) {
	router := httpx.NewRouter()
	router.Use(httpx.CORS(httpx.CORSConfig{AllowedOrigins: []string{"https://example.com"}}))
	router.GET("/", func(ctx *httpx.Context) error { return ctx.NoContent(http.StatusNoContent) })
	for _, origin := range []string{"", "https://denied.example", "https://example.com"} {
		t.Run(origin, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			if origin != "" {
				request.Header.Set("Origin", origin)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if vary := response.Header().Get("Vary"); vary != "Origin" {
				t.Fatalf("Vary = %q, want Origin", vary)
			}
			allowed := response.Header().Get("Access-Control-Allow-Origin")
			if origin == "https://example.com" && allowed != origin {
				t.Fatalf("allowed origin = %q, want %q", allowed, origin)
			}
			if origin != "https://example.com" && allowed != "" {
				t.Fatalf("unexpected allowed origin %q", allowed)
			}
		})
	}
}

func TestErrorAfterCommittedResponseDoesNotCorruptBody(t *testing.T) {
	handler := func(ctx *httpx.Context) error {
		if err := ctx.JSON(http.StatusOK, map[string]bool{"started": true}); err != nil {
			return err
		}
		return errors.New("work failed after response was committed")
	}
	router := httpx.NewRouter()
	router.GET("/stream", handler)
	logged := httpx.NewRouter()
	logged.Use(httpx.Logger(nil))
	logged.GET("/stream", handler)
	for name, server := range map[string]http.Handler{
		"router":             router,
		"router with logger": logged,
		"standard handler":   httpx.Standard(handler),
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/stream", nil))
			if response.Code != http.StatusOK || response.Body.String() != "{\"started\":true}\n" {
				t.Fatalf("committed response was corrupted: %d %q", response.Code, response.Body.String())
			}
		})
	}
}

func TestFailedRouteRegistrationDoesNotLeaveMetadata(t *testing.T) {
	router := httpx.NewRouter()
	handler := func(ctx *httpx.Context) error { return ctx.NoContent(http.StatusNoContent) }
	router.Named("original", http.MethodGet, "/users/{id}", handler)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected conflicting route to panic")
			}
		}()
		router.Named("conflict", http.MethodGet, "/users/{name}", handler)
	}()
	if routes := router.Routes(); len(routes) != 1 || routes[0].Name != "original" {
		t.Fatalf("failed route changed registry: %+v", routes)
	}
	if _, err := router.URL("conflict", map[string]string{"name": "Ada"}); err == nil {
		t.Fatal("failed route retained its name")
	}
	router.Named("conflict", http.MethodGet, "/other", handler)
}

func TestNamedRouteOmitsExactMatchMarker(t *testing.T) {
	router := httpx.NewRouter()
	router.Named("home", http.MethodGet, "/{$}", func(*httpx.Context) error { return nil })
	path, err := router.URL("home", nil)
	if err != nil || path != "/" {
		t.Fatalf("URL = %q, %v; want /", path, err)
	}
}

func TestRecoverPreservesHTTPAbortHandler(t *testing.T) {
	router := httpx.NewRouter()
	router.Use(httpx.Recover(nil))
	router.GET("/", func(*httpx.Context) error { panic(http.ErrAbortHandler) })
	defer func() {
		if recovered := recover(); recovered != http.ErrAbortHandler {
			t.Errorf("panic = %v, want http.ErrAbortHandler", recovered)
		}
	}()
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	t.Fatal("Recover swallowed http.ErrAbortHandler")
}
