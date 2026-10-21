# v0.23 read-only diagnosis and inventory scorecard

Status: **baseline established; implementation in progress** — 2026-10-21.

Acceptance is defined in [MILESTONE_0.23.md](MILESTONE_0.23.md). No v0.23
capability is accepted or publicly released yet.

| Gate | Current evidence |
| --- | --- |
| Framework baseline | Configuration, migration, and router tests passed (0.484s, 0.529s, 0.771s); fresh-scaffold/workflow group passed (31.539s) |
| Public application baseline | Retained public no-replace format-16 app passed configuration/application/routes tests (0.581s, 2.018s, 1.027s), view freshness, and asset validity with readonly module mode |
| Scope and architecture | Reuse `Verify`, actual `Router.Routes()`, and existing artifact checks; never operational `Status`. Diagnostic summaries must not echo raw configuration, driver, or compiler errors |
| Runtime preparation | Safe typed `config.FieldError` drops supplied conversion values and causes; migration verification exposes pending, drift, ledger-upgrade, and invalid-source categories without writes |
| Runtime race verification | Lead config/migration race suites passed twice (1.544s / 1.602s); subsequent whitespace-source correction passed the migration race suite twice (1.721s), with vet/diff checks |
| Broad runtime regression | All 25 packages outside internal/cli passed native race tests with PostgreSQL configured, followed by whole-framework vet; this does not substitute for the complete CLI regression |
| Candidate application | Explicit future-format fixture passed generated tests and real offline doctor/inventory (14.161s lead, 14.452s independent). Strengthened resource/all-app-race/edited-route/artifact-failure group passed in 69.157s before final cache-isolation changes; not final acceptance |
| Real PostgreSQL nonmutation | Strengthened race journey passed all 11 states in 45.796s, zero skips. Both direct JSON checks and CLI orchestration preserve app files/directories, schema, ledger, all table rows, and sequences; persisted user/session and claimable-job sentinels prevent vacuous checks |
| Independent review | Caught incomplete/whitespace-only migration pairs, application-local caches, and first-use external temp-parent handling. Runtime source findings fixed; CLI cache-boundary changes remain under verification |
| Windows temporary-build correction | Controlled distinct-source probe failed all 20 managed and 20 direct runs at the broad user-cache root; ordinary Temp and dedicated `goforge/tmp` each passed 40 cold executions. Selected the dedicated external child; corrected candidate requalification passed, with no retry, assertion change, or process-tree weakening |
| Corrected repeated candidate qualification | Native race group passed in 288.661s with two fresh-scaffold, explicit candidate, 11-state PostgreSQL, and frozen-snapshot passes. Candidate all-app-race/offline/inventory/secret/artifact workflows took 51.65s and 59.30s; PostgreSQL journeys took 47.61s and 49.92s, no skips. Each journey preserves complete file/DB snapshots |
| Diagnostic unit boundaries | All diagnostic/privacy/protocol/cache/environment/cancellation unit tests passed twice under race (23.427s); explicit full-name regex coverage includes first-use external temp creation and the io.Copy output-cap regression |
| Compatibility and CI | Normal scaffold stays format 16 on public v0.22.0; candidate CLI reports 0.23.0. Added public v0.22.1 format-16 compatibility/nonmutation gate and repeated diagnostic PostgreSQL journey without removing prior groups |
| Complete framework/two CI passes, public runtime, adoption/distribution | Pending; the repeated candidate group is not complete milestone acceptance |

The first restricted-runner diagnostic tests failed same-parent temporary
scaffold publication with Windows Access denied; equivalent native tests passed
(11.112s). A later strengthened inventory assertion initially expected kebab
paths, while actual generated routes use underscores; the assertion was corrected
to the existing source contract, not by changing generated routes. The separate
native Go temporary-cleanup failure after cache isolation was reproduced in
ordinary direct executions too; cached compilation had hidden it. Dedicated
temporary-parent selection is the correction, not a passing retry.
The temporary probe source was removed. Cleanup removed only 41 validated
probe-owned compiler directories (44,275,155 bytes); these outputs are
regenerable, and unrelated work directories were untouched.

# v0.22 resource browsing and relationship selection scorecard

Status: **accepted** — v0.22.0 runtime and v0.22.1 format-16 adoption published; public distribution verified — 2026-10-21.

Acceptance is defined in [MILESTONE_0.22.md](MILESTONE_0.22.md). Both complete
final-content matrices and public v0.22.1 installation/no-replace application
qualification passed. This closes the resource-browsing milestone, not the
remaining [roadmap](ROADMAP.md).

