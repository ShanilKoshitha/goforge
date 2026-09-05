// Package forge provides the small application kernel used by GoForge projects.
// Everything below it is ordinary net/http, database/sql, and explicit Go code.
package forge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ShanilKoshitha/goforge/httpx"
)

type Config struct {
	Address           string
	ShutdownTimeout   time.Duration
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
	Logger            *slog.Logger
}

type App struct {
	Router    *httpx.Router
	config    Config
	newServer func(Config, http.Handler) lifecycleServer
}

type lifecycleServer interface {
	ListenAndServe() error
	Shutdown(context.Context) error
	Close() error
}

func New(config Config) *App {
	return newApp(normalizeConfig(config))
}

// NewChecked validates explicit lifecycle limits before applying defaults.
// Generated applications use it so invalid environment values fail before the
// server starts. New remains source-compatible for pre-v0.7 callers.
func NewChecked(config Config) (*App, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	return newApp(normalizeConfig(config)), nil
}

func newApp(config Config) *App {
	return &App{
		Router:    httpx.NewRouter(),
		config:    config,
		newServer: newHTTPServer,
	}
}

func normalizeConfig(config Config) Config {
	config.Address = strings.TrimSpace(config.Address)
	if config.Address == "" {
		config.Address = ":8080"
	}
	if config.ShutdownTimeout <= 0 {
		config.ShutdownTimeout = 10 * time.Second
	}
	if config.ReadHeaderTimeout <= 0 {
		config.ReadHeaderTimeout = 5 * time.Second
	}
	if config.ReadTimeout <= 0 {
		config.ReadTimeout = 30 * time.Second
	}
	if config.WriteTimeout <= 0 {
		config.WriteTimeout = 30 * time.Second
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 2 * time.Minute
	}
	if config.MaxHeaderBytes <= 0 {
		config.MaxHeaderBytes = 1 << 20
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	return config
}

func validateConfig(config Config) error {
	limits := []struct {
		name  string
		value time.Duration
	}{
		{"shutdown timeout", config.ShutdownTimeout},
		{"read header timeout", config.ReadHeaderTimeout},
		{"read timeout", config.ReadTimeout},
		{"write timeout", config.WriteTimeout},
		{"idle timeout", config.IdleTimeout},
	}
	for _, limit := range limits {
		if limit.value < 0 {
			return fmt.Errorf("forge: %s cannot be negative", limit.name)
		}
	}
	if config.MaxHeaderBytes < 0 {
		return fmt.Errorf("forge: maximum header bytes cannot be negative")
	}
	return nil
}

func newHTTPServer(config Config, handler http.Handler) lifecycleServer {
	return &http.Server{
		Addr:              config.Address,
		Handler:           handler,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		ReadTimeout:       config.ReadTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
		MaxHeaderBytes:    config.MaxHeaderBytes,
	}
}

func (app *App) Handler() http.Handler {
	return app.Router
}

// Run serves until the context is cancelled. Applications that need different
// server behavior can ignore Run and pass app.Handler() to their own http.Server.
func (app *App) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("run: nil context")
	}
	server := app.newServer(app.config, app.Handler())

	result := make(chan error, 1)
	go func() {
		app.config.Logger.Info("server starting", "address", app.config.Address)
		result <- server.ListenAndServe()
	}()

	select {
	case err := <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), app.config.ShutdownTimeout)
		defer cancel()
		shutdownErr := server.Shutdown(shutdownContext)
		var closeErr error
		if shutdownErr != nil {
			// Shutdown can time out while handlers or connections remain active.
			// Close ensures Run never leaves the listener and serve goroutine behind.
			closeErr = server.Close()
		}
		serveErr := <-result

		var runErrors []error
		if shutdownErr != nil {
			runErrors = append(runErrors, fmt.Errorf("graceful shutdown: %w", shutdownErr))
		}
		if closeErr != nil {
			runErrors = append(runErrors, fmt.Errorf("force close: %w", closeErr))
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			runErrors = append(runErrors, fmt.Errorf("serve: %w", serveErr))
		}
		return errors.Join(runErrors...)
	}
}
