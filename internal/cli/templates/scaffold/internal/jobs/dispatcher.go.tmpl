package jobs

import (
	"database/sql"
	"fmt"

	"github.com/ShanilKoshitha/goforge/job"
	jobpostgres "github.com/ShanilKoshitha/goforge/job/postgres"
)

// NewDispatcher builds the application's PostgreSQL dispatcher. Call
// dispatcher.Using(tx) to enqueue in the same transaction as a domain write.
func NewDispatcher(db *sql.DB, observer job.Observer) (job.Dispatcher, error) {
	store, err := jobpostgres.New(db)
	if err != nil {
		return job.Dispatcher{}, fmt.Errorf("build job store: %w", err)
	}
	return job.NewDispatcher(store, db, job.DispatcherConfig{Observer: observer})
}
