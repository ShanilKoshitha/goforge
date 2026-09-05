package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ShanilKoshitha/goforge/security/ratelimit"
)

const (
	defaultPruneLimit = 100
	maximumPruneLimit = 10_000
	maximumPolicy     = MaxAttempts - 1
	maximumWindow     = 365 * 24 * time.Hour
)

var (
	identifierPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
	hashedKeyPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Option func(*config) error

type config struct {
	table      string
	pruneLimit int
}

// WithTable selects an application-owned PostgreSQL table. Identifiers are
// validated and quoted before being interpolated into statements.
func WithTable(table string) Option {
	return func(config *config) error {
		if !validIdentifier(table) {
			return fmt.Errorf("rate limit postgres: invalid table name %q", table)
		}
		config.table = table
		return nil
	}
}

// WithPruneLimit bounds how many expired counters each Take may remove before
// consuming an attempt. Pruning runs first, so a pruning failure never leaves a
// caller unsure whether its attempt was consumed.
func WithPruneLimit(limit int) Option {
	return func(config *config) error {
		if limit < 1 || limit > maximumPruneLimit {
			return fmt.Errorf("rate limit postgres: prune limit must be between 1 and %d", maximumPruneLimit)
		}
		config.pruneLimit = limit
		return nil
	}
}

// SQLStatements exposes the exact SQL used by a Store. Values remain bind
// parameters; only the validated table identifier is interpolated.
type SQLStatements struct {
	Prune string
	Take  string
	Reset string
}

// Store is a durable fixed-window limiter shared by every process using its
// PostgreSQL table. It owns neither the database nor schema lifecycle.
type Store struct {
	db         *sql.DB
	pruneLimit int
	statements SQLStatements
}

var _ ratelimit.Store = (*Store)(nil)

func New(db *sql.DB, options ...Option) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("rate limit postgres: database is required")
	}
	configuration := config{table: DefaultTable, pruneLimit: defaultPruneLimit}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("rate limit postgres: nil option")
		}
		if err := option(&configuration); err != nil {
			return nil, err
		}
	}
	statements, err := StatementsFor(configuration.table)
	if err != nil {
		return nil, err
	}
	return &Store{
		db:         db,
		pruneLimit: configuration.pruneLimit,
		statements: statements,
	}, nil
}

func (store *Store) Statements() SQLStatements { return store.statements }

func (store *Store) Take(ctx context.Context, key string, policy ratelimit.Policy, _ time.Time) (ratelimit.Decision, error) {
	if store == nil || store.db == nil {
		return ratelimit.Decision{}, fmt.Errorf("rate limit postgres: store is not initialized")
	}
	if err := validateKey(key); err != nil {
		return ratelimit.Decision{}, err
	}
	if err := validatePolicy(policy); err != nil {
		return ratelimit.Decision{}, err
	}
	if err := ctx.Err(); err != nil {
		return ratelimit.Decision{}, err
	}
	if _, err := store.db.ExecContext(ctx, store.statements.Prune, store.pruneLimit); err != nil {
		return ratelimit.Decision{}, fmt.Errorf("rate limit postgres: prune expired counters: %w", err)
	}

	var attempts int
	var resetAt time.Time
	var databaseNow time.Time
	err := store.db.QueryRowContext(
		ctx, store.statements.Take, key, policy.Window.Milliseconds(), policy.Limit,
	).Scan(&attempts, &resetAt, &databaseNow)
	if err != nil {
		return ratelimit.Decision{}, fmt.Errorf("rate limit postgres: consume attempt: %w", err)
	}
	if attempts < 1 || attempts > policy.Limit+1 || resetAt.IsZero() || databaseNow.IsZero() {
		return ratelimit.Decision{}, fmt.Errorf("rate limit postgres: database returned invalid counter state")
	}
	if attempts <= policy.Limit {
		return ratelimit.Decision{Allowed: true}, nil
	}
	retryAfter := resetAt.Sub(databaseNow)
	if retryAfter < 0 {
		retryAfter = 0
	}
	if retryAfter > policy.Window+time.Second {
		return ratelimit.Decision{}, fmt.Errorf("rate limit postgres: database returned invalid reset time")
	}
	return ratelimit.Decision{RetryAfter: retryAfter}, nil
}

