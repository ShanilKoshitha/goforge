package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ShanilKoshitha/goforge/orm"
	"github.com/ShanilKoshitha/goforge/orm/postgres"

	"example.com/format6/internal/models"
)

var (
	ErrEmailTaken   = errors.New("email already registered")
	ErrUserNotFound = errors.New("user not found")
)

type UserRepository interface {
	Create(ctx context.Context, name, email, passwordHash string) (User, error)
	ByEmail(ctx context.Context, email string) (User, error)
	ByID(ctx context.Context, id int64) (User, error)
}

// PostgresUserRepository keeps the executor visible so callers may bind the
// repository to either *sql.DB or *sql.Tx without a second implementation.
type PostgresUserRepository struct{ Executor orm.Executor }

func NewPostgresUserRepository(executor orm.Executor) *PostgresUserRepository {
	return &PostgresUserRepository{Executor: executor}
}

func (repository *PostgresUserRepository) Create(ctx context.Context, name, email, passwordHash string) (User, error) {
	query := models.NewStore(repository.Executor).Users()
	user, err := query.Create(models.UserCreateInput{
		Name: strings.TrimSpace(name), Email: normalizeEmail(email),
	}, models.UserColumns.PasswordHash.Set(passwordHash)).One(ctx, repository.Executor)
	err = postgres.Classify(err)
	if errors.Is(err, orm.ErrUnique) {
		return User{}, ErrEmailTaken
	}
	return userResult(user, err)
}

func (repository *PostgresUserRepository) ByEmail(ctx context.Context, email string) (User, error) {
	query := models.NewStore(repository.Executor).Users()
	user, err := query.Select().Where(models.UserColumns.Email.Eq(normalizeEmail(email))).First(ctx, repository.Executor)
	return userResult(user, err)
}

func (repository *PostgresUserRepository) ByID(ctx context.Context, id int64) (User, error) {
	query := models.NewStore(repository.Executor).Users()
	user, err := query.Select().Where(models.UserColumns.ID.Eq(id)).First(ctx, repository.Executor)
	return userResult(user, err)
}

func userResult(user models.User, err error) (User, error) {
	err = postgres.Classify(err)
	if errors.Is(err, orm.ErrNotFound) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("query user: %w", err)
	}
	return user, nil
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }
