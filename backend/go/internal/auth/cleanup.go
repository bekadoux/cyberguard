package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CleanupExpiredSessions is maintenance only. SCS/pgxstore still owns all session
// load/save/delete operations. Its built-in cleaner uses an unbounded background
// context and raw error logging, so the API disables it in favor of this task.
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