| Gate | Current evidence |
| --- | --- |
| Runnable generator baseline | Native `go test ./internal/cli -run '^(TestFormatFifteenGeneratesAccessibleRecoverableResourceForms\|TestFormatsEightThroughFourteenRetainLegacyResourceGeneration)$' -count=1` passed in 4.709s |
| Runnable public application baseline | The public v0.21.1 CLI's no-replace format-15 `example.com/releasedv0211` application passed `go test ./... -count=1`; Category, Ticket, and legacy resource tests passed |
| Baseline scale gap confirmed | Format-15 generated repository tests explicitly expect `ErrAssociationChoicesOverflow` above 100 related rows; browser rendering maps that state to 409 |
| Runtime helper | Immutable public `v0.22.0` at `5efd243` passed both corrected matrices, exact-main CI, proxy/SumDB verification, isolated installer, and a no-replace format-15 application |
| Fresh public application | Public v0.22.1 downloaded through proxy/SumDB into a fresh cache, installed with the downloaded installer, and generated a no-replace format-16 app pinned to public v0.22.0: nullable Category, boolean-only Project, all-scalar/two-relationship Ticket, and LegacyIssue; artifact/module checks, ordinary/race tests, vet, and four process builds passed |
| Format-16 implementation | Actual CLI atomicity/one-shot regeneration tests passed (17.209s); explicit-format baseline/escape fixtures and activated fresh-scaffold contracts pass; first complete native scale journey passed (47.466s) |
| Legacy output | All 22 legacy/typed/related format 8–15 render digests match the frozen pre-change revision, normalizing checkout newlines only |
| Raw draft recovery | Actual HTML refresh-to-save tests pass for padded text, leading textarea newlines, CRLF, numeric/boolean/association lexemes, malformed values, and unchanged normalized persistence; the final gated fixture combined suite passed in 15.368s |
| PostgreSQL acceptance | Each final matrix passed all ten generated-app journeys twice: 20 top-level passes per matrix, no skips/failures/timeouts in that stage. All six first/middle/last text/ID saves passed per scale repetition (18.49s, 18.51s; 18.14s, 18.32s) |
| Complete native PostgreSQL race regression | `go test -race -count=1 -timeout 30m -json ./...` passed with PostgreSQL configured (`internal/cli` 1060.460s), followed by vet/diff checks; scale journey 36.09s, development workflow 66.60s. Unix-permission-only checks retain their Windows skips and run in Linux CI |
| Independent review and two complete passes | No unresolved P0–P2 issue; complete [push 39636604194](https://github.com/ShanilKoshitha/goforge/actions/runs/39636604194) and [PR 39636612761](https://github.com/ShanilKoshitha/goforge/actions/runs/39636612761) matrices passed, with actual checkout/tree and acceptance-log audits |

## Qualification history

The entries below preserve intermediate failures and pending gates at each
checkpoint; the accepted state and final evidence are recorded above and below.

Read-only data and developer-workflow inventories identified the existing typed
query, scope, selected-value, and version boundaries. The milestone preserves
plain HTML editing and application-owned Go, and requires explicit format gating
and mandatory runtime-first publication of the search API. See D050.

The acceptance contract passed adversarial review after resolving target-field
disclosure, implicit-save intents, exact filter/transport behavior, presentation
state, hostile input, and label-size limits. Independent runtime review found
no P0–P2 issue. These reviews do not substitute for the pending real database
and complete two-pass qualification gates.

Format 15 remained the default during runtime-first qualification and remains
the public v0.22.0 scaffold. This checkout now defaults to format 16 only after
public runtime verification. Earlier explicit-format fixtures used a local
runtime replacement; the candidate no-replace check below now proves the public
runtime dependency. Published v0.22.1 CLI installation remains pending. No later
roadmap milestone has started.

Independent cross-review found and corrected UUID-backed label storage and raw
draft-whitespace loss. Final focused review found no remaining P0–P2 issue.
Before activation, the complete native framework regression passed:
`go test ./... -count=1` (`internal/cli` 289.981s), plus `go vet ./...`.
That local database URL was unset, so real-PostgreSQL workflow tests skipped;
the embedded repository proof is separately compiled by the explicit-format
fixture. These checks do not claim whole-milestone acceptance or public adoption.

Runtime qualification correction: initial hosted runs
[39614399558](https://github.com/ShanilKoshitha/goforge/actions/runs/39614399558)
and [39614467640](https://github.com/ShanilKoshitha/goforge/actions/runs/39614467640)
passed framework race tests with PostgreSQL, vet/build, Windows, and historical
compatibility, but stopped at the current-scaffold step's stale `forge 0.21.1`
assertion against the v0.22.0 candidate. The workflow assertion is corrected;
a native unit test now checks it against the source version. These failed runs
are not counted as complete acceptance passes. Native full framework regression
on the candidate also passed (`internal/cli` 323.324s). Public runtime publication
remains pending two complete corrected CI passes.

Corrected runtime qualification passed both complete Linux/PostgreSQL and
Windows matrices at `5cf1222`:
[39618054326](https://github.com/ShanilKoshitha/goforge/actions/runs/39618054326)
and [39618060116](https://github.com/ShanilKoshitha/goforge/actions/runs/39618060116).
PR #41 merged at `5efd243`; its tree matches that tested head exactly. Exact-main
CI and runtime publication are still pending. This accepts no format-16 workflow.

Follow-up acceptance review found no P0/P1 production defect, but identified
missing executable evidence for CLI atomicity/source preservation, app-owned
replacement compilation, and saving first/middle/last choices. Those checks are
being added before adoption; a compiled PostgreSQL harness is not an executed
journey. Global help now documents both choice-label spellings and the explicit
disclosure boundary (focused regression passed in 4.419s). The candidate guide
clearly distinguishes unreleased format 16 from public format 15.

The textarea acceptance helper now models line-ending normalization and the
initial HTML newline rule; its focused regression and compiled future-format
fixture passed together in 13.064s. A separate browser check was attempted but
blocked by the browser URL security policy; no real-browser evidence is claimed.

An isolated loopback-only PostgreSQL 18 cluster now provides native acceptance
infrastructure without changing the host's existing service. `go test ./orm/...
-count=1` passed with the database configured (`orm/postgres` 3.122s), including
literal-search DB/Tx behavior. The actual CLI atomicity/source-preservation tests
compile and skip deliberately while format 16 is unsupported; they become active
automatically on adoption. Their existing focused companion tests passed in
6.965s. `go vet ./...` passed. None of this substitutes for the format-16 journey.

The explicit-format application-owned replacement proof passed in 19.157s:
ordinary Go storage implements the resource repository, custom query/controller
constructors are wired into the registered routes, a replacement Forge index is
compiled, custom HTTP behavior is asserted, and all generated app tests plus a
server build pass. The generated defaults remain tested too. This is a local
replacement seam proof, not public no-replace or full PostgreSQL acceptance.
The finalized proof uses human-owned route wiring and keeps the managed registry
byte-identical; baseline plus replacement checks passed together in 27.422s.

Checkpoint verification: complete native `go test ./... -count=1` passed
(`internal/cli` 298.079s; database-dependent tests skipped without the URL).
The finalized focused help/choice/replacement/future-format suite separately
passed in 29.099s, with only the two deliberately gated actual format-16 CLI
tests skipped. Final `go vet ./...`, trimmed CLI build/version inspection, and
`git diff --check` passed. Full format-16 acceptance is still pending.

Runtime publication: exact-main
[39621243237](https://github.com/ShanilKoshitha/goforge/actions/runs/39621243237)
passed its complete Linux/PostgreSQL and Windows matrix, including repeated
generated applications. Lightweight unsigned `v0.22.0` points permanently to
`5efd243e07ffdcff0570439c035dfe9a8b3b45d1`; the
[runtime-first release](https://github.com/ShanilKoshitha/goforge/releases/tag/v0.22.0)
is public. Its scaffold remains format 15 on public v0.21.0. Proxy/checksum and
isolated installer/no-replace qualification are running before format-16 adoption.
Native live-PostgreSQL ORM race checks also passed (`orm/postgres` 4.086s).

Public runtime distribution verification completed: forced public proxy and
`sum.golang.org` resolved v0.22.0 with module sum
`h1:NgjmTT++ejfa8dOkFmcvFc5SRT7FTuutR38d8JfnLk0=` and Go-module sum
`h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=`. The isolated Windows
installer verified exact binary metadata/version without changing persistent
PATH. Its fresh no-replace `example.com/runtimeboard` app retained format 15 and
public v0.21.0, generated Category/related Ticket/LegacyIssue, and passed artifact
checks, module verify/tidy, `forge test`, vet, and server/worker/scheduler/console
builds. Local v0.22.1 adoption now enables format 16 and pins that verified
runtime with its exact public checksums; the adoption release remains unpublished.

The real CLI's format-16 atomicity/source-preservation tests now execute rather
than skip and passed in 17.2s. The activated fresh/unsupported-format/typed-form/
escape-hatch group passed 20 tests in 87.727s. A separate no-replace format-16
`example.com/publicadoption` application generated nullable Category, boolean-only
Project, all-scalar/two-relationship Ticket with an explicit nullable label, and
LegacyIssue against public v0.22.0. Artifact checks, module verify/tidy, ordinary
and race tests, vet, and all four process builds passed. This uses the local
candidate CLI and public runtime, not a published v0.22.1 CLI.

Historical format-15 compatibility is explicit: the restored manifest, legacy
textarea/compiled views, exact v0.21.0 requirement/sums, and retained local
replacement pass independent baseline/neutrality/restoration checks. The final
native PostgreSQL group passed in 220.847s: belongs-to 52.91s, fixture regression
10.24s, main workflow 124.25s, authorization 30.57s. Existing negative requests
were not changed. These journeys test current source; the additional public
v0.21.1 no-replace CI gate separately proves immutable old-runtime provenance.

Independent review found no P0–P2 activation/CI/public-documentation defect.
The repeated PostgreSQL CI gate now includes the scale journey with `-count=2`
and a finite 20-minute package timeout, retaining every existing journey.

First real scale journey passed in 47.466s after correcting acceptance-harness
module imports, independently authorized target-index expectations, and
select-scoped option parsing for overlapping table IDs. No production defect
was found. Review then identified that the Project index's privacy needle did
not match foreign rows; final repetitions must use a shared matching needle
and prove all 30 owner rows while excluding matching foreign/admin rows. Earlier
repetitions are not counted as final evidence for that strengthened assertion.

The strengthened final-source scale repeat passed twice (49.36s and 52.23s;
package 105.070s). Both repetitions saved all first/middle/last choices via text
and ID and verified exactly 30 owner Project labels while excluding matching
foreign/admin labels. Independent review confirmed the stronger proof has no
remaining P0–P2 issue. This is scale-journey repetition, not two whole-milestone
qualification passes.

Immutable old-runtime compatibility also passed locally: public v0.21.1 created
a fresh no-replace format-15 application pinned to public v0.21.0; the candidate
CLI added Category and related typed/integer RelatedIssue without new browse or
intent contracts. Artifact/module checks, generated tests, vet, and server build
passed. Format 15 kept its number controls and original module pin.

Activated full native regression passed: `go test ./... -count=1 -timeout 20m`
(`internal/cli` 352.511s), followed by successful vet/diff checks. Database tests
were exercised in the separate native groups above rather than through this
URL-unset invocation. Two complete adoption CI matrices and public v0.22.1 CLI
distribution remain required before accepting the milestone.

Fresh format-16 PostgreSQL integration also passed without skips: account
recovery 42.67s, durable jobs 34.37s, and personal tokens 23.01s (group 105.216s).
Each journey asserts the actual fresh manifest is format 16; these were not
restored historical fixtures. This closes native fresh integration coverage,
not the complete adoption two-pass gate.

Release-readiness documentation now describes the versioned v0.22.1 contract
and install commands, with explicit links to published-tag availability and this
qualification status. No runtime or generated-source contract changed. Two
complete CI passes remain required for this final release-content head; the
versioned instructions do not claim the adoption release is already published.

Hosted qualification correction: PR run
[39628695332](https://github.com/ShanilKoshitha/goforge/actions/runs/39628695332)
failed `TestWaitForChangedSourceSnapshotDebouncesBursts` in the framework race
step; later gates were not reached, so this is not a complete acceptance pass.
A controlled delayed-write reproduction showed the watcher correctly completed
100 ms of actual quiet while the test's next edit was descheduled. The test now
uses Go 1.25's isolated `testing/synctest` clock, retaining real file edits,
snapshot comparison, and an explicit 100–105 ms final debounce assertion.
Production watcher code and timeouts are unchanged. The focused race test
passed 100 repetitions (5.921s) and 300 executions across CPU settings 1/2/4
(11.169s); all 17 related watch/snapshot/tracker tests passed ten race-tested
repetitions (12.055s). Lead verification combined the corrected watcher with
fresh format-16 compilation and application-owned escape-hatch tests under the
race detector (47.708s); `go vet ./...` and diff checks passed. Independent
review found no remaining P0–P2 issue. Final complete matrices must pass again
on this correction; earlier Windows passes do not replace those gates.

Final acceptance audit closed one additional criterion-5 proof gap: denied
browser operations previously asserted the persistence counter but not the
separate relationship-choice counter. Format-16 relationship tests now prove
the counter is active on authorized New, then require zero choice calls for
every denied operation, including valid Create and PUT Update lookup refreshes
for each relationship. Production handlers are unchanged. Fresh format-16 app
tests and all 22 format 8–15 snapshots passed together (15.004s); lead race
verification including the corrected watcher passed (27.422s), followed by vet
and diff checks. Independent review found no remaining concrete implementation
or gate-coverage blocker. Two complete matrices on the final test-content head
and public v0.22.1 distribution remain required. Earlier-head push
[39628686188](https://github.com/ShanilKoshitha/goforge/actions/runs/39628686188)
completed its entire Linux/PostgreSQL and Windows matrix successfully; it is
supporting evidence, not one of the final-head passes.

Additional native PostgreSQL/race qualification exposed a supervisor-test
startup race and later exhausted the 20-minute package budget (1203.345s) while
formatting a generated view. This failed run is not acceptance evidence.
A controlled startup gate reproduced the exact 10s failure: three builds,
first-proxy target only, zero reloads, generation three. The test had injected
an edit before initial startup committed, correctly causing replacement of the
initial proxy rather than a reload. It now waits for the first source-watch
entry, bounded by supervisor exit and a 5s failure deadline. Related stress
also exposed the fake source publishing content before its generation; a
controlled publication gate reproduced the missing promotion-crash diagnostic.
Both fake setters now publish content and generation under one mutex, matching
the production tracker. All diagnostic hooks were removed; production code and
timeouts are unchanged. The two affected tests passed 6,000 race executions
across CPUs 1/2/4 (12.434s); all 11 supervisor tests passed 3,300 race executions
(4.882s). Fresh format-16 app tests and 22 legacy snapshots passed (15.824s).
Lead supervisor/watch/fresh-app/escape/snapshot race verification passed
(40.437s), with vet/diff checks and independent review clean. Final-head CI
must pass twice again. A separate finite 30-minute native Windows all-package
PostgreSQL/race qualification will account for the expensive nested builds;
Linux CI retains its existing 20-minute budget and every acceptance group.

## Final acceptance — 2026-10-21

Both complete final-content matrices passed at `c201677`: push Linux 23m51s /
Windows 9m23s; PR Linux 23m44s / Windows 6m15s. Independent log audits confirmed
formatting, race/vet/build, all nine public format 7–15 compatibility stages,
the current no-replace format-16 scaffold, and every repeated PostgreSQL journey.
The final PostgreSQL stages passed in 471.488s and 462.347s. Actual checkouts
were `c201677` and synthetic merge `20c2899`; both have tree
`ed19ce06ff41c0fb5be7399a36ea1de4010f175e`. PR #42 merged at `d71a93a` with
that exact tree. Immutable unsigned `v0.22.1` points to
`d71a93af85d212956fcc5f08bd17f2796f914a50`; the
[adoption release](https://github.com/ShanilKoshitha/goforge/releases/tag/v0.22.1)
is public. The runtime remains independently published v0.22.0.

Public proxy/SumDB verification resolved CLI module sum
`h1:02D7JfoxlnU523K0lcmj9MTILLM0h0grV1f/zZ5EJME=` and Go-module sum
`h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=`, with origin hash matching
the tagged merge. A complete fresh-cache confirmation ran the downloaded
Windows installer without changing persistent PATH, verified exact CLI binary
version/module checksum, and created `example.com/releasedscale` in the workspace.
It retained format 16, public v0.22.0 and its exact two runtime sums, with no
replacement. Nullable Category, boolean-only Project, all-scalar/two-relation
Ticket with an explicit nullable choice label, and LegacyIssue passed generator
checks, assets, module verify/tidy, ordinary/race tests, vet, and server, worker,
scheduler, and console builds. Generated source and binaries are retained for
inspection.

The first public attempt passed download/installation but Windows denied its
final scaffold directory rename. Atomic cleanup left no application or staging
directory. The same public binary subsequently created the original destination
successfully; the denial's cause remains unproven. That diagnostic retry is not
full acceptance evidence. The complete confirmation used a separate guarded
directory and newly downloaded module cache on the same workspace/OneDrive
path class, with all original assertions unchanged. No framework patch or tag
rewrite was used to conceal the interrupted attempt. Independent review found
no remaining milestone blocker; no later roadmap feature is included.

After review, the two validated run-local module caches were removed (68,935,400
bytes total). Public modules can regenerate them; generated applications,
installed binaries, scripts, evidence, shared caches, and older worktrees were
retained.

# v0.21 component attributes and accessible forms scorecard

Status: **accepted** — v0.21.0 runtime and v0.21.1 format-15 adoption published; public distribution verified — 2026-10-21

Release boundary: v0.21.0 publishes and verifies the backward-compatible view
compiler while retaining the format-14 scaffold and its public v0.19.0 pin.
The public proxy and checksum database serve both releases. The v0.21.1
release moves only fresh applications to format 15 and pins that
immutable runtime; formats 4 through 14 retain their released behavior.

Target:

> From a fresh format-15 application, compose ordinary HTML attributes through
> static Forge components and submit accessible authentication and typed
> resource forms. Invalid submissions return 422 with isolated, escaped old
> input and correctly associated errors; corrected submissions persist. The
> compiler emits deterministic ordinary `html/template` source with no runtime
> attribute machinery, and released applications retain their existing source
> and behavior.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Component attributes are complete and compile-time only | passing | One static emitter, call bag, and nested forwarding workflow compiles to static names plus ordinary Go-template actions; generated output contains no Forge directive, runtime bag, reflection path, or trusted attribute type |
| Attribute evaluation and merging are deterministic | passing | Caller expressions execute once through changed dot and nested forwarding; class order is defaults, forwarded, then local without hidden deduplication; boolean presence and meaningful ordinary empty values render exactly |
| Attribute failures are strict and source-mapped | passing at compiler boundary | Dynamic, malformed, unsafe, duplicate non-class, repeated-spread, conditional/multiple-sink, and no-sink cases fail at their original source locations; the established atomic compiler publication tests retain the last-good artifact |
| Contextual escaping remains authoritative | passing | Hostile text, markup, quotes, and URL values pass through `html/template`; URL-bearing attributes reject unsafe schemes and no bag can smuggle attribute names or trusted HTML |
| Explicit form selection is additive | passing | `form=` helpers work inside range, with, and components with evaluate-once semantics; every released legacy spelling retains its exact `.Form` expansion and behavior |
| Fresh authentication forms are accessible and isolated | passing | Generated controller/component tests and both candidate CI runs cover login, registration, recovery, token/password/action isolation, associated controls, non-secret recovery, and mixed-case password non-recovery |
| A typed resource form works end to end | passing | Correction commit dda1805 passed fresh generated tests and repeated PostgreSQL journeys, including combined errors, field accessibility, invalid-version correction, no-write rejection, and stale conflicts |
| Compatibility and escape hatches remain explicit | passing | Formats 4–14 retain their released contracts, public formats 7–14 pass CI, and a no-replace format-15 application passes against public v0.21.0; ordinary Go-template actions/functions/raw HTML and renderer replacement remain available |
| The milestone passes twice without regression | passing | PR workflow 39600373426 and push workflow 39600306047 both pass the complete Linux/PostgreSQL and Windows matrix at correction commit dda1805; independent code and evidence reviews report no remaining blocker |

## Runnable baseline — 2026-09-16

- `main` and `origin/main` resolve to v0.21.0 release-evidence merge `14411b0`.
  Lightweight tag `v0.21.0` remains fixed at accepted runtime merge `a1960a4`,
  and GitHub exposes it as the latest non-prerelease release.
- The exact baseline passes `go test ./... -count=1` with `internal/cli` in
  213.674 seconds. Exact-main workflow
  [34905720592](https://github.com/ShanilKoshitha/goforge/actions/runs/34905720592)
  passed Linux/PostgreSQL in 15m37s and native Windows in 6m20s, including all
  public-format compatibility gates and generated PostgreSQL applications.
- Components currently support strict props and slots but cannot receive or
  forward ordinary attributes. Form directives hard-code current-dot `.Form`;
  the token page already needs a manual root-form action inside a range.
- `SecurityPage` shares one `Form` between token and password workflows, so the
  common `current_password` key can render one workflow's error under both
  controls. Generated resource controls lack explicit IDs and associated ARIA
  error state. Existing PostgreSQL journeys prove value semantics and escaping
  but not this multi-form or accessibility boundary.

## Windows installation experience — 2026-09-17

- The recommended PowerShell installer reports toolchain, installation, PATH,
  and verification progress instead of relying on `go install`'s intentionally
  quiet success behavior.
- It resolves the actual Go binary destination from `GOBIN`/`GOPATH`, updates
  the current and persistent user PATH without duplicating entries, verifies
  exact module metadata and CLI version, and gives the next `forge new` command.
- A native Windows integration test uses an isolated fake Go toolchain and
  first-`GOPATH` install directory to prove the version pin, verbose install
  invocation, progress messages, output location, idempotent session discovery,
  and rejection of a mismatched CLI without changing the test user's PATH.
- A real Windows 11 / Go 1.25.3 run with empty `GOBIN` installed public
  `v0.21.0` to `C:\Users\Shani\go\bin\forge.exe`, added that directory to the
  user PATH, verified both build metadata and `forge 0.21.0`, and reported the
  already-present PATH entry on a second run.

## v0.21.1 local candidate evidence — 2026-09-17

- `go test ./... -count=1` passed in 172.017 seconds, with `internal/cli` in
  170.396 seconds. `go vet ./...`, a trimmed CLI build, PowerShell parsing, and
  `git diff --check` also passed; the built CLI reports `forge 0.21.1`.
- A fresh format-15 application pinned to the local checkout generated a typed
  string/text/integer/boolean resource, retained current view/ORM artifacts,
  and passed every generated test. Inspection confirmed explicit component
  form state, static accessible IDs/descriptions/errors, password non-recovery,
  and isolated token/password/action forms in ordinary generated source.
- Generated PostgreSQL journeys and a second independent full candidate pass
  remain delegated to the public CI acceptance gate; this local pass does not
  claim that database-backed evidence.
- Initial push run
  [35228342429](https://github.com/ShanilKoshitha/goforge/actions/runs/35228342429)
  passed the complete native Windows job but exposed a PostgreSQL authorization
  ordering defect: a cross-owner relationship update returned association 422
  before target visibility could return 404. The candidate now performs the
  scoped update first, retains composite-foreign-key 422 classification for
  visible rows, and adds a generated regression proving the query and error
  order without removing or weakening any test.
- Format-15 generated-application coverage pushes `internal/cli` past Go's
  default ten-minute timeout under Linux race instrumentation. CI now retains
  the complete `go test -race ./...` suite with a 20-minute package timeout;
  no test is deleted, skipped, narrowed, or renamed.
- Follow-up run
  [35232274693](https://github.com/ShanilKoshitha/goforge/actions/runs/35232274693)
  passed the complete native Windows job and the original cross-owner JSON
  regression, then correctly rejected the browser user's attempt to create a
  ticket with a category owned by the earlier JSON user. The fixture now creates
  a separate browser-owned category, preserving both the owner constraint and
  the full accessible browser correction journey.
- Run
  [35233911130](https://github.com/ShanilKoshitha/goforge/actions/runs/35233911130)
  passed the complete Linux/PostgreSQL matrix in 18m42s, while native Windows
  exposed an undersized test-only readiness budget in the frozen format-4
  serve journey. Its first local cold build took 15.92 seconds, exceeding the
  old 15-second whole-startup allowance before the supervisor's separate
  candidate-health timeout could begin. The test now retains the real build,
  process, HTTP render, shutdown, and last-good checks, allows one minute for a
  cold build, fails immediately if serve exits, and drains cancellation on
  timeout. Five consecutive focused runs and the full native Windows package
  set pass; no test was removed, skipped, or weakened.

## v0.21.1 acceptance revalidation — 2026-10-21

- PR [#37](https://github.com/ShanilKoshitha/goforge/pull/37) merged as
  `b7a8a23`. Candidate `ed05095` passed both complete workflows:
  [push 35236163509](https://github.com/ShanilKoshitha/goforge/actions/runs/35236163509)
  and [PR 35281268567](https://github.com/ShanilKoshitha/goforge/actions/runs/35281268567).
  Merge workflow
  [35283183823](https://github.com/ShanilKoshitha/goforge/actions/runs/35283183823)
  also passed Linux/PostgreSQL and native Windows. The Linux jobs executed
  every public compatibility gate, the no-replace format-15 application, and
  each database-backed generated workflow twice.
- Independent code and coverage reviews nevertheless reproduced two P2 gaps:
  scalar decoding errors hide unrelated semantic field errors, and malformed,
  empty, or missing optimistic versions can leave a re-rendered browser edit
  form without a usable hidden version. Existing tests checked rejection but
  did not complete correction from these states. Prior green runs therefore do
  not establish acceptance of these missing recovery cases.
- Branch `codex/v0.21.1-form-recovery` starts from merged `b7a8a23`. The
  corrections now pass focused tests, generated correction/no-write and
  stale-version regressions, independent review, and two clean candidate gates.
  Merged-main and public distribution verification subsequently passed below.
- The correction merges semantic and scalar errors, loads invalid update forms
  through the authorized member scope, recovers missing/invalid/nonpositive
  versions, and preserves valid stale versions. Generated tests cover correction
  followed by successful persistence, unchanged data on rejection, cross-owner
  404 responses, and privileged record-owner relationship choices. Focused
  generation/compatibility suites passed in 17.171s and 49.448s. Independent
  adversarial review reports no remaining P0–P2 finding after correcting two
  test assertions to match the actual field-error markup.
- A fresh no-replace `example.com/recoverygate` application is format 15 pinned
  to public v0.21.0. It generated Category, a Ticket containing every scalar type
  plus belongs-to, and a legacy-style resource; then passed view/ORM/asset
  freshness, module verification, zero tidy diff, all generated tests, vet, and
  server/worker/scheduler/console builds. Source inspection confirms scoped
  correction logic and ordinary compiled templates without attribute runtime
  machinery. Local Docker has no running daemon; the successful hosted runs
  below provide the PostgreSQL evidence.
- Native framework revalidation passed `go test ./... -count=1`
  (`internal/cli` 313.567s), and final formatting, vet, and diff checks pass.
  The initial sandboxed attempt was invalidated by Windows file-replacement,
  TCP-owner-table, and loopback restrictions; the native run resolves those
  environment failures. The completed correction was then checked by the two
  complete hosted acceptance workflows below.
- A further full native `go test ./... -count=1` on frozen correction commit
  `dda1805` passed (`internal/cli` 283.611s). Push workflow
  [39600306047](https://github.com/ShanilKoshitha/goforge/actions/runs/39600306047)
  and PR workflow
  [39600373426](https://github.com/ShanilKoshitha/goforge/actions/runs/39600373426)
  both passed the complete Linux/PostgreSQL and Windows acceptance gates.
- PR workflow `39600373426` completed successfully: Linux/PostgreSQL 17m36s
  and Windows 7m18s. Its full race suite passed (`internal/cli` 505.203s), as
  did all public compatibility and fresh public-runtime application gates.
  Logs show `TestGeneratedPostgresWorkflow` passing twice (53.85s and 58.49s),
  along with repeated recovery, token, relationship, and authorization journeys.
  An independent evidence audit traced the expanded typed-browser helper into
  that executed workflow and found every functional criterion covered.
- Push workflow `39600306047` independently passed Linux/PostgreSQL in 24m57s
  and Windows in 7m40s at the same correction commit. Its logs show the expanded
  `TestGeneratedPostgresWorkflow` passing twice (74.45s and 74.02s), together
  with repeated recovery, token, relationship, and authorization journeys.
  This establishes two complete correction passes. Further work is release
  verification and evidence publication; no additional feature is in scope.
- PR [#38](https://github.com/ShanilKoshitha/goforge/pull/38) merged as
  `8be0d79fd99b2647909722ff48b948db58d29b74`. Its tracked tree is identical to
  accepted correction commit `dda1805`. The local main branch is fast-forwarded
  to that merge while unrelated cleanup notes remain unstaged. Exact-main
  workflow [39603892753](https://github.com/ShanilKoshitha/goforge/actions/runs/39603892753)
  passed Linux/PostgreSQL in 23m35s and native Windows in 7m15s, including the
  complete compatibility, fresh-app, and repeated database acceptance gates.

## v0.21.1 release verification — 2026-10-21

- Lightweight unsigned tag `v0.21.1` points to accepted merge
  `8be0d79fd99b2647909722ff48b948db58d29b74`. The local object type is `commit`,
  and the remote tag advertises the same commit rather than an annotated tag
  object. GitHub publishes
  [GoForge v0.21.1](https://github.com/ShanilKoshitha/goforge/releases/tag/v0.21.1)
  as a non-draft, non-prerelease release.
- The public Go proxy reports that exact origin hash for `refs/tags/v0.21.1`.
  SumDB record `69661702` independently publishes module checksum
  `h1:BgJ04QCfGgmAxD+r6WpeVB26qb96lFqXPPCYnUddBCA=` and go.mod checksum
  `h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=`.
- The PowerShell installer fetched public v0.21.1 through the Go proxy and
  checksum database into an isolated workspace binary directory, verified Go
  module metadata and `forge 0.21.1`, and reported success. `-SkipPathUpdate`
  kept the user's installed CLI and PATH unchanged; ordinary shared module and
  build caches were reused.
- That installed binary generated no-replace `example.com/releasedv0211` at
  format 15 pinned to immutable public v0.21.0. It generated Category, a Ticket
  with all scalar types and a required belongs-to, and a legacy-style resource.
  View/ORM/asset freshness, module verification, zero tidy diff, `forge test`,
  vet, `forge build`, and worker/scheduler/console builds all passed. Inspection
  confirmed the scoped correction logic and compiled ordinary templates with
  no attribute runtime machinery. Four executable artifacts were produced.
- Tag workflow
  [39607483308](https://github.com/ShanilKoshitha/goforge/actions/runs/39607483308)
  passed Linux/PostgreSQL in 21m05s and native Windows in 4m46s, including the
  compatibility, fresh-app, and repeated PostgreSQL workflow gates. No feature
  work followed the two accepted implementation passes.

## Post-release roadmap review — 2026-10-21

Status: **planning only**; no later milestone is claimed as implemented.

- ROADMAP.md corrects minor-version ordering and proposes complete workflows
  from v0.22 through v0.39, followed by explicit v1 prerelease qualification.
- Independent data and developer-workflow reviews checked the established
  baseline against current code. The final proposal passed an independent
  consistency review after clarifying non-mutating migration diagnosis,
  custom-type boundaries, transactional relationship authorization, adoption
  provenance, and cumulative application integration.
- No generator, runtime, template, dependency, or published artifact changed.
  Existing accepted application and public-release evidence remains applicable;
  this entry records planning review, not a new implementation test pass.

## v0.21.0 runtime-stage evidence — 2026-09-14

- The compiler expands a static `@attributes(...)` sink and final component
  `attributes(...)` bag into static names plus ordinary Go-template variables.
  Nested forwarding reuses caller-scope captures; class, boolean, empty-value,
  duplicate, dangerous-name, changed-dot, and contextual URL-escaping cases are
  covered without a runtime map, reflection path, or trusted attribute type.
- The single-sink analysis rejects conditional, iterated, slot-fallback,
  repeated, missing, and page-level sinks. Independent adversarial review found
  and the candidate fixed an `@errors` iteration loophole, a masked
  non-component diagnostic, and literal-keyword/source-position compatibility.
  The final review reports no remaining P0, P1, or P2 finding.
- `@csrf`, `@old`, and `@errors` accept one optional final `form=` pipeline,
  captured once in lexical scope. Range, with, component, fallback, empty old
  value, and repeated-error cases pass; every legacy spelling has an exact
  byte-for-byte expansion assertion.
- Two complete local candidate passes succeed: `go test ./... -count=1`
  (`internal/cli` 178.282s and 172.969s), `go vet ./...`, trimmed builds
  reporting `forge 0.21.0`, `go test -race ./view -count=1`, and
  `git diff --check`.
- A clean public v0.20.0 CLI generated a no-replace format-14 application pinned
  to v0.19.0. The current candidate added a typed resource, then passed ORM,
  view, and asset freshness; module verification; zero tidy diff; every
  generated test; vet; and application build. CI now retains this exact public
  compatibility gate. Format 15 and its accessible generated forms remain
  intentionally absent until the v0.21.0 runtime is published and verifiable.
- Runtime candidate `6bd8014` passed exact-commit push workflow
  [34896775139](https://github.com/ShanilKoshitha/goforge/actions/runs/34896775139)
  in 18m35s on Linux/PostgreSQL and 6m29s on native Windows. Pull-request
  workflow
  [34896804219](https://github.com/ShanilKoshitha/goforge/actions/runs/34896804219)
  independently passed the same matrix in 18m38s and 6m26s, including the
  public v0.20.0 format-14 gate and doubled generated PostgreSQL journeys.

## v0.21.0 public release evidence — 2026-09-14

- Lightweight unsigned tag `v0.21.0` points directly to accepted merge
  `a1960a4cfb5a20c4da12a04d2c7db6af954f1b7f`; local and remote object type is
  `commit`, not an annotated tag. Exact-main workflow
  [34900623499](https://github.com/ShanilKoshitha/goforge/actions/runs/34900623499)
  passed Linux/PostgreSQL in 16m02s and native Windows in 5m48s before tagging.
- Exact-tag workflow
  [34902098802](https://github.com/ShanilKoshitha/goforge/actions/runs/34902098802)
  passed the complete matrix in 18m28s and 6m03s. GitHub publishes
  [GoForge v0.21.0](https://github.com/ShanilKoshitha/goforge/releases/tag/v0.21.0)
  as the latest non-draft, non-prerelease release.
- The public Go proxy reports origin hash `a1960a4` for `refs/tags/v0.21.0`.
  SumDB records module checksum
  `h1:i2oJUQaIbA3nbJJJUV6ewMm2Mds8kLYgmIC+50dBNU0=` and go.mod checksum
  `h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=`.
- A clean proxy-only `go install` reports `forge 0.21.0` and embeds the same
  module checksum. Its no-replace scaffold remains format 14 pinned to public
  v0.19.0, then passes view/ORM/asset freshness, module verification, zero tidy
  diff, every generated test, vet, and server/worker/scheduler/console builds.
  As designed, format 14 continues using its pinned compiler; v0.21 syntax enters
  generated applications only when the v0.21.1 format-15 adoption pins v0.21.0.

## Explicit non-goals

- Runtime or dynamic attribute maps/names, dynamic tags/components, reflection,
  `template.HTMLAttr`, or implicit attribute discovery.
- Untrusted template sandboxing, VDOM/hydration, client-side form frameworks,
  reflective request/model binding, or browser validation replacing server
  validation.
- File, date, multiselect, or JavaScript widgets; asset transforms and HMR;
  sanitizing explicit trusted safe types; or claiming complete WCAG compliance.

# v0.20 personal API tokens scorecard

Status: **accepted** — 2026-09-11

Target:

> From a fresh format-14 application, a signed-in user can create, inspect, use,
> and revoke a named, expiring personal API token through coherent JSON and
> browser account-security workflows. Bearer authentication reaches the same
> application-owned user, resource policy, and SQL scope as session
> authentication. Only the one-time response contains the raw secret; storage,
> later responses, errors, logs, and session payloads contain no recoverable
> token material. Every route, query, migration, and replacement seam remains
> ordinary generated Go and SQL.

GoForge already generates authenticated and authorized JSON CRUD, but non-browser
clients can authenticate only by maintaining the browser session-cookie
protocol. This milestone closes that functional gap with an opaque personal
credential rather than introducing JWT claims, OAuth machinery, or a second
authorization system. The existing resource policy stays authoritative.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Token management is a complete user workflow | passing | Generated unit and live PostgreSQL journeys list, create, and user-scope revoke through strict JSON and CSRF-protected browser routes; current-password, unique trimmed 1–80 byte name, and 1–90 day lifetime boundaries pass |
| Raw secrets are one-time values | passing | Live creation returns `goforge_pat_<selector>.<secret>` once with `no-store` and privacy headers; database rows contain only canonical selectors and 32-byte digests, and list/page/error/log/session probes find no token material |
| Bearer authentication is strict and deterministic | passing | `/auth/me` and generated resources accept one exact Bearer credential; malformed, duplicate, wrong-scheme, wrong-secret, expired, revoked, and unknown credentials share the generic 401 path without cookie fallback, session mutation, or `Set-Cookie` |
| Existing policy and transport boundaries remain authoritative | passing | The live resource journey proves owner/all/deny policy scopes, 401/403/404 behavior, validation, optimistic concurrency, and belongs-to ownership under Bearer auth; browser and credential-management routes remain session-only |
| Revocation is durable and coherent | passing | User-scoped revoke survives process restart, while password change, password reset, and sign-out-everywhere invalidate every credential issued under the prior generation without secret scans |
| Issuance fails atomically under pressure | passing | Twelve concurrent correct issuances commit exactly ten tokens and return two `token.limit` validation failures; duplicate names, stale credentials, invalid expiry, entropy/storage/cancellation failures, and rollback paths leave no partial credential state |
| Persistence is bounded, indexed, and application-owned | passing | Plain `000006` SQL enforces the user foreign key, base64url selector and digest shape, credential generation, bounded name, expiry, uniqueness, and lookup indexes; expired rows are excluded and pruned outside the ORM |
| Existing applications remain compatible | passing | Public format 7 and released formats 8–13 pass unchanged compatibility gates; only fresh format 14 receives token source, migration, routes, UI, and token-aware resource wiring |
| The workflow remains conventional and replaceable | passing | Generated interfaces, repository, middleware, controllers, routes, view, migration, and tests remain readable application-owned Go and SQL using `database/sql`, `html/template`, and ordinary test/build/run commands |
| The milestone passes twice without regression | passing | Final revision `a8243f6` passed complete push and pull-request CI, including Linux/PostgreSQL and native Windows, merge `ef091b4` passed the same exact-main matrix, and Windows-stability merge `6d8371e` passed exact main; two independent reviews report no P0–P2 finding |

## Runnable baseline — 2026-09-11

- `main` and `origin/main` resolve to merge `728b609`, the accepted v0.19.0
  release-evidence revision. The exact-main GitHub Actions run
  [34634670953](https://github.com/ShanilKoshitha/goforge/actions/runs/34634670953)
  passed Linux/PostgreSQL in 14m41s and native Windows in 6m13s.
- A clean isolated worktree at that revision passes `go test ./... -count=1`
  with `internal/cli` completing in 173.753 seconds, plus `go vet ./...`.
- The generated route table protects JSON auth and resources only with the
  database-backed session manager. There is no bearer parser, token repository,
  token management route, token migration, or machine-client credential path.
- `security/token` already provides CSPRNG selector/secret issuance,
  digest-only persistence material, canonical bounded parsing, and constant-time
  verification. The milestone reuses that accepted primitive from visible
  application code instead of defining another token runtime.
- The generated resource authorization seam already maps one authenticated
  `auth.User` into closed owner/all SQL scopes. Token authentication must feed
  that seam rather than add scopes, claims, or policy discovery.

## Acceptance evidence — 2026-09-11

- Fresh format-14 scaffolds now contain the plain `000006` migration,
  digest-only PostgreSQL repository, dedicated current-password limiter,
  strict token-aware API middleware, session-only JSON/browser management,
  account-security UI, unit tests, and visible route wiring. Format 13 keeps
  its original session middleware and receives no token source or migration.
- A fresh generated application passes its complete Go test suite. Focused
  framework checks also pass scaffold formatting, generated-app inspection,
  format-13/14 middleware compatibility, and compilation plus live execution
  of the PostgreSQL token journey.
- Independent review found and the candidate fixed an eight-attempt limiter
  conflict with the ten-token concurrency cap, a selector-alphabet constraint
  gap, and missing live evidence for authorization/relationship parity and all
  three credential-generation invalidation paths.
- The strengthened PostgreSQL journey requires explicit 403 and all-record
  policy results, owner-safe belongs-to writes, stale-write conflict, password
  change/reset/logout-all invalidation, canonical selector rejection, exact
  cap errors, durable restart behavior, and secret-leak checks. Hosted
  execution passed in both final-revision workflows and again on exact main.
- The integrated final revision passes `go test ./... -count=1` (CLI matrix
  174.740s), `go vet ./...`, and a trimmed CLI build. A separately generated
  no-replace format-14 application pinned to public v0.19.0 generated related schema-driven
  resources and passed ORM/view/asset freshness, module verification, zero
  tidy diff, every generated test, vet, and server build.
- Final revision `a8243f6` passed public push run
  [34644607658](https://github.com/ShanilKoshitha/goforge/actions/runs/34644607658)
  and pull-request run
  [34644611048](https://github.com/ShanilKoshitha/goforge/actions/runs/34644611048).
  Each ran the Linux/PostgreSQL matrix, repeated generated application journeys,
  every released-format compatibility gate, and native Windows tests. Merge
  `ef091b4` then passed the exact-main run
  [34646266620](https://github.com/ShanilKoshitha/goforge/actions/runs/34646266620)
  with Linux/PostgreSQL in 14m56s and Windows in 6m43s.
- A hosted failure exposed a real nested-template binding defect in token revoke
  forms: `@csrf` resolved against a token row instead of the root page. Commit
  `a8243f6` uses the explicit root form token and adds a generated-app regression
  that renders a non-empty token list; all three final public runs pass it.
- The accepted-main run after the documentation merge exposed a test-only
  three-second polling deadline in the development supervisor lost-edit test on
  Windows. Commit `ab2d649` synchronizes the assertion with the proxy's committed
  reload event and adds bounded diagnostic state. Its push and pull-request
  workflows passed Linux/PostgreSQL in 18m32s and 18m12s and Windows in 6m17s
  and 5m51s. Merge `6d8371e` then passed exact-main run
  [34653097740](https://github.com/ShanilKoshitha/goforge/actions/runs/34653097740)
  in 18m8s on Linux/PostgreSQL and 6m55s on Windows.
- The second independent review reports no remaining P0, P1, or P2 finding.
  It rechecked limiter separation, canonical persistence, strict Bearer/session
  precedence, policy and relationship parity, every generation-invalidation
  path, cap error identity, format-13 compatibility, the public module pin, and
  documentation honesty.

## Public release evidence — 2026-09-14

- Lightweight tag `v0.20.0` points directly to accepted merge
  `273f735d0afb75c19353943b831708143b7b9aa7`; the GitHub tag object is a
  commit reference rather than an annotated tag. Tag workflow
  [34852151949](https://github.com/ShanilKoshitha/goforge/actions/runs/34852151949)
  passed the full Linux/PostgreSQL matrix in 19m02s and native Windows in
  6m46s on that exact commit.
- The public Go proxy serves the version metadata, module file, and archive
  with origin hash `273f735d0afb75c19353943b831708143b7b9aa7`. The checksum
  database records module sum
  `h1:od4qWVarHhAJFxLraS+Wl64a3i/q/6mET0TEwtLxsDQ=` and `go.mod` sum
  `h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=`.
- An isolated public-proxy and checksum-database install of
  `github.com/ShanilKoshitha/goforge/cmd/forge@v0.20.0` reports
  `forge 0.20.0`. That binary generated a no-replace format-14 application
  pinned to the public v0.19.0 runtime; module verification, zero tidy diff,
  generated view/ORM/asset freshness, every generated test, vet, and the
  server, worker, scheduler, and console builds passed.
- [GoForge v0.20.0](https://github.com/ShanilKoshitha/goforge/releases/tag/v0.20.0)
  is the latest non-draft, non-prerelease GitHub Release. An independent
  read-only audit confirmed the tag target, workflow result, proxy origin,
  checksum-database records, and release visibility.

## Explicit non-goals

- OAuth2, OpenID Connect, JWTs, refresh tokens, authorization codes, device
  flows, service accounts, third-party delegation, or organization credentials.
- Token abilities/scopes, persisted RBAC, field permissions, per-resource
  grants, or a second authorization DSL. Application-owned resource policies
  remain the sole authorization boundary.
- Indefinite tokens, last-used tracking, audit history, notifications,
  IP/device binding, per-token quotas, rotation grace periods, automatic renewal,
  DPoP, mTLS, or cancellation of requests already authenticated before revoke.
- Operator `forge token:*` commands, automatic upgrades of application-owned
  auth source, generic runtime token storage, or reflection-based route guards.
- Component attribute bags, class merging, accessible-form rewrites, or other
  frontend compiler work. That is the next independent frontend milestone.

# v0.19 schema-complete model generation scorecard

Status: **accepted** — 2026-09-11

Target:

> From a fresh application, describe useful PostgreSQL domain models directly
> in `forge make:model` commands and receive application-owned Go declarations,
> paired SQL migrations, typed ORM mapping, mutations, queries, and explicit
> belongs-to loading as one rollback-safe generation transaction. The emitted
> Go and SQL remain authoritative and editable; no runtime schema registry,
> reflection, migration diffing, lazy loading, or Active Record lifecycle is
> introduced.

This milestone closes the largest remaining gap between the broad v0.4 ORM
runtime and the empty-directory workflow. The current command emits an empty
model and requires developers to manually keep Go declarations and migrations
in sync before regenerating ORM code. The smallest complete replacement reuses
the resource scalar vocabulary for ordinary models and adds one explicit,
required belongs-to edge to an existing model.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| One command produces a useful scalar model | passing | `make:model Customer --field name:string` emits deterministic ordinary Go, a paired PostgreSQL migration, and refreshed typed ORM output; declaration order is retained, fields are required by default, and `:nullable` has exact Go/SQL semantics |
| Required belongs-to generation is complete | passing | `make:model Invoice --field number:string --field total_cents:integer --field paid:boolean --belongs-to customer:Customer` validates the existing target and emits the typed foreign key, relationship declaration, indexed foreign key, and generated batch loader without editing the target model |
| Generation is atomic and concurrency-safe | passing | Invalid input and model, migration, ORM-render, publication, cancellation, and concurrent-generator failures either write the complete coherent set or preserve the prior tree; no orphaned model, migration, or generated ORM artifact remains |
| The generated ORM is useful end to end | passing | A fresh PostgreSQL application uses only generated types to create, find, filter, order, paginate, count, test existence, partially update, reject stale writes, safely delete, and batch-load the relationship with a bounded query count |
| Transactions and escape hatches remain visible | passing | The same generated store works with `*sql.DB` and `*sql.Tx`; commit, rollback, handwritten SQL, inspected `orm.Statement`, classified constraint errors, and cancellation are demonstrated without framework-owned runtime discovery |
| Compatibility remains explicit | passing | No-flag `make:model Name` remains byte-for-byte compatible; existing format-4 through format-13 applications retain their source, source-level frozen-format coverage starts at format 6, and the earliest public v0.7.0/format-7 module compiles the additive output without a replacement |
| Documentation is copyable and honest | passing | Root and generated-project guides link a focused ORM guide covering model generation, generated store use, transactions, relationships, raw SQL, regeneration/checking, ownership of emitted code, and deliberate non-goals |
| The milestone passes twice without regression | passing | Framework normal/race/vet/build, frozen-format compatibility, fresh generated-app inspection, isolated PostgreSQL workflow, native Windows checks, and independent adversarial review pass twice on the final revision |

## Runnable baseline — 2026-09-11

- `origin/main` and the isolated milestone branch start at merge `e27068a`.
  [GitHub Actions run 34616051890](https://github.com/ShanilKoshitha/goforge/actions/runs/34616051890)
  passed Linux/PostgreSQL and native Windows. A local `go test ./...` run also
  passed, with the generated-application CLI package completing in 173.901
  seconds.
- The generated ORM already supports typed selects, counts, existence checks,
  inserts, bulk operations, partial updates, safe deletes, row locks, optimistic
  concurrency, four explicit relationship kinds, `*sql.DB`/`*sql.Tx`
  executors, inspectable statements, and stable PostgreSQL error
  classification. This milestone does not build a second ORM.
- `forge make:model Invoice` currently emits only ID, timestamps, and version.
  Its generated `InvoiceCreateInput` and `InvoiceChanges` are empty, and the
  generated-project README tells developers to hand-edit both the Go model and
  migration before running `forge orm:generate`.
- `make:resource` already has a deterministic scalar grammar, but ordinary
  domain models cannot use it and every non-resource generator currently
  rejects additional arguments. Required belongs-to generation is likewise
  limited to HTTP resources.

## Acceptance evidence — 2026-09-11

- Final candidate `64e8f09` passes `go test ./... -count=1`, with the CLI
  matrix completing in 182.802 seconds, and `go test -race ./... -count=1`,
  with the CLI matrix completing in 271.484 seconds. `go vet ./...`,
  formatting, and `git diff --check` are clean. A separate final CLI run on
  the committed revision passed in 166.088 seconds.
- A fresh format-13 application generated `Customer` and `Invoice` models,
  including a nullable text field and required belongs-to relationship.
  Inspection confirmed ordinary Go declarations, paired SQL migrations,
  writable generated inputs and changes, an indexed foreign key, and the
  explicit batch loader. `orm:generate --check`, direct vet, generated tests,
  and the isolated application build all passed.
- The generated PostgreSQL journey runs twice in the hosted matrix and proves
  typed create/find/filter/order/pagination/count/existence, nullable value
  semantics, partial changes, optimistic concurrency, safe delete, bounded
  belongs-to loading, database and transaction executors, commit and rollback,
  raw SQL, inspectable statements, classified constraints, and cancellation.
- Frozen format-6 coverage preserves pre-existing application source while
  generating a related model with the current CLI. The public v0.7.0/format-7
  gate uses the tagged CLI and runtime without a module replacement, normalizes
  that historical scaffold's missing direct-module checksums, then proves the
  additive v0.19 output is tidy, current, testable, vet-clean, and buildable.
- Independent adversarial review found no remaining P0, P1, or P2 issue after
  the field-grammar ownership, nullable workflow, concurrent generation,
  frozen/public compatibility, custom-primary-key boundary, and documentation
  corrections. All candidate commits are unsigned, authored and committed only
  as Shanil, and contain no co-author or assistant metadata.
- [PR #27](https://github.com/ShanilKoshitha/goforge/pull/27) preserves the
  milestone's six commits and merged at `9326c7e`. Its
  [pull-request run](https://github.com/ShanilKoshitha/goforge/actions/runs/34623315452)
  and corrected
  [push run](https://github.com/ShanilKoshitha/goforge/actions/runs/34623311062)
  independently passed Linux/PostgreSQL and native Windows. The first push
  attempt was retried unchanged after the public Go proxy reset one v0.13.1
  download; the retry passed that gate and the complete matrix. The exact
  merged [main run](https://github.com/ShanilKoshitha/goforge/actions/runs/34626044528)
  passed the same matrix in 12m44s on Linux/PostgreSQL and 5m53s on Windows.
- [PR #28](https://github.com/ShanilKoshitha/goforge/pull/28) records acceptance
  on main at `2b5df91`. Its push and pull-request matrices passed independently,
  and the exact accepted-main
  [run](https://github.com/ShanilKoshitha/goforge/actions/runs/34629173483)
  passed Linux/PostgreSQL in 18m28s and Windows in 6m7s. Unsigned lightweight
  tag `v0.19.0` resolves directly to that commit, and its independent
  [tag run](https://github.com/ShanilKoshitha/goforge/actions/runs/34630983837)
  passed Linux/PostgreSQL in 15m1s and Windows in 6m7s.
- The public Go proxy returns module checksum
  `h1:06YyBa1ivkFwtkA9XNTprNzERN/FJMgFEhqtgSzoFQg=` and origin commit
  `2b5df91` for `v0.19.0`. A clean proxy install reports `forge 0.19.0`,
  generates a no-replace format-13 application pinned to public v0.17.0, adds
  scalar and nullable `Customer`/`Invoice` models with a required belongs-to
  edge, and passes ORM freshness, module verification, zero tidy diff,
  generated tests, direct vet, and the isolated application build.

## Explicit non-goals

- Active Record `Save`, lazy loading, identity maps, implicit units of work,
  automatic startup migration, schema diffing, runtime reflection, or package
  scanning.
- Editing an existing target model to add inverse relationships, generating
  has-one/has-many/many-to-many declarations, polymorphism, composite primary
  keys, automatic belongs-to generation for custom primary keys, or
  multi-dialect parity.
- HTTP controllers, routes, requests, views, resource metadata, implicit
  validation, callbacks, tenant scopes, caching, or a persisted schema
  registry.

# v0.18 explicit resource authorization scorecard

Status: **accepted** — 2026-09-11

Target:

> From a fresh format-13 application, generate an authenticated resource whose
> application-owned policy is visibly injected into both JSON and browser
> controllers. Keep the secure owner-only default, then replace it with ordinary
> Go to grant an explicit all-record scope or deny one action. Both transports
> must enforce the same decision, every database read and mutation must apply the
> authorized scope in SQL, and hidden, cross-owner, and missing records must
> remain indistinguishable.

The smallest complete workflow adds one typed authorization and scope contract to each
new resource. It is deliberately not a role database, permission DSL, global
gate registry, reflection system, or row-by-row authorization callback. Teams
own the policy source and constructor wiring and can replace the generated
controllers, repositories, or route registration with ordinary Go.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| The secure default remains owner-only | passing | A fresh resource's default policy scopes list, pagination, show, edit, update, delete, relationship loading, and relationship choices to the authenticated owner; create remains explicitly authorized and assigns the authenticated owner |
| Policy decisions are explicit application code | passing | The generated resource owns typed action/access values, one editable authorization function, and closed owner/all scopes; routes visibly inject the same function into both controllers without reflection, runtime registration, annotations, or a role DSL |
| HTTP denial semantics are coherent | passing | Unauthenticated JSON requests remain 401 while browser requests retain the 303 redirect to `/login`; an authenticated action-level denial is 403; records outside the authorized scope and absent records are both 404; malformed or zero policy/scope values never grant access |
| JSON and browser behavior stays in parity | passing | Index/new/create/show/edit/update/delete exercise the same policy contract in both transports, including validation and stale-write paths, without leaking hidden record existence |
| Persistence enforces authorization atomically | passing | List/find/paginate predicates and update/delete predicates include the validated scope in the executed SQL; no fetch-authorize-mutate window can write a record outside that scope, and optimistic concurrency retains 404-versus-409 behavior inside the authorized scope |
| A privileged scope is useful without framework surgery | passing | Replacing the default policy with application-owned Go can list, view, update, and delete another owner's resource while preserving its owner and same-owner relationship invariants; denying one action requires no controller or repository rewrite |
| Existing applications remain compatible | passing | Formats 4–12 retain their generated source and behavior; only format 13 receives the policy contract, and released-format compatibility plus fresh no-replace application checks remain green |
| The workflow remains conventional and replaceable | passing | Generated policy, controller, repository, route wiring, ORM predicates, and tests are gofmt'd readable Go; direct package tests and direct `go run`/`go build` paths work without the CLI runtime |
| The milestone passes twice without regression | passing | Framework normal/race/vet/build, frozen compatibility, current scaffold, fresh generated PostgreSQL authorization journey, native Windows checks, and independent adversarial review pass twice on the final revision |

## Runnable baseline — 2026-09-11

- `origin/main` is merge `1667f16`. GitHub Actions run
  [34546193006](https://github.com/ShanilKoshitha/goforge/actions/runs/34546193006)
  passed the Linux/PostgreSQL and native Windows matrices. A clean worktree at
  that exact commit passes `go test ./... -count=1` and `go vet ./...`.
- The repository root had remained on `feature/v0.12-auth-recovery`, 86 commits
  behind `origin/main`, which made local results appear inconsistent with the
  merged branch. It is now fast-forwarded to the exact merged commit while the
  original untracked `Agents.md` remains untouched. Milestone work stays in an
  isolated format-13 branch.
- Generated routes require authentication, but controllers accept only a
  repository and repositories take a raw `userID`. Owner predicates are
  repeated directly in every query. There is no policy, voter, gate, permission,
  or typed scope seam to express an editor, administrator, or read-only user
  without rewriting controllers and persistence together.
- The current owner predicate already prevents cross-owner reads and writes,
  and optimistic updates already distinguish a hidden row from a stale visible
  row. Those secure behaviors are the compatibility floor, not functionality to
  remove.

## Acceptance evidence — 2026-09-11

- Local candidate `e50a688` passes `go test ./... -count=1` with the CLI
  matrix completing in 147.398 seconds and `go test -race ./... -count=1` with
  the CLI matrix completing in 257.487 seconds. `go vet ./...`,
  `go mod tidy -diff`, formatting, and `git diff --check` are clean.
- A fresh format-13 application generated scalar `Category` and required
  belongs-to `Issue` resources. The generated application passed direct
  `go test ./...`, direct `go vet ./...`, `forge assets:check`, `forge test`,
  and the isolated `forge build`; inspection confirmed its visible policy,
  shared controller injection, and scoped repository predicates.
- The PostgreSQL authorization journey compiles locally and is wired into the
  hosted database matrix twice. It proves JSON 401 and browser login redirect,
  owner-hidden 404 parity in both transports, privileged cross-owner JSON and
  browser operations, owner preservation, denied deletes with valid CSRF, and
  final database state. The existing belongs-to PostgreSQL helper was migrated
  to format-13 scopes and reproduced through successful helper compilation.
- Public upgrade gates now create real format-8, format-9, format-10,
  format-11, and format-12 applications with their released CLIs, run current
  compatible generators, assert that authorization is not backported, and test,
  vet, and build the results. The fresh no-replace format-13 application also
  runs its generated tests under the race detector.
- Independent adversarial review found no remaining P0, P1, or P2 issue after
  the acceptance-helper, browser parity, documentation, compatibility, and
  no-replace race-evidence corrections. The first hosted database run exposed a
  mismatched generated module name in the new journey's reused schema helper;
  `20b47fd` aligned the module contract, reproduced helper compilation locally,
  and passed a separate final review with no P0–P2 finding.
- [PR #24](https://github.com/ShanilKoshitha/goforge/pull/24) merged the final
  candidate at `c889dea`. Its corrected
  [push run](https://github.com/ShanilKoshitha/goforge/actions/runs/34605593740)
  and [pull-request run](https://github.com/ShanilKoshitha/goforge/actions/runs/34605598255)
  independently passed Linux/PostgreSQL and native Windows. The exact merged
  [main run](https://github.com/ShanilKoshitha/goforge/actions/runs/34607290670)
  passed the same complete matrix.
- Unsigned lightweight tag `v0.18.0` resolves to accepted main `bf91c4f`. Its
  independent [tag run](https://github.com/ShanilKoshitha/goforge/actions/runs/34612543759)
  passed Linux/PostgreSQL and native Windows. The public Go proxy returns module
  checksum `h1:NFMuYt0YBjFKzImLMG55JAxyvui2UwDcc17gwjlt4pE=`; a clean proxy
  install reports `forge 0.18.0`, generates a no-replace format-13 resource
  application pinned to public v0.17.0, and passes module verification, tidy,
  asset validation, generated tests, and the isolated build.

## Explicit non-goals

- A complete RBAC/ABAC product, role or permission schema, administrator UI,
  persisted grants, policy discovery, annotations, or a global gate registry.
- Row-state callbacks that authorize after fetching a record, implicit tenant
  discovery, arbitrary SQL supplied by policies, or in-memory filtering.
- Field-level visibility or mutation rules, ownership transfer, bulk actions,
  soft deletion, audit trails, impersonation, or authorization caching.
- Updating already-generated application resources in place. Existing source
  remains application-owned; teams can adopt the explicit seam manually.

# v0.17 embedded production assets scorecard

Status: **accepted** — 2026-09-10

Release boundary: v0.17.0 adds and verifies the backward-compatible public
`asset` package while retaining the format-11 scaffold and its public v0.14.2
pin. The v0.17.1 release adopts format 12 against that published runtime,
including official proxy checksums, commands, watcher integration, and the
end-to-end acceptance criteria below. This prevents a merged generator from
producing applications that depend on an unpublished framework version.

Target:

> From a fresh format-12 application, reference application-owned CSS and
> JavaScript through the Forge `asset` function, serve those exact bytes from
> the production binary, edit either file under `forge serve` or `forge dev`,
> and reload the browser exactly once only after the last-good replacement is
> committed. Keep asset ownership, embedding, URL resolution, route wiring, and
> replacement seams visible as ordinary Go; require no Node toolchain or hidden
> production build step.

The smallest complete workflow treats opaque files below
`resources/assets/files` as the production inputs. A generated application
embeds them directly and constructs the framework's bounded asset set in its
explicit application object graph. Stable same-origin URLs, strong validators,
and mandatory revalidation are the safe first production cache contract:
current-only fingerprint URLs are intentionally not claimed because they break
old HTML across rolling deployments unless prior generations are retained.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| A fresh browser application has a complete asset path | passing | The scaffold owns CSS and JavaScript files, an explicit embedded asset package and handler route, and a Forge `asset` function used by the default layout; normal browser pages fetch both assets successfully |
| Production bytes are conventional and self-contained | passing | Direct `go build ./cmd/server` embeds the source bytes without a compiler, manifest, or runtime disk access; application startup validates the bounded inventory, `forge assets:check` and the opinionated test/build gates run the same application-owned validator without writing, and the handler can be replaced with ordinary `net/http` or external storage wiring |
| HTTP caching and representation semantics are safe | passing | Canonical stable URLs support GET, HEAD, strong ETag revalidation and byte ranges with exact media type and length; responses use `public, max-age=0, must-revalidate`, never falsely claim immutable fingerprints, and preserve global security middleware |
| The public edge fails closed | passing | Traversal, encoded separators, directories, dotfiles, case-fold collisions, unsupported active types, irregular files, excessive file count, per-file size, total size, unknown names, and unsupported methods are rejected deterministically without directory listing or content sniffing |
| The development loop includes every asset edit | passing | Regular files below `resources/assets/files` participate in source snapshots regardless of extension; a valid edit rebuilds the embedded binary, promotes through the existing last-good transaction, then emits one LiveReload event, while unreadable, failed, stale, or reverted candidates do not reload or displace the previous bytes |
| Existing applications remain compatible | passing | Formats 4–11 retain their existing test/build/serve behavior and direct commands; format 12 is required only for the new scaffold contract, and the public asset runtime is independently usable |
| Documentation tells the truth | passing | Framework and generated-project guides explain stable revalidated URLs, direct-Go use, supported defaults, replacement seams, and why transforms, fingerprints, and external asset hosts are separate contracts |
| The milestone passes twice without regression | passing | Framework race/vet/build, compatibility, current scaffold, fresh generated PostgreSQL browser/development/production asset journeys, native Windows tests, and independent adversarial review pass twice on the final revision |

## Runnable baseline — 2026-09-10

- Public v0.17.0 and `origin/main` are merge `f7f4c54`. Both pull-request
  Linux/PostgreSQL and native Windows matrices passed twice. The public Go
  proxy resolves the exact tag with module checksum
  `h1:r3JWPwcB8YNNU9+22v4K3Bx6M7S8fbQ4HIB3rXRdlDY=`.
- The merged framework suite passes from a clean worktree. Fresh format-11
  applications compile Blade/Twig-class Forge views and LiveReload accepted HTML
  edits, but the layout references no stylesheet or script, the application
  registers no static route, and the template function map has no asset helper.
- The source watcher recognizes only Go, Forge view, SQL, and root configuration
  files. CSS, JavaScript, images, and fonts cannot trigger a last-good rebuild.
- v0.16 explicitly excludes static-file conventions, production assets,
  manifests, fingerprints, transforms, and Node integration.

## Acceptance evidence — 2026-09-10

- Final candidate revision `7e20f5c` passes `go test ./... -count=1` in
  180.039 seconds and `go test -race ./... -count=1` in 235.847 seconds;
  `go vet ./...` also passes.
- The complete CLI suite creates and inspects a fresh format-12 application,
  checks the application-owned asset inventory, builds the isolated validated
  source tree, exercises the exact routed GET/HEAD/range/method semantics, and
  proves that a missing required asset fails without replacing the last binary.
- Watcher regressions cover unchanged bounded-asset caching, periodic content
  revalidation even when an edit preserves size and modification time,
  immediate non-asset content detection, cheap metadata tracking beyond the
  default asset bounds, and exclusion of the asset root from formats 4–11.
- Independent adversarial review is clean at P0–P2 after the watcher fixes.
- [PR #22](https://github.com/ShanilKoshitha/goforge/pull/22) merged the
  history-preserving adoption at `2d383ce`. Its push and pull-request runs both
  passed Linux/PostgreSQL and native Windows matrices.
- The merged [main workflow](https://github.com/ShanilKoshitha/goforge/actions/runs/34544721681)
  passed Linux/PostgreSQL in 14m35s and Windows in 6m47s. The independent
  [v0.17.1 tag workflow](https://github.com/ShanilKoshitha/goforge/actions/runs/34544797371)
  passed Linux/PostgreSQL in 14m50s and Windows in 7m21s.
- The public Go proxy resolves unsigned lightweight tag `v0.17.1` to merge
  `2d383ce` with module checksum
  `h1:ku7R4obOvY5+HftC+6fk7fqY9A0v2iEKS8WKljk00PE=`. A clean proxy install
  reports `forge 0.17.1`; its no-replace format-12 application passes module
  verification, a no-diff tidy, `assets:check`, `forge test`, and the isolated
  `forge build` workflow.

## Explicit non-goals

- Bundling, minification, transpilation, dependency graph or CSS `url()`
  rewriting, source maps, image optimization, or arbitrary compiler plugins.
- Node, npm, pnpm, Bun, Vite, esbuild, Tailwind, Sass, PostCSS, TypeScript, or
  framework-specific browser adapters.
- Immutable fingerprint URLs, retained asset generations, CDN/object-storage
  publication, external asset hosts, signed URLs, precompressed variants, or
  automatic cache purging.
- HMR, CSS-only replacement, hydration, client-side routing, or production
  LiveReload endpoints.

# v0.16 browser LiveReload scorecard

Status: **accepted** — 2026-09-10

Target:

> From a fresh format-11 application, open a server-rendered browser page through
> `forge serve` or `forge dev`, make a valid Go or Forge-view edit, and have the
> browser reload exactly once after the last-good candidate is fully committed.
> Failed, stale, or reverted candidates must never reload the browser. Keep the
> development mechanism outside generated and production application binaries,
> preserve application security policy, and retain direct Go execution as the
> complete no-rewrite escape hatch.

The smallest complete workflow extends the existing development proxy only.
The proxy injects one external same-origin client into eligible full HTML
documents and exposes randomized process-local script/event paths. Successful
promotion advances a monotonic generation observed through bounded SSE. This is
page reload, not HMR or an asset pipeline.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| The generated browser loop closes automatically | passing | A fresh format-11 page fetched through literal `forge serve` and `forge dev` contains one development-only external client; a committed view or Go edit produces one reload event and the next page contains the accepted content |
| Reload truth follows promotion truth | passing | Initial startup, build/compile failure, unhealthy candidate, stale candidate, failed target swap, rollback, and last-good serving emit no reload; notification occurs only after target promotion, old-candidate cleanup, and compiled-view commit all succeed |
| Response rewriting is narrow and representation-safe | passing | Only successful full `text/html` browser documents with identity encoding and bounded bodies are injected; HEAD, partial/range, attachment, compressed, oversized, fragmentary, and non-HTML responses retain exact bodies and representation headers |
| CSP and application semantics remain authoritative | passing | The client is an external same-origin script compatible with the generated CSP; existing CSP, cookies, status, and unrelated headers remain unchanged, while rewritten development documents discard invalidated validators and become `no-store`; no inline script or policy weakening is used |
| The event edge is bounded and race-safe | passing | A cryptographically random unguessable path prevents application-route collisions; exact GET endpoints, subscriber limits, missed-generation recovery, concurrent clients, heartbeats, disconnect cleanup, proxy shutdown, and malformed/wrong-method requests are deterministic and tested |
| Production remains conventional | passing | Generated source, routes, templates, runtime packages, and production binaries contain no reload endpoint, watcher, injected client, or hidden lifecycle; `go run ./cmd/server` and direct handler/proxy replacement remain exact escape hatches |
| Existing development guarantees remain intact | passing | Stable public address, private health promotion, in-flight request target pinning, last-good view/Go recovery, process labelling, aggregate readiness, and cross-platform tree cleanup continue to pass under `forge serve` and `forge dev` |
| The milestone passes twice without regression | passing | Framework race/vet/build, compatibility, current scaffold, fresh generated PostgreSQL browser journey, native Windows tests, and independent adversarial review pass twice on the final revision |

## Runnable baseline — 2026-09-10

- Public v0.15.0 and `origin/main` are the same merge `ce69301`. Tag workflow
  `34508599218` passed the full Linux/PostgreSQL matrix in 14m05s and native
  Windows in 5m48s; public `go install` reports `forge 0.15.0`.
- Focused merged-main proxy, development-supervisor, and generated-development
  tests pass locally. The proxy currently forwards every application response
  byte-for-byte and exposes no development route or browser notification.
- `forge serve` already identifies the single safe publication boundary: after
  a healthy target swap, stale-source checks, previous-candidate cleanup, and
  compiled-view commit. Failed edits keep the last-good server but require a
  manual browser refresh after the later successful repair.

## Accepted evidence — 2026-09-10

- The complete framework suite passes on the final implementation tree. Focused
  LiveReload, development-proxy, promotion-supervisor, generated-workflow, and
  view-engine tests also pass under the race detector; `go vet ./...` and a
  fresh CLI build pass.
- Deterministic tests prove exact randomized routing, same-origin external
  script delivery, monotonic/missed generations, bounded concurrent SSE,
  idle-deadline recovery, prompt shutdown, strict document eligibility,
  byte-preserving streamed/trailer/no-transform fallbacks, large buffered Forge
  views, and unchanged CSP/cookies/status.
- Supervisor tests prove no notification for initial, failed, unhealthy, stale,
  crashed, reverted, or failed-commit candidates and exact cleanup, compiled-view
  commit, then one reload ordering for an accepted promotion.
- The fresh PostgreSQL journey exercises initial client discovery,
  valid-edit notification and accepted HTML, invalid-edit silence with the
  byte-identical last-good page, repair notification, and active-SSE Ctrl-C
  shutdown. Push workflow `34515521526` passed Linux/PostgreSQL in 13m59s and
  Windows in 6m37s; pull-request workflow `34515548278` independently passed
  Linux/PostgreSQL in 13m53s and Windows in 6m04s.
- Independent adversarial review initially found transformation opt-out,
  streaming/trailer, idle deadline, large-view length, and duplicate-marker
  defects. Each now has a regression test; the final re-review reports no
  remaining P0, P1, or P2 finding after ten repeated focused race runs and vet.

## Explicit non-goals

- JavaScript/CSS compilation, bundling, minification, fingerprints, manifests,
  npm/Node/Tailwind integration, static-file conventions, or production assets.
- Hot module replacement, CSS-only replacement, component state preservation,
  client hydration, framework-specific browser adapters, or browser automation.
- WebSockets, a public runtime event API, generated application routes, template
  directives, service workers, browser extensions, or production middleware.
- Rewriting compressed, streamed, ranged, downloadable, fragmentary, or
  unbounded responses; weakening or replacing an application CSP to force the
  client to run under a deliberately restrictive custom policy.

# v0.15 unified development workflow scorecard

Status: **accepted** — 2026-09-10

Target:

> From a fresh format-11 application with its explicit services and migrations
> already configured, run one `forge dev` command to start the watched HTTP
> server, durable queue worker, and recurring scheduler; receive process-labelled
> output and one truthful all-ready signal; exercise HTTP and scheduled work; and
> stop every owned process tree together. Keep infrastructure, migrations,
> configuration, source, and every production process independently inspectable
> and runnable through their existing commands.

The smallest complete workflow is intentionally format 11 only. Earlier project
formats do not contain the same three-process application contract. `forge dev`
composes the existing development server, worker, and scheduler; it does not
create a combined runtime or a production supervisor.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| One command starts the complete generated application | passing | Exact no-argument `forge dev` validation starts only the watched server, `go run ./cmd/worker`, and `go run ./cmd/scheduler` from a format-11 project; older, future, malformed, and non-project invocations fail before spawning work |
| Readiness is truthful and output remains usable | passing | Every stdout/stderr line is atomically labelled by service, partial and concurrent writes cannot interleave prefixes, and the single stack-ready message appears only after the existing server, worker, and scheduler startup signals have all been forwarded |
| Failure is fail-fast and attributable | passing | A startup failure, clean premature exit, or later unexpected exit identifies the service, retains the underlying cause, cancels and reaps both peers, and never prints a false ready signal |
| Cancellation owns the whole tree | passing | Ctrl-C/SIGTERM cancels one shared context, waits for all three services within an outer grace that exceeds generated defaults and is explicitly configurable for longer application drains, returns successfully, and leaves no server listener, Go wrapper, worker, scheduler, or descendant alive on Linux or Windows |
| Existing development guarantees remain intact | passing | `forge serve` retains last-good view/Go compilation, private liveness promotion, recovery after invalid edits, one stable public address, and its current direct `go run ./cmd/server` escape hatch under `forge dev` |
| Side effects remain explicit | passing | The command does not start Compose/PostgreSQL/Mailpit, apply migrations, mutate `.env`, generate application resources or ORM source, open a browser, respawn crashes, or conceal missing configuration; only the existing managed view compilation/publication behavior remains |
| Production remains conventional | passing | Generated `cmd/server`, `cmd/worker`, and `cmd/scheduler` stay independent ordinary Go entrypoints; the existing `forge serve`, `queue:work`, and `schedule:work` commands and direct `go run`/`go build` paths remain exact escape hatches |
| Fresh application journey is end to end | passing | A fresh no-replace format-11 application with PostgreSQL materializes a registered scheduled job, executes and acknowledges its durable effect, serves HTTP, survives and recovers from an invalid view edit through the server path, then shuts down cleanly from one `forge dev` invocation |
| The milestone passes twice without regression | passing | Framework race/vet/build, released-format compatibility, current scaffold, generated PostgreSQL workflow, native Windows process tests, and independent adversarial review pass twice on the final revision |

## Accepted evidence — 2026-09-10

- Implementation revision `7961c2a` passed the complete local framework suite
  twice, vet, Linux cross-compilation, repeated native Windows/race process
  tests, and a fresh no-replace format-11 application's verify, zero-diff tidy,
  tests, vet, and server/worker/scheduler/console builds.
- GitHub push run `34505278948` and pull-request run `34505331431` independently
  passed the full Linux/PostgreSQL and native Windows matrices. Each live matrix
  ran the generated PostgreSQL journeys twice, including `forge dev` schedule
  materialization, worker effect, HTTP/view recovery, signal shutdown, and port
  release.
- Two independent adversarial reviews found no remaining P0/P1 issue after the
  canonical readiness, bounded output, attributable failure, and configurable
  descendant-aware shutdown repairs.

## Runnable baseline — 2026-09-10

- Public v0.14.3 is fixed at merge `f1550c0`; its merged-main and tag CI runs
  passed the Linux/PostgreSQL and native Windows matrices, and the public Go
  proxy resolves the exact lightweight unsigned tag and checksums.
- A clean branch from `origin/main` starts with independent `forge serve`,
  `forge queue:work`, and `forge schedule:work` commands. The generated guide
  requires separate terminals, and there is no `forge dev` command or aggregate
  process lifecycle.
- `forge serve` already owns cross-platform descendant cleanup, last-good server
  replacement, and an explicit startup line. Generated worker and scheduler
  processes already emit stable structured startup events. The new workflow can
  compose these contracts without changing application runtime packages,
  project format, or database schema.

## Explicit non-goals

- Starting or configuring Docker Compose, PostgreSQL, Mailpit, or any other
  external service; applying migrations; changing `.env`; or repairing missing
  dependencies.
- Browser LiveReload/HMR, response rewriting, frontend asset compilation, Node,
  npm, Tailwind, or a production asset manifest. Browser reload is the next
  independent development-loop boundary after this process workflow.
- Production supervision, daemonization, crash restart, a TUI, persisted logs,
  Procfile/process discovery, arbitrary user-defined processes, or remote
  development.
- Hot replacement of worker or scheduler processes. The watched HTTP server
  keeps its accepted last-good reload behavior; background-process source or
  registry changes require restarting the single `forge dev` command.
- A new project format, generated runtime abstraction, service container,
  reflection, process discovery, or changes to queue/schedule delivery semantics.

# v0.14.2–v0.14.3 schedule integration hardening scorecard

Status: **accepted** — 2026-09-10

Target:

> From a fresh format-11 application, bind a scheduled job through an explicit
> worker registration contract, run an occurrence-aware payload factory that
> cooperates with the scheduler operation deadline, and fail at startup before
> dispatch when the target name is absent from generated worker wiring.
> Preserve the existing durable cursor and public APIs, and prove password-recovery response
> equalization through deterministic control-flow invariants rather than noisy
> sequential wall-clock comparisons.

The release is staged. v0.14.2 publishes the backward-compatible runtime
factory seam, deterministic security acceptance, and generated target-name
validation that compiles against v0.14.0. v0.14.3 pins the immutable new
runtime from generated format-11 applications and adopts `DynamicContext` in
their schedule journey. No project-format or database migration boundary changes.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Dynamic schedule work can cooperate with cancellation | passing | `schedule.DynamicContext` receives the exact operation context; deadline cancellation returns promptly, preserves `errors.Is`, dispatches nothing, rolls back the schedule transaction, releases its row lock/connection, and permits a later healthy retry |
| Existing schedule source and durable identity remain compatible | passing | Existing `schedule.Dynamic` callers compile unchanged; static and legacy factories keep their behavior; switching an equivalent factory to `DynamicContext` retains the existing payload-strategy fingerprint and durable cursor |
| Schedule target names must be wired into the generated worker registry | passing | A collision-proof generated manifest package lists built-in and application job-definition target names from the generator metadata; scheduler and schedule-list startup reject every schedule target absent from that set before configuration or durable work; no reflection, scanning, or handler construction is used. Application-authored same-name definitions and deployed worker queue selection remain explicit operator-owned contracts |
| Password-recovery equalization is deterministic | passing | `Request` has one public return path through an envelope around its private branch-complete work method; the response deadline is selected before that work, its child context is shorter, envelope order and delay bounds are pure-tested, and live known/absent/failure acceptance retains identical public responses plus absent-account state invariants without comparing sequential wall times |
| Generated concurrency evidence proves actual contention | passing | Two separate scheduler processes are synchronized around one locked due row and report one enqueue plus one contention result, while the worker still executes exactly one durable effect |
| The public workflow remains ordinary Go | passing | The generated registry, registered-name set, validation, context-aware factory, scheduler process, SQL adapter, and direct commands remain inspectable and replaceable without runtime discovery |
| The milestone passes twice without regression | passing | Framework race/vet/build, fresh public no-replace format-11 application, generated PostgreSQL acceptance, native Windows CLI, and independent adversarial review passed twice on the final code revision |

## Runnable baseline — 2026-09-10

- PR 16 merged at `2ad9216`; exact-head push and pull-request CI runs
  34425850063 and 34425853148 passed Linux/PostgreSQL and native Windows.
- Merged-main run 34426817857 passed, and lightweight unsigned public tag
  `v0.14.1` points directly to that merge. Release run 34427762091 passed on
  attempt 2.
- The public Go proxy resolves v0.14.1 to `2ad9216` with module checksum
  `h1:yId9EVdFJ7ZM6MXQFh7bZ3xQb2PT6c1qsS0fqohGirA=` and module-file checksum
  `h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=`. A clean public install
  reports `forge 0.14.1` and generates a no-replace format-11 application that
  passes tests, vet, and server, worker, scheduler, and console builds.
- The first v0.14.1 tag attempt exposed the baseline defect: two sequential
  password-recovery requests independently sample response jitter and absorb
  unbounded runner scheduling noise, so their elapsed times differed by
  507.4ms against a 500ms threshold even though every functional workflow
  passed. A rerun passed; threshold relaxation is explicitly not acceptance.
- Independent review also found that legacy dynamic factories cannot observe
  the operation deadline, generated scheduler startup does not prove its job
  targets have worker handlers, and the process-level concurrency journey does
  not force or assert actual row contention.

## v0.14.2 public runtime evidence — 2026-09-10

- PR 17 exact-head push and pull-request runs 34481655944 and 34481719203
  passed Linux race/vet/build, released-format compatibility, public v0.14.1
  format-11 upgrade, the current scaffold, generated PostgreSQL applications,
  and native Windows checks. Merged-main run 34483256073 passed the same gates.
- Lightweight unsigned tag `v0.14.2` points directly to merge `5114808`.
  The public Go proxy resolves that tag with module checksum
  `h1:HIjhuU2nsE17gLc6C537lmILYB5aRkfs2dWlXFEuLYA=` and module-file checksum
  `h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=`. A clean public install
  reports `forge 0.14.2`.
- Two consecutive final-head framework suites passed locally with module files
  read-only, alongside vet, schedule race tests, tidy, a native Windows CLI
  build, and an untouched no-replace format-11 application pinned to the public
  module. Independent exact-head review found no P0–P3 actionable finding.
- Tag run 34484640732 passed Linux/PostgreSQL in 13m24s and native Windows in
  6m07s, closing the public runtime stage before v0.14.3 adoption.

## v0.14.3 adoption evidence — 2026-09-10

- Final code head `e8c8f78` passed two consecutive local read-only framework
  suites. Vet, schedule race tests, module tidy, whitespace checks, and a native
  Windows CLI build reporting `forge 0.14.3` also passed.
- An untouched no-replace format-11 application pinned the checksum-verified
  public v0.14.2 module, contained the `DynamicContext` example, passed module
  verification and tidy, and passed framework test/vet/build plus server,
  worker, scheduler, and console compilation.
- Exact-head push run 34487616892 passed Linux/PostgreSQL in 12m50s and native
  Windows in 6m31s. Pull-request run 34487651117 independently passed those
  gates in 13m21s and 5m40s. Both included full race/vet/build, released
  format-8 compatibility, public v0.14.1 format-11 upgrade compatibility, the
  current public-runtime scaffold, and generated PostgreSQL acceptance with
  forced scheduler contention.
- Independent repaired-head review found no P0–P3 actionable findings and
  verified that the commits contain only Shanil authorship with no co-author,
  sign-off, or assistant-related trailers.
- The documentation-only final-head push run 34489329926 exposed an older
  development-supervisor test race: the test mutated source after proxy
  creation but before initial startup was committed, so its intended promotion
  crash never occurred. Duplicate run 34489335374 passed. The regression test
  now waits for the existing committed-startup readiness message before either
  candidate-exit scenario; both scenarios pass 1,000 ordinary iterations and
  100 race-instrumented iterations.

## Explicit non-goals

- Preempting arbitrary Go callbacks that ignore context. Safe hard bounds for
  uncooperative application code require process isolation or a different
  reservation protocol; v0.14.2 instead provides the normal Go cancellation
  contract and documents legacy `Dynamic` as prompt computation-only code.
- A new project format, schema migration, automatic schedule retirement,
  seconds-level cron, overlap, replay, dashboards, or unified development
  orchestration.

# v0.14 durable recurring schedules scorecard

Status: **accepted** — 2026-09-10

Target:

> From a fresh application, explicitly bind a typed application job to a
> five-field recurring schedule, migrate durable PostgreSQL schedule state,
> inspect it, materialize a due occurrence once across competing scheduler
> processes, and execute it with the existing queue worker. Time zones, DST,
> missed runs, overlap, crash atomicity, and definition changes must have
> written conservative behavior while the registry, payload, SQL, processes,
> and direct database/queue escape hatches remain ordinary inspectable Go.

The intended workflow is:

```text
forge new reportboard --module example.com/reportboard
cd reportboard
forge make:job RebuildSearchIndex
# Bind RebuildSearchIndexDefinition in internal/schedules/registry.go.
forge migrate
forge schedule:list
forge schedule:run
forge schedule:work
forge queue:work
```

The scheduler only materializes typed jobs into the existing durable queue. A
separate queue worker executes them. Fresh applications move to project format
11 only after the new schedule runtime is available from a public immutable
module tag.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Schedule definitions are explicit and exact | passing | An application-owned registry binds a validated, versioned schedule name and five-field cron expression to one typed job definition and payload factory without reflection, scanning, or persisted application schema |
| Civil-time behavior is deterministic | passing | UTC and IANA zones, DOM/DOW semantics, leap dates, spring-forward gaps, fall-back repeated minutes, and strictly increasing occurrences pass fixed-clock tests |
| Durable materialization is atomic | passing | PostgreSQL row locking plus the existing transactional dispatcher commit the job and cursor together; cancellation, enqueue failure, state failure, panic, and process death commit neither half |
| Multiple scheduler processes are safe | passing | Concurrent runners coordinate per schedule without a global leader; one occurrence creates at most one queue row while unrelated schedule rows can progress independently |
| Missed-run and overlap policy are bounded | passing | Missed occurrences coalesce to the latest occurrence inside a bounded grace window, older windows record a skip, and the default active-job deduplication policy suppresses overlap while advancing the cursor |
| Definition drift fails closed | passing | A canonical declarative fingerprint is stored durably; a same-name mismatch stops without rewriting state or dispatching, dormant rows absent from the explicit registry never run, and changed-registry deployment boundaries are explicit |
| Operational commands form one inspectable workflow | passing | `schedule:list`, one-shot `schedule:run`, daemon `schedule:work`, direct `go run ./cmd/scheduler`, deterministic output, signal cancellation, and bounded configuration all work from outside the source directory |
| Fresh and existing applications remain coherent | passing | Format 11 includes visible registry, scheduler binary, config, migrations, and docs; formats 8–10 retain compatible commands; format 10 belongs-to remains supported; server, worker, scheduler, console, views, and ORM all compile |
| Escape hatches stay complete | passing | Public registry, scheduler, store/coordinator, observer, and PostgreSQL primitives are usable directly; application teams can replace cron registration, payload factories, process wiring, SQL adapter, or the entire scheduler |
| The milestone passes twice without regression | passing | Full framework race/vet/build, fresh generated application, competing-process PostgreSQL acceptance, native Windows CLI coverage, and independent architecture/adversarial review passed twice on the accepted v0.14.3 adoption revision |

## Runnable baseline — 2026-09-09

- `main` merge `a189fc2` passes Linux/PostgreSQL and native Windows in
  [CI run 34380649800](https://github.com/ShanilKoshitha/goforge/actions/runs/34380649800).
  A clean worktree at that commit passes `go test ./... -count=1`, `go vet
  ./...`, and a Windows CLI build after its isolated public module cache is
  populated.
- Lightweight public tag `v0.13.1` resolves to that exact merge, installs as
  `forge 0.13.1`, and generates a no-replace format-10 application that passes
  required belongs-to generation, module verification, tests, and build.
- Durable one-off typed jobs already provide PostgreSQL enqueue, active-job
  deduplication, delayed/absolute dispatch, transactional `Dispatcher.Using`,
  fenced workers, retries, failures, observers, administration, and direct
  store/SQL escape hatches.
- Recurrence is absent: there is no cron parser, explicit schedule registry,
  durable occurrence cursor, missed-run/overlap contract, scheduler process,
  or schedule command surface.
- The implementation is intentionally staged. The runtime/PostgreSQL contract
  lands first while fresh apps remain format 10, then an immutable v0.14.0 tag
  allows the format-11 scaffold to pin resolvable public code in a
  checksum-bearing v0.14.1 CLI release.

## Stage A runtime bridge evidence — 2026-09-09

- The explicit runtime, PostgreSQL adapter, direct operations guide, and
  v0.14.0 bridge version are committed as `678adf7`, `ded8dcf`, `c3d8e78`, and
  `a913230` after the contract commit `c7fa178`.
- `go test ./... -count=1` passed twice on the final local revision. Focused
  `go test -race ./schedule/... ./job -count=2`, `go vet ./...`, module tidy,
  whitespace, and a Windows CLI build reporting `forge 0.14.0` also passed.
- That CLI generated a fresh format-10 application pinned to public v0.13.0
  with no `replace`. Module verification and tidy-diff, generated `forge test`,
  `go vet ./...`, `forge build`, and the worker build all passed.
- An independent adversarial review found no P0. It found and closed unsafe
  mixed-registry replacement guidance and UUID-only schedule job IDs. The
  final contract documents the scheduler-only replacement window, while the
  adapter now persists bounded opaque IDs and tests a custom queue-store ID.
- The live PostgreSQL suite covers 16 competing transactions, restart state,
  row independence, current-minute initialization, misfire coalescing and
  expiry, overlap suppression, drift, cancellation, state conflict, backend
  termination, dormant rows, opaque IDs, and schema constraints. Local
  PostgreSQL credentials did not match the test role, so CI remains the
  authoritative live execution gate before merge.

## Stage A public release evidence — 2026-09-09

- Push run [34419676292](https://github.com/ShanilKoshitha/goforge/actions/runs/34419676292)
  and pull-request run
  [34419733008](https://github.com/ShanilKoshitha/goforge/actions/runs/34419733008)
  independently passed Linux race, vet, compatibility, current-scaffold,
  generated PostgreSQL, and native Windows gates on exact head `5aad387`.
- PR 15 merged as `88f0891`, and merged-main run
  [34420781683](https://github.com/ShanilKoshitha/goforge/actions/runs/34420781683)
  passed the same Linux/PostgreSQL and Windows matrix before publication.
- Lightweight unsigned tag `v0.14.0` points directly to that merge commit. The
  public Go proxy and checksum database resolve it to module checksum
  `h1:Dt5aHth+6iG5ODwUL8mLGL0ye6piP1klyYoGgZ3TE7k=` and module-file checksum
  `h1:UQE0b3seoEHYB618VF1jcflF59zBrHJOEMdGEWkMpPM=`.

## Stage B format-11 adoption evidence — 2026-09-09

- Unsigned commits `59722eb`, `c962cae`, and `045f2d6` add the centralized
  format-11 compatibility boundary and schedule commands, the generated
  application scheduler/registry/configuration/migration, and the process-level
  PostgreSQL acceptance journey respectively.
- `go test -mod=readonly ./... -count=1` passed twice on the integrated code;
  `go vet ./...`, `go mod tidy -diff`, focused command/scaffold tests, whitespace,
  and a Windows CLI build reporting `forge 0.14.1` also pass.
- A fresh local-replace format-11 application passes tests, vet, and builds for
  server, worker, scheduler, and console. A separate fresh no-replace application
  pins checksum-verified public v0.14.0, passes module download, verification and
  tidy-diff, direct tests/vet/builds, `forge test`, and `forge build`.
- The generated live journey creates an application job and explicit `.v1`
  schedule, applies the schedule schema, launches two separate one-shot scheduler
  processes from outside the source directory, asserts one current-minute queue
  row and cursor, lets the queue worker produce one durable effect and acknowledge
  the job, inspects status, and proves signal-driven daemon shutdown. It compiles
  locally; CI PostgreSQL remains the execution gate.
- Independent audit found no P0/P1 scheduler-core design blocker. It required and
  verified format-10 relationship compatibility, format-12 future refusal,
  same-database queue/schedule transactions, worker registration, synchronous
  startup signalling, exact schema/checksums, and process-level acceptance.
- The subsequent accepted v0.14.2–v0.14.3 hardening directly above records the
  final public runtime/adoption boundary, two consecutive local suites,
  independent review, and passing push/pull-request Linux/PostgreSQL and native
  Windows matrices. Public v0.14.3 is fixed at merge `f1550c0`, as independently
  recorded by the accepted v0.15 baseline.

## Explicit non-goals for v0.14

- Database-authored, dynamic, or per-tenant schedules.
- Seconds fields, cron macros, RRULE, holiday/business calendars, or arbitrary
  catch-up and replay.
- Overlapping scheduled jobs, exactly-once handler effects, chains, batches,
  DAGs, dashboards, or a scheduler web UI.
- Alternate Redis, cloud-queue, or Kubernetes scheduler adapters.
- Runtime package scanning, annotations, reflection, server embedding, or a
  generated schedule metadata file.
- Mixed-registry rolling replacement or an automatic durable retirement
  protocol; removals and replacements use a scheduler-only maintenance window.
- A `make:schedule` generator, unified `forge dev`, or frontend asset/HMR work.

# v0.13 required belongs-to resource generation scorecard

Status: **accepted** — 2026-09-09

Target:

> From a fresh application, generate one owner-scoped parent resource and one
> child resource with a required belongs-to relationship. The relationship must
> remain consistent across ordinary Go models, PostgreSQL ownership constraints, exact
> JSON and form decoding, validation, repositories, eager loading, browser
> choices, rendered views, and generated tests. Missing and cross-owner parents
> must be indistinguishable, concurrent parent deletion must fail predictably,
> and all generated behavior must remain directly inspectable and replaceable.

The intended workflow is:

```text
forge new issueboard --module example.com/issueboard
forge make:resource Category --field name:string
forge make:resource Issue \
  --field title:string \
  --belongs-to category:Category
forge migrate
forge serve
```

Fresh applications use project format 10. `--belongs-to` is repeatable and accepts
`<lower_snake_name>:<ExistingResource>`. Relationships in this milestone are
always required. Targets must already be generated owner-scoped resources;
their current model source, not persisted field metadata, is authoritative.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| CLI parsing and preflight are exact | passing | Both flag forms, declaration order, bounds, unsafe or duplicate names, scalar/FK/Go collisions, missing or self targets, and incompatible edited target models fail before any write |
| One ephemeral contract drives every generated layer | passing | The relationship reaches the application model, migration, generated ORM, separate association IDs, JSON/form request, repository, controllers, Forge views, and generated tests without being stored as runtime schema |
| Persistence is explicit and conservative | passing | Format-10 resources expose an owner/ID candidate key; child SQL uses a required indexed `BIGINT` and composite `(user_id, target_id)` foreign key with non-cascading `NO ACTION`; the model exposes the protected key and explicit `belongs_to` field; target deletion maps to an intentional conflict |
| Ownership cannot be crossed | passing | Create and update accept only a target owned by the authenticated user; a missing target and another user's target return the same field-level 422 response; raw cross-owner SQL fails; a target-delete race produces either a valid child or a disclosure-safe relation failure |
| JSON and browser workflows are complete | passing | Exact JSON/form semantics require a positive relation ID; new/edit forms provide bounded owner-only choices and preserve submitted selections and errors; list/show/create/update responses render the loaded relationship |
| Relationship reads remain bounded | passing | List and pagination eager-load in batches with constant queries per batch rather than per row; single-record paths load explicitly; every related query retains the owner predicate |
| Optimistic and rollback safety are retained | passing | Relationship-aware updates require a positive version; stale forms keep safe submitted values and fresh versions; collisions, cancellation, compilation failures, managed-write failures, and concurrent generators preserve complete state |
| Existing applications keep working | passing | Formats 8 and 9 retain implicit and scalar-only generation plus every compatible command, relationship flags fail before writes outside format 10, frozen released applications remain compatible, and handwritten ORM relationships plus direct SQL remain available |
| A fresh PostgreSQL application works end to end | passing | A two-user Category/Issue application migrates, tests, races, vets, builds, and proves owned create/read/update, cross-owner rejection, eager loading, browser choices, stale writes, delete restriction, rollback, and direct-Go escape hatches |
| The milestone passes twice without regression | passing | Push run 34367665049 and pull-request run 34367670306 independently passed Linux/PostgreSQL and native Windows on commit `88ee62f`; independent architecture/security reviews found no P0–P2 blocker |

## Acceptance evidence — 2026-09-09

- [Push CI run 34367665049](https://github.com/ShanilKoshitha/goforge/actions/runs/34367665049)
  and [pull-request CI run 34367670306](https://github.com/ShanilKoshitha/goforge/actions/runs/34367670306)
  independently passed Linux race tests, vet, CLI build, released format-8
  compatibility, current format-10 scaffold checks, the generated PostgreSQL
  suite, and native Windows tests, vet, and build on commit `88ee62f`.
- The PostgreSQL gate ran the relationship workflow twice per run. It generated
  a two-user Category/Issue application, applied and reversed its migrations,
  proved owner-only create/read/update and browser choices, indistinguishable
  missing/cross-owner rejection, exact query budgets, raw composite-FK
  enforcement, stale writes, delete restriction, and the create/delete race.
- The final local revision passed `go test ./... -count=1`, `go test -race
  ./... -count=1`, `go vet ./...`, a Windows CLI build, multi-relationship
  generated-app compilation, format-8/9 scalar compatibility, and adversarial
  candidate-key/dependency-publication tests.
- Independent architecture and security re-reviews found no remaining P0–P2
  blocker after repeatable-template scoping, bounded JSON pagination,
  association-field allowlisting, target-migration DDL validation, and final
  dependency fingerprinting were added.
- PR 13 merged as `9078e56`, and
  [main CI run 34371037790](https://github.com/ShanilKoshitha/goforge/actions/runs/34371037790)
  passed Linux/PostgreSQL and native Windows before the lightweight unsigned
  `v0.13.0` tag was published on that exact merge commit. The public Go proxy
  resolves module checksum
  `h1:ky4g5QeWdG2M3rZdeYd7pkD+8Cz1dVHHWlqewm/SMcw=` and module-file checksum
  `h1:o585rLjoR42pVE79JjdmwrIvum0C5QRRilYqZAJcAkM=`.
- The checksum-bearing v0.13.1 candidate reports the new version and generates
  a format-10 application without `replace` while pinning public v0.13.0. The
  candidate's [push CI run 34375859749](https://github.com/ShanilKoshitha/goforge/actions/runs/34375859749)
  and [pull-request CI run 34375894479](https://github.com/ShanilKoshitha/goforge/actions/runs/34375894479)
  both passed Linux/PostgreSQL and native Windows. The distribution gate
  verified the exact embedded checksums, scalar and required relationship
  generation, generated tests, vet, server and worker builds, serve, and all
  PostgreSQL workflows.
- A blocked public-module download reproduced a Windows transient-sharing
  rollback edge. Resource generation now retries removal of only still-empty
  generated directories, preserves concurrent editor content, and returns the
  final persistent error; deterministic retry, preservation, and exhaustion
  tests pass on the release candidate.
- [Final evidence run 34377344124](https://github.com/ShanilKoshitha/goforge/actions/runs/34377344124)
  exposed an overly aggressive reclaim acceptance budget: the generated worker
  had only 250ms for each PostgreSQL operation, compared with the production
  default of 5s. The same head and the test's second iteration passed, isolating
  a timing flake. The probe now retains a short 3s lease but allows 1s operations
  and reports both worker event streams on timeout, keeping ordinary CI load
  from masquerading as a failed lease-recovery invariant.

## Runnable baseline — 2026-09-09

- Public `main` commit `f23e204` passes Linux/PostgreSQL and native Windows in
  CI run 34353173213. A clean worktree at that commit also passes
  `go test ./... -count=1` after its pinned modules are available.
- Format 9 already generates complete owner-scoped JSON and browser CRUD for
  required and nullable scalar fields with exact request semantics, optimistic
  versions, failure-safe publication, and real PostgreSQL acceptance coverage.
- The static ORM already validates and generates typed belongs-to, has-one,
  has-many, and many-to-many descriptors and batched loaders without runtime
  reflection. Every resource already uses an implicit protected Owner/User
  belongs-to relationship.
- `make:resource` cannot describe any domain relationship. Treating a foreign
  key as an integer field would omit the database constraint, allow cross-owner
  attachment, provide no eager loading or browser choices, and surface races as
  raw persistence failures.
- Format 9 resource tables do not declare the owner/ID candidate key required
  for a database-enforced cross-owner relationship invariant. A visible format
  10 boundary is therefore required instead of silently weakening old projects.
- Recurring schedules remain the next operational milestone. Durable one-off
  jobs already provide delayed dispatch and production workers, while this
  milestone closes a gap in the north star's primary resource-generation path.

## Explicit non-goals for v0.13

- Nullable belongs-to, inverse relationship generation, nested routes,
  relationship mutation endpoints, has-one, has-many, many-to-many,
  polymorphism, composite keys, or configurable cascading deletes.
- Updating or regenerating an existing application-owned resource after its
  initial generation.
- Persisting relationship or field definitions in `.forge`, interpreting them
  at runtime, reflection-driven serialization, lazy loading, package scanning,
  or hidden database queries.
- Searchable/autocomplete relationship widgets, relationship pagination APIs,
  arbitrary display-label configuration, or nested form creation.
- Recurring schedules, scheduler leadership, frontend assets/HMR, or a
  multi-process `forge dev` command.

# v0.12 delivery-backed account recovery scorecard

Status: **accepted** — 2026-09-08

Target:

> From a fresh application, request a password reset through either the browser
> or JSON API, deliver a text-and-HTML message through an explicit durable mail
> path, consume the short-lived link once, and invalidate every prior session.
> Public responses must not disclose account or token state, durable storage and
> logs must not expose the reset secret, and all mail, token, persistence,
> rendering, job, and transport boundaries must remain ordinary replaceable Go.

The intended browser journey is:

```text
GET  /forgot-password
POST /forgot-password
GET  /reset-password?token=<selector.secret>
POST /reset-password
POST /login
```

The JSON equivalents are `POST /auth/password/forgot` and
`POST /auth/password/reset`.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Mail transport is explicit and production-safe | passing | Immutable messages and the small `mail.Sender` seam have bounded RFC 5322 encoding; the SMTP adapter enforces TLS/auth/plaintext policy, timeouts, cancellation, and disclosure-safe errors under focused race tests in both public runs |
| Mail authoring is application-owned | passing | `forge make:mail` and the built-in password-reset message use ordinary generated Go, compiled Forge HTML, and embedded standard-library text templates; callers directly replace the renderer, message, or sender |
| Reset issuance is enumeration-safe and bounded | passing | The generated service silently account-throttles normalized addresses, caps account-dependent work at 400ms beneath a 650–850ms response envelope, emits only a nonblocking fixed-stage failure signal, uses database-clock expiry, and supersedes by user; both PostgreSQL runs proved healthy and deliberately delayed failure parity |
| Durable delivery does not persist the reset secret in plaintext | passing | One generated transaction stores the selector/digest, AES-256-GCM envelope, outbox-ID-only delivery job, and outbox-ID-only seven-day cleanup job; both PostgreSQL runs injected rollback at token, outbox, and final job writes and proved SMTP retry/disclosure behavior |
| Reset consumption is single-use and revocation-safe | passing | A cheap selector/digest/generation preflight rejects invalid links before password hashing, then the generated transaction locks and revalidates token plus user, advances credentials, and consumes once; both PostgreSQL runs covered rollback/retry, replay, selector throttling across sources, concurrency, and session revocation |
| Browser and JSON contracts are complete | passing | Generated request/controller/view suites cover exact JSON/form decoding, validation, privacy headers, generic wrapped-sentinel failures, 202/204, PRG flashes/redirects, secret preservation, and contextual Forge escaping |
| Generation and configuration fail before partial state | passing | Format 9, random outbox keys, loopback-only Mailpit, strict process-specific settings, recovery migration/routes/job wiring, and rollback-safe `make:mail` pass fresh generated tests |
| Existing applications remain conventional and compatible | passing | A frozen format-6 application still compiles after `make:job`; format-aware registry generation adds built-ins only to format 9, and both public runs passed the released format-8 compatibility gate |
| A fresh PostgreSQL application works end to end | passing | Both isolated format-9 journeys generated, raced, vetted, built server/worker, migrated, captured SMTP, injected transaction faults, and exercised browser plus JSON recovery |
| The milestone passes twice without regression | passing | Push run 34292249546 and pull-request run 34292252232 independently passed Linux/PostgreSQL and native Windows on commit `198c6f9`; independent security and architecture reviews report no P0–P2 blocker |

## Runnable baseline — 2026-09-08

- Released `main` at `v0.11.1` passes `go test ./...`, `go vet ./...`, and a
  native Windows CLI build. Public CI already exercises every generated
  PostgreSQL workflow twice.
- Fresh applications have registration, login, password change, account-wide
  credential-version session revocation, source/account throttles, durable typed
  jobs, application-owned Forge views, and explicit transaction helpers.
- No `mail` package, SMTP settings, mail templates or generator, reset-token
  schema, forgotten/reset routes, durable encrypted outbox, or reset acceptance
  journey exists. Registration through password change is therefore strong, but
  a user who forgets the credential cannot recover the account.
- Recurring schedules and relationship-aware resource generation are valuable
  next milestones. They do not close this production authentication gap and are
  deliberately deferred until the written v0.12 target passes.

## Implementation evidence — 2026-09-08

- `go test -race ./mail/...` and `go vet ./mail/...` pass for immutable message
  construction plus STARTTLS, implicit TLS, and explicit loopback-only plaintext
  SMTP delivery.
- Focused CLI and fresh-application tests pass for strict recovery/mail
  configuration and `forge make:mail AuditNotice`, including view compilation,
  collision preflight, rollback, and generated Go tests.
- Generated mailbox race tests pass repeated AES-256-GCM round trips and reject
  ciphertext tampering, row swaps, version changes, wrong keys, invalid IDs,
  invalid messages, and nonce reuse. The migration exposes no plaintext message
  columns and stores reset selectors plus SHA-256 digests only.
- Fresh generated whole-application tests compile the recovery service, strict
  browser/JSON presentation, application-owned dual-template message, atomic
  outbox dispatch adapter, built-in delivery handler, explicit routes, and SMTP
  worker. Focused format-6 compatibility tests confirm later job generation does
  not inject format-9 built-ins into an older application.
- A dedicated real-PostgreSQL acceptance journey compiles and vets. Both public
  CI events passed its generated race and vet gates; the journey
  measures known/unknown response timing, including a two-second database delay
  cancelled by the 400ms internal budget; injects token, outbox, cleanup-job,
  and reset-update failures; and covers hostile Host headers, SMTP 451 retry,
  bounded encrypted retention, opaque payloads, supersession, malformed,
  cross-account, expired, replayed and selector-throttled links, browser and
  JSON completion, credential invalidation, sign-in, session revocation, and
  concurrent one-winner consumption.
- After the final hardening changes, `go test ./... -count=1`,
  `go test -race ./... -count=1`, `go vet ./...`, and a native Windows CLI
  build pass. The reviewed runtime source reports v0.11.1; the
  checksum-bearing distribution patch reports v0.12.1.
- [Push CI run 34292249546](https://github.com/ShanilKoshitha/goforge/actions/runs/34292249546)
  and [pull-request CI run 34292252232](https://github.com/ShanilKoshitha/goforge/actions/runs/34292252232)
  independently passed Linux/PostgreSQL and native Windows on `198c6f9`. Both
  Linux jobs passed framework race/vet/build, released-format compatibility,
  fresh-checkout scaffolding, and every generated PostgreSQL application.
- Independent architecture and security reviews of the final implementation
  found no P0–P2 blocker.
- PR 9 merged as `bc1dd2b`, and
  [main CI run 34294151854](https://github.com/ShanilKoshitha/goforge/actions/runs/34294151854)
  passed Linux/PostgreSQL and native Windows before the lightweight unsigned
  `v0.12.0` tag was published on that exact merge commit. The public Go proxy
  resolves checksum `h1:/wmB+d+3au9s6Ei3iJLPgmfE3DIMyBO3kFaeg0hXEX0=`.
- A clean v0.12.1 candidate reports the new version and generated a format-9
  application without `replace` while pinning public v0.12.0. Module
  verification, resource and mail generation, generated tests, and server
  and worker builds passed; the resulting binary reports the exact public
  runtime checksum.
- [Distribution push run 34296296919](https://github.com/ShanilKoshitha/goforge/actions/runs/34296296919)
  and [pull-request run 34296351515](https://github.com/ShanilKoshitha/goforge/actions/runs/34296351515)
  independently passed Linux/PostgreSQL and native Windows on `047cf77`. Each
  Linux job proved the frozen format-8 path and the checksum-bearing v0.12.1
  format-9 path before running every generated PostgreSQL journey twice.

## Explicit non-goals for v0.12

- Email verification, passwordless or magic-link login, invitations, MFA,
  WebAuthn/passkeys, OAuth/social login, API tokens, or device administration.
- SMS, push, webhooks, marketing/bulk delivery, campaigns, provider analytics,
  inbound mail, attachments, DKIM signing, or a web mail/queue dashboard.
- Exactly-once SMTP delivery. Queue and transport effects remain at least once;
  reset-token consumption is the single-use security boundary.
- A proprietary mail expression language, runtime template discovery, package
  scanning, dependency injection, or reflection-based message registration.
- Recurring job schedules, relationship-aware resources, frontend asset
  bundling/HMR, or a multi-process `forge dev` command.

# v0.11 typed scalar resource generation scorecard

Status: **accepted** — 2026-09-08

Target:

> From a fresh format-8 application, describe an authenticated resource with
> repeated one-shot `--field` arguments and generate one complete,
> production-shaped JSON and browser CRUD slice. String, text, integer, and
> boolean fields must have consistent required or nullable behavior across
> ordinary Go models, PostgreSQL migrations, typed requests, validation,
> repositories, compiled views, and generated tests. New output requires an
> optimistic version for updates and keeps every query owner-scoped. Invalid,
> cancelled, failed, or concurrent generation must not corrupt an existing
> project. The field description is generation input only: no persisted schema,
> runtime interpretation, reflection, or project-format change is introduced.

The intended command shape is:

```text
forge make:resource Issue \
  --field title:string \
  --field notes:text:nullable \
  --field priority:integer:required \
  --field active:boolean
```

Each `--field` is `<name>:<type>[:required|nullable]`. Required is the default;
spelling it explicitly is supported. Repeating a modifier, combining
`required` with `nullable`, or using any undeclared type or modifier is an
error. Calling `forge make:resource Issue` without `--field` retains the
existing single required `name` field workflow.

## Acceptance criteria

| Criterion | State | Evidence |
| --- | --- | --- |
| CLI parsing is exact and useful | passing locally | Parser and CLI tests cover both command spellings, both flag forms, declaration order, bounds, malformed values, unsafe/duplicate/reserved names, types, modifiers, and no-write failure |
| One field contract drives every layer | passing locally | Deterministic generation tests inspect the same ordered four-type contract through models, SQL, ORM input, requests, validation, repositories, controllers, views, and generated tests |
| Generated persistence is typed and inspectable | passing locally | Generated application tests compile concrete Go/PostgreSQL types and prove nullable pointers plus an application-writable-only `Attributes` boundary |
| JSON and form semantics are deliberate | passing locally | Generated controller suites cover missing/null/empty/zero/false, malformed and repeated input, exact JSON member names and duplicates, 400/422 classification, nullable clearing, and contextual escaping |
| New updates are concurrency-safe by default | passing locally | Generated JSON and browser tests require positive versions, prove one winner and one 409, retain the winning record, and refresh stale browser forms with safe submitted values |
| Authentication and ownership remain structural | passing | Generated repositories and controller tests retain authenticated owner derivation and predicates; two public real-PostgreSQL runs proved cross-owner JSON and browser isolation |
| Generation is one failure-safe transaction | passing locally | Failure-injection tests cover collisions, invalid state, rendering/compiler failures, every cancellation/publication boundary, exclusive/managed writes, mode restoration, editor races, and stale ORM rejection |
| Concurrent generators converge without orphaned state | passing locally | Twelve independent CLI processes and same-name contenders converge on complete source, routes, metadata, views, ORM, and migrations; compare-and-swap rollback preserves newer editor bytes |
| Production contains no field-schema machinery | passing locally | State inspection finds no persisted field descriptions; fresh generated tests/builds use static application-owned Go, SQL, and Forge templates with direct repository, ORM, `database/sql`, router, and template seams |
| Format-8 and public compatibility are retained | passing | Implicit no-field output retains TEXT, 2–200 validation, legacy rendering, and versionless updates; both public runs generated legacy and typed resources against v0.10.0 with no `replace`, then verified, tested, built, and served the application |
| A fresh PostgreSQL application works end to end | passing | Both public runs created an all-branch typed resource and exercised ORM/views, migration, JSON/browser CRUD, validation, CSRF, ownership, stale writes, zero/false versus NULL, build, test, serve, and direct-Go paths twice per run |
| The milestone passes twice without regression | passing | Push run 34265651666 and pull-request run 34265714830 independently passed Linux/PostgreSQL and native Windows on commit `8a666c7`; independent final reviews report no P0–P2 finding |

## Local evidence — 2026-09-08

- `go test ./...`, `go test -race ./...`, `go vet ./...`, and Windows CLI
  builds pass. The reviewed runtime CLI reports `forge 0.11.0`; the
  checksum-bearing distribution patch reports `forge 0.11.1`.
- Focused generation tests compile and test implicit legacy, explicit
  `name:string`, mixed scalar, text-only nullable, and nullable integer/boolean
  applications. Twelve independent CLI generator processes converge without
  orphaned registry, metadata, ORM, view, migration, or source state.
- Transaction tests inject editor changes, cancellation at every managed
  boundary, stale model/ORM input, compiler failures, and exclusive/managed
  publication failures. Rollback restores exact prior bytes and modes while
  refusing to overwrite newer editor bytes.
- A clean temporary application with no `replace` retained public runtime
  v0.10.0, generated both implicit legacy and mixed typed resources, passed
  `go mod verify` and `go test ./...`, and built the conventional application
  binary. The CI distribution gate now repeats this exact compatibility path.
- [Push CI run 34265651666](https://github.com/ShanilKoshitha/goforge/actions/runs/34265651666)
  and [pull-request CI run 34265714830](https://github.com/ShanilKoshitha/goforge/actions/runs/34265714830)
  independently passed Linux/PostgreSQL and native Windows. Each Linux job ran
  the complete generated PostgreSQL application suite twice, including the
  nullable zero/false versus SQL `NULL` proof.
- Independent architecture, security, and release reviews identified the
  public-runtime binding gap and release identity as blockers. Strict JSON
  decoding was moved into application-owned generated source, legacy behavior
  was restored, the public compatibility gate was expanded, and the release
  identity was advanced to v0.11.0. Final architecture, security, and release
  reviews found no remaining P0–P2 issue.
- PR 6 merged as `2af6000`, and
  [main CI run 34269752964](https://github.com/ShanilKoshitha/goforge/actions/runs/34269752964)
  passed Linux/PostgreSQL and native Windows before the lightweight unsigned
  `v0.11.0` tag was published on that exact merge commit. The public Go proxy
  resolves checksum `h1:PpJ4gjHJxAM8tZmQ7Ajh++Fce05+w0l9la6F4oe8Dcc=`.
- A clean v0.11.1 candidate distribution generated implicit legacy, explicit
  `name:string`, and mixed typed resources without `replace` while pinning
  public v0.11.0; `go mod verify`, generated tests, and `forge build` passed.
- Merged-main CI run 34272845400 passed the complete Linux/PostgreSQL gate but
  exposed a Windows-only ordering race in the development-supervisor test: the
  test could inject its reload before initial serving readiness. The test now
  waits for that readiness boundary; the paired startup/reload scenarios pass
  1,000 repetitions, 100 race-enabled repetitions, and the complete native
  Windows CLI suite twice.

## Baseline — 2026-09-08

- Format 8 already generates a complete authenticated JSON and browser resource,
  but every layer is fixed to one required `Name string` / `name TEXT` field.
- Resource generation already holds the project-wide interprocess generator
  lock, preflights all destination paths, renders candidate ORM and view
  artifacts, and attempts rollback after a failed managed publication. These
  are reusable primitives, not evidence that arbitrary fields are safe.
- The ORM model parser already understands concrete scalar and nullable Go
  types, protected fields, indexes, defaults, and relationships. This milestone
  uses only the four bounded resource types above and does not expose the full
  model-tag surface as a generator language.
- Existing JSON and browser resource tests prove authentication, owner-scoped
  CRUD, CSRF, escaping, pagination, validation, and optional optimistic
  concurrency for the fixed field. Field-aware output must preserve those
  behaviors while making the optimistic version mandatory for newly described
  resources.
- Public CI already runs race, vet, build, released-project compatibility,
  native Windows CLI checks, and fresh PostgreSQL applications. The milestone
  must extend the real generated-application journey rather than relying only
  on template substring or compilation tests.

## Explicit non-goals for v0.11

- Persisting field definitions in `.forge`, loading a project schema at runtime,
  reflecting over request or model structs, or adding a project format.
- Updating, merging, or regenerating the fields of an existing resource. The
  command remains a one-shot generator that refuses to overwrite human-owned
  files.
- Relationships, foreign-key selection, enums, dates/times, decimal or money
  policy, JSON, binary data, uploads, rich text, array fields, or polymorphism.
- Field defaults, unique or custom indexes, arbitrary database types or SQL
  expressions, database-backed validation, custom validation-rule syntax, or
  schema inference from a live database.
- PATCH semantics, bulk CRUD, search/filter generation, sorting policy, soft
  deletion, API versioning, authorization roles, or automatic migration at
  application startup.
- Interactive prompts, schema files, configuration-driven generation, admin UI
  builders, frontend JavaScript generation, or client-side validation output.

# v0.10 last-good development server scorecard

Status: **accepted** — 2026-09-06

Target:

> From a generated format-4 through format-8 application, run `forge serve`
> once and keep a stable development server while Go, Forge view, environment,
> module, and embedded SQL inputs change. Every stable edit recompiles views
> with the project's own semantics and builds an ordinary server binary. A
> broken edit leaves the last-good server and generated view artifact intact;
> correcting it recovers without restarting the CLI. Cancellation removes the
> complete child process tree and every development artifact. “Last-good” is a
> compile-and-liveness guarantee; `/ready` remains the dynamic PostgreSQL and
> exact-migration-state signal.

## Acceptance criteria

| Criterion | State | Evidence |
| --- | --- | --- |
| The default closes the edit-to-render gap | passing locally | Supervisor tests and the frozen format-4 journey prove compile, build, start, watch, reload, and the documented direct `go run ./cmd/server` escape hatch |
| Watch coverage is complete and loop-free | passing locally | Content-snapshot tests cover writes, creates, deletes, renames, new nested directories, every declared input, exclusions, transient read recovery, and generated-output exclusion; one observer owns polling and generation |
| Bursts converge on the newest source | passing locally | Race tests cover edits during builds, A→B→A content round trips, generation handoff before waiter registration, pre/post-promotion checks, stale discard, proxy revert, and serialized builds |
| Invalid views preserve the last-good state | passing locally | Application-owned compilers run against an isolated source tree; compiler and transaction tests retain exact bytes and mode on failure, while promotion holds the project generator lock through commit or rollback |
| Invalid Go preserves the last-good server | passing locally | Candidate views reach `go build` through a Go overlay without touching the canonical artifact; failures preserve the active process and recover after the next stable edit |
| Process replacement and exit semantics are bounded | passing locally | Managed process trees, joined cleanup failures, candidate exits during proxy startup/promotion, exact PID/address listener ownership, cancellation, proxy limits, and port cleanup have native Windows race coverage |
| Template compatibility is retained | passing locally | The frozen format-4 watched journey passes twice; format-specific compiler tests retain application-owned format 5–8 function maps and format/ORM gates are revalidated for every candidate |
| Production remains conventional | passing locally | Candidate binaries use an OS temporary directory; inspection and scaffold tests show no watcher, compiler, reload endpoint, or injected browser script in application/runtime source |
| Fresh application journey passes twice | passing | Public push and pull-request runs each generated PostgreSQL applications twice and proved valid view/Go edits, invalid-edit preservation and recovery, atomic-save/new-directory detection, readiness, response changes, cancellation cleanup, and port reuse |
| Independent compatibility and platform review passes | passing | Framework race/vet/build, frozen and public released-project checks, Linux PostgreSQL acceptance, and native Windows watcher/process tests passed in both public runs; independent review found no P0–P2 blocker |

## Local evidence — 2026-09-06

- `go test -race ./...`, `go vet ./...`, and `go build ./cmd/forge` pass on
  Windows; the CLI reports `forge 0.10.0`.
- The supervisor, observer, listener-owner, proxy, and cleanup suites pass ten
  consecutive race-enabled runs. Exact Windows PID/address ownership rejects a
  same-port wrong-address match.
- Off-tree view staging, overlay builds, promotion-lock ownership, identical
  concurrent publications, and transient rollback retry pass ten consecutive
  race-enabled runs without exposing an unpromoted generated artifact.
- The frozen format-4 build/serve/check/last-good journey passes twice with a
  real watched server and complete cancellation cleanup.
- A CGO-free Linux CLI test binary compiles successfully. The fresh PostgreSQL
  development journey remains unavailable on this workstation because it has
  no `GOFORGE_TEST_DATABASE_URL`; the public evidence below closes that gate.
- [Push CI run 34065415454](https://github.com/ShanilKoshitha/goforge/actions/runs/34065415454)
  passed Linux/PostgreSQL in 6m36s and native Windows in 3m18s.
- [Pull-request CI run 34065753884](https://github.com/ShanilKoshitha/goforge/actions/runs/34065753884)
  independently passed Linux/PostgreSQL in 6m27s and native Windows in 2m57s.
- Independent product and architecture reviews approved the final transaction,
  process, compatibility, and acceptance boundaries with no P0–P2 finding.
- Merged `main` passed [CI run 34250734126](https://github.com/ShanilKoshitha/goforge/actions/runs/34250734126),
  then lightweight unsigned v0.10.0 fixed merge commit `f6a26e5`. The public Go
  proxy resolved that exact origin and returned module checksum
  `h1:HfC1lNHqsbaJGLGSY/auzY0kg+PWBUTh/5Whj/h/jWQ=`.
- A clean-cache v0.10.1 distribution smoke generated without `--replace`,
  resolved public v0.10.0 with its checksum, passed `go mod verify`, `forge
  test`, and `forge build`, and published the canonical `bin/app.exe`.

## Baseline — 2026-09-06

- The accepted v0.9.1 CLI compiles application-owned views once and then blocks
  in `go run ./cmd/server`; every source edit requires a manual stop and rerun.
- View compilation already has deterministic, last-good publication and mapped
  diagnostics. Process delegation already owns Unix process groups and Windows
  kill-on-close Job Objects, but exposes only a blocking one-shot runner.
- The generated server, view compiler, ORM, readiness route, and direct Go
  commands are already explicit. This milestone needs no template grammar,
  generated application contract, production runtime, or project-format change.

## Explicit non-goals for v0.10

- Browser LiveReload script injection, SSE/WebSocket reload endpoints, CSS/JS
  hot replacement, hydration, frontend asset compilation, npm, or Tailwind.
- Starting Compose/PostgreSQL, applying migrations, changing `.env`, generating
  ORM source, starting workers, or supervising multiple application processes.
- A new Forge view directive, runtime template interpretation, production file
  watching, route discovery, dependency injection, or framework-owned app state.
- Test watching, `forge doctor`, remote development, TLS termination, deployment,
  or changing the strict `forge test` and `forge build` contracts.

# v0.9 cohesive test and build loop scorecard

Status: **accepted** — 2026-09-06

Target:

> From a generated format-4 through format-8 application, use one explicit
> command to prove the inspectable ORM and compiled views are current before
> running the complete Go test suite, and one explicit command to publish a
> production server binary without replacing the last-good artifact on failure.
> Both workflows remain non-mutating freshness gates over conventional Go
> commands with direct, documented escape hatches.

## Acceptance criteria

| Criterion | State | Evidence |
| --- | --- | --- |
| Command boundaries are exact | passing | Focused tests prove both commands accept no arguments, require a project root, support formats 4 through 8, and reject older, future, or malformed manifests before spawning a child or creating `bin` |
| Freshness is checked before execution | passing | Both commands check ORM first and views second; format 4 uses the frozen compiler while formats 5 through 8 delegate to application-owned `cmd/views`; missing, stale, and invalid artifacts stop execution |
| Preflight never rewrites source | passing | Unit, frozen-format, released-format, and fresh PostgreSQL tests retain generated ORM and view artifacts byte for byte; stale diagnostics name `forge orm:generate` or `forge views:compile` |
| Testing remains ordinary Go | passing | Runner tests prove exact `go test ./...` delegation with inherited context and streams; CLI tests preserve child exit codes without duplicate diagnostics |
| Production output is canonical | passing | `forge build` uses `go build -trimpath -o <staged-output> ./cmd/server` and publishes only `bin/app` or `bin/app.exe`; Linux and native Windows CI both execute the command |
| Failed builds preserve the last-good binary | passing | Simulated compiler failures, post-compile cancellation, a real cancelled child process, and a deliberately invalid fresh application all leave the canonical binary byte-identical and remove temporary output |
| Escape hatches remain complete | passing | Framework and generated-project guides give exact ORM/view checks plus direct `go test`, `go build`, and `go run` alternatives for custom flags, packages, tags, targets, and outputs |
| Existing projects remain compatible | passing | A hand-built frozen format-4 application and the frozen format-6 fixture execute both workflows; CI also installs public v0.8.1, generates a no-`replace` format-8 application, and retains its module and generated artifacts unchanged |
| Full workflow passes twice | passing | Public push and pull-request runs each passed framework race/vet/build, released-format compatibility, native Windows checks, and two fresh format-8 PostgreSQL journeys with canonical out-of-tree binary execution and failure-preservation probes |

## Passing evidence — 2026-09-06

- Local `go test -race ./...`, `go vet ./...`, and `go build ./cmd/forge`
  passed. The complete CLI package also passed after the frozen-format and real
  process-cancellation additions.
- A locally installed public v0.8.1 CLI generated a format-8 application with
  its public v0.8.0 dependency and no local replacement. The current CLI ran
  `forge test` and `forge build`, left ORM/views unchanged, and published a
  working `bin/app.exe`.
- [Push CI run 34057241836](https://github.com/ShanilKoshitha/goforge/actions/runs/34057241836)
  passed the Linux/PostgreSQL job in 5m41s and native Windows in 2m44s.
- [Pull-request CI run 34057252760](https://github.com/ShanilKoshitha/goforge/actions/runs/34057252760)
  independently passed the Linux/PostgreSQL job in 5m42s and native Windows in
  3m19s. Each Linux job ran both generated PostgreSQL workflows twice.
- Final independent review found no P0, P1, or P2 blocker after the frozen
  format, public release, real cancellation, version, and evidence closures and
  approved the source milestone for merge and the v0.9.0 runtime tag.
- Merged `main` passed [CI run 34058166132](https://github.com/ShanilKoshitha/goforge/actions/runs/34058166132),
  then lightweight unsigned v0.9.0 fixed merge commit `81cffa3`. The public Go
  proxy resolved that exact origin and returned module checksum
  `h1:qFK3Y42+UV0QyYGKbJfoS+zv/aekEZgtYpMNZ7lq8PY=`.
- The local v0.9.1 distribution smoke generated without `--replace`, required
  public v0.9.0 with its proxy checksums, passed `forge test` and `forge build`,
  retained ORM/views byte for byte, and published `bin/app.exe`.

## Baseline — 2026-09-06

- Post-v0.8 production hardening is accepted. The merged `main` branch passed
  the framework race/vet/build gates and both generated PostgreSQL workflows
  twice in public CI.
- `forge serve` already checks the project root, compiles views, and delegates
  to `go run ./cmd/server`; the CLI has no `test` or `build` command.
- Generated documentation currently sends developers directly to `go test
  ./...` and `go build -trimpath -o bin/app ./cmd/server`. These conventional
  commands work but do not first prove that inspectable ORM and view artifacts
  match their source inputs.
- ORM and view compilers already expose deterministic, non-mutating `--check`
  behavior. Format-4 view semantics are frozen, while formats 5 through 8 own
  an application compiler with the production function map.

## Explicit non-goals for v0.9

- `forge doctor`, environment diagnosis, dependency installation, or toolchain
  repair.
- File watching, process reload, browser LiveReload/HMR, frontend asset
  bundling, or a multi-process `forge dev` command.
- Starting or stopping Compose/PostgreSQL, provisioning a test database, or
  changing `.env`.
- Applying migrations before tests, builds, or server startup. Database state
  remains an explicit `forge migrate` operation.
- Test arguments, package selection, coverage policy, test watching, build
  tags, cross-compilation, release archives, signing, or container images.
- Building the worker or console. Their conventional `go build` commands remain
  documented escape hatches.
- A new runtime abstraction, generated application contract, project-format
  version, or automatic upgrade of older projects.

# Post-v0.8 P1 and production-hardening scorecard

Status: **accepted** — 2026-09-06

## Acceptance criteria

| Criterion | State | Evidence |
| --- | --- | --- |
| Parallel generators preserve managed state | passing | One project-wide interprocess lock covers every `make:*` form; a 12-process job-generation regression verifies every source, metadata, and registry entry and passed repeated local runs |
| Local CLI cannot remain silently stale | passing | The stale root `forge.exe` was removed, root CLI artifacts are no longer ignored, and verified builds target `.tmp/forge.exe` |
| Module paths follow Go's contract | passing | `new` and existing-project parsing use `golang.org/x/mod/module.CheckPath`; `../evil` and other invalid forms are rejected in tests |
| CLI cancellation stops descendants | passing | The executable uses a signal-derived context; Unix process groups and Windows kill-on-close Job Objects retain control after the direct child exits; a signal-resistant descendant-heartbeat regression proves cancellation reaches the child tree |
| Liveness and readiness are truthful | passing | `/health` is process liveness; `/ready` applies a two-second PostgreSQL ping and migration status/checksum check; acceptance polling uses `/ready` with an HTTP client timeout, and the fixture rebuilds its embedded migration manifest after adding the transactional probe |
| Applied schema identity is verified | passing | Migration rows retain name and exact up-script SHA-256; `Up`, `Down`, and `Status` reject missing, renamed, changed, or re-nullified checksums; legacy backfill is one-time and ends with a non-null constraint |
| Deployment defaults fail secure | passing | Generated `.env` defaults to production/secure cookies, both the CLI next steps and generated guide require explicit local HTTP opt-in before `forge serve`, and development PostgreSQL binds only to `127.0.0.1` |
| Durable queue failures do not disclose handler data | passing | Runtime classification, PostgreSQL writes, and historical admin reads allowlist generic diagnostics; panic values, stacks, raw errors, and malformed payload details are excluded |
| Uncooperative jobs cannot hold a live worker forever | passing | Cancellation has a configurable bound; expiry stops the worker and heartbeats without mutating the fenced delivery so lease recovery remains safe |
| Signup commits as one unit | passing | Generated JSON and browser registration create the user, reset the account limiter, and persist the initial session through one PostgreSQL transaction; custom stores require an explicit coordinator outside tests; the acceptance probe forces session insertion failure and expects the user/reset to roll back |
| Migration rollback and status are covered | passing | Unit tests cover reverse ordering, default steps, empty scripts, applied/pending state, drift, legacy tables, and PostgreSQL locking behavior |
| Repository gates | passing | `go test -race ./...`, `go vet ./...`, generated-application compilation, process-tree regression, and a CLI build to `.tmp/forge.exe` pass locally |

The PostgreSQL workflow contains the atomic-signup and readiness probes but was
not executed locally because `GOFORGE_TEST_DATABASE_URL` and a Docker engine
were unavailable. The first branch run correctly rejected an acceptance binary
built before the transactional probe migration existed; the fixture now
rebuilds that binary against the final embedded migration manifest. The
corrected branch passed the race suite and both generated PostgreSQL workflows
twice in [CI run 34054530630](https://github.com/ShanilKoshitha/goforge/actions/runs/34054530630).

# v0.8 account integrity and bounded sessions scorecard

Status: **accepted** — 2026-09-05

Target:

> From an empty directory, generate a PostgreSQL application where an
> authenticated user can change a password through explicit JSON and browser
> flows, immediately invalidate every prior session across processes and
> restarts, and continue safely in one rotated current session. Successful
> login upgrades outdated password hashes, while activity may extend idle
> expiry but can never cross a configured absolute session deadline.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Password change is complete on both transports | passing | One application-owned request explicitly decodes JSON and form fields, applies the same validation policy, verifies the current password, never redisplays password values, and drives both an authenticated API endpoint and a CSRF-protected browser form |
| Credential mutation is atomic and race-safe | passing | PostgreSQL replaces the hash and increments a visible credential generation with a compare-and-swap predicate; deterministic SQL and same-cookie tests prove stale changes and logins cannot restore an obsolete hash, both win, or erase the winning rotated session |
| Revocation is immediate and global | passing | Password change preserves only a rotated current session, while sign-out-everywhere performs an unconditional atomic generation advance linearizable with password change; stale API and browser cookies fail on another process and after restart |
| Authenticated sessions prove current credentials | passing | Sessions carry the observed credential generation, both authentication middleware compare it with the current user before protected work, and mismatches invalidate server state without emitting an out-of-order cookie that could overwrite a concurrent rotation |
| Password hashes upgrade transparently | passing | Successful login conditionally persists a current-strength replacement without advancing the credential generation; failed weak-hash verification is padded to bounded current work and concurrent password change remains authoritative |
| Idle and absolute lifetime are independent | passing | A validated explicit session policy lets activity slide idle expiry without crossing durable issuance plus the absolute deadline; fresh applications expose both durations as typed environment configuration |
| Failures remain safe and bounded | passing | Exact JSON/form failures, authenticated-before-CSRF ordering, account/source throttles, safe current-password errors, and generated HTML tests prevent password, hash, identity, and implementation-cause disclosure |
| Generated behavior stays inspectable | passing | Credential generation, repository compare-and-swap methods, routes, controllers, requests, configuration, views, and SQL are ordinary application-owned files; the generic session store and password hasher remain replaceable |
| Compatibility is explicit | passing | An application generated by immutable v0.7.1 passes current runtime tests and compatible CLI checks; format-8 resource and future-format generators refuse before writes instead of injecting incompatible contracts |
| Full workflow passes twice | passing | Framework race/vet/build and two fresh format-8 PostgreSQL applications pass the complete password, revocation, rehash, expiry, migration, browser, API, CRUD, ORM, and job journeys on both the runtime and final distribution commits |

## Baseline — 2026-09-05

- Released v0.7.1 passes `go test ./... -count=1`; the generator package
  completed in 62.172 seconds. Public CI already proves the existing generated
  PostgreSQL auth, session rotation, throttling, request-boundary, CRUD, ORM,
  views, and jobs workflows.
- Password hashes are parameterized PBKDF2-SHA256 values with dummy
  verification and a working `NeedsRehash` predicate, but login never persists
  a stronger replacement.
- Generated auth exposes registration, login, current-user, and single-session
  logout only. The user schema has no credential generation, repository writes
  no credentials after registration, and sessions carry only `user_id`.
- Browser activity continually assigns `now + lifetime`; there is no absolute
  deadline. API and browser refresh behavior is inconsistent, and the
  two-hour lifetime is hard-coded in route construction.
- Independent read-only security and product audits found no current P0
  exploit. They independently ranked missing password recovery controls and
  bounded session lifetime ahead of CLI convenience wrappers.

## Explicit non-goals for v0.8

- Forgotten-password reset, email verification, magic links, notifications,
  or outbound mail. Those require a dedicated delivery and token-lifecycle
  milestone rather than a development-only mail stub.
- MFA, WebAuthn/passkeys, OAuth/social login, API tokens, device/session
  inventory, remembered devices, impersonation, or an authentication event
  ledger.
- Password-strength breach services, organization policy, localization, or a
  proprietary password-policy language.
- Signing-key rotation or encrypted session payloads. Cookies contain only a
  signed random identifier; key rotation needs an explicit multi-key deployment
  contract.
- `forge test`, `forge build`, or a development watcher. Conventional Go
  commands already work; their cohesive CLI milestone remains next in product
  priority after account integrity.

## Acceptance evidence — 2026-09-05

- Public CI passed on the immutable `v0.8.0` runtime commit `cf4a66d` in
  [run 34002623263](https://github.com/ShanilKoshitha/goforge/actions/runs/34002623263).
  The final `v0.8.1` distribution commit `c4d206a` passed the same gates in
  [run 34002985340](https://github.com/ShanilKoshitha/goforge/actions/runs/34002985340).
  Each run executes the fresh PostgreSQL application workflows with `-count=2`.
- A clean-cache `go run
  github.com/ShanilKoshitha/goforge/cmd/forge@v0.8.1 new` generated a format-8
  application without `--replace`. Its public `v0.8.0` dependency passed
  `go mod verify`, `go test ./... -count=1`, `go vet ./...`, and
  `go build ./cmd/...`.
- The immutable `v0.7.1` CLI at commit `415449c` generated a real format-7
  application. Pointed at the v0.8 runtime, it passed `go test ./...`, current
  `views:compile --check`, and `orm:generate --check`; `make:resource` required
  format 8 and left no files behind.
- Two independent final read-only reviews reported no remaining P0, P1, or P2
  defect after the concurrency, stale-cookie, middleware-ordering, weak-hash
  timing, request-boundary, and template-disclosure fixes.
- Lightweight unsigned tags `v0.8.0` and `v0.8.1` point to their immutable
  runtime and distribution commits. The public v0.8.0 checksums embedded by
  v0.8.1 were resolved through the Go proxy and checksum database.

# v0.7 explicit request boundary and hardened HTTP kernel scorecard

Status: **accepted**

Target:

> From an empty directory, generate a PostgreSQL application whose ordinary Go
> request contracts decode JSON and browser forms, normalize deliberately,
> validate realistic typed input with reusable application rules, and return
> stable safe failures. Its explicit HTTP kernel bounds transport resources,
> correlates every request outcome, rejects unsafe cross-origin policy, and
> shares authentication throttles across processes without reflection,
> discovery, or hidden proxy trust.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Validation is application-extensible without reflection | passing | Applications implement an exported rule contract or use `RuleFunc`; rules are explicit values with no tag scanning, discovery, or framework-global registry, while format-6 callers retain their existing API |
| Real request states and composition are representable | passing | New APIs distinguish missing, explicit null, empty, and present values where policy requires it; numeric ranges, list size/uniqueness/each, nested-prefix merging, and conditional/cross-field custom rules have deterministic tests |
| Validation failures have a stable safe contract | passing | Each violation retains a field path, stable rule code, and human message without the rejected value; API output is machine-readable and browser helpers render escaped messages from the same result |
| Generated requests own both transports | passing | One application-owned request type explicitly decodes/normalizes JSON and URL-encoded form input, and JSON/browser controllers delegate to it instead of duplicating field extraction or validation policy |
| Transport failures are exact | passing | Wrong media type is 415, malformed or type-invalid input is 400, oversized JSON/form input is 413, semantic validation is 422, unknown JSON fields and repeated scalar form fields remain rejected, and causes never reach clients |
| Middleware configuration is validated and immutable | passing | Safe constructors reject invalid values at application startup, defensively copy caller collections, reject wildcard origins with credentials, and enforce requested preflight method/header policy with correct `Vary` behavior |
| Request lifecycle is correlated once | passing | Invalid or oversized inbound request IDs are replaced; a typed accessor exposes the accepted ID; success, HTTP error, cancellation, and panic each produce exactly one completion record with the same ID, method, matched route pattern, status, duration, and outcome |
| The generated HTTP server is bounded | passing | Typed defaults and environment overrides configure header/read/write/idle timeouts, handler deadline, and maximum header bytes; invalid values fail startup; custom `http.Server` and streaming routes remain explicit escape hatches |
| Browser and API error surfaces stay explicit | passing | Browser middleware failures render safe HTML while API routes retain the stable JSON envelope; no `Accept` negotiation or hidden controller switching is introduced |
| Authentication throttling is production-shaped | passing | Fresh PostgreSQL applications use an atomic shared store with database time, bounded pruning, hashed keys, restart persistence, and cross-process enforcement; the memory store remains an explicit local/test option |
| Proxy trust is explicit and spoof-resistant | passing | Only validated configured CIDRs can supply forwarded client addresses; untrusted peers and malformed/ambiguous chains fall back safely to the direct peer, with no automatic forwarding-header trust |
| Security policy stays inspectable | passing | Generated same-origin defaults include a compatible CSP and existing headers; HSTS is explicit production/TLS policy; every middleware call remains visible, removable, and replaceable in application code |
| Compatibility and full workflow pass twice | passing | Format-6 auth/CRUD/views/ORM/jobs fixtures retain their behavior; framework tests/vet/race and two fresh format-7 applications pass generated tests/vet/race/build plus two-process PostgreSQL request-boundary journeys without leaked schemas or processes |

## Baseline — 2026-09-05

- The accepted v0.6 worktree passes `go test ./... -count=1`; the slowest
  package completed in 58.721 seconds.
- Existing strengths include explicit middleware composition, standard
  `net/http` escape hatches, strict JSON media-type and unknown-field handling,
  bounded form parsing, duplicate scalar-form rejection, scoped method
  override, session-bound CSRF, Unicode-aware strings, and generated request
  structs shared at the validation-method level.
- `validation.Field` has an unexported method, so an application package cannot
  implement the advertised public extension interface. Only string and `int`
  fields exist; resource version checks already mutate the error map manually.
- `BindJSONLimit` converts `http.MaxBytesError` to HTTP 400 while the equivalent
  form boundary returns 413. JSON and browser field decoding/normalization are
  duplicated across controllers.
- `httpx.CORS` reflects every origin with credentials when `AllowedOrigins`
  contains `*` and `AllowCredentials` is true. Its caller-owned slices remain
  mutable after construction.
- Generated servers configure only `ReadHeaderTimeout`. Request IDs accept and
  reflect arbitrary inbound values, access logs omit correlation and route
  patterns, and panic paths do not produce the same completion record.
- Generated authentication throttling uses a process-local memory store. It is
  reset by restarts, multiplied by replicas, and cannot distinguish clients
  behind a trusted proxy without application-specific replacement code.
- Independent read-only audits found no P0 defect in the accepted auth/session
  contract itself. Account-wide revocation, password changes, transparent
  rehash, and absolute session lifetime form the next dedicated account-
  security milestone rather than being partially folded into this boundary.

## Accepted evidence — 2026-09-05

- Local `go test ./... -count=1`, `go test -race ./... -count=1`, `go vet
  ./...`, and `git diff --check` pass. The PostgreSQL-only tests skip locally
  because this workstation has no available PostgreSQL service.
- A frozen, inspectable format-6 application containing authentication, owner-
  scoped CRUD, compiled views, typed ORM code, and a typed job passes tests,
  vet, builds, and the current compatible generators. The v0.7-only resource
  generator requires an explicit format upgrade instead of emitting
  incompatible source into older applications.
- Fresh format-7 scaffold and resource tests compile and pass, including exact
  JSON/form failures and the reusable application-owned `SafeText` rule.
- Public [CI run 33999322140](https://github.com/ShanilKoshitha/goforge/actions/runs/33999322140)
  passed formatting, the live PostgreSQL race suite, vet, the CLI build, and
  both isolated format-7 PostgreSQL generated-application workflows with
  `-count=2` in 4 minutes 26 seconds. The workflows include generated tests,
  vet, race builds, cross-process/restart throttling, browser/API CRUD, ORM,
  jobs, migrations, graceful shutdown, and cleanup.
- Lightweight unsigned tags `v0.7.0` and `v0.7.1` point to their immutable
  public commits. A fresh-cache `go run
  github.com/ShanilKoshitha/goforge/cmd/forge@v0.7.1` generated a format-7 app
  without `--replace`; that app passed `go test ./...`, `go mod verify`, `go
  vet ./...`, and `go build ./cmd/...` against the public v0.7.0 runtime.
- Independent final read-only reviews found no remaining P0, P1, or P2 code or
  distribution defect.

## Explicit non-goals for v0.7

- Reflection/tag validators, runtime validation discovery, database-backed
  `unique`/`exists` rules, localization, multipart uploads, OpenAPI/schema
  generation, or client-side validation generation.
- Password reset, email verification, password change, account-wide session
  revocation, MFA/WebAuthn, session inventory, signing-key rotation, or an auth
  event ledger. Those require the dedicated account-security and notification
  milestones.
- Middleware aliases, annotations, dependency injection, runtime ordering,
  tracing exporters, metrics, compression, general host allowlists, or
  automatic trust of any forwarding header.
- Schema-driven resource fields, API versioning, bulk CRUD, soft deletion, or
  speculative JSON conventions unrelated to the request-boundary proof.

# v0.6 durable PostgreSQL jobs scorecard

Status: **accepted**

Target:

> From an empty directory, generate a PostgreSQL application that can define a
> typed job, enqueue it atomically with domain writes, run it in a conventional
> project-owned worker, recover from crashes, retry predictably, and operate
> terminal failures. Delivery is explicitly at least once; every durable state
> transition is inspectable SQL and every runtime component is replaceable.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Typed jobs are explicit generated Go | passing | `forge make:job SendWelcome` atomically creates an application-owned typed payload/handler/test plus a deterministic generated registry; duplicate names and partial writes are rejected without runtime scanning or reflection |
| Dispatch composes with transactions | passing | The same API accepts `*sql.DB` and `*sql.Tx`; a job is invisible before commit, durable after commit, and absent after rollback or panic |
| Claims are concurrent and bounded | passing | PostgreSQL workers claim only available capacity in short `FOR UPDATE SKIP LOCKED` transactions, commit before handlers run, use database time, and respect configured queues and concurrency |
| Leases recover safely | passing | Heartbeats extend leases; killed workers are reclaimed; every mutation is fenced by job ID, worker ID, and lease generation; stale owners cannot acknowledge, retry, release, or fail a reclaimed job |
| Retry behavior is durable and finite | passing | Attempts, timeout, maximum attempts, and backoff are snapshotted at dispatch; returned errors, timeouts, panics, explicit permanent failures, and retry-after overrides have deterministic tests; exhausted crashed leases terminate |
| Failure operations are complete | passing | Terminal jobs move atomically to `failed_jobs`; `forge queue:failed`, `queue:retry`, and `queue:forget` delegate to application-owned code, omit payloads by default, and pass live PostgreSQL tests |
| Delays and active deduplication are precise | passing | One-off delay/at scheduling uses durable database timestamps; concurrent dispatch with the same explicit key creates one active row and reports enqueued versus duplicate without claiming exactly-once execution |
| Worker lifecycle is production-shaped | passing | A generated `cmd/worker` runs outside the source tree, stops claiming on shutdown, drains within a configured grace period, cancels afterward, retains heartbeats while draining, and leaves forced work recoverable |
| Observability is useful and safe | passing | Structured lifecycle events cover dispatch through completion/failure with stable IDs, queue, attempt, timing, and outcome; default logs never contain payloads and observer failures cannot corrupt durable state |
| Storage boundaries are defensive | passing | Payload/error/name/key sizes are bounded, malformed payloads fail terminally, unknown job versions remain unclaimed for rolling deploys, SQL values are parameterized, and queue state errors preserve causes |
| Escape hatches remain complete | passing | Public store/observer contracts, raw `database/sql`, transaction dispatch, an application-owned worker main/registry/migration, and inspectable claim/ack primitives allow replacement without framework startup magic |
| Existing framework contracts remain intact | passing | Format-5 views and format-4 ORM compatibility fixtures retain their behavior; fresh format-6 auth, validation, sessions, owner CRUD, ORM, and view checks pass |
| Full generated workflow passes twice | passing | Two independent fresh format-6 applications pass tests, vet, race/build checks, generated-artifact inspection, and isolated PostgreSQL job/browser/ORM journeys without leaked schemas or processes |

## Baseline — 2026-09-05

- The accepted v0.5 worktree passes `go test ./... -count=1`; the complete run
  finished successfully in 62.113 seconds for the slowest package.
- The repository contains no job runtime, PostgreSQL queue adapter, job
  generator, worker command, queue migration, or queue operations.
- Existing `orm.Executor` and `database.Transaction` seams already allow one
  typed dispatch path to compose with both `*sql.DB` and `*sql.Tx` without
  coupling jobs to ORM policy.
- Generated applications already own explicit `cmd/server`, `cmd/console`,
  database construction, migrations, and generated registries. The worker and
  queue operations must follow the same inspectable ownership model.
- PostgreSQL is the only promised durable backend for this milestone. Polling
  with indexed claims is the correctness baseline; notification-based wakeups
  are only an optional later optimization with polling retained as fallback.

## Local passing runs — 2026-09-05

- The final worktree passed `go test ./... -count=1`, `go vet ./...`, and
  `go test -race ./... -count=1`. The slowest framework race package completed
  in 70.450 seconds.
- Direct runtime and PostgreSQL adapter suites passed both without and with
  `GOFORGE_TEST_DATABASE_URL`, including real transaction visibility, lease
  fencing/reclaim, retry/failure transitions, active-key reuse, unknown-version
  filtering, schema constraints, and defensive scans.
- Independent adversarial review found no remaining P0, P1, or P2
  implementation or acceptance-evidence defect after the final generated-app
  workflow and CI gate were added.
- CI runs both generated PostgreSQL workflows twice. Each jobs workflow also
  runs the fresh application's complete tests, vet, and race suite before its
  live worker journey.

The final generated jobs journey built the CLI, created a fresh format-6
application, generated and inspected two typed jobs and the explicit registry,
and exercised direct and transactional dispatch, commit/error/panic rollback,
durable `Delay` and `At`, active deduplication, two competing external workers,
finite retries, safe failed-job output, retry/forget administration, a hard-
killed lease owner followed by successor reclaim, acknowledgement, graceful
shutdown, schema cleanup, and process reaping.

1. 2026-09-05 — final strengthened jobs journey passed in 22.943 seconds.
2. 2026-09-05 — immediate independent fresh jobs journey passed in 23.223 seconds.

The compatibility browser/ORM journey also passed twice without intervening
edits, in 29.096 and 29.735 seconds. Those fresh applications passed generated
tests, vet, builds, migrations, relationship execution, JSON and browser auth,
CSRF, validation, owner-scoped CRUD, contextual escaping, and clean shutdown.
The exact combined CI command then repeated both workflows twice and passed in
105.083 seconds.

The isolated PostgreSQL 18 acceptance cluster was stopped and its
`.tmp/v06-postgres` data directory removed after the final run. Port 55432 no
longer accepted connections; no host PostgreSQL service was modified.

## Explicit non-goals for v0.6

- Exactly-once execution or automatic idempotency for arbitrary external side
  effects. Lease fencing protects queue state; handlers remain responsible for
  business-level idempotency.
- Recurring cron schedules, time zones/DST, missed-run policy, scheduler leader
  election, and overlap policy. Those form the separately gated scheduler
  milestone; one-off delayed dispatch is included here.
- Chains, batches, workflow orchestration, a web dashboard, Redis/SQS parity,
  dynamic package discovery, runtime dependency injection, or hidden workers
  started by the HTTP application.
- Concurrent generator processes. Generation is atomic and rollback-safe per
  process, but projects must run generators serially until a project lock or
  compare-and-swap metadata protocol is introduced.

# v0.5 view authoring core scorecard

Status: **accepted**

Target:

> From an empty directory, generate a server-rendered PostgreSQL application
> whose first-party `.forge.html` language provides Blade/Twig-class composition,
> form ergonomics, and diagnostics while compiling deterministically to ordinary
> contextually escaped `html/template`. Production performs no source discovery
> or Forge-language interpretation, and applications retain complete standard-Go
> escape hatches.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| The language has a positioned grammar | passing | A lexer/parser with source spans replaces regex rewriting; nested directives, quoted arguments, balanced pipelines, escaped `@@`, comments, and mismatched closures have exact path/line/column tests |
| Components are complete compile-time composition | passing | Static components support required and literal-default props, named arguments, default/named slots, nesting, and strict missing/unknown/duplicate/cycle failures; expansion occurs before `html/template` contextual analysis and emits no runtime component machinery |
| Control flow and stacks are coherent | passing | Nested `@if`/`@elseif`/`@else`, `@for`/`@empty`, and `@with` compile standard Go-template pipelines; page-local `@push`/`@stack` ordering is deterministic and scoped constructs cannot leak across pages |
| Forms have explicit ergonomic state | passing | `@csrf`, `@method`, `@old`, and `@errors` compile to inspectable markup/actions over explicit `.Form` data; submitted empty values differ from absent values and every validation message remains escaped |
| Custom functions use one application registry | passing | An editable application-owned `template.FuncMap` is used by the project compiler and production renderer; string results remain escaped, missing functions fail before publication, and `template.HTML` remains conspicuous |
| Diagnostics survive composition | passing | Compile and render failures identify original source path/line/column plus page-to-layout/include/component/slot expansion context; buffered rendering never commits a partial response |
| Compilation is deterministic and checkable | passing | `forge views:compile` emits byte-identical gofmt'd source and mappings; `--check` is non-mutating and distinguishes current, stale, and invalid input; every failure preserves the byte-identical last-good artifact |
| Generated applications use the language | passing | Fresh auth/resource views and `forge make:component` exercise components, props, slots, stacks, control flow, explicit form state, and application functions instead of hand-expanding framework markup |
| Standard Go remains the escape hatch | passing | Generated output is inspectable canonical `html/template`; ordinary `{{...}}` pipelines remain legal, raw `.html` works through `view.Parse`, callers can use direct `html/template` and `Engine.Templates`, and production runs outside the source tree |
| Full generated workflow passes twice | passing | Two independent fresh format-5 applications pass tests/vet/build and isolated PostgreSQL browser journeys with adversarial escaping, diagnostics, stale checks, clean shutdown, and no leaked schemas/processes |

## Baseline — 2026-09-05

- v0.4 is accepted. Its fresh applications already compile `.forge.html` into a
  deterministic generated `views_gen.go`, parse only that artifact in
  production, use standard-library contextual escaping with `missingkey=error`,
  and buffer rendering before committing a response.
- The focused baseline passes: `go test ./view ./internal/cli -run
  'Forge|View|views' -count=1`.
- The baseline source language had layouts, named sections/yields, and includes
  with explicit data. Expressions and control actions are ordinary Go-template
  pipelines, and direct `view.Parse` remains available.
- The baseline compiler was a regex/text-rewrite pipeline. It could not safely
  represent nested components, slots, or control blocks and lost useful source
  provenance after composition.
- Baseline generated forms hand-wrote CSRF fields, method overrides, old-input
  lookups, and error loops. Managed compilation and production both passed a nil FuncMap,
  so application functions cannot use the complete managed workflow.
- Baseline `forge serve` performed a compile before startup and retained the last
  good artifact, but had no non-mutating `views:compile --check` mode.

## Local passing runs — 2026-09-05

- The final merged worktree passed `go test ./...`, `go vet ./...`, and
  `go test -race ./...` after the positioned compiler, source maps, format-5
  application compiler, and compatibility fixes landed.
- Focused adversarial tests cover multiline and Unicode source positions,
  synthetic-directive clamping, layout/include/component/slot expansion chains,
  contextual text/attribute/URL escaping, explicit `template.HTML`, strict
  props/slots/cycles, page-local stacks, submitted-empty form state, missing and
  corrupt artifact recovery, non-mutating stale checks, and rollback safety.
- A frozen format-4 compiler retains the v0.4 grammar and artifact shape. A real
  compatibility fixture passed compile/check/build/serve/last-good behavior
  while preserving text that only format 5 treats as directives.
- Independent final review found no remaining P0 or P1 implementation or
  test-evidence defect.

Each final PostgreSQL run built the CLI, generated a fresh format-5 application,
generated a component and Issue resource, ran view and ORM freshness checks,
inspected the mapped artifact for canonical output, proved an exact composed
source diagnostic and byte-identical last-good artifact, and passed generated
tests, vet, and builds. Each new isolated schema then passed relationship-rich
ORM execution, concurrent/idempotent and transactional migrations, JSON/browser
authentication, explicit-form validation, owner CRUD, contextual escaping,
optimistic conflict recovery, production startup outside the source tree, and
clean shutdown.

1. 2026-09-05 — final format-5 journey passed in 33.22 seconds.
2. 2026-09-05 — immediate independent fresh journey passed in 34.58 seconds.

The isolated PostgreSQL 18 acceptance cluster was stopped and its workspace
data directory removed after both runs. The host's pre-existing PostgreSQL
service was not modified.

## Explicit non-goals for v0.5

- A proprietary expression/filter language, dynamic component or include
  names, macros/compiler plugins, runtime source discovery, or production
  recompilation.
- Class-backed/stateful components, dependency injection inside templates,
  attribute bags/class merging, implicit request globals, or automatic
  model-to-form binding.
- Localization, asset bundling, browser live reload/HMR, client hydration,
  fragment caching, streaming, or templates authored by untrusted users.
- Automatic upgrades of pre-v1 format-4 generated applications. Their public
  runtime APIs and existing compiled artifacts remain compatibility evidence.

# v0.4 typed data mapper ORM scorecard

Status: **accepted**

Target:

> From an empty directory, generate a PostgreSQL application whose ordinary Go
> models support typed querying, partial changes, relationships, eager loading,
> transactions, locking, and stable persistence errors. Generated mapping and
> SQL remain deterministic and inspectable; every operation can compose with
> `database/sql` and handwritten repositories.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| Typed mapping covers real field states | passing | Generated models and mappers round-trip required and zero values, nullable values and explicit NULL, database defaults/generated IDs, timestamps, and a `Scanner`/`Valuer` escape-hatch type without runtime reflection |
| Queries and mutations are complete and safe | passing | Typed create/find/filter/order/paginate/count/exists/partial-update/delete and bulk operations expose deterministic SQL/args; values are always parameters; updates/deletes require predicates or conspicuous `AllRows()` |
| Relationships are first-class and explicit | passing | Belongs-to, has-one, has-many, and many-to-many generation, eager loading, and attach/detach pass focused and live tests; no lazy I/O and each preload edge has a bounded constant query count |
| Transactions and concurrency compose | passing | The same generated store works with `*sql.DB` and `*sql.Tx`; cross-model commit/rollback/panic behavior, handwritten SQL in the same transaction, `FOR UPDATE`/`FOR SHARE`, and optimistic conflict handling pass against PostgreSQL |
| Persistence errors and cancellation are stable | passing | Not-found, unsafe mutation, stale data, unique, foreign-key, check, serialization, and deadlock errors preserve causes; deadlines cancel promptly and leave the pool reusable without leaking submitted values |
| Ownership and mass-assignment boundaries remain strong | passing | Generated create/change types protect IDs, owner IDs, timestamps, versions, and relationship ownership; CRUD, bulk operations, aggregates, and preloads cannot cross authenticated owners |
| Generation is deterministic and inspectable | passing | Format-4 `make:model`, relationship/resource generation, `orm:generate`, and `orm:generate --check` emit gofmt'd model/mapping/migration source atomically; stale or invalid declarations fail before writes |
| Escape hatches remain complete | passing | Callers can inspect `orm.Statement`, supply `*sql.DB`/`*sql.Tx`, execute raw SQL in the same transaction, and replace generated repositories without changing controllers |
| Existing framework behavior remains compatible | passing | Fresh v4 JSON/browser authentication, sessions, validation, CSRF, throttling, views, resource routes, pagination, migrations, and production lifecycle retain their v0.3 contracts |
| Full generated workflow passes twice | passing | A fresh relationship-rich application passes tests/vet/build and the isolated PostgreSQL ORM journey twice without regression or leaked schemas/processes |

## Baseline — 2026-09-05

- v0.3 browser applications are accepted; framework tests, vet, race tests,
  fresh generated applications, and two live PostgreSQL journeys pass.
- Current generated resources use secure owner-scoped handwritten repositories,
  not an ORM. Metadata records no fields, nullability, defaults, indexes, or
  relationships.
- Repositories hold `*sql.DB`, while transactions expose `*sql.Tx`; composed
  generated operations cannot currently share a transaction without rebuilding
  repositories.
- SQL values are parameterized, request types are explicit write allowlists,
  authenticated owners are controller-derived, and contexts already propagate.
  The ORM must preserve these strengths.

## Foundation evidence — 2026-09-05

- The reflection-free `orm` runtime now has typed table/column metadata,
  immutable query and mutation builders, explicit field states, structured
  persistence errors, transaction-required row locks, statement observation,
  and all four common relationship loaders. Its focused race tests and vet pass.
- The static model parser validates ordinary Go declarations, PostgreSQL names,
  exact foreign-key types, field states, and relationship metadata without
  runtime discovery. `forge orm:generate` and `--check` deterministically emit
  typed mappers, columns, stores, create inputs, and changesets while preserving
  the last good generated artifact on failure.
- `forge make:model` now creates a conventional application-owned model, paired
  plain SQL migration, and current ORM artifact as one rollback-safe generator
  operation. Fresh scaffolds own their User model, and `make:resource` adds an
  owner-related model plus an ORM-backed repository while retaining the public
  controller repository interface.
- The combined repository race suite and vet passed after the runtime slice.
  At that foundation checkpoint this was package-level evidence only; the
  generated-application PostgreSQL acceptance recorded below closes that gap.
- An isolated PostgreSQL 18 cluster then ran the expanded core ORM journey
  twice, with the second pass under the race detector. Both passes exercised
  real generated/default/NULL field behavior, typed single and bulk mutations,
  aggregates, optimistic success/stale/wrong-owner behavior, raw SQL composed
  inside commit and rollback transactions, `FOR UPDATE` and `FOR SHARE`
  blocking, all four relation loaders plus many-to-many attach/detach,
  classified constraint/serialization/deadlock errors, deadline cancellation
  with pool reuse, and eager-load query budgeting. The cluster is test-only and
  separate from the host's existing service. These passes proved the runtime
  SQL before the generated-application flow was closed below.
- An early fresh format-4 application completed the live PostgreSQL workflow in
  14.682 seconds. The final relationship-rich runs below supersede that partial
  checkpoint and include optimistic HTTP conflicts and all generated relation
  kinds.

## Local passing runs — 2026-09-05

The final framework verification passed `go test ./...`, `go vet ./...`, and
`go test -race ./...`. An independent closure review found no remaining P0 or
P1 implementation defect.

Each final run built the CLI, generated a fresh format-4 application and Issue
resource, added conventional Tenant, TenantProfile, Project, and Tag models,
regenerated and checked the inspectable ORM artifact, and passed generated-app
tests, vet, and builds. Against a new isolated PostgreSQL schema, each run then
proved concurrent/idempotent migrations, generated nullable belongs-to,
has-one, has-many, and many-to-many loaders, tenant-scoped preloads, exact query
budgets, attach/detach, transactional migration recovery, JSON/browser auth and
owner CRUD, optimistic conflict recovery, production startup, and clean process
shutdown.

1. 2026-09-05 — final relationship-rich journey passed in 21.97 seconds.
2. 2026-09-05 — immediate independent fresh journey passed in 22.50 seconds.

The isolated PostgreSQL 18 acceptance cluster was stopped and its workspace
data directory removed after both runs. The host's pre-existing PostgreSQL
service was not modified.

## Explicit non-goals for v0.4

- Runtime reflection, package scanning, Active Record `Save`, lazy loading,
  identity maps, implicit units of work, automatic startup migration, or schema
  diffing.
- Hidden validation/hooks, automatic tenant scopes, transparent caching,
  polymorphic relationships, composite primary keys, nested transactions, or
  multi-dialect parity.
- Rewriting the specialized session store or migration bookkeeping through the
  ORM; they remain deliberate raw-SQL examples.

# v0.3 browser applications scorecard

Status: **accepted**

Target:

> From an empty directory, produce a secure server-rendered application with
> HTML authentication and owner-scoped CRUD. GoForge templates compile into
> deterministic, inspectable `html/template` artifacts while retaining
> contextual escaping and ordinary Go escape hatches.

## Acceptance criteria

| Criterion | State | Required evidence |
| --- | --- | --- |
| GoForge view source compiles deterministically | passing | `.forge.html` layouts, sections, yields, and explicit includes compile to canonical `html/template`; invalid dependency graphs have focused tests |
| Generated view artifacts are inspectable | passing | Fresh scaffolds contain managed `views_gen.go`; generated production startup parses canonical output rather than the GoForge DSL |
| HTML authentication is secure end to end | passing | Fresh generated-app and live PostgreSQL tests cover registration, login/logout, CSRF, rotation, validation, sanitized input, redirects, one-request flash, API/browser boundary isolation, and bounded source/account throttling |
| Generated HTML CRUD is usable end to end | passing | Fresh generated-app and live PostgreSQL tests cover protected paginated index/create/show/edit/update/delete, bounded query parsing, method override, CSRF, owner assignment/isolation, and contextual escaping |
| Existing JSON behavior remains compatible | passing | Fresh v0.3 applications retain the unchanged JSON tests and run them first in the combined PostgreSQL journey; pre-v1 generated-project upgrades are not claimed |
| Browser session concurrency is safe | passing | Create/update/atomic-rotate persistence prevents a stale request from restoring a rotated session ID |
| Fresh generated application passes twice | passing | Two independent fresh v3 applications passed tests/vet/builds, followed by two isolated-schema PostgreSQL browser journeys without regression |

The first grammar is intentionally limited to inheritance, named sections,
yields, includes with explicit data, and standard Go template actions.
Components, slots, macros, asset pipelines, and the ORM remain separate later
milestones; they were not added after this milestone's acceptance gate passed.

## Local passing runs — 2026-09-05

The framework passed `go test ./...`, `go vet ./...`, and `go test -race ./...`.
Two applications generated independently from the freshly built v0.3 CLI then
each generated an `Issue` resource and passed `go test ./...`, `go vet ./...`,
and `go build ./cmd/...`. Inspection confirmed that production startup consumes
the generated `views_gen.go` artifact rather than the GoForge source grammar.

An isolated PostgreSQL 18 cluster was then used for the written end-to-end
journey. Both runs generated a fresh application/resource, applied migrations
concurrently, recovered from failed transactional DDL, exercised JSON and HTML
authentication plus owner-scoped CRUD, ran the production binary outside the
source directory, and cleaned their schemas and processes.

1. 2026-09-05 — live PostgreSQL journey passed in 12.871 seconds.
2. 2026-09-05 — immediate second journey passed in 12.740 seconds.

The temporary acceptance cluster was stopped and removed after both runs. The
host's pre-existing PostgreSQL service was not modified.

# v0.2 milestone scorecard

## Repository quality review — 2026-09-04

Current goal: review the whole repository for clean, idiomatic Go, understandable
structure, and concise useful comments; fix identified issues and verify emitted
applications. This is a quality pass on v0.2, not a feature milestone.

| Requirement | State | Evidence |
| --- | --- | --- |
| Review every runtime package | passing | Independent reviews covered HTTP/lifecycle, storage/password/session, and cache/config/validation/views; concrete defects have focused regressions |
| Separate source templates from generation logic | passing | Embedded scaffold/resource trees and one renderer replace the obsolete overlay and escaped source strings; fresh scaffold/resource tests pass |
| Use readable names, contracts, and comments | passing | Initialism handling, named repository parameters, concise concurrency/lifecycle contracts, and CONTRIBUTING.md |
| Verify the integrated runtime and generated app | passing | Full race/vet suite, fresh app tests/vet/builds, healthy generated Compose, and two PostgreSQL journeys in 13.859s and 13.807s |
| Record the assessment and remaining limitations | passing | CODE_QUALITY.md records findings, evidence, compatibility changes, and serial generator assumption |

Final independent closure review caught one remaining discarded migration-pair
cleanup error. It now joins the original failure; migration allocation/refusal
tests and CLI vet passed afterward. The temporary Compose database and volume
were removed after confirming zero leaked acceptance schemas.

The original v0.2 acceptance record below is retained as historical evidence.

Target:

> From an empty directory, produce a working authenticated PostgreSQL CRUD
> application in under ten minutes, with migrations, validation, HTML or JSON
> responses, tests, and production middleware.

Status values are **missing**, **partial**, or **passing**. Evidence must come
from the current worktree or a freshly generated application.

## Acceptance criteria

| Criterion | Baseline | Required evidence |
| --- | --- | --- |
| `forge new issueboard` creates a production-shaped PostgreSQL application | passing | Fresh scaffold contains database configuration, pinned pgx driver, JSON auth, database sessions, embedded migrations, Compose file, console, tests, and compiles |
| `forge make:resource Issue` creates a complete inspectable resource | passing | Model, owner-scoped PostgreSQL repository, validation, controller, migration, authenticated routes, and tests are generated; duplicate and successive-resource tests pass |
| `forge migrate` applies pending migrations safely | passing | Real PostgreSQL acceptance applies auth and resource migrations exactly once under two concurrent application processes; failed DDL rolls back and the same version reapplies |
| `forge serve` runs the generated application | passing | Real PostgreSQL acceptance launches the literal project command and observes readiness; the generated Unix entrypoint handles SIGTERM and lifecycle unit tests cover graceful/failed shutdown |
| Authentication is usable end to end | passing | Generated unit and real PostgreSQL tests cover registration, duplicate email, live logout/login, login session rotation, invalid credentials, protected routes, bounded expired-session pruning, and password hashing |
| Generated CRUD is usable end to end | passing | Real PostgreSQL acceptance covers authenticated create/show/update/delete, invalid input, unauthenticated access, and cross-owner isolation |
| Standard `net/http` compatibility is preserved | passing | `ResponseController` plus direct `Flusher`, `Hijacker`, and `Pusher` capability tests pass through logging middleware; unsupported capabilities are not falsely advertised |
| High-risk infrastructure is directly tested | passing | Lifecycle shutdown/failure, transaction commit/rollback/panic, failed migration, fake contention, and real PostgreSQL contention tests pass |
| Claims and CI match reality | passing | Dialect guarantees are precise; README workflow is exercised; CI provisions PostgreSQL and runs the generated-app acceptance twice |
| Milestone passes twice without regression | passing | Two consecutive isolated-schema PostgreSQL acceptance runs are recorded below |

## Baseline evidence — 2026-09-04

- Existing `go test ./...` passes and the scaffold compilation test is green.
- Package coverage: application 0%, database transaction helper 0%, migrations
  19.3%, HTTP 52.3%, CLI 67.4%.
- CLI exposes `new`, controller/request generation, and migration-file generation.
- Generated applications have no database construction, auth, project command,
  resource workflow, or integrated migration execution.
- The workspace is not a Git repository; release tagging cannot yet be verified.

## Explicitly deferred

- `forge rollback`, `forge routes`, wrapper commands for `forge test` and
  `forge build`
- background jobs and workers
- HTML authentication and resource views
- field-definition DSLs and additional SQL dialect guarantees
- release tagging and licensing, which require a Git repository and an explicit
  project licensing decision

## Passing runs

1. 2026-09-04 — final PostgreSQL 18 acceptance pass completed the literal
   four-command journey in 12.570 seconds.
2. 2026-09-04 — an immediate second isolated-schema pass completed in 13.210
   seconds and left no acceptance schemas behind.

Final verification also passed `go test -race ./...`, `go vet ./...`, and
`go build ./cmd/forge`. A fresh `issueboard` scaffold with an `Issue` resource
passed `go test ./...`, `go vet ./...`, and builds of both generated commands.
Independent architecture review reported no P0 or P1 blockers; its remaining
P2 findings were fixed before the final two runs above.
