package auth

import (
	"errors"

	"github.com/ShanilKoshitha/goforge/httpx"
	"github.com/ShanilKoshitha/goforge/web"
)

func RequireWeb(users UserRepository) httpx.Middleware {
	return func(next httpx.Handler) httpx.Handler {
		return func(ctx *httpx.Context) error {
			current, ok := web.Session(ctx)
			if !ok {
				return errors.New("browser session is unavailable")
			}
			var userID int64
			present, err := current.Get("user_id", &userID)
			if err != nil {
				return err
			}
			if !present {
				return web.Redirect(ctx, "/login")
			}
			user, err := users.ByID(ctx.Request.Context(), userID)
			if errors.Is(err, ErrUserNotFound) {
				current.Forget("user_id")
				return web.Redirect(ctx, "/login")
			}
			if err != nil {
				return err
			}
			ctx.Set(userContextKey, user)
			return next(ctx)
		}
	}
}
