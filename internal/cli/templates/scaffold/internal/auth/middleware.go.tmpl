package auth

import (
	"errors"
	"net/http"

	"github.com/ShanilKoshitha/goforge/httpx"
	"github.com/ShanilKoshitha/goforge/session"
)

const userContextKey = "auth.user"

func Require(sessions *session.Manager, users UserRepository) httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx *httpx.Context) error {
			current, err := sessions.Load(ctx.Request.Context(), ctx.Request)
			if err != nil {
				return err
			}
			var userID int64
			present, err := current.Get("user_id", &userID)
			if err != nil {
				return err
			}
			if !present {
				return httpx.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}
			user, err := users.ByID(ctx.Request.Context(), userID)
			if errors.Is(err, ErrUserNotFound) {
				return httpx.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}
			if err != nil {
				return err
			}
			ctx.Set(userContextKey, user)
			return next(ctx)
		}
	}
}

func UserFrom(ctx *httpx.Context) (User, bool) {
	value, ok := ctx.Get(userContextKey)
	if !ok {
		return User{}, false
	}
	user, ok := value.(User)
	return user, ok
}
