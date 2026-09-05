# GoForge view language

GoForge views use the `.forge.html` suffix. The compiler resolves structure at
development/build time and emits ordinary `html/template` source in
`resources/views/views_gen.go`. Production parses only that generated artifact.

Expressions are Go-template pipelines. GoForge does not add a second expression
or filter language:

```html
<h1>{{.Title}}</h1>
@if(.User.Admin)
  <a href="/admin">Administration</a>
@endif
```

The standard library performs contextual escaping after every layout,
component, slot, include, and stack has been composed. The compiler never
pre-renders fragments or converts them to trusted HTML.

## Layouts and includes

Layouts declare named insertion points:

```html
<!doctype html>
<title>@yield("title")</title>
<head>@stack("head")</head>
<body>@yield("content")</body>
```

A page supplies sections and page-local stack entries:

```html
@extends("layouts/app")
@section("title")Issues@endsection
@push("head")<meta name="description" content="Issue list">@endpush
@section("content")
  @include("partials/pager", .Pagination)
@endsection
```

Includes have static names and receive dot unless an explicit `.` or `$`
pipeline is supplied. Stack entries are deterministic and belong only to the
root page being compiled. A push cannot capture a runtime `@for` or `@with`
scope.

## Components, props, and slots

Component files live below `resources/views/components`. A component declares
its public props at the start of the file. Bare props are required; v0.5
defaults are quoted scalar literals:

```html
@props(title, tone="neutral")
<article class="card card--{{$tone}}">
  <header>{{$title}}</header>
  <div>@slot("default")@endslot</div>
  <footer>@slot("actions")@endslot</footer>
</article>
```

Callers use a static component name and named prop values. Unquoted values are
Go-template pipelines evaluated in caller scope:

```html
@component("components/card", title=.Issue.Name, tone="warning")
  <p>{{.Issue.Description}}</p>
  @slot("actions")
    <a href="/issues/{{.Issue.ID}}/edit">Edit</a>
  @endslot
@endcomponent
```

Content outside a named caller slot supplies the `default` slot. Components may
nest. The compiler rejects dynamic component names, recursion, missing or
unknown props, duplicate or unknown slots, and slot directives outside their
valid component context. Component prop variables are lexically scoped and
renamed during expansion so nested components cannot collide.

## Control flow

Control directives are thin structural forms over standard Go-template
pipelines:

```html
@if(.Notice)
  <p>{{.Notice}}</p>
@elseif(.Warning)
  <p>{{.Warning}}</p>
@else
  <p>No notices.</p>
@endif

@for(.Issues)
  <a href="/issues/{{.ID}}">{{.Name}}</a>
@empty
  <p>No issues yet.</p>
@endfor

@with(.User)
  <span>{{.Name}}</span>
@else
  <a href="/login">Sign in</a>
@endwith
```

Ordinary interpolation, variables, functions, and Go-template control actions
remain available. Raw `define`, `template`, and `block` actions are rejected in
Forge source because they bypass the static dependency graph; use `@include`, a
component, or the direct standard-library escape hatch instead. Write `@@` for
a literal `@`.

## Forms

Form directives read an explicit `.Form` value supplied by the controller. They
never inspect a request or global context:

```html
<form method="post" action="/issues/{{.Issue.ID}}">
  @csrf
  @method("PUT")
  <input name="name" value='@old("name", .Issue.Name)'>
  @errors("name")<p role="alert">{{.}}</p>@enderrors
</form>
```

`@csrf` emits the synchronizer-token field, and `@method` emits the existing
method-override field. `@old` returns a submitted value even when it is the
empty string; it uses the fallback only when the field was not submitted.
`@errors` iterates every message. All resulting values remain ordinary strings
subject to `html/template` contextual escaping.

## Application functions and escape hatches

Format-5 applications own `resources/views/functions.go`. Its
`template.FuncMap` is used by both the project view compiler and the production
renderer. Adding a function therefore changes ordinary application code, and a
missing function fails before a stale artifact is published.

`forge views:compile` refreshes the generated artifact. The `--check` form never
writes: it succeeds only when sources, mappings, and generated Go are current.

Applications can always use raw Go-template actions inside a Forge view, parse
ordinary `.html` through `view.Parse`, configure `Engine.Templates`, or replace
the renderer with direct `html/template`. These are supported boundaries, not
implementation accidents.
