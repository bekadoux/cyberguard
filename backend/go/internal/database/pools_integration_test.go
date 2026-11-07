//go:build integration

package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This suite needs two dedicated, disposable PostgreSQL databases. It performs
// only read-only probes; it does not create roles, schemas or application data.
func TestPostgresPoolsIntegration(t *testing.T) {
	identityURL := os.Getenv("TEST_IDENTITY_DATABASE_URL")
	incidentURL := os.Getenv("TEST_INCIDENT_DATABASE_URL")
	if identityURL == "" || incidentURL == "" {
		t.Fatal("set TEST_IDENTITY_DATABASE_URL and TEST_INCIDENT_DATABASE_URL to dedicated test databases")
	}
	pools, err := NewPools(context.Background(), identityURL, incidentURL, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer pools.Close()
	identity, incidents := pools.Identity.Config().ConnConfig, pools.Incidents.Config().ConnConfig
	if identity.Host == incidents.Host && identity.Port == incidents.Port {
		t.Fatal("integration requires two separate PostgreSQL instances")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pools.Check(ctx); err != nil {
		t.Fatal(err)
	}
	for name, pool := range map[string]*pgxpool.Pool{"identity": pools.Identity, "incident": pools.Incidents} {
		t.Run(name, func(t *testing.T) {
			var database, user string
			var superuser, createDB, createRole bool
			err := pool.QueryRow(ctx, `
				SELECT current_database(), current_user, rolsuper, rolcreatedb, rolcreaterole
				FROM pg_roles WHERE rolname = current_user
			`).Scan(&database, &user, &superuser, &createDB, &createRole)
			if err != nil {
				t.Fatal("could not inspect runtime database role")
			}
			cfg := pool.Config().ConnConfig
			if database != cfg.Database || user != cfg.User {
				t.Fatal("pool connected to an unexpected database or role")
			}
			if superuser || createDB || createRole {
				t.Fatal("runtime database role must not have superuser, CREATEDB or CREATEROLE privileges")
			}
		})
	}
	// A closed pool is an unavailable dependency even when the other is healthy.
	pools.Identity.Close()
	if err := pools.Check(ctx); err == nil {
		t.Fatal("readiness must fail when the identity pool is closed")
	}
}
