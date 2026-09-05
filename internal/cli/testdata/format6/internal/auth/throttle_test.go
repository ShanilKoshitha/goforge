package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/httpx"
	"github.com/ShanilKoshitha/goforge/security/password"
	"github.com/ShanilKoshitha/goforge/security/ratelimit"
	"github.com/ShanilKoshitha/goforge/session"
	"github.com/ShanilKoshitha/goforge/web"

	"example.com/format6/internal/auth"
	views "example.com/format6/resources/views"
)

type duplicateUsers struct{ fakeUsers }

func (*duplicateUsers) Create(context.Context, string, string, string) (auth.User, error) {
	return auth.User{}, auth.ErrEmailTaken
}

func strictAttemptLimiter(t *testing.T) *ratelimit.Limiter {
	t.Helper()
	limiter, err := ratelimit.New(ratelimit.NewMemoryStore(10), ratelimit.Policy{Limit: 1, Window: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return limiter
}

func TestJSONLoginThrottlesNormalizedAccountWithoutDisclosure(t *testing.T) {
	users := &fakeUsers{}
	manager, err := session.NewManager(session.NewMemoryStore(), []byte(strings.Repeat("s", 32)), session.Cookie{UnsafeAllowHTTP: true}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	controller := auth.NewController(users, manager, password.Hasher{Iterations: 1}, strictAttemptLimiter(t))
	router := httpx.NewRouter()
	router.POST("/auth/login", controller.Login)

	login := func(email string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":"`+email+`","password":"incorrect password"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	first := login("Ada@Example.com")
	second := login("ada@example.com")
	if first.Code != http.StatusUnauthorized {
		t.Fatalf("first login status = %d: %s", first.Code, first.Body.String())
	}
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" {
		t.Fatalf("throttled login = %d retry=%q: %s", second.Code, second.Header().Get("Retry-After"), second.Body.String())
	}
	if strings.Contains(strings.ToLower(second.Body.String()), "ada@example.com") {
		t.Fatal("throttle response disclosed the account identifier")
	}
}

func TestJSONRegisterThrottlesNormalizedAccount(t *testing.T) {
	users := &duplicateUsers{}
	manager, err := session.NewManager(session.NewMemoryStore(), []byte(strings.Repeat("s", 32)), session.Cookie{UnsafeAllowHTTP: true}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	controller := auth.NewController(users, manager, password.Hasher{Iterations: 1}, strictAttemptLimiter(t))
	router := httpx.NewRouter()
	router.POST("/auth/register", controller.Register)

	register := func(email string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(`{"name":"Ada","email":"`+email+`","password":"a secure passphrase","password_confirmation":"a secure passphrase"}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	first := register("Ada@Example.com")
	second := register("ada@example.com")
	if first.Code != http.StatusConflict {
		t.Fatalf("first registration status = %d: %s", first.Code, first.Body.String())
	}
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" {
		t.Fatalf("throttled registration = %d retry=%q: %s", second.Code, second.Header().Get("Retry-After"), second.Body.String())
	}
	if strings.Contains(strings.ToLower(second.Body.String()), "ada@example.com") {
		t.Fatal("throttle response disclosed the account identifier")
	}
}

func TestBrowserLoginUsesTheSameAccountThrottle(t *testing.T) {
	users := &fakeUsers{}
	manager, err := session.NewManager(session.NewMemoryStore(), []byte(strings.Repeat("s", 32)), session.Cookie{UnsafeAllowHTTP: true}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := views.New()
	if err != nil {
		t.Fatal(err)
	}
	controller := auth.NewController(users, manager, password.Hasher{Iterations: 1}, strictAttemptLimiter(t))
	browser := auth.NewWebController(controller, renderer)
	router := httpx.NewRouter()
	public := router.Group("", web.Sessions(manager), web.CSRF())
	public.GET("/login", browser.LoginForm)
	public.POST("/login", browser.Login)

	form := httptest.NewRecorder()
	router.ServeHTTP(form, httptest.NewRequest(http.MethodGet, "/login", nil))
	token := csrfToken(t, form.Body.String())
	cookie := form.Result().Cookies()[0]
	first := submitForm(router, http.MethodPost, "/login", url.Values{
		"_token": {token}, "email": {"Ada@Example.com"}, "password": {"incorrect password"},
	}, cookie)
	second := submitForm(router, http.MethodPost, "/login", url.Values{
		"_token": {token}, "email": {"ada@example.com"}, "password": {"incorrect password"},
	}, cookie)
	if first.Code != http.StatusUnprocessableEntity {
		t.Fatalf("first browser login = %d: %s", first.Code, first.Body.String())
	}
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" {
		t.Fatalf("throttled browser login = %d retry=%q: %s", second.Code, second.Header().Get("Retry-After"), second.Body.String())
	}
	if contentType := second.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("throttled browser login content type = %q", contentType)
	}
	if strings.Contains(strings.ToLower(second.Body.String()), "ada@example.com") {
		t.Fatal("browser throttle response disclosed the account identifier")
	}
}

func TestBrowserRegisterUsesTheSameAccountThrottle(t *testing.T) {
	users := &duplicateUsers{}
	manager, err := session.NewManager(session.NewMemoryStore(), []byte(strings.Repeat("s", 32)), session.Cookie{UnsafeAllowHTTP: true}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := views.New()
	if err != nil {
		t.Fatal(err)
	}
	controller := auth.NewController(users, manager, password.Hasher{Iterations: 1}, strictAttemptLimiter(t))
	browser := auth.NewWebController(controller, renderer)
	router := httpx.NewRouter()
	public := router.Group("", web.Sessions(manager), web.CSRF())
	public.GET("/register", browser.RegisterForm)
	public.POST("/register", browser.Register)

	form := httptest.NewRecorder()
	router.ServeHTTP(form, httptest.NewRequest(http.MethodGet, "/register", nil))
	token := csrfToken(t, form.Body.String())
	cookie := form.Result().Cookies()[0]
	first := submitForm(router, http.MethodPost, "/register", url.Values{
		"_token": {token}, "name": {"Ada"}, "email": {"Ada@Example.com"}, "password": {"a secure passphrase"}, "password_confirmation": {"a secure passphrase"},
	}, cookie)
	second := submitForm(router, http.MethodPost, "/register", url.Values{
		"_token": {token}, "name": {"Ada"}, "email": {"ada@example.com"}, "password": {"a secure passphrase"}, "password_confirmation": {"a secure passphrase"},
	}, cookie)
	if first.Code != http.StatusUnprocessableEntity {
		t.Fatalf("first browser registration = %d: %s", first.Code, first.Body.String())
	}
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" {
		t.Fatalf("throttled browser registration = %d retry=%q: %s", second.Code, second.Header().Get("Retry-After"), second.Body.String())
	}
	if contentType := second.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("throttled browser registration content type = %q", contentType)
	}
	if strings.Contains(strings.ToLower(second.Body.String()), "ada@example.com") {
		t.Fatal("browser throttle response disclosed the account identifier")
	}
}
