// Package migrate applies paired .up.sql and .down.sql files using database/sql.
// It intentionally does not choose a SQL driver or hide the underlying *sql.DB.
package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Dialect string

const (
	// Postgres migrations are serialized with a database advisory lock and each
	// migration is atomic when its SQL is transaction-safe.
	Postgres Dialect = "postgres"
	// MySQL is supported for bindings and bookkeeping, but MySQL DDL may commit
	// implicitly and therefore is not promised to be atomic.
	MySQL Dialect = "mysql"
	// SQLite relies on SQLite's transaction and locking behavior.
	SQLite Dialect = "sqlite"
)

type Migration struct {
	// Version is ordered lexically; use consistently padded numbers or timestamps.
	Version string
	Name    string
	Up      string
	Down    string
}

type Status struct {
	Version   string
	Name      string
	Applied   bool
	AppliedAt *time.Time
}

type Migrator struct {
	DB      *sql.DB
	Files   fs.FS
	Dir     string
	Dialect Dialect
	Table   string
	// LockID identifies the PostgreSQL advisory lock used to serialize a complete
	// migration run. Zero selects GoForge's stable default lock ID.
	LockID int64
	mu     sync.Mutex
}

var (
	migrationName = regexp.MustCompile(`^([0-9]+)_([a-zA-Z0-9_-]+)\.(up|down)\.sql$`)
	tableName     = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

const defaultPostgresLockID int64 = 0x474f464f524745

type runner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// Up applies pending migrations in version order. A nonpositive limit applies
// all pending migrations. On error, the result includes earlier committed work.
func (migrator *Migrator) Up(ctx context.Context, limit int) ([]Migration, error) {
	migrator.mu.Lock()
	defer migrator.mu.Unlock()
	var completed []Migration
	err := migrator.withRunLock(ctx, func(database runner) error {
		if err := migrator.prepare(ctx, database); err != nil {
			return err
		}
		migrations, err := migrator.load()
		if err != nil {
			return err
		}
		applied, err := migrator.applied(ctx, database)
		if err != nil {
			return err
		}
		for _, migration := range migrations {
			if _, exists := applied[migration.Version]; exists {
				continue
			}
			if limit > 0 && len(completed) >= limit {
				break
			}
			if strings.TrimSpace(migration.Up) == "" {
				return fmt.Errorf("migration %s has an empty up script", migration.Version)
			}
			err := inTransaction(ctx, database, func(tx *sql.Tx) error {
				if _, err := tx.ExecContext(ctx, migration.Up); err != nil {
					return fmt.Errorf("execute migration %s: %w", migration.Version, err)
				}
				table, err := migrator.table()
				if err != nil {
					return err
				}
				query := fmt.Sprintf("INSERT INTO %s (version, name, applied_at) VALUES (%s, %s, %s)", table, migrator.bind(1), migrator.bind(2), migrator.bind(3))
				if _, err := tx.ExecContext(ctx, query, migration.Version, migration.Name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
					return fmt.Errorf("record migration %s: %w", migration.Version, err)
				}
				return nil
			})
			if err != nil {
				return err
			}
			completed = append(completed, migration)
		}
		return nil
	})
	return completed, err
}

// Down reverses the latest applied versions. A nonpositive step count means one.
// On error, the result includes earlier committed reversals.
func (migrator *Migrator) Down(ctx context.Context, steps int) ([]Migration, error) {
	migrator.mu.Lock()
	defer migrator.mu.Unlock()
	if steps <= 0 {
		steps = 1
	}
	var completed []Migration
	err := migrator.withRunLock(ctx, func(database runner) error {
		if err := migrator.prepare(ctx, database); err != nil {
			return err
		}
		migrations, err := migrator.load()
		if err != nil {
			return err
		}
		byVersion := make(map[string]Migration, len(migrations))
		for _, migration := range migrations {
			byVersion[migration.Version] = migration
		}
		table, err := migrator.table()
		if err != nil {
			return err
		}
		rows, err := database.QueryContext(ctx, fmt.Sprintf("SELECT version FROM %s ORDER BY version DESC", table))
		if err != nil {
			return fmt.Errorf("list applied migrations: %w", err)
		}
		var versions []string
		for rows.Next() && len(versions) < steps {
			var version string
			if err := rows.Scan(&version); err != nil {
				_ = rows.Close()
				return err
			}
			versions = append(versions, version)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}

		for _, version := range versions {
			migration, exists := byVersion[version]
			if !exists {
				return fmt.Errorf("applied migration %s is missing from the migration filesystem", version)
			}
			if strings.TrimSpace(migration.Down) == "" {
				return fmt.Errorf("migration %s has an empty down script", version)
			}
			err := inTransaction(ctx, database, func(tx *sql.Tx) error {
				if _, err := tx.ExecContext(ctx, migration.Down); err != nil {
					return fmt.Errorf("rollback migration %s: %w", migration.Version, err)
				}
				query := fmt.Sprintf("DELETE FROM %s WHERE version = %s", table, migrator.bind(1))
				_, err := tx.ExecContext(ctx, query, migration.Version)
				return err
			})
			if err != nil {
				return err
			}
			completed = append(completed, migration)
		}
		return nil
	})
	return completed, err
}

func (migrator *Migrator) Status(ctx context.Context) ([]Status, error) {
	if err := migrator.prepare(ctx, migrator.DB); err != nil {
		return nil, err
	}
	migrations, err := migrator.load()
	if err != nil {
		return nil, err
	}
	applied, err := migrator.applied(ctx, migrator.DB)
	if err != nil {
		return nil, err
	}
	result := make([]Status, 0, len(migrations))
	for _, migration := range migrations {
		appliedAt, ok := applied[migration.Version]
		status := Status{Version: migration.Version, Name: migration.Name, Applied: ok}
		if ok {
			status.AppliedAt = &appliedAt
		}
		result = append(result, status)
	}
	return result, nil
}

func (migrator *Migrator) prepare(ctx context.Context, database runner) error {
	if migrator.DB == nil || migrator.Files == nil {
		return fmt.Errorf("migrator requires DB and Files")
	}
	if migrator.Dialect != Postgres && migrator.Dialect != MySQL && migrator.Dialect != SQLite {
		return fmt.Errorf("unsupported SQL dialect %q", migrator.Dialect)
	}
	table, err := migrator.table()
	if err != nil {
		return err
	}
	query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (version VARCHAR(255) PRIMARY KEY, name VARCHAR(255) NOT NULL, applied_at VARCHAR(35) NOT NULL)", table)
	if _, err := database.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("prepare migrations table: %w", err)
	}
	return nil
}

