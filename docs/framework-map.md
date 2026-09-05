# Framework map

GoForge keeps familiar application-framework concepts while expressing them as
normal Go.

| Familiar concept | GoForge convention | What the compiler sees |
| --- | --- | --- |
| Artisan / Console / Rails CLI | `forge` generators | Newly created `.go` and `.sql` files |
| Route DSL / annotations | `routes.Register` | Direct method calls on `httpx.Router` |
| Controller action | Method accepting `*httpx.Context` | `func(*httpx.Context) error` |
| Form request / validator | Request struct with `Validate` | Explicit field rules and an error map |
| Service container | Constructor calls in `application.New` | An ordinary, navigable object graph |
| Environment configuration | Application `config.Load` | Explicit typed reads with joined errors |
| Middleware stack / filters | `router.Use` and route groups | Function composition in declared order |
| Named routes | `router.Named` and `router.URL` | Stored route metadata and escaped parameters |
| Active Record / ORM | Generated typed Data Mapper plus application repositories | Inspectable query/mapping Go over `database/sql`, with handwritten SQL and repository replacement preserved |
| Schema migrations | Paired up/down SQL files | PostgreSQL advisory-locked execution; database-specific transaction semantics |
| Cache | `cache.Store` | Four-method interface plus JSON helpers |
| Session / flash | `session.Manager` | Signed random ID and replaceable server store |
| Blade / Twig templates | `.forge.html` layouts, components, props, slots, stacks, control flow, and form directives | Managed `views_gen.go` containing canonical `html/template` source plus source mappings |
| Queues / Active Job / Spring Batch jobs | Generated typed jobs plus an explicit registry and `cmd/worker` | Application-owned handlers and worker wiring over inspectable PostgreSQL rows, leases, and ordinary Go interfaces |
| Browser request lifecycle | `web.Sessions`, `web.CSRF`, and explicit page data | Buffered `net/http` response plus signed server-side session |
| Test client | `httptest` against `app.Handler()` | Standard `http.Handler` testing |

The differences are intentional. GoForge does not reproduce annotation scanning,
runtime dependency injection, implicit model persistence, or hidden package
discovery. Those conveniences have high debugging and escape costs in Go. Where
automation helps, the CLI writes readable source that becomes application-owned.
