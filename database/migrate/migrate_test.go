package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

var migrationDriverSequence atomic.Uint64

type migrationDriver struct{ state *migrationDriverState }

type migrationDriverState struct {
	lock             chan struct{}
	mu               sync.Mutex
	tableExists      bool
	checksumColumn   bool
	checksumNullable bool
	applied          bool
	records          map[string]migrationRecord
	migrationRuns    int
	migrationErr     error
	executedSQL      []string
	checksumAddErr   error
}

type migrationRecord struct {
	name      string
	checksum  *string
	appliedAt string
}

func (value migrationDriver) Open(string) (driver.Conn, error) {
	return &migrationConn{state: value.state}, nil
}

type migrationConn struct{ state *migrationDriverState }

func (connection *migrationConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare is not supported")
}
func (connection *migrationConn) Close() error { return nil }
func (connection *migrationConn) Begin() (driver.Tx, error) {
	return connection.BeginTx(context.Background(), driver.TxOptions{})
}
func (connection *migrationConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	connection.state.mu.Lock()
	defer connection.state.mu.Unlock()
	return &migrationTx{state: connection.state, snapshot: snapshotMigrationState(connection.state)}, nil
}
func (connection *migrationConn) ExecContext(_ context.Context, query string, arguments []driver.NamedValue) (driver.Result, error) {
	connection.state.mu.Lock()
	defer connection.state.mu.Unlock()
	if connection.state.records == nil {
		connection.state.records = make(map[string]migrationRecord)
	}
	switch {
	case strings.HasPrefix(query, "CREATE TABLE IF NOT EXISTS"):
		if !connection.state.tableExists {
			connection.state.tableExists = true
			connection.state.checksumColumn = true
			connection.state.checksumNullable = false
		}
	case strings.Contains(query, "ADD COLUMN checksum"):
		connection.state.checksumColumn = true
		connection.state.checksumNullable = true
		if connection.state.checksumAddErr != nil {
			return nil, connection.state.checksumAddErr
		}
	case strings.Contains(query, "ALTER COLUMN checksum SET NOT NULL") || strings.Contains(query, "MODIFY COLUMN checksum VARCHAR(64) NOT NULL"):
		connection.state.checksumNullable = false
	case strings.Contains(query, "_goforge_checksum_upgrade") || query == "DROP TABLE schema_migrations":
		connection.state.executedSQL = append(connection.state.executedSQL, query)
		if strings.Contains(query, " RENAME TO ") {
			connection.state.checksumNullable = false
		}
	case strings.HasPrefix(query, "INSERT INTO") && len(arguments) > 0 && arguments[0].Value == mysqlUpgradeMarker:
		connection.state.records[mysqlUpgradeMarker] = migrationRecord{
			name:      mysqlUpgradeMarker,
			appliedAt: arguments[len(arguments)-1].Value.(string),
		}
	case strings.HasPrefix(query, "INSERT INTO"):
		connection.state.applied = true
		checksumValue := arguments[2].Value.(string)
		connection.state.records[arguments[0].Value.(string)] = migrationRecord{
			name:      arguments[1].Value.(string),
			checksum:  &checksumValue,
			appliedAt: arguments[3].Value.(string),
		}
	case strings.HasPrefix(query, "UPDATE schema_migrations SET checksum"):
		version := arguments[1].Value.(string)
		record := connection.state.records[version]
		checksumValue := arguments[0].Value.(string)
		record.checksum = &checksumValue
		connection.state.records[version] = record
	case strings.HasPrefix(query, "DELETE FROM"):
		delete(connection.state.records, arguments[0].Value.(string))
		connection.state.applied = len(connection.state.records) > 0
	case query == "CREATE TABLE widgets (id BIGINT)":
		connection.state.migrationRuns++
		err := connection.state.migrationErr
		if err != nil {
			return nil, err
		}
		connection.state.executedSQL = append(connection.state.executedSQL, query)
	case strings.HasPrefix(query, "DROP TABLE") || strings.HasPrefix(query, "UP ") || strings.HasPrefix(query, "DOWN "):
		connection.state.executedSQL = append(connection.state.executedSQL, query)
	default:
		return nil, fmt.Errorf("unexpected exec query %q", query)
	}
	return driver.RowsAffected(1), nil
}
func (connection *migrationConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	switch query {
	case "SELECT pg_advisory_lock($1)":
		<-connection.state.lock
		return &migrationRows{columns: []string{"pg_advisory_lock"}, values: [][]driver.Value{{""}}}, nil
	case "SELECT pg_advisory_unlock($1)":
		connection.state.lock <- struct{}{}
		return &migrationRows{columns: []string{"pg_advisory_unlock"}, values: [][]driver.Value{{true}}}, nil
	case "SELECT is_nullable FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2",
		"SELECT is_nullable FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?":
		connection.state.mu.Lock()
		hasChecksum := connection.state.checksumColumn
		checksumNullable := connection.state.checksumNullable
		connection.state.mu.Unlock()
		rows := &migrationRows{columns: []string{"is_nullable"}}
		if hasChecksum {
			nullable := "NO"
			if checksumNullable {
				nullable = "YES"
			}
			rows.values = [][]driver.Value{{nullable}}
		}
		return rows, nil
	case "SELECT version FROM schema_migrations WHERE version = ?":
		connection.state.mu.Lock()
		_, exists := connection.state.records[mysqlUpgradeMarker]
		connection.state.mu.Unlock()
		rows := &migrationRows{columns: []string{"version"}}
		if exists {
			rows.values = [][]driver.Value{{mysqlUpgradeMarker}}
		}
		return rows, nil
	case "SELECT version, name, checksum, applied_at FROM schema_migrations":
		connection.state.mu.Lock()
		defer connection.state.mu.Unlock()
		rows := &migrationRows{columns: []string{"version", "name", "checksum", "applied_at"}}
		for version, record := range connection.state.records {
			var storedChecksum driver.Value
			if record.checksum != nil {
				storedChecksum = *record.checksum
			}
			rows.values = append(rows.values, []driver.Value{version, record.name, storedChecksum, record.appliedAt})
		}
		return rows, nil
	case "SELECT version FROM schema_migrations ORDER BY version DESC":
		connection.state.mu.Lock()
		defer connection.state.mu.Unlock()
		versions := make([]string, 0, len(connection.state.records))
		for version := range connection.state.records {
			versions = append(versions, version)
		}
		sort.Sort(sort.Reverse(sort.StringSlice(versions)))
		rows := &migrationRows{columns: []string{"version"}}
		for _, version := range versions {
			rows.values = append(rows.values, []driver.Value{version})
		}
		return rows, nil
	default:
		return nil, fmt.Errorf("unexpected query %q", query)
	}
}

