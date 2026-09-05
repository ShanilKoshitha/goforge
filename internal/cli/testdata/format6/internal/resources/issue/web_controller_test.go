package issue_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/httpx"
	"github.com/ShanilKoshitha/goforge/session"
	"github.com/ShanilKoshitha/goforge/web"

	"example.com/format6/internal/auth"
	resource "example.com/format6/internal/resources/issue"
	views "example.com/format6/resources/views"
)

var browserCSRFInput = regexp.MustCompile(`name="_token" value="([^"]+)"`)
var browserVersionInput = regexp.MustCompile(`name="version" value="([0-9]+)"`)

func TestBrowserResourceLifecycle(t *testing.T) {
	repository := &fakeRepository{}
	users := fakeUsers{user: auth.User{ID: 9, Name: "Ada", Email: "ada@example.com"}}
	manager, err := session.NewManager(session.NewMemoryStore(), []byte(strings.Repeat("s", 32)), session.Cookie{UnsafeAllowHTTP: true}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	renderer, err := views.New()
	if err != nil {
		t.Fatal(err)
	}
	controller := resource.NewWebController(repository, renderer)
	router := httpx.NewRouter()
	router.Use(httpx.MethodOverride("/app/"))
	browserRoute := func(handler httpx.Handler) httpx.Handler {
		return web.Sessions(manager)(web.CSRF()(auth.RequireWeb(users)(handler)))
	}
	router.GET("/app/issues", browserRoute(controller.Index))
	router.GET("/app/issues/new", browserRoute(controller.New))
	router.POST("/app/issues", browserRoute(controller.Create))
	router.GET("/app/issues/{id}", browserRoute(controller.Show))
	router.GET("/app/issues/{id}/edit", browserRoute(controller.Edit))
	router.PUT("/app/issues/{id}", browserRoute(controller.Update))
	router.DELETE("/app/issues/{id}", browserRoute(controller.Delete))

	guest := httptest.NewRecorder()
	router.ServeHTTP(guest, httptest.NewRequest(http.MethodGet, "/app/issues/new", nil))
	if guest.Code != http.StatusSeeOther || guest.Header().Get("Location") != "/login" {
		t.Fatalf("guest response: %d %q", guest.Code, guest.Header().Get("Location"))
	}

	current, err := manager.Load(context.Background(), httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := current.Put("user_id", int64(9)); err != nil {
		t.Fatal(err)
	}
	seed := httptest.NewRecorder()
	if err := manager.Save(context.Background(), seed, current); err != nil {
		t.Fatal(err)
	}
	cookie := seed.Result().Cookies()[0]

	newPage := browserRequest(router, http.MethodGet, "/app/issues/new", nil, cookie)
	if newPage.Code != http.StatusOK {
		t.Fatalf("new page: %d: %s", newPage.Code, newPage.Body.String())
	}
	token := browserCSRFToken(t, newPage.Body.String())
	cookie = responseCookie(newPage, cookie)

	missing := browserRequest(router, http.MethodPost, "/app/issues", url.Values{"name": {"Example"}}, cookie)
	if missing.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF: %d: %s", missing.Code, missing.Body.String())
	}

	created := browserRequest(router, http.MethodPost, "/app/issues", url.Values{
		"_token": {token}, "name": {"<script>alert(1)</script>"},
	}, cookie)
	if created.Code != http.StatusSeeOther || created.Header().Get("Location") != "/app/issues/1" || repository.item.UserID != 9 {
		t.Fatalf("create: %d %q owner=%d: %s", created.Code, created.Header().Get("Location"), repository.item.UserID, created.Body.String())
	}
	cookie = responseCookie(created, cookie)
	index := browserRequest(router, http.MethodGet, "/app/issues", nil, cookie)
	if index.Code != http.StatusOK || strings.Contains(index.Body.String(), "<script>") || !strings.Contains(index.Body.String(), "&lt;script&gt;") {
		t.Fatalf("index did not list escaped model data: %d: %s", index.Code, index.Body.String())
	}
	if repository.paginationUserID != 9 || repository.pageNumber != 1 || repository.pageSize != 20 {
		t.Fatalf("default pagination: owner=%d page=%d per_page=%d", repository.paginationUserID, repository.pageNumber, repository.pageSize)
	}
	cookie = responseCookie(index, cookie)
	repository.paginationHasNext = true
	paged := browserRequest(router, http.MethodGet, "/app/issues?page=2&per_page=5", nil, cookie)
	if paged.Code != http.StatusOK || repository.paginationUserID != 9 || repository.pageNumber != 2 || repository.pageSize != 5 {
		t.Fatalf("explicit pagination: status=%d owner=%d page=%d per_page=%d: %s", paged.Code, repository.paginationUserID, repository.pageNumber, repository.pageSize, paged.Body.String())
	}
	if !strings.Contains(paged.Body.String(), `/app/issues?page=1&amp;per_page=5`) || !strings.Contains(paged.Body.String(), `/app/issues?page=3&amp;per_page=5`) {
		t.Fatalf("pagination links are missing or do not retain page size: %s", paged.Body.String())
	}
	cookie = responseCookie(paged, cookie)
	bounded := browserRequest(router, http.MethodGet, "/app/issues?page=999999&per_page=999999", nil, cookie)
	if bounded.Code != http.StatusOK || repository.pageNumber != 10_000 || repository.pageSize != 100 {
		t.Fatalf("bounded pagination: status=%d page=%d per_page=%d: %s", bounded.Code, repository.pageNumber, repository.pageSize, bounded.Body.String())
	}
	cookie = responseCookie(bounded, cookie)
	for _, target := range []string{
		"/app/issues?page=0",
		"/app/issues?page=not-a-number",
		"/app/issues?page=1&page=2",
		"/app/issues?per_page=0",
	} {
		invalid := browserRequest(router, http.MethodGet, target, nil, cookie)
		if invalid.Code != http.StatusBadRequest {
			t.Fatalf("invalid pagination %q: %d: %s", target, invalid.Code, invalid.Body.String())
		}
	}

	show := browserRequest(router, http.MethodGet, "/app/issues/1", nil, cookie)
	if show.Code != http.StatusOK || strings.Contains(show.Body.String(), "<script>") || !strings.Contains(show.Body.String(), "&lt;script&gt;") {
		t.Fatalf("show did not contextually escape model data: %d: %s", show.Code, show.Body.String())
	}
	cookie = responseCookie(show, cookie)
	edit := browserRequest(router, http.MethodGet, "/app/issues/1/edit", nil, cookie)
	if edit.Code != http.StatusOK || strings.Contains(edit.Body.String(), "<script>") || !strings.Contains(edit.Body.String(), "&lt;script&gt;") {
		t.Fatalf("edit did not display escaped model data: %d: %s", edit.Code, edit.Body.String())
	}
	token = browserCSRFToken(t, edit.Body.String())
	version := browserVersion(t, edit.Body.String())
	cookie = responseCookie(edit, cookie)

	repository.item.Version++ // Simulate another request winning after the edit page was loaded.
	stale := browserRequest(router, http.MethodPost, "/app/issues/1", url.Values{
		"_token": {token}, "_method": {http.MethodPut}, "name": {"Renamed"}, "version": {version},
	}, cookie)
	if stale.Code != http.StatusConflict || repository.item.Name == "Renamed" || !strings.Contains(stale.Body.String(), "was changed by another request") {
		t.Fatalf("stale browser update: %d name=%q: %s", stale.Code, repository.item.Name, stale.Body.String())
	}
	refreshedVersion := browserVersion(t, stale.Body.String())
	if refreshedVersion == version || !strings.Contains(stale.Body.String(), `value="Renamed"`) {
		t.Fatalf("stale form did not preserve input and refresh version: old=%s new=%s: %s", version, refreshedVersion, stale.Body.String())
	}
	token = browserCSRFToken(t, stale.Body.String())
	cookie = responseCookie(stale, cookie)
	updated := browserRequest(router, http.MethodPost, "/app/issues/1", url.Values{
		"_token": {token}, "_method": {http.MethodPut}, "name": {"Renamed"}, "version": {refreshedVersion},
	}, cookie)
	if updated.Code != http.StatusSeeOther || repository.item.Name != "Renamed" {
		t.Fatalf("method-override update: %d name=%q: %s", updated.Code, repository.item.Name, updated.Body.String())
	}
	cookie = responseCookie(updated, cookie)

	show = browserRequest(router, http.MethodGet, "/app/issues/1", nil, cookie)
	token = browserCSRFToken(t, show.Body.String())
	cookie = responseCookie(show, cookie)
	deleted := browserRequest(router, http.MethodPost, "/app/issues/1", url.Values{
		"_token": {token}, "_method": {http.MethodDelete},
	}, cookie)
	if deleted.Code != http.StatusSeeOther || deleted.Header().Get("Location") != "/app/issues" {
		t.Fatalf("method-override delete: %d %q: %s", deleted.Code, deleted.Header().Get("Location"), deleted.Body.String())
	}
}

func browserRequest(router http.Handler, method, target string, values url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	var body *strings.Reader
	if values == nil {
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(values.Encode())
	}
	request := httptest.NewRequest(method, target, body)
	if values != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func browserCSRFToken(t *testing.T, body string) string {
	t.Helper()
	match := browserCSRFInput.FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("response has no CSRF field: %s", body)
	}
	return match[1]
}

func browserVersion(t *testing.T, body string) string {
	t.Helper()
	match := browserVersionInput.FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("response has no version field: %s", body)
	}
	return match[1]
}

func responseCookie(response *httptest.ResponseRecorder, fallback *http.Cookie) *http.Cookie {
	if cookies := response.Result().Cookies(); len(cookies) > 0 {
		return cookies[0]
	}
	return fallback
}
