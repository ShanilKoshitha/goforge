package view_test

import (
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ShanilKoshitha/goforge/view"
)

func TestForgeV05CompositionCompilesAndEscapes(t *testing.T) {
	files := fstest.MapFS{
		"layouts/app.forge.html": {Data: []byte(`<!doctype html><title>@yield("title")</title><head>@stack("head")</head><body>@yield("content")</body>`)},
		"components/card.forge.html": {Data: []byte(`@props(title, tone="neutral")
<article class="card {{$tone}}"><h2>{{$title}}</h2><div>@slot("default")fallback@endslot</div><footer>@slot("actions")none@endslot</footer></article>`)},
		"pages/home.forge.html": {Data: []byte(`@extends("layouts/app")
@push("head")<meta name="description" content="{{.Description}}">@endpush
@section("title"){{.Title}}@endsection
@section("content")
@component("components/card", title=.Title, tone="warning")
@if(.Show)<p>{{.Unsafe}}</p>@elseif(.Alternate)<p>alternate</p>@else<p>hidden</p>@endif
@slot("actions")@for(.Links)<a href="{{.URL}}">{{.Label}}</a>@empty<span>none</span>@endfor@endslot
@endcomponent
@with(.User)<b>{{.}}</b>@else<i>guest</i>@endwith
<form>@csrf@method("PUT")<input name="name" value='@old("name", .Title)'>@errors("name")<p>{{.}}</p>@enderrors</form>
@@forge
@endsection`)},
	}
	compiled, err := view.CompileForge(files)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range compiled {
		if strings.HasPrefix(item.Name, "components/") {
			t.Fatalf("component leaked into runtime templates: %q", item.Name)
		}
		for _, directive := range []string{"@component(", "@slot(", "@if(", "@for(", "@csrf", "@push(", "@stack("} {
			if strings.Contains(item.Source, directive) {
				t.Fatalf("%s remained in compiled %s:\n%s", directive, item.Name, item.Source)
			}
		}
	}
	engine, err := view.ParseCompiled(compiled, nil)
	if err != nil {
		t.Fatal(err)
	}
	type link struct{ URL, Label string }
	page := struct {
		Title, Description, Unsafe string
		Show, Alternate            bool
		Links                      []link
		User                       string
		Form                       view.Form
	}{
		Title: "Issues", Description: `\"><script>alert(1)</script>`, Unsafe: `<script>alert(2)</script>`, Show: true,
		Links: []link{{URL: "javascript:alert(3)", Label: "Edit & save"}}, User: "Ada <admin>",
		Form: view.Form{CSRFToken: `\"><img src=x>`, OldValues: map[string]string{"name": ""}, ValidationErrors: map[string][]string{"name": {"<required>"}}},
	}
	response := httptest.NewRecorder()
	if err := engine.Render(response, http.StatusOK, "pages/home", page); err != nil {
		t.Fatal(err)
	}
	body := response.Body.String()
	for _, expected := range []string{
		`class="card warning"`, `&lt;script&gt;alert(2)&lt;/script&gt;`, `href="#ZgotmplZ"`, `Edit &amp; save`,
		`Ada &lt;admin&gt;`, `name="_method" value="PUT"`, `&lt;required&gt;`, `value=''`, `@forge`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("rendered body lacks %q:\n%s", expected, body)
		}
	}
	if strings.Contains(body, "fallback") || strings.Contains(body, "none") || strings.Contains(body, "<script>") || strings.Contains(body, "value='Issues'") {
		t.Fatalf("unexpected fallback or unsafe output:\n%s", body)
	}
}