type migrationStateSnapshot struct {
	tableExists      bool
	checksumColumn   bool
	checksumNullable bool
	applied          bool
	records          map[string]migrationRecord
	migrationRuns    int
	executedSQL      []string
}

func snapshotMigrationState(state *migrationDriverState) migrationStateSnapshot {
	records := make(map[string]migrationRecord, len(state.records))
	for version, record := range state.records {
		records[version] = record
	}
	return migrationStateSnapshot{
		tableExists:      state.tableExists,
		checksumColumn:   state.checksumColumn,
		checksumNullable: state.checksumNullable,
		applied:          state.applied,
		records:          records,
		migrationRuns:    state.migrationRuns,
		executedSQL:      append([]string(nil), state.executedSQL...),
	}
}

type migrationTx struct {
	state    *migrationDriverState
	snapshot migrationStateSnapshot
}

func (*migrationTx) Commit() error { return nil }
func (tx *migrationTx) Rollback() error {
	tx.state.mu.Lock()
	defer tx.state.mu.Unlock()
	tx.state.tableExists = tx.snapshot.tableExists
	tx.state.checksumColumn = tx.snapshot.checksumColumn
	tx.state.checksumNullable = tx.snapshot.checksumNullable
	tx.state.applied = tx.snapshot.applied
	tx.state.records = tx.snapshot.records
	tx.state.migrationRuns = tx.snapshot.migrationRuns
	tx.state.executedSQL = tx.snapshot.executedSQL
	return nil
}

type migrationRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (rows *migrationRows) Columns() []string { return rows.columns }
func (rows *migrationRows) Close() error      { return nil }
func (rows *migrationRows) Next(destination []driver.Value) error {
	if rows.index >= len(rows.values) {
		return io.EOF
	}
	copy(destination, rows.values[rows.index])
	rows.index++
	return nil
}

func TestLoadPairsAndSortsMigrations(t *testing.T) {
	migrator := Migrator{Files: fstest.MapFS{
		"migrations/20_second.up.sql":   {Data: []byte("UP 20")},
		"migrations/20_second.down.sql": {Data: []byte("DOWN 20")},
		"migrations/001_first.up.sql":   {Data: []byte("UP 1")},
		"migrations/001_first.down.sql": {Data: []byte("DOWN 1")},
		"migrations/README.md":          {Data: []byte("ignored")},
	}, Dir: "migrations"}
	migrations, err := migrator.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 2 || migrations[0].Version != "001" || migrations[1].Name != "second" {
		t.Fatalf("unexpected migrations: %+v", migrations)
	}
}

func TestLoadRequiresUpAndDownPair(t *testing.T) {
	migrator := Migrator{Files: fstest.MapFS{
		"1_create_users.up.sql": {Data: []byte("CREATE TABLE users")},
	}}
	_, err := migrator.load()
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("expected pair error, got %v", err)
	}
}

func TestPostgresBindingsAreNumbered(t *testing.T) {
	migrator := Migrator{Dialect: Postgres}
	if migrator.bind(2) != "$2" {
		t.Fatalf("unexpected binding %q", migrator.bind(2))
	}
}

func TestPostgresAdvisoryLockSerializesCompleteUpRuns(t *testing.T) {
	state := &migrationDriverState{}
	db := openMigrationDB(t, state)
	db.SetMaxOpenConns(2)
	files := fstest.MapFS{
		"000001_widgets.up.sql":   {Data: []byte("CREATE TABLE widgets (id BIGINT)")},
		"000001_widgets.down.sql": {Data: []byte("DROP TABLE widgets")},
	}
	migrators := []*Migrator{
		{DB: db, Files: files, Dialect: Postgres},
		{DB: db, Files: files, Dialect: Postgres},
	}

	start := make(chan struct{})
	results := make(chan []Migration, len(migrators))
	errorsFound := make(chan error, len(migrators))
	var wait sync.WaitGroup
	for _, migrator := range migrators {
		wait.Add(1)
		go func(migrator *Migrator) {
			defer wait.Done()
			<-start
			completed, err := migrator.Up(context.Background(), 0)
			results <- completed
			errorsFound <- err
		}(migrator)
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsFound)

	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	completedCount := 0
	for completed := range results {
		completedCount += len(completed)
	}
	if completedCount != 1 || state.migrationRuns != 1 {
		t.Fatalf("completed=%d migration executions=%d", completedCount, state.migrationRuns)
	}
}

func TestFailedPostgresMigrationIsNotRecorded(t *testing.T) {
	migrationErr := errors.New("syntax error")
	state := &migrationDriverState{migrationErr: migrationErr}
	migrator := &Migrator{
		DB:      openMigrationDB(t, state),
		Files:   fstest.MapFS{"000001_widgets.up.sql": {Data: []byte("CREATE TABLE widgets (id BIGINT)")}, "000001_widgets.down.sql": {Data: []byte("DROP TABLE widgets")}},
		Dialect: Postgres,
	}
	completed, err := migrator.Up(context.Background(), 0)
	if !errors.Is(err, migrationErr) {
		t.Fatalf("expected migration error, got %v", err)
	}
	if len(completed) != 0 || state.applied {
		t.Fatalf("failed migration was reported or recorded: completed=%v applied=%t", completed, state.applied)
	}
}

