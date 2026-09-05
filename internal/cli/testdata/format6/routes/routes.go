// Package routes is the complete, searchable route table.
package routes

import (
	"database/sql"
	"time"

	"github.com/ShanilKoshitha/goforge/httpx"
	"github.com/ShanilKoshitha/goforge/security/password"
	"github.com/ShanilKoshitha/goforge/security/ratelimit"
	"github.com/ShanilKoshitha/goforge/session"
	"github.com/ShanilKoshitha/goforge/view"
	"github.com/ShanilKoshitha/goforge/web"

	"example.com/format6/internal/auth"
	"example.com/format6/internal/config"
	"example.com/format6/internal/http/controllers"
)

func Register(router *httpx.Router, renderer *view.Engine, db *sql.DB, sessions session.Store, attemptStore ratelimit.Store, settings config.Config) error {
	manager, err := session.NewManager(sessions, []byte(settings.SessionSecret), session.Cookie{
		UnsafeAllowHTTP: settings.Environment == "local",
	}, 2*time.Hour)
	if err != nil {
		return err
	}
	trustedProxies, err := ratelimit.ParseTrustedProxies(settings.TrustedProxies)
	if err != nil {
		return err
	}
	sourceAttempts, err := ratelimit.New(attemptStore, ratelimit.Policy{Limit: 20, Window: time.Minute})
	if err != nil {
		return err
	}
	accountAttempts, err := ratelimit.New(attemptStore, ratelimit.Policy{Limit: 8, Window: 15 * time.Minute})
	if err != nil {
		return err
	}
	sourceKey := func(action string) ratelimit.KeyFunc {
		return func(ctx *httpx.Context) string {
			return ratelimit.Key("auth-source", action, trustedProxies.ClientAddress(ctx.Request))
		}
	}
	limitRegister := ratelimit.Middleware(sourceAttempts, sourceKey("register"))
	limitLogin := ratelimit.Middleware(sourceAttempts, sourceKey("login"))

	users := auth.NewPostgresUserRepository(db)
	authController := auth.NewController(users, manager, password.New(), accountAttempts)
	webAuthController := auth.NewWebController(authController, renderer)
	requireAuth := auth.Require(manager, users)
	requireWebAuth := auth.RequireWeb(users)

	health := controllers.NewHealthController()
	welcome := controllers.NewWelcomeController(renderer)
	router.Named("welcome", "GET", "/", welcome.Show)
	router.Named("health.show", "GET", "/health", health.Show)
	router.Named("auth.register", "POST", "/auth/register", limitRegister(authController.Register))
	router.Named("auth.login", "POST", "/auth/login", limitLogin(authController.Login))
	authenticated := router.Group("", requireAuth)
	authenticated.GET("/auth/me", authController.Me)
	authenticated.POST("/auth/logout", authController.Logout)
	browser := router.Group("", web.Sessions(manager), web.CSRF())
	browser.Named("web.auth.register.form", "GET", "/register", webAuthController.RegisterForm)
	browser.Named("web.auth.login.form", "GET", "/login", webAuthController.LoginForm)
	router.Named("web.auth.register", "POST", "/register",
		limitRegister(web.Sessions(manager)(web.CSRF()(webAuthController.Register))))
	router.Named("web.auth.login", "POST", "/login",
		limitLogin(web.Sessions(manager)(web.CSRF()(webAuthController.Login))))
	authenticatedBrowser := router.Group("", web.Sessions(manager), web.CSRF(), requireWebAuth)
	authenticatedBrowser.Named("web.dashboard", "GET", "/app", webAuthController.Dashboard)
	authenticatedBrowser.Named("web.auth.logout", "POST", "/logout", webAuthController.Logout)
	registerResources(router, renderer, db, manager, requireAuth, requireWebAuth)
	return nil
}
