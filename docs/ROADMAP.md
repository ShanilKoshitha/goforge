# Proposed roadmap from v0.21.1 to v1.0

Status: **proposal for product review** — 2026-10-21. These milestones are not
implemented merely because they appear here. The accepted v0.21 milestone is
closed; this document does not add features to its release.

The [north star](North_Star.md) remains the objective: a complete, coherent
application workflow with ordinary Go, inspectable generated source, small
runtime boundaries, and explicit replacements for every framework component.

Version components are integers. The forward sequence is `v0.22`, `v0.23`,
through `v0.34`, then `v0.35` and later minors before `v1.0`. `v0.9` is an older
released milestone. There is no requirement to reach `v0.99` before v1.
The minor allocations below are a proposed order, not dates or a percentage of
completion. Discovery may require additional minors. Runtime publication and
checksum-bearing CLI adoption may require patch releases within a milestone.

## Established baseline

Public v0.21.1 generates format-15 applications pinned to public v0.21.0.
Authentication, PostgreSQL sessions, request validation and middleware, JSON and
browser CRUD, authorization, recovery, personal API tokens, jobs, schedules,
compiled Forge views, embedded assets, the development loop, and test/build
commands already work. Acceptance and public distribution evidence are in the
[scorecard](SCORECARD.md).

The ORM already provides typed queries and writes, transactions, optimistic
versions, locks, count/exists, bulk writes, all four relationship loaders, and
many-to-many attach/detach. Its model authoring is richer than the CLI field
grammar. Future data work extends those capabilities into complete application
workflows; it does not introduce a second ORM.

## Proposed functional milestones

Each row names a complete developer workflow and an example acceptance target.
Exact written criteria must be established from a runnable baseline before its
implementation starts.

Each functional milestone also contributes its accepted slice to a maintained
integration application. v0.37 completes the public learning and reproducibility
contract; it is not the first time the subsystems are exercised together.

| Version | Milestone | Example acceptance target |
| --- | --- | --- |
| v0.22 | Resource lists and relationship choices at realistic scale | An owner with 10,000 categories can find and select any authorized category, retain the current selection, and filter/sort paginated resource lists through explicit typed queries. The current 100-choice limit cannot disable create/edit. Browser and JSON paths preserve ownership and bounded queries. |
| v0.23 | Read-only diagnosis and application inventory | A fresh app reports invalid configuration, unavailable PostgreSQL, migration drift/pending state, stale ORM/views, and invalid assets with redacted repair guidance. Use migration Verify/read-only inspection, not Status, which can upgrade bookkeeping. Route inspection shows the actual explicit route table. Diagnosis mutates no files, services, or database state. |
| v0.24 | Application database tests, factories, and seed data | An application-owned Go test exercises login, CSRF-protected CRUD, persisted rows, and transactional job dispatch against PostgreSQL. Concurrent tests have isolated data and reliable cleanup. Explicit ordinary Go factories and a deliberate development seed command replace copied framework test internals. |
| v0.25 | Complete migration operations and schema evolution | Inspect pending/applied migrations, apply bounded steps, deliberately roll back one step, and reapply using reviewed paired SQL. Disclose operational Status's bookkeeping updates. Evolve a model through explicit edits/regeneration while retaining data; checksum verification and advisory locks remain authoritative. No automatic schema diff is implied. |
| v0.26 | Temporal and constrained fields through generated applications | Generate date, nullable timestamp, and bounded status fields across model, SQL, requests, validation, browser forms, and JSON. Define date-only representation, timestamp time-zone conversion, wire formats, and null semantics without depending on v0.27. Invalid input produces correctable field errors and no write. |
| v0.27 | Exact values and explicit custom-type integration | An invoice amount round-trips without floating-point drift, with explicit precision/scale validation. A declared custom value type participates in generated mapping through visible Go conversion/Scanner/Valuer boundaries, including imports, SQL type mapping, nullable values, and create/change inputs. Parsing, validation, SQL, and form semantics agree. |
| v0.28 | Optional relationship authoring | Create an uncategorized resource, assign an authorized parent, and clear the association through browser and JSON. Distinguish clearing from omitted input; reject wrong-owner parents. Nullable foreign keys, constraints, eager loading, errors, and optimistic versions agree across the generated slice. |
| v0.29 | Collection relationship authoring | Generate the pivot migration and assign/remove tags through browser and JSON using an explicit application-owned mutation service and the ORM's existing collection/pivot primitives. Database constraints and transactional service authorization jointly prevent duplicate/unauthorized links and partial multi-association writes; loading remains bounded. |
| v0.30 | Authorized user uploads and storage | Upload, download, replace, and delete an owned resource attachment through bounded multipart handling and an explicit storage interface. Traversal, spoofed types, oversized bodies, collisions, cross-owner access, and partial failures are covered; replacement does not leak orphaned files. |
| v0.31 | General transactional application mail | Generate an assignment message and enqueue it atomically with a domain write using the existing rendered-mail, encrypted-outbox, SMTP, delivery-job, and cleanup machinery. Rollback sends nothing; worker retry and development delivery are exercised. |
| v0.32 | Coherent application cache wiring | Inject the existing cache interface into an ordinary application service and demonstrate expiry/invalidation across processes with one declared shared backend. Tests substitute Memory; namespace clearing and backend-error behavior are explicit. |
| v0.33 | Email ownership verification | A public signup can require verified ownership before protected actions. Delivery, token expiry, replay resistance, resend limits, browser/JSON behavior, session effects, and privacy are tested through the existing mail and security primitives. |
| v0.34 | Typed reporting and result projections | Produce an owner-scoped grouped invoice report into a declared Go result type, with parameterized filters, exact aggregate/null semantics, SQL inspection, and matching DB/Tx behavior. Handwritten SQL remains a complete path. |
| v0.35 | Production release for the complete application | Produce server, worker, scheduler, and console artifacts plus a reviewable deployment example. Explicit migrations, readiness, queued delivery, scheduled work, graceful shutdown, and restart recovery work against disposable PostgreSQL. Configuration, TLS/reverse proxy, backup/restore, and rolling-asset constraints are documented. |
| v0.36 | Application adoption and compatibility | Define CLI/runtime/generated-source compatibility and security support, then provide a read-only adoption plan for a released app. Distinguish verified unchanged, modified, and unknown-baseline files using an exact public baseline or supplied originals; project format alone is not provenance. Report ambiguity for review. At least one real upgrade is exercised without overwriting application code. |
| v0.37 | Reference application and complete learning path | A maintained application exercises the combined domain, authentication, forms, relationships, uploads, mail, jobs, schedules, tests, and production release workflow using documented commands. Fresh setup and an upgrade from a prior public release are independently reproducible. |
| v0.38 | Integrated security, performance, and failure qualification | Run explicit abuse, concurrency, cancellation, migration, crash/restart, and resource-bound scenarios across the reference application. Publish repeatable baselines and limits; fix findings across subsystem boundaries. |
| v0.39 | v1 API and support-contract freeze | Review exported APIs, generator ownership, configuration, errors, project-format policy, supported platforms/toolchains/PostgreSQL, and deprecations. Compile documented escape-hatch replacements and freeze the accepted contract before release candidates. |

