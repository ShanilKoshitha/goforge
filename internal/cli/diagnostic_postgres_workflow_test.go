package cli

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ShanilKoshitha/goforge/database/migrate"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Each state is deliberately prepared by the test, not by doctor. Snapshots
// surround the actual generated command, including failures where a mistakenly
// operational migration inspection could otherwise silently repair bookkeeping.
func TestGeneratedDiagnosticsPostgresWorkflow(t *testing.T) {
	databaseURL := os.Getenv("GOFORGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOFORGE_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	admin, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal("cannot open acceptance PostgreSQL handle")
	}
	defer admin.Close()
	if err := admin.PingContext(ctx); err != nil {
		t.Fatal("acceptance PostgreSQL is unavailable")
	}
	schema := fmt.Sprintf("goforge_diagnostics_%d", time.Now().UnixNano())
	if !regexp.MustCompile(`^goforge_diagnostics_[0-9]+$`).MatchString(schema) {
		t.Fatal("invalid generated schema identifier")
	}
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatalf("create isolated diagnostic schema: %v", err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := admin.ExecContext(cleanup, `DROP SCHEMA "`+schema+`" CASCADE`); err != nil {
			t.Errorf("drop isolated diagnostic schema: %v", err)
		}
	}()
	isolatedURL, err := postgresSchemaURL(databaseURL, schema)
	if err != nil {
		t.Fatal("cannot form isolated PostgreSQL URL")
	}
	db, err := sql.Open("pgx", isolatedURL)
	if err != nil {
		t.Fatal("cannot open isolated PostgreSQL handle")
	}
	defer db.Close()
	directory := diagnosticCandidateProject(t)
	t.Chdir(directory)
	for key, value := range map[string]string{
		"GOWORK": "off", "APP_ENV": "local", "APP_URL": "http://localhost:8080", "DATABASE_URL": isolatedURL,
	} {
		t.Setenv(key, value)
	}

	runState := func(t *testing.T, wantCode string) {
		t.Helper()
		beforeFiles := diagnosticSnapshot(t, directory)
		beforeDB := diagnosticDatabaseSnapshot(t, ctx, db, schema)
		raw, processErr := captureDiagnosticProcess(ctx, execProcessRunner{}, "./cmd/diagnostics", "doctor")
		var report diagnosticReport
		if err := decodeDiagnosticJSON(raw, &report); err != nil || !validDiagnosticReport(report) {
			t.Fatalf("generated doctor returned an invalid report: %v (process error=%v)", err, processErr)
		}
		for index, check := range report.Checks {
			if index != 6 && check.Status != "pass" {
				t.Errorf("independent check %s did not pass: status=%s code=%s", check.ID, check.Status, check.Code)
			}
		}
		migration := report.Checks[6]
		wantStatus := "fail"
		if wantCode == "ok" {
			wantStatus = "pass"
		}
		if migration.Code != wantCode || migration.Status != wantStatus || (processErr == nil) != (wantCode == "ok") {
			t.Fatalf("migration verdict=%s/%s process=%v, want=%s/%s", migration.Status, migration.Code, processErr, wantStatus, wantCode)
		}
		assertDiagnosticPrivacy(t, string(raw))
		if strings.Contains(string(raw), "never-print-ledger-secret") {
			t.Fatal("diagnostic report disclosed an applied ledger value")
		}
		// The CLI presentation/orchestration must preserve the same boundaries,
		// including ORM/view/asset checks after an integrity failure.
		var output bytes.Buffer
		cliErr := diagnoseProject(ctx, &output, execProcessRunner{})
		if (cliErr == nil) != (wantCode == "ok") {
			t.Fatalf("CLI doctor exit disagrees with migration state %s: %v", wantCode, cliErr)
		}
		for _, id := range []string{"orm", "views", "assets"} {
			if !strings.Contains(output.String(), "pass\t"+id+"\t") {
				t.Errorf("independent artifact %s did not run successfully", id)
			}
		}
		assertDiagnosticPrivacy(t, output.String())
		if strings.Contains(output.String(), "never-print-ledger-secret") {
			t.Fatal("CLI doctor disclosed an applied ledger value")
		}
		if !reflect.DeepEqual(beforeFiles, diagnosticSnapshot(t, directory)) {
			t.Fatal("doctor changed application files/directories")
		}
		if !reflect.DeepEqual(beforeDB, diagnosticDatabaseSnapshot(t, ctx, db, schema)) {
			t.Fatal("doctor changed database schema, ledger, domain rows, or sequence state")
		}
	}

	t.Run("uninitialized_ledger", func(t *testing.T) { runState(t, "ledger_upgrade") })
	runner := &migrate.Migrator{DB: db, Files: os.DirFS(filepath.Join(directory, "database", "migrations")), Dialect: migrate.Postgres}
	if _, err := runner.Up(ctx, 0); err != nil {
		t.Fatalf("prepare migrated diagnostic fixture: %v", err)
	}
	// Nonempty domain/session data and an immediately claimable, registered job
	// make the nonmutation assertions meaningful beyond an empty database. The
	// mail job points at a missing outbox row: an accidentally started worker
	// would consume it rather than needing a real SMTP server to change state.
	for _, query := range []string{
		`INSERT INTO users (name,email,password_hash) VALUES ('diagnostic sentinel','diagnostic-sentinel@example.test','private-password-hash-secret')`,
		`INSERT INTO sessions (id,payload,expires_at) VALUES ('diagnostic-sentinel','private-session-payload-secret',NOW()+INTERVAL '1 day')`,
		`INSERT INTO goforge_jobs (id,queue,name,payload,available_at,max_attempts,timeout_ms,backoff_ms,created_at,updated_at) VALUES ('8de09b90-7130-4b88-8641-6cc5a8b9e841','default','goforge.mail.deliver.v1','{"outbox_id":"c27b2d4b-8880-4afc-bc43-e0a3778692ce"}',NOW()-INTERVAL '1 minute',8,120000,ARRAY[5000::bigint],NOW(),NOW())`,
	} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatalf("prepare persistent nonmutation sentinel: %v", err)
		}
	}
	t.Run("healthy", func(t *testing.T) { runState(t, "ok") })

	// Derive the first actual applied pair rather than assuming how many tables,
	// migrations, or resource fixtures the fresh scaffold contains.
	var version, name, originalChecksum string
	if err := db.QueryRowContext(ctx, "SELECT version, name, checksum FROM schema_migrations ORDER BY version LIMIT 1").Scan(&version, &name, &originalChecksum); err != nil {
		t.Fatal(err)
	}
	upPath := filepath.Join(directory, "database", "migrations", version+"_"+name+".up.sql")
	downPath := filepath.Join(directory, "database", "migrations", version+"_"+name+".down.sql")
	up, err := os.ReadFile(upPath)
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile(downPath)
	if err != nil {
		t.Fatal(err)
	}
	write := func(path string, contents []byte) {
		t.Helper()
		if err := os.WriteFile(path, contents, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	exec := func(query string, arguments ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, arguments...); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("pending", func(t *testing.T) {
		pendingUp := filepath.Join(directory, "database", "migrations", "999999_diagnostic_pending.up.sql")
		pendingDown := filepath.Join(directory, "database", "migrations", "999999_diagnostic_pending.down.sql")
		write(pendingUp, []byte("CREATE TABLE diagnostic_pending (id BIGINT PRIMARY KEY);\n"))
		write(pendingDown, []byte("DROP TABLE diagnostic_pending;\n"))
		defer os.Remove(pendingUp)
		defer os.Remove(pendingDown)
		runState(t, "pending")
	})
	t.Run("changed_applied_source_checksum", func(t *testing.T) {
		write(upPath, append(append([]byte(nil), up...), []byte("\n-- deliberate changed applied source\n")...))
		defer write(upPath, up)
		runState(t, "drift")
	})
	t.Run("changed_applied_recorded_checksum", func(t *testing.T) {
		exec("UPDATE schema_migrations SET checksum = $1 WHERE version = $2", strings.Repeat("0", 64), version)
		defer exec("UPDATE schema_migrations SET checksum = $1 WHERE version = $2", originalChecksum, version)
		runState(t, "drift")
	})
	t.Run("changed_applied_name_is_private", func(t *testing.T) {
		exec("UPDATE schema_migrations SET name = $1 WHERE version = $2", "never-print-ledger-secret", version)
		defer exec("UPDATE schema_migrations SET name = $1 WHERE version = $2", name, version)
		runState(t, "drift")
	})
	t.Run("missing_applied_source_pair", func(t *testing.T) {
		if err := os.Remove(upPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(downPath); err != nil {
			t.Fatal(err)
		}
		defer write(upPath, up)
		defer write(downPath, down)
		runState(t, "drift")
	})
	t.Run("missing_one_source_half", func(t *testing.T) {
		if err := os.Remove(downPath); err != nil {
			t.Fatal(err)
		}
		defer write(downPath, down)
		runState(t, "invalid_source")
	})
	t.Run("legacy_nullable_checksum", func(t *testing.T) {
		exec("ALTER TABLE schema_migrations ALTER COLUMN checksum DROP NOT NULL")
		defer exec("ALTER TABLE schema_migrations ALTER COLUMN checksum SET NOT NULL")
		runState(t, "ledger_upgrade")
	})
	t.Run("legacy_absent_checksum", func(t *testing.T) {
		rows, err := db.QueryContext(ctx, "SELECT version, checksum FROM schema_migrations")
		if err != nil {
			t.Fatal(err)
		}
		checksums := make(map[string]string)
		for rows.Next() {
			var version, checksum string
			if err := rows.Scan(&version, &checksum); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			checksums[version] = checksum
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		exec("ALTER TABLE schema_migrations DROP COLUMN checksum")
		defer func() {
			exec("ALTER TABLE schema_migrations ADD COLUMN checksum VARCHAR(64)")
			for version, checksum := range checksums {
				exec("UPDATE schema_migrations SET checksum = $1 WHERE version = $2", checksum, version)
			}
			exec("ALTER TABLE schema_migrations ALTER COLUMN checksum SET NOT NULL")
		}()
		runState(t, "ledger_upgrade")
	})
	t.Run("healthy_after_fixture_restoration", func(t *testing.T) { runState(t, "ok") })
}

func diagnosticDatabaseSnapshot(t *testing.T, ctx context.Context, db *sql.DB, schema string) map[string][]string {
	t.Helper()
	result := make(map[string][]string)
	queries := map[string]string{
		"relations":   `SELECT c.relname || ':' || c.relkind::text || ':' || c.relfilenode::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 ORDER BY c.relname`,
		"columns":     `SELECT table_name || ':' || column_name || ':' || data_type || ':' || is_nullable || ':' || COALESCE(column_default,'') || ':' || ordinal_position::text FROM information_schema.columns WHERE table_schema=$1 ORDER BY table_name, ordinal_position`,
		"indexes":     `SELECT tablename || ':' || indexname || ':' || indexdef FROM pg_indexes WHERE schemaname=$1 ORDER BY tablename,indexname`,
		"constraints": `SELECT c.relname || ':' || con.conname || ':' || pg_get_constraintdef(con.oid) FROM pg_constraint con JOIN pg_class c ON c.oid=con.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 ORDER BY c.relname,con.conname`,
	}
	read := func(key, query string, args ...any) {
		t.Helper()
		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			t.Fatalf("snapshot %s: %v", key, err)
		}
		defer rows.Close()
		values := make([]string, 0)
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				t.Fatal(err)
			}
			values = append(values, value)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		sort.Strings(values)
		result[key] = values
	}
	for key, query := range queries {
		read(key, query, schema)
	}
	rows, err := db.QueryContext(ctx, `SELECT c.relname,c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relkind IN ('r','S') ORDER BY c.relname`, schema)
	if err != nil {
		t.Fatal(err)
	}
	type relation struct{ name, kind string }
	var relations []relation
	for rows.Next() {
		var item relation
		if err := rows.Scan(&item.name, &item.kind); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		relations = append(relations, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	quote := func(identifier string) string { return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"` }
	for _, relation := range relations {
		qualified := quote(schema) + "." + quote(relation.name)
		if relation.kind == "S" {
			read("sequence:"+relation.name, "SELECT last_value::text || ':' || is_called::text FROM "+qualified)
		} else {
			read("rows:"+relation.name, "SELECT row_to_json(snapshot_row)::text FROM "+qualified+" AS snapshot_row")
		}
	}
	return result
}
