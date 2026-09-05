package issue

import (
	"context"
	"errors"
	"fmt"

	"github.com/ShanilKoshitha/goforge/orm"
	"github.com/ShanilKoshitha/goforge/orm/postgres"

	"example.com/format6/internal/models"
)

var (
	ErrNotFound = errors.New("issue not found")
	ErrStale    = orm.ErrStale
)

// Repository remains the application-facing escape hatch. Alternative SQL,
// service, cache, or test implementations need not depend on the ORM.
type Repository interface {
	List(ctx context.Context, userID int64) ([]Issue, error)
	Paginate(ctx context.Context, userID int64, page, perPage int) (IssuePage, error)
	Create(ctx context.Context, userID int64, name string) (Issue, error)
	Find(ctx context.Context, userID, id int64) (Issue, error)
	Update(ctx context.Context, userID, id int64, name string, expectedVersion *int64) (Issue, error)
	Delete(ctx context.Context, userID, id int64) error
}

const (
	defaultPageNumber = 1
	defaultPageSize   = 20
	maximumPageNumber = 10_000
	maximumPageSize   = 100
)

// IssuePage is an explicit, bounded page of owner-scoped records.
type IssuePage struct {
	Items       []Issue
	Number      int
	PerPage     int
	HasPrevious bool
	HasNext     bool
}

// PostgresRepository accepts either *sql.DB or *sql.Tx through orm.Executor.
// Its typed queries remain directly inspectable through internal/models.
type PostgresRepository struct{ Executor orm.Executor }

func NewPostgresRepository(executor orm.Executor) *PostgresRepository {
	return &PostgresRepository{Executor: executor}
}

func (repository *PostgresRepository) query() models.IssueQuery {
	return models.NewStore(repository.Executor).Issues()
}

func (repository *PostgresRepository) List(ctx context.Context, userID int64) ([]Issue, error) {
	// Preserve the JSON List contract while keeping every database round-trip
	// bounded. Keyset batches avoid an ever-growing OFFSET and cannot cross an
	// owner boundary.
	items := make([]Issue, 0)
	var beforeID int64
	for {
		predicates := []orm.Predicate[models.Issue]{models.IssueColumns.UserID.Eq(userID)}
		if beforeID != 0 {
			predicates = append(predicates, models.IssueColumns.ID.Lt(beforeID))
		}
		batch, err := repository.query().Select().
			Where(predicates...).
			OrderBy(models.IssueColumns.ID.Desc()).
			Limit(maximumPageSize).
			All(ctx, repository.Executor)
		if err != nil {
			return nil, fmt.Errorf("list issues: %w", postgres.Classify(err))
		}
		items = append(items, batch...)
		if len(batch) < maximumPageSize {
			return items, nil
		}
		nextID := batch[len(batch)-1].ID
		if nextID < 1 || beforeID != 0 && nextID >= beforeID {
			return nil, fmt.Errorf("list issues: database returned a non-descending primary key page")
		}
		beforeID = nextID
	}
}

func (repository *PostgresRepository) Paginate(ctx context.Context, userID int64, page, perPage int) (IssuePage, error) {
	page, perPage = normalizePagination(page, perPage)
	items, err := repository.query().Select().
		Where(models.IssueColumns.UserID.Eq(userID)).
		OrderBy(models.IssueColumns.ID.Desc()).
		Limit(perPage+1).
		Offset((page-1)*perPage).
		All(ctx, repository.Executor)
	if err != nil {
		return IssuePage{}, fmt.Errorf("paginate issues: %w", postgres.Classify(err))
	}
	hasNext := len(items) > perPage
	if hasNext {
		items = items[:perPage]
	}
	return IssuePage{
		Items: items, Number: page, PerPage: perPage,
		HasPrevious: page > 1, HasNext: hasNext,
	}, nil
}

func normalizePagination(page, perPage int) (int, int) {
	if page < 1 {
		page = defaultPageNumber
	} else if page > maximumPageNumber {
		page = maximumPageNumber
	}
	if perPage < 1 {
		perPage = defaultPageSize
	} else if perPage > maximumPageSize {
		perPage = maximumPageSize
	}
	return page, perPage
}

func (repository *PostgresRepository) Create(ctx context.Context, userID int64, name string) (Issue, error) {
	item, err := repository.query().Create(
		models.IssueCreateInput{Name: name},
		models.IssueColumns.UserID.Set(userID),
	).One(ctx, repository.Executor)
	if err != nil {
		return Issue{}, fmt.Errorf("create issue: %w", postgres.Classify(err))
	}
	return item, nil
}

func (repository *PostgresRepository) Find(ctx context.Context, userID, id int64) (Issue, error) {
	item, err := repository.query().Select().Where(
		models.IssueColumns.ID.Eq(id),
		models.IssueColumns.UserID.Eq(userID),
	).First(ctx, repository.Executor)
	return repositoryResult(item, err)
}

func (repository *PostgresRepository) Update(ctx context.Context, userID, id int64, name string, expectedVersion *int64) (Issue, error) {
	if expectedVersion != nil && *expectedVersion < 1 {
		return Issue{}, fmt.Errorf("update issue: expected version must be at least 1")
	}
	changes := models.IssueChanges{Name: orm.Value(name)}
	updatedAt := orm.SetCurrentTime(models.IssueColumns.UpdatedAt)
	var update orm.UpdateBuilder[models.Issue]
	if expectedVersion == nil {
		update = repository.query().Update(changes, updatedAt, orm.Increment(models.IssueColumns.Version, int64(1)))
	} else {
		update = repository.query().Update(changes, updatedAt).
			OptimisticVersion(orm.ExpectVersion(models.IssueColumns.Version, *expectedVersion))
	}
	item, err := update.Where(
		models.IssueColumns.ID.Eq(id),
		models.IssueColumns.UserID.Eq(userID),
	).One(ctx, repository.Executor)
	err = postgres.Classify(err)
	if expectedVersion != nil && errors.Is(err, orm.ErrNotFound) {
		exists, existsErr := repository.query().Select().Where(
			models.IssueColumns.ID.Eq(id),
			models.IssueColumns.UserID.Eq(userID),
		).Exists(ctx, repository.Executor)
		if existsErr != nil {
			return Issue{}, fmt.Errorf("check issue after guarded update: %w", postgres.Classify(existsErr))
		}
		if exists {
			return Issue{}, ErrStale
		}
	}
	return repositoryResult(item, err)
}

func (repository *PostgresRepository) Delete(ctx context.Context, userID, id int64) error {
	rows, err := repository.query().Delete().Where(
		models.IssueColumns.ID.Eq(id),
		models.IssueColumns.UserID.Eq(userID),
	).Exec(ctx, repository.Executor)
	if err != nil {
		return fmt.Errorf("delete issue: %w", postgres.Classify(err))
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func repositoryResult(item models.Issue, err error) (Issue, error) {
	err = postgres.Classify(err)
	if errors.Is(err, orm.ErrNotFound) {
		return Issue{}, ErrNotFound
	}
	if err != nil {
		return Issue{}, fmt.Errorf("query issue: %w", err)
	}
	return item, nil
}
