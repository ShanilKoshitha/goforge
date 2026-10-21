# v0.22 — Resource browsing and relationship selection at scale

Status: **acceptance defined; runtime and format-16 implementation in progress** — 2026-10-21.

This milestone extends the accepted v0.21.1 application workflow. It does not
reopen the completed component/form milestone or claim the rest of ROADMAP.md.

## Runnable baseline and observed gap

- Existing format-15 accessible-form generation and formats 8–14 legacy
  generation tests passed together with `-count=1` on native Windows.
- The public v0.21.1 CLI's no-replace `example.com/releasedv0211` application
  passed `go test ./... -count=1`, including Category, all-scalar related Ticket,
  and legacy-style resource packages. It remains format 15 on public v0.21.0.
- Existing generated repository tests explicitly expect an overflow error when
  related choices exceed 100; browser rendering turns this into 409. Thus an
  otherwise valid application becomes unusable for create/edit as data grows.
- Browser pagination exists, but filters/sorting and relationship search do not.
  Choice labels use only model name and ID. The typed ORM, authorization scopes,
  transactional constraints, recoverable form decoding, and view compiler are
  existing foundations, not replacements to build again.

## Complete workflow

An issue-board owner with 10,000 categories can search/sort a bounded Ticket
index, open New/Edit, find any authorized category, preserve unfinished inputs
while searching/paging, correct validation errors, and save without losing a
selection or bypassing optimistic concurrency. It works using ordinary HTML
without JavaScript. JSON indexes expose the same declared filtering/sort rules;
JSON writes retain their strict decoding, authorization, and association rules.

## Implementation boundaries

- New application-owned typed list/choice option and page structs; no runtime
  schema discovery, query DSL, arbitrary SQL fragments, or automatic rewrites.
- Search is bounded literal case-insensitive text matching, not a user-controlled
  LIKE pattern. `%`, `_`, and backslash are data. A separate positive `id` lookup
  provides a universal path without reserving hashtag text. PostgreSQL determines
  text case/collation behavior; do not promise locale-independent folding.
