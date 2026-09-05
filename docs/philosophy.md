# Philosophy and constraints

GoForge is optimized for teams moving from full-stack application frameworks,
not for replacing Go with a different language hidden inside Go.

## Opinionated defaults

A generated project has one unsurprising place for each concern: application
construction, configuration, controllers, requests, routes, and migrations.
Production middleware is installed by default. JSON rejects unknown fields and
large bodies. Shutdown is graceful. Generators refuse to overwrite files.

## Minimal runtime magic

The runtime does not scan packages, reflect over controller methods, synthesize
dependencies, or discover routes. The route table is executable Go. Constructors
show the object graph. Configuration fields are read explicitly. SQL migrations
are plain files and the database remains an exposed `*sql.DB`.

## Escape-hatch rule

Every GoForge abstraction must expose or accept its standard-library equivalent.
Today that means:

- `app.Handler()` returns `http.Handler`;
- `httpx.Adapt` accepts any `http.Handler`;
- `httpx.Standard` converts an individual framework handler;
- database helpers accept `*sql.DB` and `*sql.Tx`;
- middleware is plain function composition;
- generated files are owned by the application after creation.

New framework features should be rejected when they cannot meet this rule.

## Compile-time preference

When ergonomics require automation, GoForge prefers generating readable source
to interpreting metadata at runtime. Generated source must be formatted, safe to
edit, and replaceable with handwritten code. Runtime helpers should stay small
enough to understand in one sitting.