func (migrator *Migrator) load() ([]Migration, error) {
	directory := migrator.Dir
	if directory == "" {
		directory = "."
	}
	entries, err := fs.ReadDir(migrator.Files, directory)
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	byVersion := make(map[string]*Migration)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := migrationName.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}
		version, name, direction := matches[1], matches[2], matches[3]
		migration := byVersion[version]
		if migration == nil {
			migration = &Migration{Version: version, Name: name}
			byVersion[version] = migration
		} else if migration.Name != name {
			return nil, fmt.Errorf("migration version %s has inconsistent names", version)
		}
		contents, err := fs.ReadFile(migrator.Files, path.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		if direction == "up" {
			migration.Up = string(contents)
		} else {
			migration.Down = string(contents)
		}
	}
	result := make([]Migration, 0, len(byVersion))
	for _, migration := range byVersion {
		if migration.Up == "" || migration.Down == "" {
			return nil, fmt.Errorf("migration %s must have both .up.sql and .down.sql files", migration.Version)
		}
		result = append(result, *migration)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	return result, nil
}

func (migrator *Migrator) applied(ctx context.Context, database runner) (map[string]time.Time, error) {
	table, err := migrator.table()
	if err != nil {
		return nil, err
	}
	rows, err := database.QueryContext(ctx, fmt.Sprintf("SELECT version, applied_at FROM %s", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]time.Time)
	for rows.Next() {
		var version string
		var encodedTime string
		if err := rows.Scan(&version, &encodedTime); err != nil {
			return nil, err
		}
		appliedAt, err := time.Parse(time.RFC3339Nano, encodedTime)
		if err != nil {
			return nil, fmt.Errorf("parse applied time for migration %s: %w", version, err)
		}
		result[version] = appliedAt
	}
	return result, rows.Err()
}

func (migrator *Migrator) table() (string, error) {
	if migrator.Table == "" {
		return "schema_migrations", nil
	}
	if !tableName.MatchString(migrator.Table) {
		return "", fmt.Errorf("migrate: unsafe table name %q", migrator.Table)
	}
	return migrator.Table, nil
}

func (migrator *Migrator) bind(position int) string {
	if migrator.Dialect == Postgres {
		return fmt.Sprintf("$%d", position)
	}
	return "?"
}

func inTransaction(ctx context.Context, database runner, work func(*sql.Tx) error) (err error) {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = tx.Rollback()
			panic(recovered)
		}
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				err = errors.Join(err, fmt.Errorf("rollback migration transaction: %w", rollbackErr))
			}
		}
	}()
	if err = work(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit migration transaction: %w", err)
	}
	return nil
}

// withRunLock pins PostgreSQL work to one database/sql connection and holds a
// session-level advisory lock from before the migration table is inspected until
// every migration has committed. Other dialects retain their database-native
// behavior and are not represented as transactionally safe for DDL by this API.
func (migrator *Migrator) withRunLock(ctx context.Context, work func(runner) error) (err error) {
	if migrator.DB == nil || migrator.Files == nil {
		return fmt.Errorf("migrator requires DB and Files")
	}
	if migrator.Dialect != Postgres {
		return work(migrator.DB)
	}

	connection, err := migrator.DB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("reserve migration connection: %w", err)
	}
	defer connection.Close()

	lockID := migrator.LockID
	if lockID == 0 {
		lockID = defaultPostgresLockID
	}
	var ignored any
	if err := connection.QueryRowContext(ctx, "SELECT pg_advisory_lock($1)", lockID).Scan(&ignored); err != nil {
		return fmt.Errorf("acquire PostgreSQL migration lock: %w", err)
	}
	defer func() {
		unlockContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var unlocked bool
		unlockErr := connection.QueryRowContext(unlockContext, "SELECT pg_advisory_unlock($1)", lockID).Scan(&unlocked)
		if unlockErr == nil && !unlocked {
			unlockErr = fmt.Errorf("lock was not held by the migration connection")
		}
		if unlockErr != nil {
			// Do not return a pooled connection that might still own a session lock.
			_ = connection.Raw(func(any) error { return driver.ErrBadConn })
			err = errors.Join(err, fmt.Errorf("release PostgreSQL migration lock: %w", unlockErr))
		}
	}()

	return work(connection)
}
