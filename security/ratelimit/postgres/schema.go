// Package postgres provides an inspectable durable PostgreSQL rate-limit store.
package postgres

import (
	"fmt"
)

const (
	DefaultTable = "goforge_rate_limits"
	MaxAttempts  = 1_000_001

	// Schema is the default plain SQL schema for application migrations.
	Schema = `CREATE TABLE "goforge_rate_limits" (
    key text PRIMARY KEY CHECK (octet_length(key) = 64 AND key ~ '^[0-9a-f]{64}$'),
    attempts integer NOT NULL CHECK (attempts BETWEEN 1 AND 1000001),
    reset_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE INDEX ON "goforge_rate_limits" (reset_at, key);`

	// DropSchema is intended for isolated integration-test cleanup.
	DropSchema = `DROP TABLE IF EXISTS "goforge_rate_limits";`
)

// SchemaFor returns the same plain schema for an application-selected table.
func SchemaFor(table string) (string, error) {
	if !validIdentifier(table) {
		return "", fmt.Errorf("rate limit postgres: invalid table name %q", table)
	}
	quoted := quote(table)
	return fmt.Sprintf(`CREATE TABLE %s (
    key text PRIMARY KEY CHECK (octet_length(key) = 64 AND key ~ '^[0-9a-f]{64}$'),
    attempts integer NOT NULL CHECK (attempts BETWEEN 1 AND 1000001),
    reset_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE INDEX ON %s (reset_at, key);`, quoted, quoted), nil
}

// DropSchemaFor returns a quoted cleanup statement for an isolated table.
func DropSchemaFor(table string) (string, error) {
	if !validIdentifier(table) {
		return "", fmt.Errorf("rate limit postgres: invalid table name %q", table)
	}
	return fmt.Sprintf("DROP TABLE IF EXISTS %s;", quote(table)), nil
}
