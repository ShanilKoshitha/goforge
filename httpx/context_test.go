package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ShanilKoshitha/goforge/httpx"
)

func TestBindJSONRequiresJSONContentType(t *testing.T) {
	for _, test := range []struct {
		name        string
		contentType string
		wantStatus  int
	}{
		{"missing", "", http.StatusUnsupportedMediaType},
		{"plain text form", "text/plain", http.StatusUnsupportedMediaType},
		{"HTML form", "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType},
		{"malformed parameters", "application/json; charset", http.StatusUnsupportedMediaType},
		{"different JSON format", "application/problem+json", http.StatusUnsupportedMediaType},
		{"JSON", "application/json", http.StatusNoContent},
		{"JSON with parameters", "application/json; charset=utf-8", http.StatusNoContent},
		{"case insensitive", "Application/JSON", http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			var payload struct {
				Email    string `json:"email"`
				Password string `json:"password"`
			}
			handler := httpx.Standard(func(ctx *httpx.Context) error {
				if err := ctx.BindJSON(&payload); err != nil {
					return err
				}
				return ctx.NoContent(http.StatusNoContent)
			})
			// A text/plain HTML form can produce valid JSON with '=' in its value.
			body := "{\"email\":\"attacker@example.com\",\"password\":\"chosen-password=\"}\r\n"
			request := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body))
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantStatus == http.StatusUnsupportedMediaType && payload.Email != "" {
				t.Fatal("decoded a request with unsupported Content-Type")
			}
			if test.wantStatus == http.StatusNoContent && payload.Password != "chosen-password=" {
				t.Fatalf("password = %q, want decoded JSON value", payload.Password)
			}
		})
	}
}

func TestBindJSONLimitClassifiesOversizedBodiesAndRejectsInvalidLimits(t *testing.T) {
	t.Run("oversized", func(t *testing.T) {
		handler := httpx.Standard(func(ctx *httpx.Context) error {
			var payload map[string]string
			return ctx.BindJSONLimit(&payload, 8)
		})
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"larger than eight"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413: %s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "http: request body too large") {
			t.Fatalf("response disclosed decoder cause: %s", response.Body.String())
		}
	})

	t.Run("invalid limit", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		var received error
		httpx.Standard(func(ctx *httpx.Context) error {
			received = ctx.BindJSONLimit(&struct{}{}, 0)
			return ctx.NoContent(http.StatusNoContent)
		}).ServeHTTP(response, request)
		if received == nil || received.Error() != "JSON body limit must be positive" {
			t.Fatalf("error = %v", received)
		}
	})
}

func TestContextExposesMatchedRouteMetadata(t *testing.T) {
	router := httpx.NewRouter()
	router.Named("issues.show", http.MethodGet, "/issues/{id}", func(ctx *httpx.Context) error {
		if ctx.RouteName() != "issues.show" || ctx.RoutePattern() != "/issues/{id}" {
			t.Fatalf("route metadata = %q %q", ctx.RouteName(), ctx.RoutePattern())
		}
		return ctx.NoContent(http.StatusNoContent)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/issues/42", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
}
