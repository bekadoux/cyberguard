package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/demoseed"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger, os.Args[1:]); err != nil {
		logger.Error("demo seed failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, args []string) error {
	flags := flag.NewFlagSet("seed", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	demo := flags.Bool("demo", false, "explicitly allow creating development accounts")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || !*demo {
		return errors.New("demo setup requires an explicit -demo flag; never run against production")
	}
	accounts, err := demoseed.LoadAccounts()
	if err != nil {
		return err
	}
	databaseURL, err := config.LoadIdentityMigrationURL()
	if err != nil {
		return err
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("invalid identity seed database configuration")
	}
	poolConfig.MaxConns, poolConfig.MinConns, poolConfig.MinIdleConns = 1, 0, 0
	poolConfig.ConnConfig.ConnectTimeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return errors.New("could not initialize identity seed pool")
	}
	defer pool.Close()
	var migrationRole bool
	if err := pool.QueryRow(ctx, "SELECT current_user = 'identity_migrator'").Scan(&migrationRole); err != nil || !migrationRole {
		return errors.New("demo setup requires an available identity_migrator connection")
	}
	result, err := demoseed.Seed(ctx, pool, accounts)
	if err != nil {
		return err
	}
	logger.Info("demo accounts prepared", "created", result.Created, "existing", result.Existing)
	return nil
}
