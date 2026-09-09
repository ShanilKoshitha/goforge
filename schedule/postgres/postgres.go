package postgres

import (
	"database/sql"
	"fmt"
	"regexp"

	"github.com/ShanilKoshitha/goforge/schedule"
)

var identifierPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Option configures validated PostgreSQL identifiers.
type Option func(*config) error

type config struct {
	table string
}

// WithTable selects an application-owned schedule table name.
func WithTable(table string) Option {
	return func(config *config) error {
		if !validIdentifier(table) {
			return fmt.Errorf("schedule/postgres: invalid table name %q", table)
		}
		config.table = table
		return nil
	}
}

// Store owns schedule operations but not the supplied database handle.
type Store struct {
	db    *sql.DB
	table string
}

var _ schedule.Store = (*Store)(nil)

// New creates a PostgreSQL schedule store. The caller retains database ownership.
func New(db *sql.DB, options ...Option) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("schedule/postgres: database is required")
	}
	config := config{table: "goforge_schedules"}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("schedule/postgres: nil option")
		}
		if err := option(&config); err != nil {
			return nil, err
		}
	}
	return &Store{db: db, table: quote(config.table)}, nil
}

func validIdentifier(value string) bool {
	return len(value) > 0 && len(value) <= 63 && identifierPattern.MatchString(value)
}

func quote(value string) string { return `"` + value + `"` }
