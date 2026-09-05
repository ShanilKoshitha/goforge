package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/security/ratelimit"
	ratelimitpostgres "github.com/ShanilKoshitha/goforge/security/ratelimit/postgres"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestSchemaAndStatementsAreInspectable(t *testing.T) {
	for _, fragment := range []string{
		`CREATE TABLE "goforge_rate_limits"`, "attempts integer", "reset_at timestamptz",
		"octet_length(key) = 64", `CREATE INDEX ON "goforge_rate_limits"`,
	} {
		if !strings.Contains(ratelimitpostgres.Schema, fragment) {
			t.Fatalf("schema is missing %q", fragment)
		}
	}
	defaultSchema, err := ratelimitpostgres.SchemaFor(ratelimitpostgres.DefaultTable)
	if err != nil || defaultSchema != ratelimitpostgres.Schema {
		t.Fatalf("default Schema and SchemaFor drifted: err=%v", err)
	}
	statements, err := ratelimitpostgres.StatementsFor("application_rate_limits")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{statements.Prune, statements.Take, statements.Reset} {
		if !strings.Contains(statement, `"application_rate_limits"`) {
			t.Fatalf("custom table is not quoted in statement: %s", statement)
		}
	}
	for _, fragment := range []string{"clock_timestamp()", "ON CONFLICT (key)", "LEAST(", "LIMIT $1"} {
		if !strings.Contains(statements.String(), fragment) {
			t.Fatalf("statements are missing %q", fragment)
		}
	}
	if strings.Contains(statements.String(), ratelimit.Key("private@example.com")) {
		t.Fatal("SQL contains a concrete key rather than bind parameters")
	}
	db := &sql.DB{}
	store, err := ratelimitpostgres.New(db, ratelimitpostgres.WithTable("application_rate_limits"))
	if err != nil || store.Statements() != statements {
		t.Fatalf("store statements drifted: err=%v", err)
	}
}

func TestStoreConfigurationAndInputsAreDefensive(t *testing.T) {
	if _, err := ratelimitpostgres.New(nil); err == nil {
		t.Fatal("nil database was accepted")
	}
	db := &sql.DB{}
	invalidOptions := []ratelimitpostgres.Option{
		nil,
		ratelimitpostgres.WithTable("bad-table"),
		ratelimitpostgres.WithTable(strings.Repeat("a", 64)),
		ratelimitpostgres.WithPruneLimit(0),
		ratelimitpostgres.WithPruneLimit(10_001),
	}
	for _, option := range invalidOptions {
		if _, err := ratelimitpostgres.New(db, option); err == nil {
			t.Fatalf("invalid option %#v was accepted", option)
		}
	}
	store, err := ratelimitpostgres.New(db)
	if err != nil {
		t.Fatal(err)
	}
	validKey := ratelimit.Key("login", "ada@example.com")
	tests := []struct {
		name   string
		key    string
		policy ratelimit.Policy
	}{
		{name: "raw key", key: "ada@example.com", policy: ratelimit.Policy{Limit: 1, Window: time.Second}},
		{name: "uppercase hash", key: strings.ToUpper(validKey), policy: ratelimit.Policy{Limit: 1, Window: time.Second}},
		{name: "zero limit", key: validKey, policy: ratelimit.Policy{Window: time.Second}},
		{name: "excessive limit", key: validKey, policy: ratelimit.Policy{Limit: 1_000_001, Window: time.Second}},
		{name: "submillisecond", key: validKey, policy: ratelimit.Policy{Limit: 1, Window: time.Nanosecond}},
		{name: "fractional millisecond", key: validKey, policy: ratelimit.Policy{Limit: 1, Window: time.Millisecond + time.Nanosecond}},
		{name: "excessive window", key: validKey, policy: ratelimit.Policy{Limit: 1, Window: 366 * 24 * time.Hour}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.Take(context.Background(), test.key, test.policy, time.Now()); err == nil {
				t.Fatal("invalid input was accepted")
			}
		})
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Take(canceled, validKey, ratelimit.Policy{Limit: 1, Window: time.Second}, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Take error = %v", err)
	}
	if err := store.Reset(context.Background(), "raw"); err == nil {
		t.Fatal("Reset accepted a raw key")
	}
	if _, err := ratelimitpostgres.SchemaFor("bad-table"); err == nil {
		t.Fatal("SchemaFor accepted an unsafe identifier")
	}
}