func TestForgeV05FormOldUsesFallbackOnlyWhenAbsent(t *testing.T) {
	compiled, err := view.CompileForge(fstest.MapFS{"page.forge.html": {Data: []byte(`@old("empty", "fallback")|@old("absent", "fallback")`)}})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := view.ParseCompiled(compiled, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	data := struct{ Form view.Form }{Form: view.Form{OldValues: map[string]string{"empty": ""}}}
	if err := engine.Render(response, http.StatusOK, "page", data); err != nil {
		t.Fatal(err)
	}
	if got := response.Body.String(); got != "|fallback" {
		t.Fatalf("old values = %q", got)
	}
}

func TestForgeV05ComponentsAreStrictAndLexicallyScoped(t *testing.T) {
	base := fstest.MapFS{
		"components/label.forge.html": {Data: []byte(`@props(title, tone="plain")<span class="{{$tone}}">{{$title}}:@slot("default")@endslot</span>`)},
		"components/panel.forge.html": {Data: []byte(`@props(title)<div>{{$title}}@component("components/label", title=$title){{$title}}@endcomponent|{{$title}}@slot("default")@endslot</div>`)},
		"page.forge.html":             {Data: []byte(`@component("components/panel", title=.Title){{.Body}}@endcomponent`)},
	}
	compiled, err := view.CompileForge(base)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := view.ParseCompiled(compiled, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	if err := engine.Render(response, http.StatusOK, "page", struct{ Title, Body string }{"outer", "body"}); err != nil {
		t.Fatal(err)
	}
	if got := response.Body.String(); !strings.Contains(got, `<span class="plain">outer:outer</span>|outerbody`) {
		t.Fatalf("lexical component render = %q", got)
	}

	tests := map[string]struct{ component, page, want string }{
		"missing prop":        {`@props(title){{$title}}`, `@component("components/card")@endcomponent`, `requires prop "title"`},
		"unknown prop":        {`@props(title="ok"){{$title}}`, `@component("components/card", extra="x")@endcomponent`, `has no prop "extra"`},
		"duplicate call prop": {`@props(title){{$title}}`, `@component("components/card", title="a", title="b")@endcomponent`, `duplicate component prop "title"`},
		"unknown slot":        {`@props()@slot("default")@endslot`, `@component("components/card")@slot("actions")x@endslot@endcomponent`, `has no slot "actions"`},
		"duplicate slot":      {`@props()@slot("default")a@endslot@slot("default")b@endslot`, `@component("components/card")@endcomponent`, `declares slot "default" more than once`},
		"cycle":               {`@props()@component("components/card")@endcomponent`, `@component("components/card")@endcomponent`, `component cycle`},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := view.CompileForge(fstest.MapFS{
				"components/card.forge.html": {Data: []byte(test.component)},
				"page.forge.html":            {Data: []byte(test.page)},
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestForgeV05RejectsSectionYieldCycles(t *testing.T) {
	_, err := view.CompileForge(fstest.MapFS{
		"layout.forge.html": {Data: []byte(`@yield("content")`)},
		"page.forge.html":   {Data: []byte(`@extends("layout")@section("content")@yield("content")@endsection`)},
	})
	if err == nil || !strings.Contains(err.Error(), "section yield cycle") {
		t.Fatalf("expected section cycle, got %v", err)
	}
}

func TestForgeV05PositionedErrorsAndRawActionBoundary(t *testing.T) {
	tests := map[string]struct{ source, want string }{
		"mismatched": {"line one\n@if(.Ready)\nhello\n@endfor", "page.forge.html:4:1"},
		"balanced":   {`@if(missing .Value (printf "%s" "("))ok@endif`, "function \"missing\" not defined"},
		"block":      {"first\n{{block \"hidden\" .}}x{{end}}", "page.forge.html:2:1"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			compiled, err := view.CompileForge(fstest.MapFS{"page.forge.html": {Data: []byte(test.source)}})
			if err == nil && name == "balanced" {
				_, err = view.ParseCompiled(compiled, nil)
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q, got %v", test.want, err)
			}
		})
	}
}

func TestForgeV05RejectsRuntimeScopedPush(t *testing.T) {
	for _, source := range []string{
		`@if(.Ready)@push("head")x@endpush@endif`,
		`@for(.Items)@push("head")x@endpush@endfor`,
		`@with(.User)@push("head")x@endpush@endwith`,
	} {
		_, err := view.CompileForge(fstest.MapFS{"page.forge.html": {Data: []byte(source)}})
		if err == nil || !strings.Contains(err.Error(), "cannot capture runtime") {
			t.Fatalf("expected scoped push rejection, got %v", err)
		}
	}
}

func TestForgeV05GoSourceIsDeterministicAndValid(t *testing.T) {
	files := fstest.MapFS{"page.forge.html": {Data: []byte("Hello {{.Name}}")}}
	first, err := view.CompileForge(files)
	if err != nil {
		t.Fatal(err)
	}
	second, err := view.CompileForge(files)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) || len(first[0].Mappings) == 0 {
		t.Fatal("compilation or source mappings are not deterministic")
	}
	firstSource, err := view.GoSource("views", first)
	if err != nil {
		t.Fatal(err)
	}
	secondSource, err := view.GoSource("views", second)
	if err != nil {
		t.Fatal(err)
	}
	if firstSource != secondSource || !strings.Contains(firstSource, "Mappings:") {
		t.Fatal("generated Go source is not deterministic or lacks mappings")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "views_gen.go", firstSource, parser.AllErrors); err != nil {
		t.Fatalf("generated Go is invalid: %v\n%s", err, firstSource)
	}
}

func TestForgeV05RenderErrorMapsToOriginalSource(t *testing.T) {
	files := fstest.MapFS{
		"components/card.forge.html": {Data: []byte("@props()\n<div>{{.Missing}}</div>")},
		"page.forge.html":            {Data: []byte("@component(\"components/card\")@endcomponent")},
	}
	engine, err := view.ParseForge(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	err = engine.Render(response, http.StatusOK, "page", struct{}{})
	if err == nil || !strings.Contains(err.Error(), "components/card.forge.html:2:7:") || !strings.Contains(err.Error(), "component components/card") {
		t.Fatalf("expected mapped component error, got %v", err)
	}
	if response.Body.Len() != 0 {
		t.Fatal("render error committed a partial response")
	}
}

func TestForgeV05MappingsPreserveOffsetsAndClampSyntheticDirectives(t *testing.T) {
	source := "αβ\nplain\n{{.Missing}}"
	compiled, err := view.CompileForge(fstest.MapFS{"unicode.forge.html": {Data: []byte(source)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled) != 1 || len(compiled[0].Mappings) != 2 {
		t.Fatalf("mappings = %#v", compiled)
	}
	textMapping, actionMapping := compiled[0].Mappings[0], compiled[0].Mappings[1]
	if textMapping.SourceStart != 0 || textMapping.Synthetic || actionMapping.SourceStart != len("αβ\nplain\n") || actionMapping.Line != 3 || actionMapping.Column != 1 || actionMapping.Synthetic {
		t.Fatalf("unexpected literal mappings: %#v", compiled[0].Mappings)
	}

	oldSource := "α\n  @old(\"name\")"
	oldEngine, err := view.ParseForge(fstest.MapFS{"form.forge.html": {Data: []byte(oldSource)}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	err = oldEngine.Render(response, http.StatusOK, "form", struct{}{})
	if err == nil || !strings.HasPrefix(err.Error(), "form.forge.html:2:3:") {
		t.Fatalf("synthetic directive did not clamp to its UTF-8 source position: %v", err)
	}
	if response.Body.Len() != 0 {
		t.Fatal("synthetic directive failure committed a response")
	}

	unicodeEngine, err := view.ParseForge(fstest.MapFS{
		"unicode-runtime.forge.html": {Data: []byte("first\nαβ {{.Missing}}")},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	err = unicodeEngine.Render(response, http.StatusOK, "unicode-runtime", struct{}{})
	if err == nil || !strings.HasPrefix(err.Error(), "unicode-runtime.forge.html:2:5:") {
		t.Fatalf("multiline UTF-8 literal mapping is not exact: %v", err)
	}
}

func TestForgeV05DiagnosticsPreserveLayoutAndIncludeChains(t *testing.T) {
	t.Run("compile through layout", func(t *testing.T) {
		_, err := view.CompileForge(fstest.MapFS{
			"layouts/app.forge.html": {Data: []byte("<head>\n  @include(\"partials/missing\", .)\n</head>@yield(\"content\")")},
			"pages/home.forge.html":  {Data: []byte(`@extends("layouts/app")@section("content")ok@endsection`)},
		})
		if err == nil || !strings.HasPrefix(err.Error(), "layouts/app.forge.html:2:3:") || !strings.Contains(err.Error(), "view pages/home -> layout layouts/app") {
			t.Fatalf("layout compile diagnostic = %v", err)
		}
	})

	t.Run("layout", func(t *testing.T) {
		engine, err := view.ParseForge(fstest.MapFS{
			"layouts/app.forge.html": {Data: []byte("<html>\n<body>{{.Missing}}</body>@yield(\"content\")")},
			"pages/home.forge.html":  {Data: []byte(`@extends("layouts/app")@section("content")ok@endsection`)},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		err = engine.Render(response, http.StatusOK, "pages/home", struct{}{})
		if err == nil || !strings.HasPrefix(err.Error(), "layouts/app.forge.html:2:8:") || !strings.Contains(err.Error(), "view pages/home -> layout layouts/app") {
			t.Fatalf("layout diagnostic = %v", err)
		}
	})

	t.Run("nested include render", func(t *testing.T) {
		engine, err := view.ParseForge(fstest.MapFS{
			"pages/home.forge.html":     {Data: []byte(`@include("partials/outer", .)`)},
			"partials/outer.forge.html": {Data: []byte("outer\n@include(\"partials/inner\", .)")},
			"partials/inner.forge.html": {Data: []byte("inner\n  {{.Missing}}")},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		err = engine.Render(response, http.StatusOK, "pages/home", struct{}{})
		if err == nil || !strings.HasPrefix(err.Error(), "partials/inner.forge.html:2:4:") || !strings.Contains(err.Error(), "view pages/home -> include partials/outer -> include partials/inner") {
			t.Fatalf("include render diagnostic = %v", err)
		}
	})

	t.Run("include from layout render", func(t *testing.T) {
		engine, err := view.ParseForge(fstest.MapFS{
			"layouts/app.forge.html":  {Data: []byte(`@include("partials/nav", .)@yield("content")`)},
			"pages/home.forge.html":   {Data: []byte(`@extends("layouts/app")@section("content")ok@endsection`)},
			"partials/nav.forge.html": {Data: []byte("nav\n {{.Missing}}")},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		err = engine.Render(response, http.StatusOK, "pages/home", struct{}{})
		if err == nil || !strings.HasPrefix(err.Error(), "partials/nav.forge.html:2:3:") || !strings.Contains(err.Error(), "view pages/home -> layout layouts/app -> include partials/nav") {
			t.Fatalf("layout include render diagnostic = %v", err)
		}
	})

	t.Run("nested include parse", func(t *testing.T) {
		compiled, err := view.CompileForge(fstest.MapFS{
			"pages/home.forge.html":     {Data: []byte(`@include("partials/outer", .)`)},
			"partials/outer.forge.html": {Data: []byte(`@include("partials/inner", .)`)},
			"partials/inner.forge.html": {Data: []byte("α\n {{missing .}}")},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = view.ParseCompiled(compiled, nil)
		if err == nil || !strings.HasPrefix(err.Error(), "partials/inner.forge.html:2:") || !strings.Contains(err.Error(), "view pages/home -> include partials/outer -> include partials/inner") {
			t.Fatalf("include parse diagnostic = %v", err)
		}
	})

	t.Run("include from layout parse", func(t *testing.T) {
		compiled, err := view.CompileForge(fstest.MapFS{
			"layouts/app.forge.html":  {Data: []byte(`@include("partials/nav", .)@yield("content")`)},
			"pages/home.forge.html":   {Data: []byte(`@extends("layouts/app")@section("content")ok@endsection`)},
			"partials/nav.forge.html": {Data: []byte(`{{missing .}}`)},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = view.ParseCompiled(compiled, nil)
		if err == nil || !strings.Contains(err.Error(), "view pages/home -> layout layouts/app -> include partials/nav") {
			t.Fatalf("layout include parse diagnostic = %v", err)
		}
	})
}

func TestForgeV05DiagnosticsPreserveComponentSlotSources(t *testing.T) {
	component := `@props()
<main>@slot("default")@endslot|@slot("actions")@endslot</main>`
	tests := map[string]struct {
		page   string
		prefix string
		slot   string
	}{
		"default": {"@component(\"components/card\")\n  {{.Missing}}\n@endcomponent", "pages/default.forge.html:2:4:", "slot default"},
		"named":   {"@component(\"components/card\")\nok\n@slot(\"actions\")\n    {{.Missing}}\n@endslot\n@endcomponent", "pages/named.forge.html:4:6:", "slot actions"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			pagePath := "pages/" + name + ".forge.html"
			engine, err := view.ParseForge(fstest.MapFS{
				"components/card.forge.html": {Data: []byte(component)},
				pagePath:                     {Data: []byte(test.page)},
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			err = engine.Render(response, http.StatusOK, "pages/"+name, struct{}{})
			if err == nil || !strings.HasPrefix(err.Error(), test.prefix) || !strings.Contains(err.Error(), "component components/card -> "+test.slot) {
				t.Fatalf("slot diagnostic = %v", err)
			}
		})
	}

	t.Run("nested default and named slots", func(t *testing.T) {
		page := `@component("components/outer")
@component("components/inner")
@slot("actions")
    {{.Missing}}
@endslot
@endcomponent
@endcomponent`
		engine, err := view.ParseForge(fstest.MapFS{
			"components/outer.forge.html": {Data: []byte(`@props()<section>@slot("default")@endslot</section>`)},
			"components/inner.forge.html": {Data: []byte(`@props()<aside>@slot("actions")@endslot</aside>`)},
			"pages/nested.forge.html":     {Data: []byte(page)},
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		err = engine.Render(response, http.StatusOK, "pages/nested", struct{}{})
		wantChain := "component components/outer -> slot default -> component components/inner -> slot actions"
		if err == nil || !strings.HasPrefix(err.Error(), "pages/nested.forge.html:4:6:") || !strings.Contains(err.Error(), wantChain) {
			t.Fatalf("nested slot diagnostic = %v", err)
		}
	})
}
