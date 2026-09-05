// Package postgres provides an inspectable PostgreSQL job store.
package postgres

// Schema is the default plain SQL queue schema for application migrations.
const Schema = `CREATE TABLE goforge_jobs (
    id uuid PRIMARY KEY,
    queue text NOT NULL CHECK (octet_length(queue) <= 64 AND queue ~ '^[a-z][a-z0-9._-]*$'),
    name text NOT NULL CHECK (octet_length(name) <= 128 AND name ~ '^[a-z][a-z0-9._-]*\.v[1-9][0-9]*$'),
    payload jsonb NOT NULL CHECK (octet_length(payload::text) <= 262144),
    priority smallint NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts integer NOT NULL CHECK (max_attempts BETWEEN 1 AND 1000),
    timeout_ms bigint NOT NULL CHECK (timeout_ms BETWEEN 1 AND 86400000),
    backoff_ms bigint[] NOT NULL CHECK (
        cardinality(backoff_ms) BETWEEN 1 AND 1000
        AND 0 <= ALL(backoff_ms)
        AND 2592000000 >= ALL(backoff_ms)
    ),
    lease_owner text CHECK (lease_owner IS NULL OR octet_length(lease_owner) BETWEEN 1 AND 128),
    lease_generation bigint NOT NULL DEFAULT 0 CHECK (lease_generation >= 0),
    leased_at timestamptz,
    lease_expires_at timestamptz,
    dedup_key text CHECK (dedup_key IS NULL OR octet_length(dedup_key) BETWEEN 1 AND 256),
    last_error_kind text CHECK (last_error_kind IS NULL OR octet_length(last_error_kind) <= 64),
    last_error text CHECK (last_error IS NULL OR octet_length(last_error) <= 8192),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (
        (lease_owner IS NULL AND leased_at IS NULL AND lease_expires_at IS NULL)
        OR
        (lease_owner IS NOT NULL AND leased_at IS NOT NULL AND lease_expires_at IS NOT NULL)
    )
);

CREATE INDEX goforge_jobs_claim_idx
    ON goforge_jobs (queue, name, priority DESC, available_at, id);

CREATE UNIQUE INDEX goforge_jobs_active_dedup_idx
    ON goforge_jobs (queue, name, dedup_key)
    WHERE dedup_key IS NOT NULL;

CREATE TABLE goforge_failed_jobs (
    id uuid PRIMARY KEY,
    queue text NOT NULL CHECK (octet_length(queue) <= 64 AND queue ~ '^[a-z][a-z0-9._-]*$'),
    name text NOT NULL CHECK (octet_length(name) <= 128 AND name ~ '^[a-z][a-z0-9._-]*\.v[1-9][0-9]*$'),
    payload jsonb NOT NULL CHECK (octet_length(payload::text) <= 262144),
    priority smallint NOT NULL,
    attempts integer NOT NULL CHECK (attempts >= 0),
    max_attempts integer NOT NULL CHECK (max_attempts BETWEEN 1 AND 1000),
    timeout_ms bigint NOT NULL CHECK (timeout_ms BETWEEN 1 AND 86400000),
    backoff_ms bigint[] NOT NULL CHECK (
        cardinality(backoff_ms) BETWEEN 1 AND 1000
        AND 0 <= ALL(backoff_ms)
        AND 2592000000 >= ALL(backoff_ms)
    ),
    dedup_key text CHECK (dedup_key IS NULL OR octet_length(dedup_key) BETWEEN 1 AND 256),
    failure_kind text NOT NULL CHECK (octet_length(failure_kind) BETWEEN 1 AND 64),
    failure_message text NOT NULL CHECK (octet_length(failure_message) <= 8192),
    created_at timestamptz NOT NULL,
    failed_at timestamptz NOT NULL
);

CREATE INDEX goforge_failed_jobs_failed_at_idx
    ON goforge_failed_jobs (failed_at DESC, id);`

// DropSchema is intended for isolated integration-test cleanup.
const DropSchema = `DROP TABLE IF EXISTS goforge_failed_jobs; DROP TABLE IF EXISTS goforge_jobs;`