The immediate candidates above come from current code boundaries: the resource
choice loader refuses more than 100 rows; field generation accepts four scalar
families; relationship authoring accepts required belongs-to; the console exposes
migration Up while the migrator already has Down/Status/Verify; `forge build`
publishes the HTTP binary while other processes require separate Go commands;
and application PostgreSQL test helpers are not supplied as a coherent scaffold
workflow. Cache and mail integration reuse existing packages. User-upload
storage is a new capability, distinct from embedded deployment assets.

## v1 release runway

| Release stage | Required evidence |
| --- | --- |
| v1.0.0-alpha series | All committed v1 functional criteria are complete together. The reference app, supported environment matrix, compatibility/adoption path, generated-source inspection, and release/install workflow pass from public artifacts. |
| v1.0.0-beta series | External or independent application journeys exercise the frozen contracts. Documentation and upgrade defects are resolved, support boundaries are explicit, and remaining findings have reproducible tests. |
| v1.0.0-rc series | Only release-blocking corrections enter. Security, race, integration, failure recovery, public installation, upgrade, and production qualification run on the release artifacts; consecutive candidates/pass cycles show no regression. |
| v1.0.0 | Publish the accepted contract, support policy, release notes, public artifacts, and verified installation/upgrade instructions. No required criterion or unresolved blocker is hidden behind a release label. |

## Gates and scope boundaries

For every milestone: establish a runnable baseline, write measurable criteria,
implement the smallest complete workflow, test the framework and a freshly
generated application, obtain independent review, fix failures by north-star
impact, and stop feature work after two clean passes. Publishing a runtime and
adopting it in generated applications must never require an unpublished module.

Before implementation, each proposed row needs concrete acceptance criteria and
an explicit scope decision. This proposal does not claim that every capability
of another framework is required for v1, or that a convenience lacking a query
builder needs a new runtime abstraction. Clean direct Go, SQL, and HTTP paths
remain part of the product promise.

Active Record lifecycle discovery, lazy loading, reflection-driven injection or
binding, hidden model hooks/scopes, runtime component or attribute discovery,
automatic schema diffing/startup migration, and a second template expression
language remain outside the intended architecture. Multi-dialect parity,
OAuth/OIDC/MFA, a notification-channel framework, media processing, JavaScript
hydration, asset transforms, and cloud provisioning need their own demonstrated
workflow and scope decision; they are not silently bundled into these rows.

The v0.34-to-v1 interval therefore includes production delivery, adoption,
reference-app integration, hardening, API freeze, and prerelease qualification.
It does not jump backward to v0.9 or substitute a feature list for stabilization.
