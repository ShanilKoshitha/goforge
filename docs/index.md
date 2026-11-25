# GoForge documentation

GoForge projects are ordinary Go applications. Start with the CLI, then follow
the generated source as you add routes, data, views, and background work.

## Get started

- [Quick Start](quick-start.md) walks through installing Forge, running an app
  locally, and creating your first record.
- [Building an application](building-an-app.md) shows how to implement
  endpoints and resources, work with forms and authentication, and test an app.
- [Framework map](framework-map.md) maps concepts from other frameworks to
  their GoForge equivalents.

## Work on a feature

| If you want to… | Read |
| --- | --- |
| Write typed PostgreSQL queries or model relationships | [Typed ORM](orm.md) |
| Build layouts, components, or forms | [Forge view language](view-language.md) |
| Give an API client its own credentials | [Personal API tokens](api-tokens.md) |
| Run work asynchronously | [Durable jobs](jobs.md) |
| Run a job on a recurring schedule | [Recurring schedules](schedules.md) |
| Diagnose startup or inspect the registered routes | [Diagnostics](diagnostics.md) |

## Reference

- [Forge CLI](cli.md) covers command syntax, the development loop, and project
  format compatibility.
- [Framework packages](packages.md) maps runtime responsibilities to Go packages.
- [Design philosophy](philosophy.md) explains the framework's design choices.

## Project information

- [Release history](release-history.md) covers published changes and links to
  qualification evidence.
- [Roadmap](ROADMAP.md) covers proposed work.
- [Contributing](../CONTRIBUTING.md) covers framework development and verification.
- [Code quality](CODE_QUALITY.md) records the repository assessment.
