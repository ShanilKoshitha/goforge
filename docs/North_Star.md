# GoForge North Star

## Mission

GoForge gives developers the productivity and coherence of Laravel, Symfony,
Rails, and Spring while preserving the clarity, performance, and operational
simplicity of Go.

A developer should be able to move from an empty directory to a
production-shaped application in minutes without surrendering control of the
resulting code.

## Product promise

> Batteries included. Magic excluded.

GoForge provides an opinionated path for building complete Go applications.
The generated application remains ordinary, inspectable, editable Go.

The framework removes repetitive decisions and boilerplate. It does not conceal
the application behind runtime discovery, reflection-heavy machinery, or a
framework-specific language.

## Who it is for

GoForge is primarily for:

- Laravel, Symfony, Rails, Spring, and ASP.NET developers moving to Go.
- Product teams that want Go without designing an internal framework first.
- Go teams that value consistent application conventions and rapid delivery.
- Developers who want a complete default path with clean escape hatches.

GoForge is not optimized around convincing developers who prefer assembling
every application from unrelated libraries. They may still use individual
packages when useful.

## The experience we are building

A new developer should be able to:

1. Create an application.
2. Configure a database.
3. Generate an application resource.
4. Run migrations.
5. Start the development server.
6. Use authentication, validation, sessions, views, jobs, logging, and testing
   through one coherent workflow.
7. Build a production-ready binary.
8. Replace any framework component with ordinary Go when necessary.

The commands should feel cohesive:

```text
forge new issueboard
forge make:resource Issue
forge migrate
forge serve
forge test
forge build
```
