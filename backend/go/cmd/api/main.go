package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/config"
	"github.com/bekadoux/cyberguard/backend/go/internal/database"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	incidentQueries "github.com/bekadoux/cyberguard/backend/go/internal/db/incidents"
	"github.com/bekadoux/cyberguard/backend/go/internal/httpapi"
	"github.com/bekadoux/cyberguard/backend/go/internal/secretconfig"
	"github.com/bekadoux/cyberguard/backend/go/internal/sessionstore"
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
	sessionStore := sessionstore.NewPostgres(pools.Identity)
	authentication, err := httpapi.NewAuthentication(identity.New(pools.Identity), sessionStore, cfg.Auth, cfg.DatabaseTimeout, logger)
	if err != nil {
		return err
	}
	authentication.Mount(router)
	incidentAPI, err := httpapi.NewIncidentAPI(incidentQueries.New(pools.Incidents), identity.New(pools.Identity), authentication)
	if err != nil {
		return err
	}
	incidentAPI.Mount(router)
	aiToken, err := secretconfig.Value(os.LookupEnv, "AI_INTERNAL_TOKEN")
	if err != nil {
		return err
	}
	httpapi.MountAIWorkerContext(router, pools.Incidents, aiToken)
	server := httpapi.NewServer(cfg.HTTPAddr, router, logger)
	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen HTTP: %w", err)
	}
	logger.Info("API listening", "address", listener.Addr().String())
	cleanupCtx, stopCleanup := context.WithCancel(ctx)
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		auth.RunSessionCleanup(cleanupCtx, pools.Identity, cfg.DatabaseTimeout, logger)
	}()
	defer func() { stopCleanup(); <-cleanupDone }()
	if err := httpapi.Serve(ctx, server, listener, cfg.ShutdownTimeout); err != nil {
		return err
	}
	logger.Info("API stopped gracefully")
	return nil
}
