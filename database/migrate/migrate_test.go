package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
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
	lock          chan struct{}
	mu            sync.Mutex
	applied       bool
	migrationRuns int
	migrationErr  error
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
	return migrationTx{}, nil
}
func (connection *migrationConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	switch {
	case strings.HasPrefix(query, "CREATE TABLE IF NOT EXISTS"):
	case strings.HasPrefix(query, "INSERT INTO"):
		connection.state.mu.Lock()
		connection.state.applied = true
		connection.state.mu.Unlock()
	case query == "CREATE TABLE widgets (id BIGINT)":
		connection.state.mu.Lock()
		connection.state.migrationRuns++
		err := connection.state.migrationErr
		connection.state.mu.Unlock()
		if err != nil {
			return nil, err
		}
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
	case "SELECT version, applied_at FROM schema_migrations":
		connection.state.mu.Lock()
		applied := connection.state.applied
		connection.state.mu.Unlock()
		rows := &migrationRows{columns: []string{"version", "applied_at"}}
		if applied {
			rows.values = [][]driver.Value{{"000001", time.Unix(1, 0).UTC().Format(time.RFC3339Nano)}}
		}
		return rows, nil
	default:
		return nil, fmt.Errorf("unexpected query %q", query)
	}
}

type migrationTx struct{}

func (migrationTx) Commit() error   { return nil }
func (migrationTx) Rollback() error { return nil }

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
