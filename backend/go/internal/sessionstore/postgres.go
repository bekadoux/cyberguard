package sessionstore

import (
	"context"
	"time"

	"github.com/alexedwards/scs/pgxstore"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresBackend struct {
	*pgxstore.PostgresStore
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Store {
	return New(&postgresBackend{
		PostgresStore: pgxstore.NewWithCleanupInterval(pool, 0),
		pool:          pool,
	})
}

func (s *postgresBackend) CreateCtx(ctx context.Context, token string, data []byte, expiry time.Time) error {
	_, err := s.pool.Exec(ctx, "INSERT INTO sessions (token, data, expiry) VALUES ($1, $2, $3)", token, data, expiry)
	return err
}

func (s *postgresBackend) RefreshCtx(ctx context.Context, token string, data []byte, expiry time.Time) error {
	// PostgreSQL serializes this UPDATE with DELETE on the same row. If DELETE
	// wins, no row is updated; if UPDATE wins, DELETE subsequently removes it.
	// No process-local lock or check-then-upsert window is involved.
	result, err := s.pool.Exec(ctx, "UPDATE sessions SET data = $2, expiry = $3 WHERE token = $1 AND expiry > current_timestamp", token, data, expiry)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrRevoked
	}
	return nil
}
