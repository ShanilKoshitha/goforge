package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ShanilKoshitha/goforge/httpx"
	"github.com/ShanilKoshitha/goforge/security/password"
	"github.com/ShanilKoshitha/goforge/security/ratelimit"
	"github.com/ShanilKoshitha/goforge/session"
)

type Controller struct {
	users     UserRepository
	sessions  *session.Manager
	passwords password.Hasher
	attempts  *ratelimit.Limiter
}

var ErrInvalidCredentials = errors.New("invalid credentials")

func NewController(users UserRepository, sessions *session.Manager, passwords password.Hasher, attempts *ratelimit.Limiter) *Controller {
	return &Controller{users: users, sessions: sessions, passwords: passwords, attempts: attempts}
}

func (controller *Controller) Register(ctx *httpx.Context) error {
	var request RegisterRequest
	if err := request.DecodeJSON(ctx); err != nil {
		return err
	}
	if problems := request.Validate(); !problems.Empty() {
		return httpx.NewHTTPError(http.StatusUnprocessableEntity, "validation failed").WithDetails(problems)
	}
	user, err := controller.createUserForRequest(ctx, request)
	if errors.Is(err, ErrEmailTaken) {
		return httpx.NewHTTPError(http.StatusConflict, ErrEmailTaken.Error())
	}
	if err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	if err := controller.startSession(ctx, user.ID); err != nil {
		return err
	}
	return ctx.JSON(http.StatusCreated, map[string]any{"data": user})
}

func (controller *Controller) Login(ctx *httpx.Context) error {
	var request LoginRequest
	if err := request.DecodeJSON(ctx); err != nil {
		return err
	}
	if problems := request.Validate(); !problems.Empty() {
		return httpx.NewHTTPError(http.StatusUnprocessableEntity, "validation failed").WithDetails(problems)
	}
	user, err := controller.authenticateRequest(ctx, request)
	if errors.Is(err, ErrInvalidCredentials) {
		return httpx.NewHTTPError(http.StatusUnauthorized, ErrInvalidCredentials.Error())
	}
	if err != nil {
		return err
	}
	if err := controller.startSession(ctx, user.ID); err != nil {
		return err
	}
	return ctx.JSON(http.StatusOK, map[string]any{"data": user})
}

func (controller *Controller) createUser(ctx context.Context, request RegisterRequest) (User, error) {
	encoded, err := controller.passwords.Hash(request.Password)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}
	return controller.users.Create(ctx, request.Name, request.Email, encoded)
}

func (controller *Controller) createUserForRequest(ctx *httpx.Context, request RegisterRequest) (User, error) {
	key := ratelimit.Key("auth-account-register", normalizeEmail(request.Email))
	if err := controller.attempts.Enforce(ctx, key); err != nil {
		return User{}, err
	}
	user, err := controller.createUser(ctx.Request.Context(), request)
	if err != nil {
		return User{}, err
	}
	if err := controller.attempts.Reset(ctx.Request.Context(), key); err != nil {
		return User{}, err
	}
	return user, nil
}

func (controller *Controller) authenticate(ctx context.Context, request LoginRequest) (User, error) {
	user, err := controller.users.ByEmail(ctx, request.Email)
	if errors.Is(err, ErrUserNotFound) {
		controller.passwords.DummyVerify(request.Password)
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	matched, err := controller.passwords.Verify(user.PasswordHash, request.Password)
	if err != nil {
		return User{}, err
	}
	if !matched {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

func (controller *Controller) authenticateRequest(ctx *httpx.Context, request LoginRequest) (User, error) {
	key := ratelimit.Key("auth-account-login", normalizeEmail(request.Email))
	if err := controller.attempts.Enforce(ctx, key); err != nil {
		return User{}, err
	}
	user, err := controller.authenticate(ctx.Request.Context(), request)
	if err != nil {
		return User{}, err
	}
	if err := controller.attempts.Reset(ctx.Request.Context(), key); err != nil {
		return User{}, err
	}
	return user, nil
}

func (controller *Controller) Logout(ctx *httpx.Context) error {
	// Requiring a JSON value prevents ambient cookie credentials from making
	// this API endpoint form-submittable. Browser logout has its own CSRF token.
	var request struct{}
	if err := ctx.BindJSON(&request); err != nil {
		return err
	}
	current, err := controller.sessions.Load(ctx.Request.Context(), ctx.Request)
	if err != nil {
		return err
	}
	if err := controller.sessions.Destroy(ctx.Request.Context(), ctx.Response, current); err != nil {
		return err
	}
	return ctx.NoContent(http.StatusNoContent)
}

func (controller *Controller) Me(ctx *httpx.Context) error {
	user, ok := UserFrom(ctx)
	if !ok {
		return httpx.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	return ctx.JSON(http.StatusOK, map[string]any{"data": user})
}

func (controller *Controller) startSession(ctx *httpx.Context, userID int64) error {
	current, err := controller.sessions.Load(ctx.Request.Context(), ctx.Request)
	if err != nil {
		return err
	}
	if err := controller.sessions.Regenerate(ctx.Request.Context(), current); err != nil {
		return err
	}
	if err := current.Put("user_id", userID); err != nil {
		return err
	}
	return controller.sessions.Save(ctx.Request.Context(), ctx.Response, current)
}