func TestAppliedMigrationChecksumPreventsSchemaDrift(t *testing.T) {
	files := fstest.MapFS{
		"000001_widgets.up.sql":   {Data: []byte("CREATE TABLE widgets (id BIGINT)")},
		"000001_widgets.down.sql": {Data: []byte("DROP TABLE widgets")},
	}
	state := &migrationDriverState{}
	migrator := &Migrator{DB: openMigrationDB(t, state), Files: files, Dialect: Postgres}
	if _, err := migrator.Up(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	recorded := state.records["000001"]
	if recorded.checksum == nil || *recorded.checksum != checksum(Migration{Up: "CREATE TABLE widgets (id BIGINT)"}) {
		t.Fatalf("migration checksum was not recorded: %+v", recorded)
	}

	files["000001_widgets.up.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE widgets (id UUID)")}
	completed, err := migrator.Up(context.Background(), 0)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got completed=%v error=%v", completed, err)
	}
	if state.migrationRuns != 1 {
		t.Fatalf("drifted migration ran again: executions=%d", state.migrationRuns)
	}
}

func TestLegacyMigrationRowsAreUpgradedAfterNameVerification(t *testing.T) {
	appliedAt := time.Unix(123, 0).UTC().Format(time.RFC3339Nano)
	state := &migrationDriverState{
		tableExists: true,
		records: map[string]migrationRecord{
			"000001": {name: "widgets", appliedAt: appliedAt},
		},
	}
	migration := Migration{Version: "000001", Name: "widgets", Up: "UP widgets", Down: "DOWN widgets"}
	migrator := &Migrator{
		DB:      openMigrationDB(t, state),
		Files:   migrationFiles(migration),
		Dialect: Postgres,
	}
	statuses, err := migrator.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || !statuses[0].Applied {
		t.Fatalf("unexpected statuses: %+v", statuses)
	}
	if !state.checksumColumn {
		t.Fatal("legacy migration table was not upgraded")
	}
	if state.checksumNullable {
		t.Fatal("legacy checksum column remained nullable")
	}
	recorded := state.records["000001"]
	if recorded.checksum == nil || *recorded.checksum != checksum(migration) {
		t.Fatalf("legacy checksum was not backfilled: %+v", recorded)
	}
}

func TestMissingChecksumCannotBeRebaselined(t *testing.T) {
	migration := Migration{Version: "000001", Name: "widgets", Up: "UP widgets", Down: "DOWN widgets"}
	state := migrationStateWithApplied(migration)
	record := state.records[migration.Version]
	record.checksum = nil
	state.records[migration.Version] = record
	migrator := &Migrator{DB: openMigrationDB(t, state), Files: migrationFiles(migration), Dialect: Postgres}

	_, err := migrator.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refusing to rebaseline") {
		t.Fatalf("expected missing checksum rejection, got %v", err)
	}
	if state.records[migration.Version].checksum != nil {
		t.Fatal("missing checksum was silently rebaselined")
	}
}

func TestLegacyChecksumCanOnlyBeBackfilledOnce(t *testing.T) {
	migration := Migration{Version: "000001", Name: "widgets", Up: "UP widgets", Down: "DOWN widgets"}
	state := &migrationDriverState{
		tableExists: true,
		records: map[string]migrationRecord{
			migration.Version: {name: migration.Name, appliedAt: time.Unix(1, 0).UTC().Format(time.RFC3339Nano)},
		},
	}
	migrator := &Migrator{DB: openMigrationDB(t, state), Files: migrationFiles(migration), Dialect: Postgres}
	if _, err := migrator.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if state.checksumNullable {
		t.Fatal("legacy checksum column remained nullable after backfill")
	}

	record := state.records[migration.Version]
	record.checksum = nil // Simulate external ledger tampering despite the DB constraint.
	state.records[migration.Version] = record
	_, err := migrator.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refusing to rebaseline") {
		t.Fatalf("expected cleared checksum rejection, got %v", err)
	}
	if state.records[migration.Version].checksum != nil {
		t.Fatal("cleared checksum was silently rebaselined")
	}
}

