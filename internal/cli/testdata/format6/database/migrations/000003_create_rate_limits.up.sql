CREATE TABLE goforge_rate_limits (
    key TEXT PRIMARY KEY CHECK (octet_length(key) = 64 AND key ~ '^[0-9a-f]{64}$'),
    attempts INTEGER NOT NULL CHECK (attempts BETWEEN 1 AND 1000001),
    reset_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX goforge_rate_limits_reset_at_key_idx ON goforge_rate_limits (reset_at, key);
