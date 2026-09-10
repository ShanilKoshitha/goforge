package asset_test

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ShanilKoshitha/goforge/asset"
)

func TestSetResolvesAndServesExactAsset(t *testing.T) {
	body := []byte("body { color: #123; }\n")
	files := fstest.MapFS{"files/css/app.css": &fstest.MapFile{Data: body}}
	set, err := asset.New(files, asset.Config{Root: "files", URLPrefix: "/static"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	url, err := set.URL("css/app.css")
	if err != nil || url != "/static/css/app.css" {
		t.Fatalf("URL = %q, %v", url, err)
	}

	// Construction takes an immutable copy rather than retaining a mutable
	// source slice or opening the filesystem again per request.
	copy(body, []byte("changed after creation!!!"))
	response := request(set, http.MethodGet, url, nil)
	if response.Code != http.StatusOK || response.Body.String() != "body { color: #123; }\n" {
		t.Fatalf("GET = %d %q", response.Code, response.Body.String())
	}
	digest := sha256.Sum256([]byte("body { color: #123; }\n"))
	wantETag := fmt.Sprintf("\"%x\"", digest)
	assertHeader(t, response, "Cache-Control", "public, max-age=0, must-revalidate")
	assertHeader(t, response, "Content-Type", "text/css; charset=utf-8")
	assertHeader(t, response, "Content-Length", "22")
	assertHeader(t, response, "ETag", wantETag)
	assertHeader(t, response, "X-Content-Type-Options", "nosniff")
}

func TestSetSupportsHeadConditionalAndRangeRequests(t *testing.T) {
	set := mustSet(t, fstest.MapFS{"files/app.js": &fstest.MapFile{Data: []byte("0123456789")}})
	url, _ := set.URL("app.js")
	get := request(set, http.MethodGet, url, nil)
	etag := get.Header().Get("ETag")

	head := request(set, http.MethodHead, url, nil)
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD = %d %q", head.Code, head.Body.String())
	}
	assertHeader(t, head, "Content-Length", "10")
	assertHeader(t, head, "ETag", etag)

	notModified := request(set, http.MethodGet, url, map[string]string{"If-None-Match": etag})
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("conditional GET = %d %q", notModified.Code, notModified.Body.String())
	}
	assertHeader(t, notModified, "ETag", etag)
	assertHeader(t, notModified, "Cache-Control", "public, max-age=0, must-revalidate")

	partial := request(set, http.MethodGet, url, map[string]string{"Range": "bytes=2-5"})
	if partial.Code != http.StatusPartialContent || partial.Body.String() != "2345" {
		t.Fatalf("range GET = %d %q", partial.Code, partial.Body.String())
	}
	assertHeader(t, partial, "Content-Range", "bytes 2-5/10")
	assertHeader(t, partial, "Content-Length", "4")
	assertHeader(t, partial, "ETag", etag)

	matchedRange := request(set, http.MethodGet, url, map[string]string{"Range": "bytes=7-", "If-Range": etag})
	if matchedRange.Code != http.StatusPartialContent || matchedRange.Body.String() != "789" {
		t.Fatalf("matched If-Range = %d %q", matchedRange.Code, matchedRange.Body.String())
	}
	staleRange := request(set, http.MethodGet, url, map[string]string{"Range": "bytes=7-", "If-Range": `"stale"`})
	if staleRange.Code != http.StatusOK || staleRange.Body.String() != "0123456789" {
		t.Fatalf("stale If-Range = %d %q", staleRange.Code, staleRange.Body.String())
	}
	failedPrecondition := request(set, http.MethodGet, url, map[string]string{"If-Match": `"stale"`})
	if failedPrecondition.Code != http.StatusPreconditionFailed || failedPrecondition.Body.Len() != 0 {
		t.Fatalf("failed If-Match = %d %q", failedPrecondition.Code, failedPrecondition.Body.String())
	}
	unsatisfiedRange := request(set, http.MethodGet, url, map[string]string{"Range": "bytes=20-30"})
	if unsatisfiedRange.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("unsatisfied range = %d %q", unsatisfiedRange.Code, unsatisfiedRange.Body.String())
	}
	assertHeader(t, unsatisfiedRange, "Content-Range", "bytes */10")
}

