package httpx_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ShanilKoshitha/goforge/httpx"
)

func TestFormUsesOnlyURLBodyValues(t *testing.T) {
	router := httpx.NewRouter()
	router.POST("/submit", func(ctx *httpx.Context) error {
		form, err := ctx.Form()
		if err != nil {
			return err
		}
		if form.Has("query") {
			t.Fatal("query parameter was exposed as a form body value")
		}
		name, err := form.Value("name")
		if err != nil {
			return err
		}
		got := form.Values("roles")
		if name != "Ada" || len(got) != 2 || got[0] != "admin" || got[1] != "editor" {
			t.Fatalf("unexpected form values: name=%q roles=%v", name, got)
		}
		return ctx.NoContent(http.StatusNoContent)
	})

	values := url.Values{"name": {"Ada"}, "roles": {"admin", "editor"}}
	request := httptest.NewRequest(http.MethodPost, "/submit?query=%zz", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", response.Code, response.Body.String())
	}
}

func TestFormRejectsDuplicateScalarValues(t *testing.T) {
	router := httpx.NewRouter()
	router.POST("/submit", func(ctx *httpx.Context) error {
		form, err := ctx.Form()
		if err != nil {
			return err
		}
		_, err = form.Value("name")
		return err
	})
	request := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader("name=Ada&name=Grace"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
	}
}

func TestFormRequiresDeclaredMediaTypeAndHonorsLimit(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		limit       int64
		wantStatus  int
	}{
		{name: "missing media type", body: "name=Ada", limit: 1024, wantStatus: http.StatusUnsupportedMediaType},
		{name: "wrong media type", contentType: "text/plain", body: "name=Ada", limit: 1024, wantStatus: http.StatusUnsupportedMediaType},
		{name: "body too large", contentType: "application/x-www-form-urlencoded", body: "name=Ada", limit: 4, wantStatus: http.StatusRequestEntityTooLarge},
		{name: "malformed encoding", contentType: "application/x-www-form-urlencoded", body: "name=%zz", limit: 1024, wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := httpx.NewRouter()
			router.POST("/submit", func(ctx *httpx.Context) error {
				_, err := ctx.FormLimit(test.limit)
				return err
			})
			request := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(test.body))
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("expected %d, got %d: %s", test.wantStatus, response.Code, response.Body.String())
			}
		})
	}
}

func TestMethodOverrideRunsBeforeRouteMatching(t *testing.T) {
	router := httpx.NewRouter()
	router.Use(httpx.MethodOverride())
	router.PATCH("/issues/{id}", func(ctx *httpx.Context) error {
		if ctx.OriginalMethod() != http.MethodPost || ctx.Request.Method != http.MethodPatch {
			t.Fatalf("methods = original %q, effective %q", ctx.OriginalMethod(), ctx.Request.Method)
		}
		return ctx.NoContent(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/issues/42", strings.NewReader("_method=patch"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected PATCH route status 204, got %d: %s", response.Code, response.Body.String())
	}
}

func TestMethodOverrideNeverReadsQueryParameters(t *testing.T) {
	router := httpx.NewRouter()
	router.Use(httpx.MethodOverride())
	router.POST("/issues", func(ctx *httpx.Context) error {
		if ctx.OriginalMethod() != http.MethodPost || ctx.Request.Method != http.MethodPost {
			t.Fatalf("methods = original %q, effective %q", ctx.OriginalMethod(), ctx.Request.Method)
		}
		return ctx.NoContent(http.StatusCreated)
	})
	request := httptest.NewRequest(http.MethodPost, "/issues?_method=DELETE", strings.NewReader("name=kept"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected POST route status 201, got %d: %s", response.Code, response.Body.String())
	}
}

func TestMethodOverrideAllowsOnlyMutationMethods(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodHead, http.MethodOptions, http.MethodConnect, http.MethodTrace} {
		t.Run(method, func(t *testing.T) {
			router := httpx.NewRouter()
			router.Use(httpx.MethodOverride())
			router.POST("/issues", func(ctx *httpx.Context) error {
				return errors.New("POST handler must not run")
			})
			request := httptest.NewRequest(http.MethodPost, "/issues", strings.NewReader("_method="+url.QueryEscape(method)))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestMethodOverrideIgnoresNonPOSTAndNonFormRequests(t *testing.T) {
	router := httpx.NewRouter()
	router.Use(httpx.MethodOverride())
	router.PUT("/issues", func(ctx *httpx.Context) error {
		if ctx.Request.Method != http.MethodPut || ctx.OriginalMethod() != http.MethodPut {
			t.Fatalf("methods = original %q, effective %q", ctx.OriginalMethod(), ctx.Request.Method)
		}
		return ctx.NoContent(http.StatusNoContent)
	})
	router.POST("/json", func(ctx *httpx.Context) error {
		return ctx.NoContent(http.StatusAccepted)
	})

	put := httptest.NewRequest(http.MethodPut, "/issues", strings.NewReader("_method=DELETE"))
	put.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	putResponse := httptest.NewRecorder()
	router.ServeHTTP(putResponse, put)
	if putResponse.Code != http.StatusNoContent {
		t.Fatalf("expected PUT status 204, got %d", putResponse.Code)
	}

	jsonRequest := httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(`{"_method":"DELETE"}`))
	jsonRequest.Header.Set("Content-Type", "application/json")
	jsonResponse := httptest.NewRecorder()
	router.ServeHTTP(jsonResponse, jsonRequest)
	if jsonResponse.Code != http.StatusAccepted {
		t.Fatalf("expected JSON POST status 202, got %d", jsonResponse.Code)
	}
}

func TestMethodOverrideCanBeScopedAwayFromAPIRoutes(t *testing.T) {
	router := httpx.NewRouter()
	router.Use(httpx.MethodOverride("/app/"))
	deleted := false
	router.DELETE("/issues/{id}", func(*httpx.Context) error {
		deleted = true
		return nil
	})
	router.DELETE("/app/issues/{id}", func(ctx *httpx.Context) error {
		return ctx.NoContent(http.StatusNoContent)
	})

	apiRequest := httptest.NewRequest(http.MethodPost, "/issues/1", strings.NewReader("_method=DELETE"))
	apiRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	apiResponse := httptest.NewRecorder()
	router.ServeHTTP(apiResponse, apiRequest)
	if deleted || apiResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("scoped override reached API DELETE: deleted=%v status=%d", deleted, apiResponse.Code)
	}

	webRequest := httptest.NewRequest(http.MethodPost, "/app/issues/1", strings.NewReader("_method=DELETE"))
	webRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	webResponse := httptest.NewRecorder()
	router.ServeHTTP(webResponse, webRequest)
	if webResponse.Code != http.StatusNoContent {
		t.Fatalf("scoped browser override status = %d", webResponse.Code)
	}
}
