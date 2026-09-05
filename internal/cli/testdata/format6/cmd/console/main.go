package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/ShanilKoshitha/goforge/database/migrate"
	"github.com/ShanilKoshitha/goforge/job"
	jobpostgres "github.com/ShanilKoshitha/goforge/job/postgres"

	"example.com/format6/database/migrations"
	"example.com/format6/internal/config"
	appdatabase "example.com/format6/internal/database"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: go run ./cmd/console <migrate|queue:failed|queue:retry|queue:forget>")
	}
	settings, err := config.LoadDatabase()
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
	switch args[0] {
	case "migrate":
		if len(args) != 1 {
			return errors.New("usage: go run ./cmd/console migrate")
		}
		runner := migrate.Migrator{DB: db, Files: migrations.Files, Dialect: migrate.Postgres}
		applied, err := runner.Up(ctx, 0)
		if err != nil {
			return err
		}
		if len(applied) == 0 {
			fmt.Println("No pending migrations.")
			return nil
		}
		for _, migration := range applied {
			fmt.Printf("migrated %s_%s\n", migration.Version, migration.Name)
		}
		return nil
	case "queue:failed", "queue:retry", "queue:forget":
		store, err := jobpostgres.New(db)
		if err != nil {
			return fmt.Errorf("build job store: %w", err)
		}
		return runQueueCommand(ctx, store, args)
	default:
		return fmt.Errorf("unknown console command %q", args[0])
	}
}

func runQueueCommand(ctx context.Context, store job.AdminStore, args []string) error {
	switch args[0] {
	case "queue:failed":
		if len(args) != 1 {
			return errors.New("usage: go run ./cmd/console queue:failed")
		}
		failed, err := store.ListFailed(ctx, 100)
		if err != nil {
			return err
		}
		if len(failed) == 0 {
			fmt.Println("No failed jobs.")
			return nil
		}
		for _, item := range failed {
			fmt.Printf("%s\t%s\t%s\tattempts=%d/%d\t%s\tmessage=%s\n", item.ID, item.Queue, item.Name, item.Attempts, item.MaxAttempts, item.FailedAt.Format("2006-01-02T15:04:05Z07:00"), strconv.QuoteToASCII(item.FailureMessage))
		}
		return nil
	case "queue:retry":
		if len(args) != 2 {
			return errors.New("usage: go run ./cmd/console queue:retry <id|--all>")
		}
		return changeFailed(ctx, store, args[0], args[1])
	case "queue:forget":
		if len(args) != 2 || args[1] == "--all" {
			return errors.New("usage: go run ./cmd/console queue:forget <id>")
		}
		return changeFailed(ctx, store, args[0], args[1])
	default:
		return fmt.Errorf("unknown queue command %q", args[0])
	}
}

func changeFailed(ctx context.Context, store job.AdminStore, operation, target string) error {
	if target != "--all" {
		changed, err := changeOne(ctx, store, operation, job.ID(target))
		if err != nil {
			return err
		}
		if !changed {
			return fmt.Errorf("failed job %q was not found", target)
		}
		fmt.Printf("%s %s\n", operation, target)
		return nil
	}
	changed := 0
	for {
		failed, err := store.ListFailed(ctx, 100)
		if err != nil {
			return err
		}
		if len(failed) == 0 {
			break
		}
		progress := 0
		for _, item := range failed {
			ok, err := changeOne(ctx, store, operation, item.ID)
			if err != nil {
				return err
			}
			if ok {
				changed++
				progress++
			}
		}
		if progress == 0 || len(failed) < 100 {
			break
		}
	}
	fmt.Printf("%s %s failed jobs\n", operation, strconv.Itoa(changed))
	return nil
}

func changeOne(ctx context.Context, store job.AdminStore, operation string, id job.ID) (bool, error) {
	if operation == "queue:retry" {
		return store.RetryFailed(ctx, id)
	}
	return store.ForgetFailed(ctx, id)
}