func TestPostgresLegacyUpgradeRollsBackAndResumesAfterInterruption(t *testing.T) {
	migration := Migration{Version: "000001", Name: "widgets", Up: "UP widgets", Down: "DOWN widgets"}
	state := &migrationDriverState{
		tableExists: true,
		records: map[string]migrationRecord{
			migration.Version: {name: migration.Name, appliedAt: time.Unix(1, 0).UTC().Format(time.RFC3339Nano)},
		},
		checksumAddErr: errors.New("connection interrupted after ALTER TABLE"),
	}
	migrator := &Migrator{DB: openMigrationDB(t, state), Files: migrationFiles(migration), Dialect: Postgres}
	if _, err := migrator.Status(context.Background()); err == nil {
		t.Fatal("expected interrupted upgrade to fail")
	}
	if state.checksumColumn {
		t.Fatal("transactional checksum schema change survived rollback")
	}
	if state.records[migration.Version].checksum != nil {
		t.Fatal("interrupted upgrade unexpectedly backfilled checksum")
	}

	state.checksumAddErr = nil
	if _, err := migrator.Status(context.Background()); err != nil {
		t.Fatalf("resume legacy upgrade: %v", err)
	}
	if !state.checksumColumn || state.checksumNullable || state.records[migration.Version].checksum == nil {
		t.Fatalf("legacy upgrade did not resume to completion: %+v", state)
	}
}

func TestMySQLLegacyUpgradeMarkerResumesAfterImplicitDDLInterruption(t *testing.T) {
	migration := Migration{Version: "000001", Name: "widgets", Up: "UP widgets", Down: "DOWN widgets"}
	state := &migrationDriverState{
		tableExists: true,
		records: map[string]migrationRecord{
			migration.Version: {name: migration.Name, appliedAt: time.Unix(1, 0).UTC().Format(time.RFC3339Nano)},
		},
		checksumAddErr: errors.New("connection interrupted after implicit DDL commit"),
	}
	migrator := &Migrator{DB: openMigrationDB(t, state), Files: migrationFiles(migration), Dialect: MySQL}
	if _, err := migrator.Status(context.Background()); err == nil {
		t.Fatal("expected interrupted upgrade to fail")
	}
	if _, exists := state.records[mysqlUpgradeMarker]; !exists {
		t.Fatal("durable MySQL upgrade marker was not recorded")
	}
	if !state.checksumColumn || !state.checksumNullable {
		t.Fatal("test did not simulate committed nullable MySQL column")
	}

	state.checksumAddErr = nil
	if _, err := migrator.Status(context.Background()); err != nil {
		t.Fatalf("resume MySQL legacy upgrade: %v", err)
	}
	if _, exists := state.records[mysqlUpgradeMarker]; exists {
		t.Fatal("completed MySQL upgrade marker was not removed")
	}
	if state.checksumNullable || state.records[migration.Version].checksum == nil {
		t.Fatalf("MySQL upgrade did not resume to completion: %+v", state)
	}
}

func TestVerifyIsReadOnlyAndRejectsLedgerUpgrade(t *testing.T) {
	migration := Migration{Version: "000001", Name: "widgets", Up: "UP widgets", Down: "DOWN widgets"}
	state := &migrationDriverState{
		tableExists: true,
		records: map[string]migrationRecord{
			migration.Version: {name: migration.Name, appliedAt: time.Unix(1, 0).UTC().Format(time.RFC3339Nano)},
		},
	}
	migrator := &Migrator{DB: openMigrationDB(t, state), Files: migrationFiles(migration), Dialect: Postgres}
	if err := migrator.Verify(context.Background()); err == nil || !strings.Contains(err.Error(), "operational upgrade") {
		t.Fatalf("expected upgrade-required error, got %v", err)
	}
	if state.checksumColumn || state.records[migration.Version].checksum != nil {
		t.Fatal("Verify mutated the legacy migration ledger")
	}
}

