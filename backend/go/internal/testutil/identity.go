//go:build integration

// Package testutil owns disposable integration fixtures, never application data.
package testutil

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

type IdentityFixture struct {
	Runtime  *pgxpool.Pool
	Migrator *pgxpool.Pool
}

func Identity(t *testing.T, migrationDirectory string) IdentityFixture {
	t.Helper()
	runtimeURL := os.Getenv("TEST_IDENTITY_DATABASE_URL")
	migrationURL := os.Getenv("TEST_IDENTITY_MIGRATION_DATABASE_URL")
	if runtimeURL == "" || migrationURL == "" {
		t.Fatal("set dedicated TEST_IDENTITY_DATABASE_URL and TEST_IDENTITY_MIGRATION_DATABASE_URL")
	}
	schema := "auth_test_" + strings.ToLower(rand.Text())
	newPool := func(url string) *pgxpool.Pool {
		t.Helper()
		cfg, err := pgxpool.ParseConfig(url)
		if err != nil {
			t.Fatal("invalid test database configuration")
		}
		cfg.MaxConns, cfg.MinConns, cfg.MinIdleConns = 4, 0, 0
		cfg.ConnConfig.ConnectTimeout = 3 * time.Second
		cfg.ConnConfig.RuntimeParams["search_path"] = schema
		pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
		if err != nil {
			t.Fatal("could not initialize test database pool")
		}
		t.Cleanup(pool.Close)
		return pool
	}
	migrator := newPool(migrationURL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := migrator.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal("could not create isolated authentication test schema")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := migrator.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error("could not remove owned authentication test schema")
		}
	})
	if _, err := migrator.Exec(ctx, "GRANT USAGE ON SCHEMA "+quoted+" TO go_identity"); err != nil {
		t.Fatal("could not grant runtime access to test schema")
	}
	db := stdlib.OpenDB(*migrator.Config().ConnConfig)
	t.Cleanup(func() { _ = db.Close() })
	provider, err := migrations.NewProvider(db, os.DirFS(migrationDirectory))
	if err != nil {
		t.Fatal("could not load test migrations")
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal("could not apply test migrations")
	}
	return IdentityFixture{Runtime: newPool(runtimeURL), Migrator: migrator}
}