func TestPostgresStoreIsAtomicDurableAndBoundedlyPruned(t *testing.T) {
	databaseURL := os.Getenv("GOFORGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOFORGE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close PostgreSQL connection: %v", err)
		}
	})
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	table := fmt.Sprintf("ratelimit_live_%d", time.Now().UnixNano())
	schema, err := ratelimitpostgres.SchemaFor(table)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		t.Fatal(err)
	}
	secondDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := secondDB.Close(); err != nil {
			t.Errorf("close second PostgreSQL connection: %v", err)
		}
	})
	if err := secondDB.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	drop, _ := ratelimitpostgres.DropSchemaFor(table)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := db.ExecContext(cleanupCtx, drop); err != nil {
			t.Errorf("drop rate-limit table: %v", err)
		}
	})

	first, err := ratelimitpostgres.New(db, ratelimitpostgres.WithTable(table), ratelimitpostgres.WithPruneLimit(2))
	if err != nil {
		t.Fatal(err)
	}
	// A separately constructed store represents another process or a restart;
	// all correctness state must remain in PostgreSQL rather than Go memory.
	second, err := ratelimitpostgres.New(secondDB, ratelimitpostgres.WithTable(table), ratelimitpostgres.WithPruneLimit(2))
	if err != nil {
		t.Fatal(err)
	}
	policy := ratelimit.Policy{Limit: 2, Window: 150 * time.Millisecond}
	key := ratelimit.Key("account-login", "ada@example.com")
	for attempt := 1; attempt <= 2; attempt++ {
		decision, err := first.Take(ctx, key, policy, time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC))
		if err != nil || !decision.Allowed {
			t.Fatalf("allowed attempt %d = %+v, %v", attempt, decision, err)
		}
	}
	decision, err := second.Take(ctx, key, policy, time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || decision.Allowed || decision.RetryAfter <= 0 || decision.RetryAfter > policy.Window+time.Second {
		t.Fatalf("shared denied attempt = %+v, %v", decision, err)
	}
	expiryDeadline := time.Now().Add(2 * time.Second)
	for {
		decision, err = second.Take(ctx, key, policy, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if decision.Allowed {
			break
		}
		if time.Now().After(expiryDeadline) {
			t.Fatalf("fixed window did not expire: %+v", decision)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if decision, err = first.Take(ctx, key, policy, time.Time{}); err != nil || !decision.Allowed {
		t.Fatalf("second attempt in renewed window = %+v, %v", decision, err)
	}
	if decision, err = first.Take(ctx, key, policy, time.Time{}); err != nil || decision.Allowed {
		t.Fatalf("renewed window did not enforce its limit = %+v, %v", decision, err)
	}
	if err := secondDB.Close(); err != nil {
		t.Fatal(err)
	}
	secondDB, err = sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	second, err = ratelimitpostgres.New(secondDB, ratelimitpostgres.WithTable(table), ratelimitpostgres.WithPruneLimit(2))
	if err != nil {
		t.Fatal(err)
	}
	decision, err = second.Take(ctx, key, policy, time.Time{})
	if err != nil || decision.Allowed {
		t.Fatalf("counter did not survive connection restart = %+v, %v", decision, err)
	}
	if err := second.Reset(ctx, key); err != nil {
		t.Fatal(err)
	}
	decision, err = first.Take(ctx, key, policy, time.Time{})
	if err != nil || !decision.Allowed {
		t.Fatalf("cross-instance reset = %+v, %v", decision, err)
	}

	concurrentKey := ratelimit.Key("source-login", "198.51.100.7")
	// Keep the whole race-instrumented burst in one fixed window. The row-level
	// conflict is deliberately serialized by PostgreSQL and can exceed one
	// second on shared CI runners without indicating a second-window allowance.
	concurrentPolicy := ratelimit.Policy{Limit: 7, Window: 30 * time.Second}
	var allowed atomic.Int64
	var group sync.WaitGroup
	errorsFound := make(chan error, 64)
	for index := range 64 {
		group.Add(1)
		go func(store *ratelimitpostgres.Store) {
			defer group.Done()
			result, err := store.Take(ctx, concurrentKey, concurrentPolicy, time.Time{})
			if err != nil {
				errorsFound <- err
				return
			}
			if result.Allowed {
				allowed.Add(1)
			}
		}(map[bool]*ratelimitpostgres.Store{true: first, false: second}[index%2 == 0])
	}
	group.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent Take: %v", err)
	}
	if got := allowed.Load(); got != int64(concurrentPolicy.Limit) {
		t.Fatalf("concurrent allowed attempts = %d, want %d", got, concurrentPolicy.Limit)
	}

	if _, err := db.ExecContext(ctx, `DELETE FROM "`+table+`"`); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO "` + table + `" (key, attempts, reset_at, updated_at) VALUES ($1, 1, clock_timestamp() - interval '1 second', clock_timestamp())`
	for index := range 5 {
		if _, err := db.ExecContext(ctx, insert, ratelimit.Key("expired", fmt.Sprint(index))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := first.Take(ctx, ratelimit.Key("fresh"), ratelimit.Policy{Limit: 1, Window: time.Second}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	var expired int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+table+`" WHERE reset_at <= clock_timestamp()`).Scan(&expired); err != nil {
		t.Fatal(err)
	}
	if expired != 3 {
		t.Fatalf("one Take pruned %d rows, want exactly 2", 5-expired)
	}

	if _, err := db.ExecContext(ctx, `INSERT INTO "`+table+`" (key, attempts, reset_at, updated_at) VALUES ('raw', 1, NOW(), NOW())`); err == nil {
		t.Fatal("schema accepted a non-hashed key")
	}
}