func TestVerifyRejectsPendingMigration(t *testing.T) {
	first := Migration{Version: "000001", Name: "users", Up: "UP users", Down: "DOWN users"}
	second := Migration{Version: "000002", Name: "widgets", Up: "UP widgets", Down: "DOWN widgets"}
	state := migrationStateWithApplied(first)
	migrator := &Migrator{DB: openMigrationDB(t, state), Files: mergeMigrationFiles(first, second), Dialect: Postgres}
	if err := migrator.Verify(context.Background()); err == nil || !strings.Contains(err.Error(), "000002 is pending") {
		t.Fatalf("expected pending migration error, got %v", err)
	}
}

func TestVerifyAcceptsCompleteIntegrityCheckedLedger(t *testing.T) {
	migration := Migration{Version: "000001", Name: "widgets", Up: "UP widgets", Down: "DOWN widgets"}
	state := migrationStateWithApplied(migration)
	migrator := &Migrator{DB: openMigrationDB(t, state), Files: migrationFiles(migration), Dialect: Postgres}
	if err := migrator.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestEnforceChecksumNotNullUsesDialectCompatibleDDL(t *testing.T) {
	for _, dialect := range []Dialect{Postgres, MySQL, SQLite} {
		t.Run(string(dialect), func(t *testing.T) {
			state := &migrationDriverState{tableExists: true, checksumColumn: true, checksumNullable: true}
			migrator := &Migrator{DB: openMigrationDB(t, state), Files: fstest.MapFS{}, Dialect: dialect}
			if err := migrator.enforceChecksumNotNull(context.Background(), migrator.DB); err != nil {
				t.Fatal(err)
			}
			if state.checksumNullable {
				t.Fatalf("%s checksum column remained nullable", dialect)
			}
			if dialect == SQLite && len(state.executedSQL) != 4 {
				t.Fatalf("SQLite table rebuild executed %d statements, want 4: %v", len(state.executedSQL), state.executedSQL)
			}
		})
	}
}

func TestLegacyMigrationNameMismatchIsRejectedBeforeChecksumBackfill(t *testing.T) {
	state := &migrationDriverState{
		tableExists: true,
		records: map[string]migrationRecord{
			"000001": {name: "old_name", appliedAt: time.Unix(1, 0).UTC().Format(time.RFC3339Nano)},
		},
	}
	migration := Migration{Version: "000001", Name: "widgets", Up: "UP widgets", Down: "DOWN widgets"}
	migrator := &Migrator{DB: openMigrationDB(t, state), Files: migrationFiles(migration), Dialect: Postgres}
	_, err := migrator.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), "name mismatch") {
		t.Fatalf("expected name mismatch, got %v", err)
	}
	if state.records["000001"].checksum != nil {
		t.Fatal("mismatched legacy row was trusted and backfilled")
	}
}

func TestAppliedMigrationMissingFromFilesystemIsRejected(t *testing.T) {
	value := checksum(Migration{Up: "UP missing"})
	state := &migrationDriverState{
		tableExists:    true,
		checksumColumn: true,
		records: map[string]migrationRecord{
			"000009": {name: "missing", checksum: &value, appliedAt: time.Unix(1, 0).UTC().Format(time.RFC3339Nano)},
		},
	}
	migrator := &Migrator{DB: openMigrationDB(t, state), Files: fstest.MapFS{}, Dialect: Postgres}
	_, err := migrator.Status(context.Background())
	if err == nil || !strings.Contains(err.Error(), "missing from the migration filesystem") {
		t.Fatalf("expected missing migration error, got %v", err)
	}
}