- Default target labels/search to ID-only. An explicit generator declaration
  `--choice-label category:title` opts one relationship into disclosing that
  target string/text field to actors permitted to create/edit the source. It
  declares a choice-safe presentation capability, not full target list access.
  Initial declarations accept editable plain string/*string fields stored as
  PostgreSQL text or sized varchar/character varying; protected, generated,
  UUID-backed, and custom wrapper fields require application-owned choice code.
  Never infer a private field merely because it is textual. Resolve/validate
  declared fields once at generation time; emit direct Go references, include
  IDs, and truncate label text deterministically to 120 UTF-8 runes
  before ordinary HTML escaping. Null labels fall back to ID-only. Application
  code remains editable; runtime reflection/discovery is not involved.
- List search, allowlisted sort/direction, page/per-page, and exact declared
  relationship filters use explicit typed predicates. Sorts have an ID tie-breaker
  and documented PostgreSQL ordering (ASC puts NULL last; DESC puts NULL first).
  Browser links retain the declared query state. Bound search to 200 UTF-8
  runes, resource page size to 100, page number to 10,000, and choice page
  size to 20. Choice pages may use bounded offsets; neither a row limit nor an
  offset cap is a promise of constant database execution cost. Measure the
  10,000-category journey without requiring extra database extensions.
- Relationship search is a CSRF-protected full-form refresh using an explicit
  allowlisted intent on existing Create/Update handlers. It decodes all submitted
  values, authorizes the operation, and renders without calling any domain write.
  Handle existing PUT method override deliberately. Do not put dirty forms into
  query strings or introduce server-side draft storage.
- A bounded choice page and a separate owner-scoped selected-ID lookup preserve
  an authorized selection outside search results. Multiple relationships retain
  independent state. Errors are recomputed server-side, never trusted from hidden
  error messages. Validation-error displays remain correctable after refresh.
- JSON clients use the shared resource index filtering/pagination contract to
  discover targets and the existing association write validation to submit
  them. A separate autocomplete/choice API is not required for this HTML-first
  slice. Never accept a caller-supplied owner ID to select another owner's data.
- New contracts are gated to project format 16. Existing projects and routes
  retain their released behavior; no automatic adoption or human-owned rewrite.
- The typed ORM literal search primitive must be independently tested/published
  before generated applications pin it: runtime-first v0.22.0, then public
  checksum-backed format-16 adoption in v0.22.1. Do not scaffold an unpublished
  runtime version. The two-pass implementation gate applies before publication.

## Exact generated transport contract

- One app-owned `ListOptions` parser serves browser and JSON indexes. Accepted
  query keys are `q`, `id`, `sort`, `direction`, `page`, `per_page`, and each
  declared relationship's foreign-key name. Reject unknown/duplicate keys,
  malformed URL encoding, invalid UTF-8, and out-of-range values with 400 before
  repository access. Absent `q` means no text predicate; nonempty `q` is literal
  contains across the source resource's declared string/text fields, grouped
  with OR and then ANDed with other filters. Reject NUL and Unicode control
  characters before repository access. Rune bounds use utf8.RuneCountInString.
  A resource
  without text fields rejects nonempty `q`. `id` and foreign-key filters are
  positive int64 equality predicates; combined filters all apply with AND.
  Empty optional ID controls mean no filter so native GET forms work; explicit
  zero, negative, malformed, or surrounding-whitespace IDs reject.
- Defaults: page 1, per_page 20, sort `id`, direction `desc`. Sort keys are `id`,
  declared scalar field names, and declared foreign-key names; direction accepts
  only `asc`/`desc`. Values beyond page 10,000 or size 100 reject rather than
  silently changing the contract. Valid pages beyond available data return an
  empty page with no next page, not 404. Previous is available when page > 1.
- JSON keeps `data` plus `pagination` containing `page`, `per_page`,
  `has_previous`, and `has_next`. Browser and JSON HTTP tests compare IDs/order
  and this metadata for identical options. Previous/next retain every filter.
- Relationship controls use `choice[category][q]`, `choice[category][id]`, and
  `choice[category][page]` (substitute the declared relationship name). They use
  the same bounds, fixed page size 20, and an independent selected foreign-key
  value. Without an explicit choice label, nonempty text search rejects and the
  form offers ID lookup instead. Text Search clears that relationship's ID
  filter; ID Lookup clears its text filter. Both reset only that relationship
  to page 1. Previous/Next and Save reject ambiguous nonempty text plus ID state.
  Choice text shares the list search rune/control-character policy.
- Every submit carries exactly one `intent`: `save`, or
  `choice:category:search`, `choice:category:lookup`, `choice:category:previous`,
  `choice:category:next`.
  Missing, duplicate, or unknown intent rejects without writes, including
  implicit Enter submissions that lack a submitter. No default-save fallthrough.
  Refresh dispatch follows CSRF, operation authorization, and structural decoding.
- A scalar `validation_attempted` value of `0`/`1` controls only error display.
  Initial forms use 0; a failed save uses 1. Refresh with 0 returns 200 without
  displaying required-field errors; refresh with 1 recomputes errors and returns
  422 when errors remain, otherwise 200. This flag never disables validation on
  save, authorization, owner checks, or optimistic concurrency. Reject malformed
  or duplicate state controls. Never accept client-supplied error messages.
- Runtime functions `ContainsFold` and `ContainsFoldNullable` accept string and
  pointer-to-string descriptors, including named strings. They bind an escaped
  contains pattern with fixed `ILIKE ... ESCAPE '!'`; `!`, `%`, and `_` are
  escaped and backslash remains literal. Empty search at ORM level matches
  non-NULL strings, while transport parsing omits empty-search predicates. NULL
  does not match. Existing caller-pattern `Like` semantics remain unchanged.

## Measurable acceptance

1. **Reachability:** Seed 10,000 owned categories plus foreign-owner categories.
   Declare a choice-safe category title field. Find/select first, middle, and
   last targets by text and ID, distinguish equal
   labels by ID, and retain a selected item outside the current page/search.
   Create and edit never fail merely because the owner has more than 100 targets.
   Exercise text-to-ID and ID-to-text transitions without stale filters hiding
   the target. Unknown relationship/field, wrong-type, duplicate, and cross-target
   choice-label declarations fail before any file/registry mutation. A valid
   declaration is one-shot generation input: its emitted source survives ORM,
   views, and registry regeneration and later resource additions unchanged;
   no persistent duplicate schema metadata is required.
2. **Bounded work:** List requests return at most 100 resource rows. A choice
   request fetches at most 21 rows for a 20-item page plus next-page sentinel,
   and at most one separately selected row per relationship. Query counts grow
   with declared relationships, not result rows. Observe SQL, parameters, owner
   predicates, row limits, and deterministic ordering in DB and Tx paths,
   including nullable ASC/DESC sorts with ID tie-breaking. Bound/truncate labels
   before escaping, including long multibyte text.
3. **No-JavaScript editing:** Full-form Search/Previous/Next refreshes preserve
   scalar strings, empty/null values, zero/false, malformed recoverable scalars,
   independent relationship states, and optimistic versions. Refresh buttons use
   `formnovalidate`. Verify actual submitted form actions/controls and rendered
   HTML rather than only constructing page structs in a test. Missing, duplicate,
   or unknown intents (including implicit submission) reject without writes.
4. **Correctable failures:** A 422 followed by lookup/paging then correction can
   save. Missing/invalid versions regain an authorized current version; a valid
   stale submitted version remains stale and correction still returns 409.
   Lookup/paging never writes resources, associations, jobs, or other domain data.
5. **Authorization/privacy:** Denied operations perform no choice queries.
   Inaccessible edit members return indistinguishable 404s before choice loading.
   Privileged edits use the member owner's choices, not the actor's. Foreign,
   deleted, and missing selected IDs reveal no labels or existence distinction;
   unsafe text is escaped. An actor allowed to edit a source but denied target
   listing cannot see/search undeclared target text through the ID-only picker.
   Explicit choice-safe text is the only additional disclosure. Missing CSRF
   rejects refresh and save alike.
6. **Input contract:** Reject duplicate, malformed, oversized, or invalid UTF-8
   query/control values and invalid URL encoding. Unknown sort keys/directions
   and unknown refresh intents cannot become writes or SQL. Literal wildcard and
   injection-like inputs remain parameterized. List filter/sort results agree
   between browser and JSON for the same authorized scope.
   Isolate matches to each text field (including nullable text) to prove OR
   semantics; ID/relationship predicates still apply with AND. NUL/control text
   returns 400 before queries rather than a PostgreSQL encoding error.
7. **Compatibility/escape hatches:** Format-15 and older supported output,
   registry routes, and contracts remain unchanged. Old registry regeneration
   cannot reference new methods. App-owned repositories, controllers, query
   structs, and templates compile when explicitly replaced with ordinary Go.
8. **Product evidence:** Framework tests, race/vet/build, fresh format-16 app
   tests/builds, repeated real-PostgreSQL owner/privileged browser and JSON
   workflows, and public no-replace distribution checks pass. Independent review
   finds no unresolved acceptance blocker; two complete passes show no regression.

## Deliberately deferred

AJAX/autocomplete dependencies, fuzzy/full-text search, configurable query
languages, optional/collection relationship authoring, field-family expansion,
automatic upgrade/merge tooling, and later roadmap capabilities are out of scope.
Do not add them after these criteria pass. Keep a cumulative integration journey
for accepted functionality rather than postponing combined testing until v0.37.