func TestSetRejectsNonCanonicalAndUnknownRequests(t *testing.T) {
	set := mustSet(t, fstest.MapFS{
		"files/app.css":        &fstest.MapFile{Data: []byte("css")},
		"files/icons/logo.png": &fstest.MapFile{Data: []byte("png")},
	})
	tests := []struct {
		name   string
		method string
		url    string
		status int
	}{
		{name: "unknown", method: http.MethodGet, url: "/assets/missing.css", status: http.StatusNotFound},
		{name: "unknown head", method: http.MethodHead, url: "/assets/missing.css", status: http.StatusNotFound},
		{name: "directory", method: http.MethodGet, url: "/assets/icons", status: http.StatusNotFound},
		{name: "listing root", method: http.MethodGet, url: "/assets/", status: http.StatusNotFound},
		{name: "wrong prefix", method: http.MethodGet, url: "/asset/app.css", status: http.StatusNotFound},
		{name: "extra slash", method: http.MethodGet, url: "/assets//app.css", status: http.StatusNotFound},
		{name: "traversal", method: http.MethodGet, url: "/assets/../app.css", status: http.StatusNotFound},
		{name: "encoded slash", method: http.MethodGet, url: "/assets/icons%2Flogo.png", status: http.StatusNotFound},
		{name: "encoded backslash", method: http.MethodGet, url: "/assets/icons%5Clogo.png", status: http.StatusNotFound},
		{name: "query alias", method: http.MethodGet, url: "/assets/app.css?v=1", status: http.StatusNotFound},
		{name: "wrong case", method: http.MethodGet, url: "/assets/App.css", status: http.StatusNotFound},
		{name: "method", method: http.MethodPost, url: "/assets/app.css", status: http.StatusMethodNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := request(set, test.method, test.url, nil)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			assertHeader(t, response, "Cache-Control", "no-store")
			assertHeader(t, response, "Content-Type", "text/plain; charset=utf-8")
			assertHeader(t, response, "X-Content-Type-Options", "nosniff")
			if test.status == http.StatusMethodNotAllowed {
				assertHeader(t, response, "Allow", "GET, HEAD")
			}
			if test.method == http.MethodHead && response.Body.Len() != 0 {
				t.Fatalf("HEAD error body = %q", response.Body.String())
			}
		})
	}
}

func TestSetURLRejectsInvalidAndUnknownLogicalNames(t *testing.T) {
	set := mustSet(t, fstest.MapFS{"files/app.css": &fstest.MapFile{Data: []byte("css")}})
	for _, name := range []string{"", "/app.css", "../app.css", "a/../app.css", ".secret.css", "dir/.secret.css", `dir\app.css`, "app%2ecss", "app css"} {
		if _, err := set.URL(name); err == nil {
			t.Errorf("URL(%q) succeeded", name)
		}
	}
	if _, err := set.URL("missing.css"); err == nil || !strings.Contains(err.Error(), "unknown asset") {
		t.Fatalf("unknown URL error = %v", err)
	}
	var nilSet *asset.Set
	if _, err := nilSet.URL("app.css"); err == nil {
		t.Fatal("nil Set URL succeeded")
	}
}

func TestNewRejectsInvalidConfigurationAndRoot(t *testing.T) {
	valid := fstest.MapFS{"files/app.css": &fstest.MapFile{Data: []byte("css")}}
	tests := []struct {
		name   string
		files  fs.FS
		config asset.Config
	}{
		{name: "nil filesystem", config: asset.Config{Root: "files"}},
		{name: "missing root", files: valid, config: asset.Config{Root: "missing"}},
		{name: "file root", files: valid, config: asset.Config{Root: "files/app.css"}},
		{name: "traversing root", files: valid, config: asset.Config{Root: "../files"}},
		{name: "dot root", files: valid, config: asset.Config{Root: ".files"}},
		{name: "relative prefix", files: valid, config: asset.Config{Root: "files", URLPrefix: "assets"}},
		{name: "root prefix", files: valid, config: asset.Config{Root: "files", URLPrefix: "/"}},
		{name: "trailing prefix", files: valid, config: asset.Config{Root: "files", URLPrefix: "/assets/"}},
		{name: "unclean prefix", files: valid, config: asset.Config{Root: "files", URLPrefix: "/static/../assets"}},
		{name: "encoded prefix", files: valid, config: asset.Config{Root: "files", URLPrefix: "/%61ssets"}},
		{name: "negative count", files: valid, config: asset.Config{Root: "files", MaxFiles: -1}},
		{name: "negative file size", files: valid, config: asset.Config{Root: "files", MaxFileBytes: -1}},
		{name: "negative total size", files: valid, config: asset.Config{Root: "files", MaxTotalBytes: -1}},
		{name: "file exceeds total", files: valid, config: asset.Config{Root: "files", MaxFileBytes: 2, MaxTotalBytes: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := asset.New(test.files, test.config); err == nil {
				t.Fatal("New succeeded")
			}
		})
	}
}

