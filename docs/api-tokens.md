# Personal API tokens

Format-14 applications include an application-owned personal token workflow for
scripts, native clients, and other machine callers. Tokens authenticate the same
user as a database session, so generated resource policies and owner/all SQL
scopes behave identically. They do not introduce separate token permissions.

## Create a token

Token management requires a signed-in database session. A bearer token cannot
create another credential. The JSON request also requires the current password:

```http
POST /auth/tokens
Content-Type: application/json
Cookie: goforge_session=...

{
  "name": "deploy laptop",
  "expires_in_days": 30,
  "current_password": "the current account password"
}
```

Names are trimmed, unique per user, and between 1 and 80 bytes. Lifetimes are
required and must be from 1 through 90 days. Each account may have at most ten
unexpired tokens. These bounds are generated application policy and can be
edited directly when an application needs a different contract. Current-
password confirmation has its own account-keyed twenty-attempt, fifteen-minute limiter,
so concurrent valid requests can reach the transactional ten-token cap without
sharing login or password-change counters.

A successful `201 Created` response includes a value shaped like this:

```text
goforge_pat_<selector>.<secret>
```

Copy it immediately. The complete value appears only in that creation response.
The response is non-cacheable and the application does not put the token in a
redirect, URL, log message, session, or later list response. The browser account
security page exposes the same current-password-protected workflow and renders
the value once in the direct POST response.

## Use a token

Send the complete value in one Authorization header:

```http
GET /auth/me
Authorization: Bearer goforge_pat_<selector>.<secret>
```

Generated JSON resource routes accept the same header. Once authenticated, the
existing application-owned resource authorization function decides whether the
user receives denied, owner, or all-record access. A token therefore preserves
the normal 401 authentication, 403 action denial, hidden-record 404, validation,
relationship, and optimistic-concurrency behavior.

The header contract is intentionally strict. Duplicate headers, another scheme,
extra whitespace, malformed or oversized values, wrong secrets, expired rows,
revoked rows, and unknown selectors all receive the same generic 401 response
with a Bearer challenge. If any Authorization header is present it is
authoritative: an invalid header never falls back to a valid session cookie and
the request does not load, refresh, or write a session.

With no Authorization header, existing cookie-authenticated JSON clients keep
working. Browser pages and credential-management routes remain session-only and
CSRF-protected; a bearer credential alone never authenticates them.

## List and revoke

Session-authenticated JSON management uses:

```text
GET    /auth/tokens
DELETE /auth/tokens/{id}
```

Lists contain only the user-scoped numeric ID, name, expiry, and creation time.
They never contain the selector, digest, or complete value. Revocation is scoped
by both user and token ID, so an absent ID and another user's ID are the same
not-found result. The account-security page provides the corresponding
CSRF-protected revoke action.

An individual revoke prevents every subsequent authentication across processes
and restarts. Password change, password reset, and **Sign out everywhere**
advance the account credential generation; tokens issued under the previous
generation then fail without scanning or recovering token secrets.

## Storage and replacement

The plain `000006_create_personal_access_tokens` migration stores the user,
bounded name, public lookup selector, SHA-256 secret digest, credential
generation, expiry, and creation time. It enforces canonical selector and digest
lengths, uniqueness, ownership, and indexed authentication lookup. Issuance
locks the user row and performs credential revalidation, expired-row pruning,
capacity enforcement, and insertion in one PostgreSQL transaction.

Token persistence deliberately does not use the generated domain ORM: generic
model create/update inputs should never expose authentication digests. The
repository, service, middleware, controllers, request validation, routes, view,
and migration are ordinary files in the generated application. Replace their
constructor wiring, implement their small interfaces, call `database/sql`
directly, or replace the route middleware when the application needs scopes,
service credentials, OAuth/OIDC, mTLS, or a different token format.

GoForge does not provide indefinite tokens, token abilities, refresh tokens,
last-used auditing, automatic renewal, device binding, or operator token
commands in this milestone.
