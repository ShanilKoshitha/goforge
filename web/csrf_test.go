package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ShanilKoshitha/goforge/httpx"
	"github.com/ShanilKoshitha/goforge/web"
)

func TestCSRFTokenPersistsAndProtectsFormSubmission(t *testing.T) {
	store := newObservedStore()
	router := httpx.NewRouter()
	router.Use(web.Sessions(managerFor(t, store)), web.CSRF())
	router.GET("/form", func(ctx *httpx.Context) error {
		token, err := web.CSRFToken(ctx)
		if err != nil {
			return err
		}
		_, err = io.WriteString(ctx.Response, token)
		return err
	})
	router.POST("/submit", func(ctx *httpx.Context) error {
		return ctx.NoContent(http.StatusNoContent)
	})

	formResponse := httptest.NewRecorder()
	router.ServeHTTP(formResponse, httptest.NewRequest(http.MethodGet, "/form", nil))
	if formResponse.Code != http.StatusOK || len(formResponse.Result().Cookies()) != 1 {
		t.Fatalf("form response = %d cookies=%d", formResponse.Code, len(formResponse.Result().Cookies()))
	}
	token := formResponse.Body.String()
	request := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(url.Values{web.CSRFField: {token}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(formResponse.Result().Cookies()[0])
	submitResponse := httptest.NewRecorder()
	router.ServeHTTP(submitResponse, request)
	if submitResponse.Code != http.StatusNoContent {
		t.Fatalf("valid form status = %d: %s", submitResponse.Code, submitResponse.Body.String())
	}
	if store.creates != 1 || store.updates != 1 {
		t.Fatalf("session writes: create=%d update=%d", store.creates, store.updates)
	}
}

func TestCSRFRejectsMissingWrongQueryAndDuplicateTokens(t *testing.T) {
	store := newObservedStore()
	router := httpx.NewRouter()
	router.Use(web.Sessions(managerFor(t, store)), web.CSRF())
	router.GET("/form", func(ctx *httpx.Context) error {
		token, err := web.CSRFToken(ctx)
		if err != nil {
			return err
		}
		_, err = io.WriteString(ctx.Response, token)
		return err
	})
	called := false
	router.POST("/submit", func(ctx *httpx.Context) error {
		called = true
		return ctx.NoContent(http.StatusNoContent)
	})
	formResponse := httptest.NewRecorder()
	router.ServeHTTP(formResponse, httptest.NewRequest(http.MethodGet, "/form", nil))
	token := formResponse.Body.String()
	cookie := formResponse.Result().Cookies()[0]

	tests := []struct {
		name       string
		path       string
		body       string
		wantStatus int
	}{
		{name: "missing", path: "/submit", body: "", wantStatus: http.StatusForbidden},
		{name: "wrong", path: "/submit", body: url.Values{web.CSRFField: {strings.Repeat("A", len(token))}}.Encode(), wantStatus: http.StatusForbidden},
		{name: "query ignored", path: "/submit?" + url.Values{web.CSRFField: {token}}.Encode(), body: "", wantStatus: http.StatusForbidden},
		{name: "duplicate", path: "/submit", body: url.Values{web.CSRFField: {token, token}}.Encode(), wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			called = false
			request := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.AddCookie(cookie)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if called {
				t.Fatal("protected handler ran for invalid token")
			}
		})
	}
}

func TestCSRFHeaderProtectsNonFormRequest(t *testing.T) {
	router := httpx.NewRouter()
	router.Use(web.Sessions(managerFor(t, newObservedStore())), web.CSRF())
	router.GET("/token", func(ctx *httpx.Context) error {
		token, err := web.CSRFToken(ctx)
		if err != nil {
			return err
		}
		_, err = io.WriteString(ctx.Response, token)
		return err
	})
	router.POST("/json", func(ctx *httpx.Context) error {
		return ctx.NoContent(http.StatusNoContent)
	})
	tokenResponse := httptest.NewRecorder()
	router.ServeHTTP(tokenResponse, httptest.NewRequest(http.MethodGet, "/token", nil))

	request := httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(`{"value":true}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(web.CSRFHeader, tokenResponse.Body.String())
	request.AddCookie(tokenResponse.Result().Cookies()[0])
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("header token status = %d: %s", response.Code, response.Body.String())
	}
}

func TestCSRFProtectsMethodOverrideTarget(t *testing.T) {
	router := httpx.NewRouter()
	router.Use(httpx.MethodOverride(), web.Sessions(managerFor(t, newObservedStore())), web.CSRF())
	router.GET("/token", func(ctx *httpx.Context) error {
		token, err := web.CSRFToken(ctx)
		if err != nil {
			return err
		}
		_, err = io.WriteString(ctx.Response, token)
		return err
	})
	router.DELETE("/items/1", func(ctx *httpx.Context) error {
		return ctx.NoContent(http.StatusNoContent)
	})
	tokenResponse := httptest.NewRecorder()
	router.ServeHTTP(tokenResponse, httptest.NewRequest(http.MethodGet, "/token", nil))
	values := url.Values{"_method": {http.MethodDelete}, web.CSRFField: {tokenResponse.Body.String()}}
	request := httptest.NewRequest(http.MethodPost, "/items/1", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.AddCookie(tokenResponse.Result().Cookies()[0])
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("override status = %d: %s", response.Code, response.Body.String())
	}
}

func TestRotateCSRFInvalidatesPreviousToken(t *testing.T) {
	router := httpx.NewRouter()
	router.Use(web.Sessions(managerFor(t, newObservedStore())))
	router.GET("/rotate", func(ctx *httpx.Context) error {
		first, err := web.CSRFToken(ctx)
		if err != nil {
			return err
		}
		second, err := web.RotateCSRF(ctx)
		if err != nil {
			return err
		}
		if first == second {
			t.Fatal("CSRF rotation reused the prior token")
		}
		_, err = io.WriteString(ctx.Response, second)
		return err
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/rotate", nil))
	if response.Code != http.StatusOK || response.Body.Len() == 0 {
		t.Fatalf("rotation response = %d %q", response.Code, response.Body.String())
	}
}
