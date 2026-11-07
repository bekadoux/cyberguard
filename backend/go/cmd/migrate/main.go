package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/migrations"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger, os.Args[1:]); err != nil {
		logger.Error("identity migration command failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	directory := flags.String("dir", "", "identity SQL migration directory")
	status := flags.Bool("status", false, "report migration versions without applying migrations")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *directory == "" {
		return errors.New("usage: migrate -dir PATH [-status]")
	}
	databaseURL, err := config.LoadIdentityMigrationURL()
	if err != nil {
		return err
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return errors.New("invalid identity migration database configuration")
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return errors.New("identity migration database is unavailable")
	}
	// Refuse accidental use of an administrator or runtime connection string.
	var migrationRole bool
	if err := db.QueryRowContext(ctx, "SELECT current_user = 'identity_migrator'").Scan(&migrationRole); err != nil || !migrationRole {
		return errors.New("identity migrations require the identity_migrator database role")
	}
	provider, err := migrations.NewProvider(db, os.DirFS(*directory))
	if err != nil {
		return errors.New("could not load identity migrations from the supplied directory")
	}
	if *status {
		current, target, err := provider.GetVersions(ctx)
		if err != nil {
			return errors.New("could not read identity migration versions")
		}
		logger.Info("identity migration versions", "current", current, "target", target)
		return nil
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return errors.New("could not apply identity migrations; check schema, grants and migration lock availability")
	}
	logger.Info("identity migrations applied", "count", len(results))
	return nil
}
