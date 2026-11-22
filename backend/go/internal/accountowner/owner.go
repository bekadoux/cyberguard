// Package accountowner coordinates identity mutations that affect ownership.
package accountowner

import (
	"context"
	"errors"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInitialized = errors.New("ownership is already initialized")
	ErrIneligible  = errors.New("select an existing active administrator")
	ErrUsername    = errors.New("owner username is invalid or unavailable")
)

// Lock serializes owner initialization/transfer with all admin account writes.
// Preserve the existing admin advisory key, including during a rolling upgrade.
func Lock(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(12473, 9001)`)
	return err
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Current returns an invalid UUID if the operator has not initialized ownership.
func Current(ctx context.Context, db rowQuerier) (pgtype.UUID, error) {
	var id pgtype.UUID
	err := db.QueryRow(ctx, `SELECT user_id FROM account_ownership WHERE singleton`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return id, err
}

// Initialize is only for the one-off migration-role command. A nonempty hash
// means create a new administrator, never silently adopt an existing username.
// An empty hash explicitly selects an existing active admin without changing it.
func Initialize(ctx context.Context, db *pgxpool.Pool, username, passwordHash string) error {
	username, valid := auth.NewAccountUsername(username)
	if !valid {
		return ErrUsername
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if err = Lock(ctx, tx); err != nil {
		return err
	}
	owner, err := Current(ctx, tx)
	if err != nil {
		return err
	}
	if owner.Valid {
		return ErrInitialized
	}
	var user identity.User
	queries := identity.New(tx)
	if passwordHash == "" {
		user, err = queries.GetUserByUsername(ctx, username)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (user.IsBlocked || user.Role != "admin")) {
			return ErrIneligible
		}
	} else {
		user, err = queries.CreateInitialOwner(ctx, identity.CreateInitialOwnerParams{Username: username, PasswordHash: passwordHash})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUsername
		}
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO account_ownership(user_id) VALUES($1)`, user.ID); err != nil {
		return err
	}
	// The operator is not an application account. Record the subject as actor
	// with explicit operator provenance, rather than inventing an account ID.
	if _, err = tx.Exec(ctx, `INSERT INTO identity_audit(actor_id,subject_id,action,details)
        VALUES($1,$1,'ownership_initialized','{"source":"operator"}')`, user.ID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