func TestDownRevertsLatestAppliedMigrationsInReverseOrder(t *testing.T) {
	first := Migration{Version: "000001", Name: "users", Up: "UP users", Down: "DOWN users"}
	second := Migration{Version: "000002", Name: "widgets", Up: "UP widgets", Down: "DOWN widgets"}
	state := migrationStateWithApplied(first, second)
	migrator := &Migrator{
		DB:      openMigrationDB(t, state),
		Files:   mergeMigrationFiles(first, second),
		Dialect: Postgres,
	}
	completed, err := migrator.Down(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(completed) != 2 || completed[0].Version != "000002" || completed[1].Version != "000001" {
		t.Fatalf("unexpected completed migrations: %+v", completed)
	}
	if len(state.records) != 0 {
		t.Fatalf("rolled-back migrations remain recorded: %+v", state.records)
	}
	wantSQL := []string{"DOWN widgets", "DOWN users"}
	if fmt.Sprint(state.executedSQL) != fmt.Sprint(wantSQL) {
		t.Fatalf("rollback order=%v want=%v", state.executedSQL, wantSQL)
	}
}

func TestDownDefaultsToOneStep(t *testing.T) {
	first := Migration{Version: "000001", Name: "users", Up: "UP users", Down: "DOWN users"}
	second := Migration{Version: "000002", Name: "widgets", Up: "UP widgets", Down: "DOWN widgets"}
	state := migrationStateWithApplied(first, second)
	migrator := &Migrator{DB: openMigrationDB(t, state), Files: mergeMigrationFiles(first, second), Dialect: Postgres}
	completed, err := migrator.Down(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(completed) != 1 || completed[0].Version != "000002" {
		t.Fatalf("unexpected completed migrations: %+v", completed)
	}
	if _, exists := state.records["000001"]; !exists {
		t.Fatal("older migration was unexpectedly removed")
	}
}

func TestDownRejectsEmptyScriptWithoutDeletingRecord(t *testing.T) {
	migration := Migration{Version: "000001", Name: "widgets", Up: "UP widgets", Down: "   "}
	state := migrationStateWithApplied(migration)
	migrator := &Migrator{DB: openMigrationDB(t, state), Files: migrationFiles(migration), Dialect: Postgres}
	completed, err := migrator.Down(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "empty down script") {
		t.Fatalf("expected empty down error, got completed=%v error=%v", completed, err)
	}
	if _, exists := state.records["000001"]; !exists {
		t.Fatal("migration record was deleted despite rollback failure")
	}
}

func TestStatusReportsAppliedAndPendingMigrations(t *testing.T) {
	first := Migration{Version: "000001", Name: "users", Up: "UP users", Down: "DOWN users"}
	second := Migration{Version: "000002", Name: "widgets", Up: "UP widgets", Down: "DOWN widgets"}
	state := migrationStateWithApplied(first)
	migrator := &Migrator{DB: openMigrationDB(t, state), Files: mergeMigrationFiles(second, first), Dialect: Postgres}
	statuses, err := migrator.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0].Version != "000001" || !statuses[0].Applied || statuses[0].AppliedAt == nil {
		t.Fatalf("unexpected applied status: %+v", statuses)
	}
	if statuses[1].Version != "000002" || statuses[1].Applied || statuses[1].AppliedAt != nil {
		t.Fatalf("unexpected pending status: %+v", statuses[1])
	}
}

func migrationStateWithApplied(migrations ...Migration) *migrationDriverState {
	state := &migrationDriverState{tableExists: true, checksumColumn: true, records: make(map[string]migrationRecord)}
	for index, migration := range migrations {
		value := checksum(migration)
		state.records[migration.Version] = migrationRecord{
			name:      migration.Name,
			checksum:  &value,
			appliedAt: time.Unix(int64(index+1), 0).UTC().Format(time.RFC3339Nano),
		}
	}
	state.applied = len(state.records) > 0
	return state
}

func migrationFiles(migration Migration) fstest.MapFS {
	return fstest.MapFS{
		migration.Version + "_" + migration.Name + ".up.sql":   {Data: []byte(migration.Up)},
		migration.Version + "_" + migration.Name + ".down.sql": {Data: []byte(migration.Down)},
	}
}

func mergeMigrationFiles(migrations ...Migration) fstest.MapFS {
	files := fstest.MapFS{}
	for _, migration := range migrations {
		for name, file := range migrationFiles(migration) {
			files[name] = file
		}
	}
	return files
}

func openMigrationDB(t *testing.T, state *migrationDriverState) *sql.DB {
	t.Helper()
	state.lock = make(chan struct{}, 1)
	state.lock <- struct{}{}
	name := fmt.Sprintf("goforge-migration-test-%d", migrationDriverSequence.Add(1))
	sql.Register(name, migrationDriver{state: state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
