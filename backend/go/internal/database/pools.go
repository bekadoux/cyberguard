// Package database manages the independently configured PostgreSQL pools.
package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Pools struct {
	Identity  *pgxpool.Pool
	Incidents *pgxpool.Pool
}

// NewPools validates both configurations but leaves connectivity to readiness.
// This lets the process start and expose liveness while a database is offline.
func NewPools(ctx context.Context, identityURL, incidentURL string, timeout time.Duration) (*Pools, error) {
	identityConfig, err := poolConfig("identity", identityURL, timeout)
	if err != nil {
		return nil, err
	}
	incidentConfig, err := poolConfig("incident", incidentURL, timeout)
	if err != nil {
		return nil, err
	}
	identity, err := pgxpool.NewWithConfig(ctx, identityConfig)
	if err != nil {
		return nil, errors.New("initialize identity database pool")
	}
	incidents, err := pgxpool.NewWithConfig(ctx, incidentConfig)
	if err != nil {
		identity.Close()
		return nil, errors.New("initialize incident database pool")
	}
	return &Pools{Identity: identity, Incidents: incidents}, nil
}

func poolConfig(name, databaseURL string, timeout time.Duration) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// Driver errors can include the connection string and its password.
		return nil, fmt.Errorf("invalid %s database configuration", name)
	}
	if timeout <= 0 {
		return nil, errors.New("database timeout must be positive")
	}
	cfg.MaxConns = 4
	cfg.MinConns = 0
	cfg.MinIdleConns = 0
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnLifetimeJitter = time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = time.Minute
	cfg.PingTimeout = timeout
	cfg.ConnConfig.ConnectTimeout = timeout
	return cfg, nil
}

// Check pings both databases concurrently under the caller's deadline.
// Its errors identify dependencies without exposing connection details.
func (p *Pools) Check(ctx context.Context) error {
	checks := []struct {
		name string
		pool *pgxpool.Pool
	}{
		{"identity", p.Identity},
		{"incident", p.Incidents},
	}
	results := make(chan error, len(checks))
	for _, check := range checks {
		go func() {
			if err := check.pool.Ping(ctx); err != nil {
				results <- fmt.Errorf("%s database is unavailable", check.name)
				return
			}
			results <- nil
		}()
	}
	var errs []error
	for range checks {
		if err := <-results; err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (p *Pools) Close() {
	p.Identity.Close()
	p.Incidents.Close()
}
