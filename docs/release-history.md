# Release history

[Documentation home](index.md)

GoForge is pre-v1. CLI v0.23.1 generates format-17 projects pinned to runtime
v0.23.0. Existing applications keep their generated source when you install a
new CLI or update the runtime. See the [CLI compatibility notes](cli.md).

## Recent releases

| Release | Application changes |
| --- | --- |
| v0.23.0–v0.23.1 | v0.23.0 published the diagnostic runtime. v0.23.1 adopted it in format 17 and added read-only `forge doctor` and `forge routes:list` backed by application-owned Go. [Diagnostics](diagnostics.md) · [v0.23 acceptance](MILESTONE_0.23.md) |
| v0.22.0–v0.22.1 | Format 16 added typed list filters and sorting, and bounded, no-JavaScript search and pagination for relationship choices. [Resource browsing](building-an-app.md#browse-resources-and-select-relationships-at-scale) · [v0.22 acceptance](MILESTONE_0.22.md) |
| v0.21.0–v0.21.1 | v0.21.0 added compile-time component attributes, deterministic class forwarding, and explicit selection for multi-form views. v0.21.1 adopted the compiler in format 15 alongside accessible input, textarea, and select components and isolated form state. [View language](view-language.md) |
| v0.20 | Format 14 added named, expiring personal API tokens. Session-authenticated users manage them through JSON endpoints or the account security page. Bearer requests use the existing user, resource policy, and SQL scope. [Personal API tokens](api-tokens.md) |
| v0.19 | Extended format-13 `make:model` with typed fields and belongs-to. It supports domain models and SQL migrations in an empty app without generating HTTP endpoints. [Typed ORM](orm.md) |
| v0.18 | Format 13 added an application-owned resource authorization function shared by browser and JSON controllers. The repository applies the resulting scope directly to each SQL operation. [Resource guide](building-an-app.md#generate-an-authenticated-crud-resource) |
| v0.17 | Added application-owned embedded CSS and JavaScript, explicit asset routing, and the Forge `asset` helper. Format 12 uses the public asset runtime. [Ship frontend assets](building-an-app.md#ship-frontend-assets) |
| v0.16 | Added development browser reload on committed server replacements. v0.16.0 pinned the public v0.14.2 runtime. [CLI development loop](cli.md#development) |
| v0.15 | Added `forge dev` to supervise the watched server, durable worker, and recurring scheduler together. [CLI development loop](cli.md#development) |
| v0.14 | Added versioned recurring schedules with explicit cron and IANA time-zone semantics, bounded misfire handling, and PostgreSQL coordination. Follow-up releases hardened cancellation and registry validation and adopted the scheduler in format 11. [Recurring schedules](schedules.md) |
| v0.13 | Added required belongs-to resource generation with eager loading, bounded browser choices, and owner-safe PostgreSQL constraints. |
| v0.12 | Added browser and JSON password recovery with single-use reset tokens, session revocation, an encrypted durable mail outbox, and an explicit SMTP adapter. |
| v0.11 | Added typed scalar resource generation with required and nullable fields and optimistic versions on schema-driven updates. |
| v0.10 | Added `forge serve` with watched, health-checked server replacement and last-good behavior on invalid edits. |
| v0.9 | Added the `forge test` and `forge build` freshness gates. |
| v0.8 | Added password changes, transparent hash upgrades, immediate session revocation, and separate idle and absolute session lifetimes. |
| v0.7 | Added typed request validation, defined JSON and form error handling, bounded HTTP timeouts and headers, request logs, security policy, trusted proxies, and PostgreSQL auth throttling. |
| v0.6 | Added typed durable PostgreSQL jobs with transactional dispatch, retries, leases, and failed-job operations. |
| v0.5 | Added the positioned `.forge.html` compiler for components, props, slots, control directives, form helpers, and application-owned functions. |
| v0.4 | Added the typed Data Mapper, static Go-model parser, generated mappings, and explicit relationship loading. |
| v0.3 | Added server-rendered authentication, CSRF-protected forms, and browser CRUD alongside the JSON workflow. |
| v0.2 | Established explicit PostgreSQL wiring, database-backed sessions, embedded migrations, and owner-scoped JSON CRUD. |

Find tagged releases and installation artifacts on [GitHub Releases](https://github.com/ShanilKoshitha/goforge/releases).
The [scorecard](SCORECARD.md) records acceptance and public distribution
evidence by milestone.
