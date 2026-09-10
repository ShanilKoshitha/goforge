package view_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ShanilKoshitha/goforge/view"
)

func TestEngineEscapesHTML(t *testing.T) {
	files := fstest.MapFS{"profile.html": {Data: []byte(`<h1>{{.Name}}</h1>`)}}
	engine, err := view.Parse(files, nil, "*.html")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	if err := engine.Render(response, http.StatusCreated, "profile.html", map[string]string{"Name": `<script>alert(1)</script>`}); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), "&lt;script&gt;") {
		t.Fatalf("unexpected response %d %s", response.Code, response.Body.String())
	}
	if got, want := response.Header().Get("Content-Length"), strconv.Itoa(response.Body.Len()); got != want {
		t.Fatalf("Content-Length = %q, want %q", got, want)
	}
}

func TestEngineDoesNotCommitPartialResponses(t *testing.T) {
	files := fstest.MapFS{"profile.html": {Data: []byte(`{{.Missing.Field}}`)}}
	engine, err := view.Parse(files, nil, "*.html")
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	err = engine.Render(response, http.StatusOK, "profile.html", nil)
	if err == nil {
		t.Fatal("expected render error")
	}
	if response.Body.Len() != 0 {
		t.Fatalf("expected empty response, got %q", response.Body.String())
	}
}
