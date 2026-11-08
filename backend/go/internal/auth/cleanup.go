package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CleanupExpiredSessions is maintenance only. The SCS persistence adapter owns
// request-time load/save/delete operations. pgxstore's built-in cleaner uses an
// unbounded background context and raw error logging, so it remains disabled.
func CleanupExpiredSessions(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if _, err := pool.Exec(ctx, "DELETE FROM sessions WHERE expiry < current_timestamp"); err != nil {
		return ErrUnavailable
	}
	return nil
}

func RunSessionCleanup(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := CleanupExpiredSessions(ctx, pool, timeout); err != nil && ctx.Err() == nil {
				logger.Warn("expired session cleanup failed")
			}
		}
	}
}
