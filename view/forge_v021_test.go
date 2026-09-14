package view_test

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ShanilKoshitha/goforge/view"
)

func TestForgeV021AttributesRenderWithStaticNamesAndStandardEscaping(t *testing.T) {
	files := fstest.MapFS{
		"components/link.forge.html": {Data: []byte(`@props(label, url, note, disabled)
<a @attributes(class="button", class="", href=$url, title="", data-note=$note, disabled=$disabled, hidden=false)>{{$label}}</a>`)},
		"page.forge.html": {Data: []byte(`@component("components/link", label=.Label, url=.URL, note=.Note, disabled=.Disabled, attributes(class="button", class="wide"))@endcomponent`)},
	}

	compiled, err := view.CompileForge(files)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := view.ParseCompiled(compiled, nil)
	if err != nil {
		t.Fatal(err)
	}

	render := func(disabled bool) string {
		t.Helper()
		response := httptest.NewRecorder()
		data := struct {
			Label, URL, Note string
			Disabled         bool
		}{
			Label:    `Open <account>`,
			URL:      `javascript:alert("unsafe")`,
			Note:     `"><strong>unsafe & visible</strong>`,
			Disabled: disabled,
		}
		if err := engine.Render(response, http.StatusOK, "page", data); err != nil {
			t.Fatal(err)
		}
		return response.Body.String()
	}

	enabled := render(false)
	for _, expected := range []string{
		`class="button button wide"`,
		`href="#ZgotmplZ"`,
		`title=""`,
		`data-note="&#34;&gt;&lt;strong&gt;unsafe &amp; visible&lt;/strong&gt;"`,
		`Open &lt;account&gt;`,
	} {
		if !strings.Contains(enabled, expected) {
			t.Fatalf("rendered attributes lack %q:\n%s", expected, enabled)
		}
	}
	if strings.Contains(enabled, " disabled") || strings.Contains(enabled, " hidden") || strings.Contains(enabled, "<strong>") {
		t.Fatalf("false boolean or unsafe markup leaked into output:\n%s", enabled)
	}

	disabled := render(true)
	if !strings.Contains(disabled, ` disabled`) || strings.Contains(disabled, `disabled="`) {
		t.Fatalf("boolean attribute does not use presence semantics:\n%s", disabled)
	}
}

func TestForgeV021AttributesCaptureChangedDotOnceThroughTwoForwarders(t *testing.T) {
	files := fstest.MapFS{
		"components/leaf.forge.html":   {Data: []byte(`@props()<input @attributes(class="leaf")>`)},
		"components/middle.forge.html": {Data: []byte(`@props()@component("components/leaf", attributes(..., class="middle"))@endcomponent`)},
		"components/outer.forge.html":  {Data: []byte(`@props()@component("components/middle", attributes(..., class="outer"))@endcomponent`)},
		"page.forge.html":              {Data: []byte(`@with(.Scope)@component("components/outer", attributes(data-token=(touch .Token), class=(touch .Class)))@endcomponent@endwith`)},
	}
	counts := make(map[string]int)
	functions := template.FuncMap{
		"touch": func(value string) string {
			counts[value]++
			return value
		},
	}
	engine, err := view.ParseForge(files, functions)
	if err != nil {
		t.Fatal(err)
	}
	data := struct {
		Scope struct{ Token, Class string }
	}{}
	data.Scope.Token = "token-value"
	data.Scope.Class = "caller"
	response := httptest.NewRecorder()
	if err := engine.Render(response, http.StatusOK, "page", data); err != nil {
		t.Fatal(err)
	}
	if got := response.Body.String(); !strings.Contains(got, `class="leaf caller outer middle"`) || !strings.Contains(got, `data-token="token-value"`) {
		t.Fatalf("forwarded attributes = %q", got)
	}
	if counts["token-value"] != 1 || counts["caller"] != 1 {
		t.Fatalf("caller expressions were reevaluated while forwarding: %#v", counts)
	}
}

