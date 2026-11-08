package database

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPoolsHaveIndependentBoundedConfiguration(t *testing.T) {
	t.Parallel()
	pools, err := NewPools(context.Background(),
		"postgres://identity_user:test@127.0.0.1:5433/identity?sslmode=disable&pool_max_conns=99&pool_min_conns=20",
		"postgres://incident_user:test@127.0.0.1:5434/incidents?sslmode=disable",
		500*time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer pools.Close()
	if pools.Identity == pools.Incidents {
		t.Fatal("databases must have separate pools")
	}
	identity, incidents := pools.Identity.Config(), pools.Incidents.Config()
	if identity.ConnConfig.User != "identity_user" || identity.ConnConfig.Database != "identity" || identity.ConnConfig.Port != 5433 {
		t.Fatal("identity pool has incorrect configuration")
	}
	if incidents.ConnConfig.User != "incident_user" || incidents.ConnConfig.Database != "incidents" || incidents.ConnConfig.Port != 5434 {
		t.Fatal("incident pool has incorrect configuration")
	}
	for _, cfg := range []*pgxpool.Config{identity, incidents} {
		if cfg.MaxConns != 4 || cfg.MinConns != 0 || cfg.MinIdleConns != 0 || cfg.ConnConfig.ConnectTimeout != 500*time.Millisecond || cfg.PingTimeout != 500*time.Millisecond {
			t.Fatal("pool limits were not applied")
		}
	}
	if pools.Identity.Stat().TotalConns() != 0 || pools.Incidents.Stat().TotalConns() != 0 {
		t.Fatal("pool construction should not require database connectivity")
	}
}

func TestInvalidDriverConfigurationDoesNotLeakCredentials(t *testing.T) {
	t.Parallel()
	valid := "postgres://user:test@127.0.0.1:5433/db?sslmode=disable"
	invalid := "postgres://user:sensitive-password@127.0.0.1/db?pool_max_conns=invalid"
	for _, test := range []struct {
		identity, incident, name string
	}{
		{invalid, valid, "identity"},
		{valid, invalid, "incident"},
	} {
		pools, err := NewPools(context.Background(), test.identity, test.incident, time.Second)
		if err == nil {
			pools.Close()
			t.Fatal("expected configuration error")
		}
		if strings.Contains(err.Error(), "sensitive") || !strings.Contains(err.Error(), test.name) {
			t.Fatal("error should identify the dependency without credentials")
		}
	}
}

func TestCheckHonorsDeadlineAndSanitizesErrors(t *testing.T) {
	t.Parallel()
	// A TCP listener that never completes a PostgreSQL handshake simulates a hung
	// dependency without relying on an external database or a free fixed port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	url := fmt.Sprintf("postgres://user:sensitive-password@%s/db?sslmode=disable", listener.Addr())
	pools, err := NewPools(context.Background(), url, url, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer pools.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = pools.Check(ctx)
	if err == nil {
		t.Fatal("expected unavailable databases")
	}
	if time.Since(started) > time.Second {
		t.Fatal("database check exceeded its deadline")
	}
	if strings.Contains(err.Error(), "sensitive") || !strings.Contains(err.Error(), "identity") || !strings.Contains(err.Error(), "incident") {
		t.Fatal("check errors must name both dependencies without credentials")
	}
}

func TestPoolConfigRejectsNonPositiveTimeout(t *testing.T) {
	t.Parallel()
	_, err := poolConfig("identity", "postgres://user:test@127.0.0.1/db", 0)
	if err == nil {
		t.Fatal("expected timeout validation error")
	}
}
