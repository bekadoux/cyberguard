package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/database"
	"github.com/bekadoux/cyberguard/backend/go/internal/httpapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("API stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pools, err := database.NewPools(ctx, cfg.IdentityDatabaseURL, cfg.IncidentDatabaseURL, cfg.DatabaseTimeout)
	if err != nil {
		return err
	}
	defer pools.Close()
	router := httpapi.NewRouter(logger, pools.Check, cfg.DatabaseTimeout)
	server := httpapi.NewServer(cfg.HTTPAddr, router, logger)
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	logger.Info("API listening", "address", listener.Addr().String())
	if err := httpapi.Serve(ctx, server, listener, cfg.ShutdownTimeout); err != nil {
		return err
	}
	logger.Info("API stopped gracefully")
	return nil
}
