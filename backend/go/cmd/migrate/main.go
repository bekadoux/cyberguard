package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
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
		logger.Error("migration command failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("migrate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	directory := flags.String("dir", "", "SQL migration directory")
	database := flags.String("database", "identity", "identity or incidents database")
	status := flags.Bool("status", false, "report migration versions without applying migrations")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *directory == "" || (*database != "identity" && *database != "incidents") {
		return errors.New("usage: migrate -dir PATH [-database identity|incidents] [-status]")
	}
	loadURL, role := config.LoadIdentityMigrationURL, "identity_migrator"
	if *database == "incidents" {
		loadURL, role = config.LoadIncidentMigrationURL, "incidents_migrator"
	}
	databaseURL, err := loadURL()
	if err != nil {
		return err
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return errors.New("invalid migration database configuration")
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return errors.New("migration database is unavailable")
	}
	// Refuse accidental use of an administrator or runtime connection string.
	var migrationRole bool
	if err := db.QueryRowContext(ctx, "SELECT current_user = $1", role).Scan(&migrationRole); err != nil || !migrationRole {
		return fmt.Errorf("%s migrations require the %s database role", *database, role)
	}
	provider, err := migrations.NewProvider(db, os.DirFS(*directory))
	if err != nil {
		return errors.New("could not load migrations from the supplied directory")
	}
	if *status {
		current, target, err := provider.GetVersions(ctx)
		if err != nil {
			return errors.New("could not read migration versions")
		}
		logger.Info("migration versions", "database", *database, "current", current, "target", target)
		return nil
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return errors.New("could not apply migrations; check schema, grants and migration lock availability")
	}
	logger.Info("migrations applied", "database", *database, "count", len(results))
	return nil
}