func TestNewRejectsUnsafeAndAmbiguousInventory(t *testing.T) {
	tests := []struct {
		name  string
		files fstest.MapFS
	}{
		{name: "dotfile", files: fstest.MapFS{"files/.secret.css": &fstest.MapFile{Data: []byte("x")}}},
		{name: "dot directory", files: fstest.MapFS{"files/.private/app.css": &fstest.MapFile{Data: []byte("x")}}},
		{name: "space", files: fstest.MapFS{"files/app theme.css": &fstest.MapFile{Data: []byte("x")}}},
		{name: "html", files: fstest.MapFS{"files/index.html": &fstest.MapFile{Data: []byte("<script></script>")}}},
		{name: "svg", files: fstest.MapFS{"files/logo.svg": &fstest.MapFile{Data: []byte("<svg></svg>")}}},
		{name: "unknown type", files: fstest.MapFS{"files/data.bin": &fstest.MapFile{Data: []byte("x")}}},
		{name: "irregular", files: fstest.MapFS{"files/link.css": &fstest.MapFile{Data: []byte("x"), Mode: fs.ModeSymlink}}},
		{name: "file case collision", files: fstest.MapFS{
			"files/app.css": &fstest.MapFile{Data: []byte("a")},
			"files/APP.css": &fstest.MapFile{Data: []byte("b")},
		}},
		{name: "directory case collision", files: fstest.MapFS{
			"files/icons/a.png": &fstest.MapFile{Data: []byte("a")},
			"files/ICONS/b.png": &fstest.MapFile{Data: []byte("b")},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := asset.New(test.files, asset.Config{Root: "files"}); err == nil {
				t.Fatal("New succeeded")
			}
		})
	}
}

func TestNewEnforcesEveryInventoryLimit(t *testing.T) {
	tests := []struct {
		name   string
		files  fstest.MapFS
		config asset.Config
	}{
		{
			name: "count",
			files: fstest.MapFS{
				"files/a.css": &fstest.MapFile{Data: []byte("a")},
				"files/b.css": &fstest.MapFile{Data: []byte("b")},
			},
			config: asset.Config{Root: "files", MaxFiles: 1, MaxFileBytes: 1, MaxTotalBytes: 2},
		},
		{
			name:   "per file",
			files:  fstest.MapFS{"files/a.css": &fstest.MapFile{Data: []byte("ab")}},
			config: asset.Config{Root: "files", MaxFiles: 1, MaxFileBytes: 1, MaxTotalBytes: 2},
		},
		{
			name: "total",
			files: fstest.MapFS{
				"files/a.css": &fstest.MapFile{Data: []byte("ab")},
				"files/b.css": &fstest.MapFile{Data: []byte("cd")},
			},
			config: asset.Config{Root: "files", MaxFiles: 2, MaxFileBytes: 2, MaxTotalBytes: 3},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := asset.New(test.files, test.config); err == nil {
				t.Fatal("New succeeded")
			}
		})
	}
}

func TestNewUsesDeterministicAllowlistedMediaTypes(t *testing.T) {
	tests := map[string]string{
		"app.css":     "text/css; charset=utf-8",
		"app.js":      "text/javascript; charset=utf-8",
		"app.mjs":     "text/javascript; charset=utf-8",
		"data.json":   "application/json",
		"image.png":   "image/png",
		"font.woff2":  "font/woff2",
		"module.wasm": "application/wasm",
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			set := mustSet(t, fstest.MapFS{"files/" + name: &fstest.MapFile{Data: []byte("content")}})
			response := request(set, http.MethodGet, "/assets/"+name, nil)
			assertHeader(t, response, "Content-Type", want)
		})
	}
}

func mustSet(t *testing.T, files fs.FS) *asset.Set {
	t.Helper()
	set, err := asset.New(files, asset.Config{Root: "files", URLPrefix: "/assets"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return set
}

func request(handler http.Handler, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertHeader(t *testing.T, response *httptest.ResponseRecorder, name, want string) {
	t.Helper()
	if got := response.Header().Get(name); got != want {
		t.Fatalf("%s = %q, want %q", name, got, want)
	}
}