func (store *Store) Reset(ctx context.Context, key string) error {
	if store == nil || store.db == nil {
		return fmt.Errorf("rate limit postgres: store is not initialized")
	}
	if err := validateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := store.db.ExecContext(ctx, store.statements.Reset, key); err != nil {
		return fmt.Errorf("rate limit postgres: reset counter: %w", err)
	}
	return nil
}

func validateKey(key string) error {
	if len(key) != 64 || !hashedKeyPattern.MatchString(key) {
		return fmt.Errorf("rate limit postgres: key must be a lowercase SHA-256 value")
	}
	return nil
}

func validatePolicy(policy ratelimit.Policy) error {
	if policy.Limit < 1 || policy.Limit > maximumPolicy {
		return fmt.Errorf("rate limit postgres: policy limit must be between 1 and %d", maximumPolicy)
	}
	if policy.Window < time.Millisecond || policy.Window > maximumWindow || policy.Window%time.Millisecond != 0 {
		return fmt.Errorf("rate limit postgres: policy window must be a whole millisecond between 1ms and %s", maximumWindow)
	}
	return nil
}

func validIdentifier(value string) bool {
	return len(value) > 0 && len(value) <= 63 && identifierPattern.MatchString(value)
}

func quote(value string) string { return `"` + value + `"` }

// StatementsFor returns the parameterized operational SQL for a validated
// application-owned table without requiring a live database.
func StatementsFor(table string) (SQLStatements, error) {
	if !validIdentifier(table) {
		return SQLStatements{}, fmt.Errorf("rate limit postgres: invalid table name %q", table)
	}
	quoted := quote(table)
	return SQLStatements{
		Prune: fmt.Sprintf(`WITH db_clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
), expired AS MATERIALIZED (
    SELECT limits.key
    FROM %s AS limits, db_clock
    WHERE limits.reset_at <= db_clock.now
    ORDER BY limits.reset_at, limits.key
    LIMIT $1
)
DELETE FROM %s AS limits
USING expired, db_clock
WHERE limits.key = expired.key
  AND limits.reset_at <= db_clock.now`, quoted, quoted),
		Take: fmt.Sprintf(`WITH db_clock AS MATERIALIZED (
    SELECT clock_timestamp() AS now
), consumed AS (
    INSERT INTO %s AS limits (key, attempts, reset_at, updated_at)
    SELECT $1, 1, db_clock.now + ($2::bigint * interval '1 millisecond'), db_clock.now
    FROM db_clock
    ON CONFLICT (key) DO UPDATE SET
        attempts = CASE
            WHEN limits.reset_at <= (SELECT now FROM db_clock) THEN 1
            ELSE LEAST(limits.attempts + 1, $3::integer + 1)
        END,
        reset_at = CASE
            WHEN limits.reset_at <= (SELECT now FROM db_clock)
                THEN (SELECT now FROM db_clock) + ($2::bigint * interval '1 millisecond')
            ELSE limits.reset_at
        END,
        updated_at = (SELECT now FROM db_clock)
    RETURNING attempts, reset_at
)
SELECT consumed.attempts, consumed.reset_at, db_clock.now
FROM consumed CROSS JOIN db_clock`, quoted),
		Reset: fmt.Sprintf(`DELETE FROM %s WHERE key = $1`, quoted),
	}, nil
}

// Ensure SQL stays easy to inspect in source and diagnostics.
func (statements SQLStatements) String() string {
	return strings.Join([]string{statements.Prune, statements.Take, statements.Reset}, "\n\n")
}
