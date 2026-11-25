# Framework packages

GoForge's public packages can be used from generated projects or from your own
Go code. Package paths below are relative to
`github.com/ShanilKoshitha/goforge`. See the [framework map](framework-map.md)
for where these concepts fit in an application.

## HTTP, configuration, and views

| Package | What it provides |
| --- | --- |
| [`forge`](../app.go) | Application lifecycle and graceful shutdown. Import the module root. |
| [`httpx`](../httpx/) | Routing, JSON binding, errors, middleware, and `net/http` adapters. |
| [`config`](../config/) | Typed environment configuration and optional dotenv loading. |
| [`validation`](../validation/) | Composable validation rules and field errors. |
| [`view`](../view/) | `.forge.html` compilation to inspectable, contextually escaped `html/template` source. |
| [`asset`](../asset/) | Embedded files, canonical URLs, strong ETags, byte ranges, and deterministic media policy through `http.Handler`. |
| [`web`](../web/) | Buffered browser sessions, CSRF middleware, flash messages, and redirects. |
| [`session`](../session/) | Signed-ID server-side sessions with flash data and replaceable storage. |

See [Building an application](building-an-app.md) and the
[Forge view language](view-language.md) for integration examples.

## Data and security

| Package | What it provides |
| --- | --- |
| [`cache`](../cache/) | Cache contracts, typed JSON helpers, and an in-memory store. |
| [`database`](../database/) | Transaction helper over `database/sql`. |
| [`database/migrate`](../database/migrate/) | Paired SQL migrations. PostgreSQL runs use advisory locks and transactions for transaction-safe SQL. MySQL DDL can commit implicitly. |
| [`orm`](../orm/) | Typed PostgreSQL builders and mappings, partial fields, optimistic guards, bulk inserts, and explicit batched relationships over `*sql.DB` or `*sql.Tx`. |
| [`orm/postgres`](../orm/postgres/) | Disclosure-safe PostgreSQL constraint and retryable-error classification that preserves native causes. |
| [`security/csrf`](../security/csrf/) | Session-backed CSRF tokens. |
| [`security/password`](../security/password/) | Password hashing and verification, including upgrade detection. |
| [`security/ratelimit`](../security/ratelimit/) | Bounded authentication throttling, trusted-proxy parsing, and a replaceable store contract with a PostgreSQL adapter. |
| [`security/token`](../security/token/) | Opaque personal API token issuance, parsing, and digest verification. |

See the [typed ORM guide](orm.md) and [personal API token guide](api-tokens.md)
for application-level patterns.

## Background work and mail

| Package | What it provides |
| --- | --- |
| [`job`](../job/) | Typed job definitions, explicit registries, dispatch and retry policies, at-least-once workers, and replaceable stores and observers. |
| [`job/postgres`](../job/postgres/) | Durable queue storage with database-clock claims, `SKIP LOCKED`, generation-fenced leases, and failed-job operations. |
| [`schedule`](../schedule/) | Versioned cron definitions and recurring dispatch of typed jobs. |
| [`schedule/postgres`](../schedule/postgres/) | PostgreSQL schedule cursor and coordination state. |
| [`mail`](../mail/) | Message validation and the sender interface. |
| [`mail/smtp`](../mail/smtp/) | Explicit SMTP delivery adapter. |

See [Durable jobs](jobs.md) and [Recurring schedules](schedules.md) for usage
and operating behavior.
