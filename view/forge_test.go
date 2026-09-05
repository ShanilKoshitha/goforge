package view_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ShanilKoshitha/goforge/view"
)

func TestForgeTemplatesCompileLayoutsIncludesAndEscaping(t *testing.T) {
	files := fstest.MapFS{
		"layouts/app.forge.html":     {Data: []byte(`<!doctype html><title>@yield("title")</title><body>@include("partials/banner", .Banner)@yield("content")</body>`)},
		"partials/banner.forge.html": {Data: []byte(`<aside>{{.}}</aside>`)},
		"pages/home.forge.html": {Data: []byte(`@extends("layouts/app")
@section("title")Home@endsection
@section("content")<h1>{{.Name}}</h1>@endsection`)},
	}
	engine, err := view.ParseForge(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	err = engine.Render(response, http.StatusOK, "pages/home", map[string]string{
		"Name": "<Ada>", "Banner": "Ship & learn",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `<!doctype html><title>Home</title><body><aside>Ship &amp; learn</aside><h1>&lt;Ada&gt;</h1></body>`
	if response.Body.String() != want {
		t.Fatalf("unexpected rendered page:\n%s", response.Body.String())
	}

	compiled, err := view.CompileForge(files)
	if err != nil {
		t.Fatal(err)
	}
	var home string
	for _, item := range compiled {
		if item.Name == "pages/home" {
			home = item.Source
		}
	}
	if !strings.Contains(home, `{{template "partials/banner" .Banner}}`) || strings.Contains(home, "@extends") {
		t.Fatalf("compiler output is not canonical and inspectable:\n%s", home)
	}
}

func TestForgeCompilerReportsInvalidDependencyGraphs(t *testing.T) {
	tests := map[string]struct {
		files fs.FS
		want  string
	}{
		"unknown layout": {
			files: fstest.MapFS{"page.forge.html": {Data: []byte(`@extends("layouts/missing")
@section("content")Hello@endsection`)}},
			want: `unknown layout "layouts/missing"`,
		},
		"unknown include": {
			files: fstest.MapFS{"page.forge.html": {Data: []byte(`@include("missing")`)}},
			want:  `unknown include "missing"`,
		},
		"inheritance cycle": {
			files: fstest.MapFS{
				"a.forge.html": {Data: []byte(`@extends("b")`)},
				"b.forge.html": {Data: []byte(`@extends("a")`)},
			},
			want: "template inheritance cycle",
		},
		"include cycle": {
			files: fstest.MapFS{
				"a.forge.html": {Data: []byte(`@include("b")`)},
				"b.forge.html": {Data: []byte(`@include("a")`)},
			},
			want: "template include cycle",
		},
		"duplicate section": {
			files: fstest.MapFS{
				"layout.forge.html": {Data: []byte(`@yield("content")`)},
				"page.forge.html": {Data: []byte(`@extends("layout")
@section("content")First@endsection
@section("content")Second@endsection`)},
			},
			want: "duplicate section",
		},
		"missing section": {
			files: fstest.MapFS{
				"layout.forge.html": {Data: []byte(`@yield("title")@yield("content")`)},
				"page.forge.html": {Data: []byte(`@extends("layout")
@section("content")Hello@endsection`)},
			},
			want: `requires section "title"`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := view.CompileForge(test.files)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestParseCompiledRejectsDuplicateNames(t *testing.T) {
	_, err := view.ParseCompiled([]view.CompiledTemplate{
		{Name: "home", Source: "first"},
		{Name: "home", Source: "second"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "duplicate compiled view") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestForgeCompilerResolvesTransitiveSections(t *testing.T) {
	files := fstest.MapFS{
		"base.forge.html": {Data: []byte(`@yield("content")`)},
		"mid.forge.html": {Data: []byte(`@extends("base")
@section("content")<main>@yield("body")</main>@endsection`)},
		"page.forge.html": {Data: []byte(`@extends("mid")
@section("body")Hello@endsection`)},
	}
	compiled, err := view.CompileForge(files)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range compiled {
		if item.Name == "page" && item.Source != "<main>Hello</main>" {
			t.Fatalf("transitive inheritance = %q", item.Source)
		}
	}
}

func TestForgeCompilerRejectsMissingNestedSectionOnConcretePage(t *testing.T) {
	files := fstest.MapFS{
		"base.forge.html": {Data: []byte(`@yield("content")`)},
		"mid.forge.html": {Data: []byte(`@extends("base")
@section("content")<main>@yield("body")</main>@endsection`)},
		"page.forge.html": {Data: []byte(`@extends("mid")`)},
	}
	_, err := view.CompileForge(files)
	if err == nil || !strings.Contains(err.Error(), `requires section "body"`) {
		t.Fatalf("expected nested section error, got %v", err)
	}
}

func TestForgeCompilerRejectsDirectTemplateActions(t *testing.T) {
	for _, action := range []string{`{{template "partial" .}}`, "{{ template `partial` .}}"} {
		_, err := view.CompileForge(fstest.MapFS{"page.forge.html": {Data: []byte(action)}})
		if err == nil || !strings.Contains(err.Error(), "use @include") {
			t.Fatalf("direct action %q error = %v", action, err)
		}
	}
}