func TestForgeV021CompiledAttributesContainNoRuntimeBagOrTrustedAttributeType(t *testing.T) {
	compiled, err := view.CompileForge(fstest.MapFS{
		"components/card.forge.html": {Data: []byte(`@props(title)<article @attributes(class="card", data-title=$title)>@slot("default")@endslot</article>`)},
		"page.forge.html":            {Data: []byte(`@component("components/card", title=.Title, attributes(id=.ID, class=.Class)){{.Body}}@endcomponent`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := view.GoSource("views", compiled)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range compiled {
		for _, forbidden := range []string{"@attributes", "@component", "@props", "attributes(", "template.HTMLAttr", "HTMLAttr", "AttributeBag", "map["} {
			if strings.Contains(item.Source, forbidden) {
				t.Fatalf("compiled %s retains %q or runtime bag machinery:\n%s", item.Name, forbidden, item.Source)
			}
		}
	}
	for _, forbidden := range []string{"@attributes", "@component", "@props", "attributes(", "template.HTMLAttr", "HTMLAttr", "AttributeBag", "map["} {
		if strings.Contains(generated, forbidden) {
			t.Fatalf("generated Go retains %q or runtime bag machinery:\n%s", forbidden, generated)
		}
	}
}

func TestForgeV021AttributesOnlyClaimInvocationSyntax(t *testing.T) {
	compiled, err := view.CompileForge(fstest.MapFS{
		"page.forge.html": {Data: []byte(`<p>plain @attributes text|@@attributes(id="literal")</p>`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled) != 1 || compiled[0].Source != `<p>plain @attributes text|@attributes(id="literal")</p>` {
		t.Fatalf("literal compatibility changed: %#v", compiled)
	}
}

func TestForgeV021AttributesRejectMalformedDynamicAndUnsafeNames(t *testing.T) {
	tests := map[string]struct {
		emitter string
		call    string
		want    string
	}{
		"dynamic emitter name": {`@attributes(.Name="x")`, ``, "attribute name"},
		"dynamic caller name":  {`@attributes()`, `attributes(.Name="x")`, "attribute name"},
		"map spread":           {`@attributes()`, `attributes(.Values...)`, "spread"},
		"upper case":           {`@attributes(aria-Label="x")`, ``, "attribute name"},
		"leading hyphen":       {`@attributes(-name="x")`, ``, "attribute name"},
		"double hyphen":        {`@attributes(data--name="x")`, ``, "attribute name"},
		"event handler":        {`@attributes(onclick="run()")`, ``, `attribute "onclick" requires explicit trusted HTML source`},
		"style":                {`@attributes(style="display:none")`, ``, `attribute "style" requires explicit trusted HTML source`},
		"srcdoc":               {`@attributes(srcdoc="<p>x</p>")`, ``, `attribute "srcdoc" requires explicit trusted HTML source`},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			component := `@props()<div ` + test.emitter + `></div>`
			page := `@component("components/box"`
			if test.call != "" {
				page += `, ` + test.call
			}
			page += `)@endcomponent`
			assertForgeV021CompileError(t, fstest.MapFS{
				"components/box.forge.html": {Data: []byte(component)},
				"page.forge.html":           {Data: []byte(page)},
			}, test.want)
		})
	}
}

func TestForgeV021AttributesRejectDuplicateNonClassAcrossBoundaries(t *testing.T) {
	tests := map[string]fstest.MapFS{
		"emitter defaults": {
			"components/box.forge.html": {Data: []byte(`@props()<div @attributes(id="a", id="b")></div>`)},
			"page.forge.html":           {Data: []byte(`@component("components/box")@endcomponent`)},
		},
		"caller locals": {
			"components/box.forge.html": {Data: []byte(`@props()<div @attributes()></div>`)},
			"page.forge.html":           {Data: []byte(`@component("components/box", attributes(id="a", id="b"))@endcomponent`)},
		},
		"emitter and caller": {
			"components/box.forge.html": {Data: []byte(`@props()<div @attributes(id="default")></div>`)},
			"page.forge.html":           {Data: []byte(`@component("components/box", attributes(id="caller"))@endcomponent`)},
		},
		"forwarded and wrapper local": {
			"components/leaf.forge.html": {Data: []byte(`@props()<div @attributes()></div>`)},
			"components/box.forge.html":  {Data: []byte(`@props()@component("components/leaf", attributes(..., id="wrapper"))@endcomponent`)},
			"page.forge.html":            {Data: []byte(`@component("components/box", attributes(id="caller"))@endcomponent`)},
		},
		"forwarded and leaf default": {
			"components/leaf.forge.html": {Data: []byte(`@props()<div @attributes(id="leaf")></div>`)},
			"components/box.forge.html":  {Data: []byte(`@props()@component("components/leaf", attributes(...))@endcomponent`)},
			"page.forge.html":            {Data: []byte(`@component("components/box", attributes(id="caller"))@endcomponent`)},
		},
	}
	for name, files := range tests {
		t.Run(name, func(t *testing.T) {
			assertForgeV021CompileError(t, files, "duplicate", "attribute", `"id"`, "first declared at")
		})
	}
}

func TestForgeV021AttributesEnforceOneUnconditionalLexicalSink(t *testing.T) {
	tests := map[string]struct {
		files fstest.MapFS
		want  string
	}{
		"spread must be first": {
			files: v021AttributeFiles(`@props()@component("components/leaf", attributes(class="box", ...))@endcomponent`, `@component("components/box", attributes(id="x"))@endcomponent`),
			want:  "spread",
		},
		"spread appears once": {
			files: v021AttributeFiles(`@props()@component("components/leaf", attributes(..., ...))@endcomponent`, `@component("components/box", attributes(id="x"))@endcomponent`),
			want:  "spread",
		},
		"page cannot forward": {
			files: v021AttributeFiles(`@props()<div @attributes()></div>`, `@component("components/box", attributes(..., id="x"))@endcomponent`),
			want:  "attribute forwarding requires an enclosing component",
		},
		"emitter cannot spread": {
			files: v021AttributeFiles(`@props()<div @attributes(...)></div>`, `@component("components/box", attributes(id="x"))@endcomponent`),
			want:  "@attributes",
		},
		"conditional emitter": {
			files: v021AttributeFiles(`@props(show)@if($show)<div @attributes()></div>@endif`, `@component("components/box", show=true, attributes(id="x"))@endcomponent`),
			want:  "attribute sink must be unconditional",
		},
		"conditional forward": {
			files: v021AttributeFiles(`@props(show)@if($show)@component("components/leaf", attributes(...))@endcomponent@endif`, `@component("components/box", show=true, attributes(id="x"))@endcomponent`),
			want:  "attribute sink must be unconditional",
		},
		"two emitters": {
			files: v021AttributeFiles(`@props()<div @attributes()></div><aside @attributes()></aside>`, `@component("components/box", attributes(id="x"))@endcomponent`),
			want:  "more than one attribute sink",
		},
		"emitter and forward": {
			files: v021AttributeFiles(`@props()<div @attributes()></div>@component("components/leaf", attributes(...))@endcomponent`, `@component("components/box", attributes(id="x"))@endcomponent`),
			want:  "more than one attribute sink",
		},
		"fallback slot sink": {
			files: v021AttributeFiles(`@props()@slot("default")<div @attributes()></div>@endslot`, `@component("components/box", attributes(id="x"))@endcomponent`),
			want:  "attribute sink cannot appear in slot fallback content",
		},
		"errors emitter": {
			files: v021AttributeFiles(`@props()@errors("name")<div @attributes()></div>@enderrors`, `@component("components/box", attributes(id="x"))@endcomponent`),
			want:  "attribute sink must be unconditional",
		},
		"errors forward": {
			files: v021AttributeFiles(`@props()@errors("name")@component("components/leaf", attributes(...))@endcomponent@enderrors`, `@component("components/box", attributes(id="x"))@endcomponent`),
			want:  "attribute sink must be unconditional",
		},
		"incoming bag without sink": {
			files: v021AttributeFiles(`@props()<div>plain</div>`, `@component("components/box", attributes(id="x"))@endcomponent`),
			want:  "receives attributes but has no attribute sink",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assertForgeV021CompileError(t, test.files, test.want)
		})
	}

	// A component that is never given an attribute bag need not declare a sink.
	if _, err := view.CompileForge(v021AttributeFiles(`@props()<div>plain</div>`, `@component("components/box")@endcomponent`)); err != nil {
		t.Fatalf("component without incoming attributes should remain valid: %v", err)
	}
}

func TestForgeV021AttributesDoNotMaskNonComponentDiagnostics(t *testing.T) {
	_, err := view.CompileForge(fstest.MapFS{
		"partial.forge.html": {Data: []byte(`<p>not a component</p>`)},
		"page.forge.html":    {Data: []byte(`@component("partial", attributes(id="x"))@endcomponent`)},
	})
	if err == nil || !strings.Contains(err.Error(), `unknown component "partial"`) {
		t.Fatalf("non-component diagnostic = %v", err)
	}
}

func TestForgeV021AttributeErrorsPreserveSourcePositionAndComponentContext(t *testing.T) {
	t.Run("caller", func(t *testing.T) {
		_, err := view.CompileForge(fstest.MapFS{
			"components/box.forge.html": {Data: []byte(`@props()<div @attributes()></div>`)},
			"pages/show.forge.html":     {Data: []byte("first\n  @component(\"components/box\", attributes(onload=\"run()\"))@endcomponent")},
		})
		if err == nil || !strings.HasPrefix(err.Error(), "pages/show.forge.html:2:43:") || !strings.Contains(err.Error(), `attribute "onload" requires explicit trusted HTML source`) {
			t.Fatalf("caller diagnostic = %v", err)
		}
	})

	t.Run("component", func(t *testing.T) {
		_, err := view.CompileForge(fstest.MapFS{
			"components/box.forge.html": {Data: []byte("@props(show)\n@if($show)\n  <div @attributes()></div>\n@endif")},
			"pages/show.forge.html":     {Data: []byte(`@component("components/box", show=true, attributes(id="x"))@endcomponent`)},
		})
		if err == nil || !strings.HasPrefix(err.Error(), "components/box.forge.html:3:8:") || !strings.Contains(err.Error(), "attribute sink must be unconditional") || !strings.Contains(err.Error(), "component components/box") {
			t.Fatalf("component diagnostic = %v", err)
		}
	})
}

func TestForgeV021ExplicitFormsSurviveRangeWithAndComponentScope(t *testing.T) {
	files := fstest.MapFS{
		"components/field.forge.html": {Data: []byte(`@props(form)<input value='@old("name", "fallback", form=$form)'>@csrf(form=$form)@errors("name", form=$form)<i>{{.}}</i>@enderrors`)},
		"page.forge.html":             {Data: []byte(`@for(.Forms)@component("components/field", form=.)@endcomponent@endfor|@with(.TokenForm)@old("name", form=.)@errors("name", form=.)<b>{{.}}</b>@enderrors@endwith`)},
	}
	engine, err := view.ParseForge(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	data := struct {
		Forms     []view.Form
		TokenForm view.Form
	}{
		Forms: []view.Form{
			{CSRFToken: `first<&`, OldValues: map[string]string{"name": ""}, ValidationErrors: map[string][]string{"name": {"first <error>"}}},
			{CSRFToken: `second`, OldValues: map[string]string{}, ValidationErrors: map[string][]string{}},
		},
		TokenForm: view.Form{OldValues: map[string]string{"name": "token"}, ValidationErrors: map[string][]string{"name": {"token error"}}},
	}
	response := httptest.NewRecorder()
	if err := engine.Render(response, http.StatusUnprocessableEntity, "page", data); err != nil {
		t.Fatal(err)
	}
	got := response.Body.String()
	for _, expected := range []string{
		`<input value=''>`,
		`value="first&lt;&amp;"`,
		`<i>first &lt;error&gt;</i>`,
		`<input value='fallback'>`,
		`value="second"`,
		`|token<b>token error</b>`,
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("scoped forms lack %q:\n%s", expected, got)
		}
	}
}

func TestForgeV021ExplicitFormExpressionIsEvaluatedOncePerDirective(t *testing.T) {
	form := view.Form{
		CSRFToken:        "csrf",
		OldValues:        map[string]string{"name": "old"},
		ValidationErrors: map[string][]string{"name": {"one", "two"}},
	}
	calls := 0
	functions := template.FuncMap{
		"selected": func(candidate view.Form) view.Form {
			calls++
			return candidate
		},
	}
	engine, err := view.ParseForge(fstest.MapFS{
		"page.forge.html": {Data: []byte(`@csrf(form=(selected .Form))|@old("name", "fallback", form=(selected .Form))|@errors("name", form=(selected .Form))[{{.}}]@enderrors`)},
	}, functions)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	if err := engine.Render(response, http.StatusOK, "page", struct{ Form view.Form }{form}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("three selected form expressions evaluated %d times", calls)
	}
	if got := response.Body.String(); !strings.Contains(got, `value="csrf"`) || !strings.Contains(got, `|old|[one][two]`) {
		t.Fatalf("explicit form output = %q", got)
	}
}

func TestForgeV021LegacyFormExpansionIsByteForByteUnchanged(t *testing.T) {
	compiled, err := view.CompileForge(fstest.MapFS{
		"page.forge.html": {Data: []byte(`@csrf|@old("name")|@old("name", .Fallback)|@errors("name"){{.}}@enderrors`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `<input type="hidden" name="_token" value="{{.Form.CSRFToken}}">|{{.Form.Old "name"}}|{{.Form.Old "name" .Fallback}}|{{range .Form.Errors "name"}}{{.}}{{end}}`
	if len(compiled) != 1 || compiled[0].Source != want {
		t.Fatalf("legacy form expansion changed:\nwant: %s\n got: %s", want, compiled[0].Source)
	}
}

func TestForgeV021ExplicitFormArgumentMustBeNamedAndFinal(t *testing.T) {
	tests := map[string]struct {
		source string
		want   string
	}{
		"csrf empty":       {`@csrf(form=)`, "form expression is required"},
		"csrf unknown":     {`@csrf(source=.Form)`, `@csrf accepts only a final form= argument`},
		"old not final":    {`@old("name", form=.Form, .Fallback)`, "form= must be the final argument"},
		"old duplicate":    {`@old("name", form=.Form, form=.Other)`, "form= may appear only once"},
		"errors not final": {`@errors("name", form=.Form, "extra")x@enderrors`, "form= must be the final argument"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assertForgeV021CompileError(t, fstest.MapFS{
				"page.forge.html": {Data: []byte("before\n  " + test.source)},
			}, test.want)
		})
	}
}

func v021AttributeFiles(box, page string) fstest.MapFS {
	return fstest.MapFS{
		"components/leaf.forge.html": {Data: []byte(`@props()<span @attributes()></span>`)},
		"components/box.forge.html":  {Data: []byte(box)},
		"page.forge.html":            {Data: []byte(page)},
	}
}

func assertForgeV021CompileError(t *testing.T, files fstest.MapFS, fragments ...string) {
	t.Helper()
	_, err := view.CompileForge(files)
	if err == nil {
		t.Fatalf("expected compile error containing %q", fragments)
	}
	for _, fragment := range fragments {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("expected compile error containing %q, got %v", fragment, err)
		}
	}
}
