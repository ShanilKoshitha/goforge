// Package migrate applies paired .up.sql and .down.sql files using database/sql.
// It intentionally does not choose a SQL driver or hide the underlying *sql.DB.
package migrate

import (
	"context"
	"crypto/sha256"
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

type appliedMigration struct {
	Name      string
	Checksum  sql.NullString
	AppliedAt time.Time
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

const mysqlUpgradeMarker = "__goforge_checksum_upgrade__"

type executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type runner interface {
	executor
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// Up applies pending migrations in version order. A nonpositive limit applies
// all pending migrations. On error, the result includes earlier committed work.
func (migrator *Migrator) Up(ctx context.Context, limit int) ([]Migration, error) {
	migrator.mu.Lock()
	defer migrator.mu.Unlock()
	var completed []Migration
	err := migrator.withRunLock(ctx, func(database runner) error {
		migrations, err := migrator.load()
		if err != nil {
			return err
		}
		if err := migrator.prepare(ctx, database, migrations); err != nil {
			return err
		}
		applied, err := migrator.applied(ctx, database)
		if err != nil {
			return err
		}
		if err := migrator.verifyApplied(ctx, database, migrations, applied, false); err != nil {
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
				query := fmt.Sprintf("INSERT INTO %s (version, name, checksum, applied_at) VALUES (%s, %s, %s, %s)", table, migrator.bind(1), migrator.bind(2), migrator.bind(3), migrator.bind(4))
				if _, err := tx.ExecContext(ctx, query, migration.Version, migration.Name, checksum(migration), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
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
		migrations, err := migrator.load()
		if err != nil {
			return err
		}
		if err := migrator.prepare(ctx, database, migrations); err != nil {
			return err
		}
		byVersion := make(map[string]Migration, len(migrations))
		for _, migration := range migrations {
			byVersion[migration.Version] = migration
		}
		applied, err := migrator.applied(ctx, database)
		if err != nil {
			return err
		}
		if err := migrator.verifyApplied(ctx, database, migrations, applied, false); err != nil {
			return err
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
	migrator.mu.Lock()
	defer migrator.mu.Unlock()
	var result []Status
	err := migrator.withRunLock(ctx, func(database runner) error {
		migrations, err := migrator.load()
		if err != nil {
			return err
		}
		if err := migrator.prepare(ctx, database, migrations); err != nil {
			return err
		}
		applied, err := migrator.applied(ctx, database)
		if err != nil {
			return err
		}
		if err := migrator.verifyApplied(ctx, database, migrations, applied, false); err != nil {
			return err
		}
		result = make([]Status, 0, len(migrations))
		for _, migration := range migrations {
			appliedMigration, ok := applied[migration.Version]
			status := Status{Version: migration.Version, Name: migration.Name, Applied: ok}
			if ok {
				appliedAt := appliedMigration.AppliedAt
				status.AppliedAt = &appliedAt
			}
			result = append(result, status)
		}
		return nil
	})
	return result, err
}

// Verify checks migration integrity without creating or altering database
// objects. It is suitable for request-path readiness checks; use Status for
// operational inspection that may upgrade a legacy migration ledger.
func (migrator *Migrator) Verify(ctx context.Context) error {
	if err := migrator.validate(); err != nil {
		return err
	}
	migrations, err := migrator.load()
	if err != nil {
		return err
	}
	table, err := migrator.table()
	if err != nil {
		return err
	}
	hasChecksum, checksumNullable, err := migrator.columnState(ctx, migrator.DB, table, "checksum")
	if err != nil {
		return fmt.Errorf("inspect migrations table: %w", err)
	}
	if !hasChecksum || checksumNullable {
		return fmt.Errorf("migration ledger requires an operational upgrade")
	}
	applied, err := migrator.applied(ctx, migrator.DB)
	if err != nil {
		return err
	}
	if err := migrator.verifyApplied(ctx, migrator.DB, migrations, applied, false); err != nil {
		return err
	}
	for _, migration := range migrations {
		if _, exists := applied[migration.Version]; !exists {
			return fmt.Errorf("migration %s is pending", migration.Version)
		}
	}
	return nil
}

func (migrator *Migrator) validate() error {
	if migrator.DB == nil || migrator.Files == nil {
		return fmt.Errorf("migrator requires DB and Files")
	}
	if migrator.Dialect != Postgres && migrator.Dialect != MySQL && migrator.Dialect != SQLite {
		return fmt.Errorf("unsupported SQL dialect %q", migrator.Dialect)
	}
	return nil
}

func (migrator *Migrator) prepare(ctx context.Context, database runner, migrations []Migration) error {
	if err := migrator.validate(); err != nil {
		return err
	}
	table, err := migrator.table()
	if err != nil {
		return err
	}
	query := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (version VARCHAR(255) PRIMARY KEY, name VARCHAR(255) NOT NULL, checksum VARCHAR(64) NOT NULL, applied_at VARCHAR(35) NOT NULL)", table)
	if _, err := database.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("prepare migrations table: %w", err)
	}
	hasChecksum, checksumNullable, err := migrator.columnState(ctx, database, table, "checksum")
	if err != nil {
		return fmt.Errorf("inspect migrations table: %w", err)
	}
	if !hasChecksum || checksumNullable {
		return migrator.upgradeLegacyChecksums(ctx, database, migrations, hasChecksum)
	}
	if migrator.Dialect == MySQL {
		return migrator.removeMySQLUpgradeMarker(ctx, database)
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

func (migrator *Migrator) applied(ctx context.Context, database executor) (map[string]appliedMigration, error) {
	table, err := migrator.table()
	if err != nil {
		return nil, err
	}
	rows, err := database.QueryContext(ctx, fmt.Sprintf("SELECT version, name, checksum, applied_at FROM %s", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]appliedMigration)
	for rows.Next() {
		var version string
		var name string
		var storedChecksum sql.NullString
		var encodedTime string
		if err := rows.Scan(&version, &name, &storedChecksum, &encodedTime); err != nil {
			return nil, err
		}
		if migrator.Dialect == MySQL && version == mysqlUpgradeMarker {
			continue
		}
		appliedAt, err := time.Parse(time.RFC3339Nano, encodedTime)
		if err != nil {
			return nil, fmt.Errorf("parse applied time for migration %s: %w", version, err)
		}
		result[version] = appliedMigration{Name: name, Checksum: storedChecksum, AppliedAt: appliedAt}
	}
	return result, rows.Err()
}

// verifyApplied rejects filesystem drift before any migration operation. Rows
// created by versions before checksum support are upgraded after their recorded
// name is verified. Their checksum necessarily uses trust-on-first-use because
// the original SQL was not retained by the old schema.
func (migrator *Migrator) verifyApplied(ctx context.Context, database executor, migrations []Migration, applied map[string]appliedMigration, allowChecksumBackfill bool) error {
	byVersion := make(map[string]Migration, len(migrations))
	for _, migration := range migrations {
		byVersion[migration.Version] = migration
	}
	table, err := migrator.table()
	if err != nil {
		return err
	}
	for version, recorded := range applied {
		migration, exists := byVersion[version]
		if !exists {
			return fmt.Errorf("applied migration %s is missing from the migration filesystem", version)
		}
		if recorded.Name != migration.Name {
			return fmt.Errorf("applied migration %s name mismatch: database has %q, filesystem has %q", version, recorded.Name, migration.Name)
		}
		expected := checksum(migration)
		if recorded.Checksum.Valid {
			if recorded.Checksum.String != expected {
				return fmt.Errorf("applied migration %s checksum mismatch", version)
			}
			continue
		}
		if !allowChecksumBackfill {
			return fmt.Errorf("applied migration %s has no checksum; refusing to rebaseline", version)
		}
		query := fmt.Sprintf("UPDATE %s SET checksum = %s WHERE version = %s AND checksum IS NULL", table, migrator.bind(1), migrator.bind(2))
		result, err := database.ExecContext(ctx, query, expected, version)
		if err != nil {
			return fmt.Errorf("backfill checksum for migration %s: %w", version, err)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("confirm checksum backfill for migration %s: %w", version, err)
		}
		if rowsAffected != 1 {
			return fmt.Errorf("backfill checksum for migration %s affected %d rows", version, rowsAffected)
		}
		recorded.Checksum = sql.NullString{String: expected, Valid: true}
		applied[version] = recorded
	}
	return nil
}

func checksum(migration Migration) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(migration.Up)))
}

func (migrator *Migrator) upgradeLegacyChecksums(ctx context.Context, database runner, migrations []Migration, hasChecksum bool) error {
	if migrator.Dialect == MySQL {
		return migrator.upgradeMySQLChecksums(ctx, database, migrations, hasChecksum)
	}
	return inTransaction(ctx, database, func(tx *sql.Tx) error {
		if !hasChecksum {
			if err := migrator.addNullableChecksum(ctx, tx); err != nil {
				return err
			}
		}
		applied, err := migrator.applied(ctx, tx)
		if err != nil {
			return err
		}
		if err := migrator.verifyApplied(ctx, tx, migrations, applied, true); err != nil {
			return err
		}
		return migrator.enforceChecksumNotNull(ctx, tx)
	})
}

func (migrator *Migrator) addNullableChecksum(ctx context.Context, database executor) error {
	table, err := migrator.table()
	if err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN checksum VARCHAR(64) NULL", table)); err != nil {
		return fmt.Errorf("upgrade migrations table: %w", err)
	}
	return nil
}

func (migrator *Migrator) upgradeMySQLChecksums(ctx context.Context, database runner, migrations []Migration, hasChecksum bool) error {
	table, err := migrator.table()
	if err != nil {
		return err
	}
	markerExists, err := migrator.mysqlUpgradeMarkerExists(ctx, database)
	if err != nil {
		return err
	}
	if !markerExists {
		if hasChecksum {
			query := fmt.Sprintf("INSERT INTO %s (version, name, checksum, applied_at) VALUES (?, ?, NULL, ?)", table)
			_, err = database.ExecContext(ctx, query, mysqlUpgradeMarker, mysqlUpgradeMarker, time.Now().UTC().Format(time.RFC3339Nano))
		} else {
			query := fmt.Sprintf("INSERT INTO %s (version, name, applied_at) VALUES (?, ?, ?)", table)
			_, err = database.ExecContext(ctx, query, mysqlUpgradeMarker, mysqlUpgradeMarker, time.Now().UTC().Format(time.RFC3339Nano))
		}
		if err != nil {
			return fmt.Errorf("record MySQL checksum upgrade: %w", err)
		}
	}
	if !hasChecksum {
		if err := migrator.addNullableChecksum(ctx, database); err != nil {
			return err
		}
	}
	applied, err := migrator.applied(ctx, database)
	if err != nil {
		return err
	}
	if err := migrator.verifyApplied(ctx, database, migrations, applied, true); err != nil {
		return err
	}
	markerChecksum := fmt.Sprintf("%x", sha256.Sum256([]byte(mysqlUpgradeMarker)))
	query := fmt.Sprintf("UPDATE %s SET checksum = ? WHERE version = ?", table)
	if _, err := database.ExecContext(ctx, query, markerChecksum, mysqlUpgradeMarker); err != nil {
		return fmt.Errorf("advance MySQL checksum upgrade: %w", err)
	}
	if err := migrator.enforceChecksumNotNull(ctx, database); err != nil {
		return err
	}
	return migrator.removeMySQLUpgradeMarker(ctx, database)
}

func (migrator *Migrator) mysqlUpgradeMarkerExists(ctx context.Context, database executor) (bool, error) {
	table, err := migrator.table()
	if err != nil {
		return false, err
	}
	rows, err := database.QueryContext(ctx, fmt.Sprintf("SELECT version FROM %s WHERE version = ?", table), mysqlUpgradeMarker)
	if err != nil {
		return false, fmt.Errorf("inspect MySQL checksum upgrade: %w", err)
	}
	defer rows.Close()
	exists := rows.Next()
	if err := rows.Err(); err != nil {
		return false, err
	}
	return exists, nil
}

func (migrator *Migrator) removeMySQLUpgradeMarker(ctx context.Context, database executor) error {
	table, err := migrator.table()
	if err != nil {
		return err
	}
	if _, err := database.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE version = ?", table), mysqlUpgradeMarker); err != nil {
		return fmt.Errorf("finish MySQL checksum upgrade: %w", err)
	}
	return nil
}

func (migrator *Migrator) enforceChecksumNotNull(ctx context.Context, database executor) error {
	table, err := migrator.table()
	if err != nil {
		return err
	}
	var query string
	switch migrator.Dialect {
	case Postgres:
		query = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN checksum SET NOT NULL", table)
	case MySQL:
		query = fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN checksum VARCHAR(64) NOT NULL", table)
	case SQLite:
		temporaryTable := table + "_goforge_checksum_upgrade"
		queries := []string{
			fmt.Sprintf("CREATE TABLE %s (version VARCHAR(255) PRIMARY KEY, name VARCHAR(255) NOT NULL, checksum VARCHAR(64) NOT NULL, applied_at VARCHAR(35) NOT NULL)", temporaryTable),
			fmt.Sprintf("INSERT INTO %s (version, name, checksum, applied_at) SELECT version, name, checksum, applied_at FROM %s", temporaryTable, table),
			fmt.Sprintf("DROP TABLE %s", table),
			fmt.Sprintf("ALTER TABLE %s RENAME TO %s", temporaryTable, table),
		}
		for _, query := range queries {
			if _, err := database.ExecContext(ctx, query); err != nil {
				return fmt.Errorf("enforce migration checksum constraint: %w", err)
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported SQL dialect %q", migrator.Dialect)
	}
	if _, err := database.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("enforce migration checksum constraint: %w", err)
	}
	return nil
}

func (migrator *Migrator) columnState(ctx context.Context, database executor, table, column string) (bool, bool, error) {
	if migrator.Dialect == SQLite {
		rows, err := database.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
		if err != nil {
			return false, false, err
		}
		defer rows.Close()
		for rows.Next() {
			var position, notNull, primaryKey int
			var name, dataType string
			var defaultValue any
			if err := rows.Scan(&position, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
				return false, false, err
			}
			if name == column {
				return true, notNull == 0, nil
			}
		}
		return false, false, rows.Err()
	}

	binding := migrator.bind(1)
	query := "SELECT is_nullable FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = " + binding + " AND column_name = " + migrator.bind(2)
	if migrator.Dialect == Postgres {
		query = "SELECT is_nullable FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = " + binding + " AND column_name = " + migrator.bind(2)
		// Unquoted PostgreSQL identifiers are folded to lower case.
		table = strings.ToLower(table)
		column = strings.ToLower(column)
	}
	rows, err := database.QueryContext(ctx, query, table, column)
	if err != nil {
		return false, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, false, err
		}
		return false, false, nil
	}
	var nullable string
	if err := rows.Scan(&nullable); err != nil {
		return false, false, err
	}
	if err := rows.Err(); err != nil {
		return false, false, err
	}
	return true, strings.EqualFold(nullable, "YES"), nil
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
