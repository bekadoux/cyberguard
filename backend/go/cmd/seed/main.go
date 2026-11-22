package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/accountowner"
	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/demoseed"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger, os.Args[1:], os.Stdin); err != nil {
		logger.Error("account provisioning failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, args []string, stdin io.Reader) error {
	flags := flag.NewFlagSet("seed", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	demo := flags.Bool("demo", false, "explicitly allow creating development accounts")
	owner := flags.String("owner", "", "initialize ownership for this username")
	createOwner := flags.Bool("create-owner", false, "create a new owner; read the initial password from stdin")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *demo == (*owner != "") || (*createOwner && *owner == "") {
		return errors.New("choose -demo (development only) or -owner USERNAME [-create-owner]")
	}
	var accounts []demoseed.Account
	var hash string
	var err error
	switch {
	case *demo:
		accounts, err = demoseed.LoadAccounts()
	case *createOwner:
		hash, err = readOwnerPassword(stdin)
	}
	if err != nil {
		return err
	}
	databaseURL, err := config.LoadIdentityMigrationURL()
	if err != nil {
		return err
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return errors.New("invalid identity provisioning database configuration")
	}
	poolConfig.MaxConns, poolConfig.MinConns, poolConfig.MinIdleConns = 1, 0, 0
	poolConfig.ConnConfig.ConnectTimeout = 3 * time.Second
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return errors.New("could not initialize identity provisioning pool")
	}
	defer pool.Close()
	var migrationRole bool
	if err := pool.QueryRow(ctx, "SELECT current_user = 'identity_migrator'").Scan(&migrationRole); err != nil || !migrationRole {
		return errors.New("account provisioning requires an available identity_migrator connection")
	}
	if *owner != "" {
		if err := accountowner.Initialize(ctx, pool, *owner, hash); err != nil {
			if errors.Is(err, accountowner.ErrInitialized) || errors.Is(err, accountowner.ErrIneligible) || errors.Is(err, accountowner.ErrUsername) {
				return err
			}
			return errors.New("could not initialize ownership; check identity migrations and connectivity")
		}
		logger.Info("initial application owner configured")
		return nil
	}
	result, err := demoseed.Seed(ctx, pool, accounts)
	if err != nil {
		return err
	}
	logger.Info("demo accounts prepared", "created", result.Created, "existing", result.Existing)
	return nil
}

func readOwnerPassword(stdin io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(stdin, 131))
	if err != nil || len(data) > 130 {
		return "", errors.New("provide a 12 to 128 byte owner password on stdin")
	}
	password := string(data)
	if strings.HasSuffix(password, "\n") {
		password = strings.TrimSuffix(strings.TrimSuffix(password, "\n"), "\r")
	}
	hash, err := auth.HashNewPassword(password)
	if err != nil {
		return "", errors.New("provide a valid nonblank 12 to 128 byte owner password on stdin")
	}
	return hash, nil
}
