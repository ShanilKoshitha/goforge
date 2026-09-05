package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"example.com/format6/internal/application"
	"example.com/format6/internal/auth"
	"example.com/format6/internal/config"
	appdatabase "example.com/format6/internal/database"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	settings, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := appdatabase.Open(ctx, settings.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	app, err := application.New(settings, application.Dependencies{
		DB: db, Sessions: auth.NewPostgresSessionStore(db),
	})
	if err != nil {
		return fmt.Errorf("build application: %w", err)
	}
	return app.Run(ctx)
}
