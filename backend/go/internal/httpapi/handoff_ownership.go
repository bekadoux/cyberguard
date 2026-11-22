package httpapi

import (
	"context"
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/bekadoux/cyberguard/backend/go/internal/accountowner"
	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/incidents"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type ownershipResponse struct {
	OwnerID *string `json:"owner_id"`
}

// Recheck the session's account after taking the same lock used by role and
// password mutations. A queued write must not use an admin check from before
// another identity transaction revoked that account's authority.
func (a *HandoffAPI) lockCurrentAdmin(w http.ResponseWriter, r *http.Request, ctx context.Context, tx pgx.Tx) bool {
	if err := accountowner.Lock(ctx, tx); err != nil {
		handoffStorageError(w, err)
		return false
	}
	user := a.user(r)
	id, _ := actorUUID(user)
	var role string
	var blocked bool
	var version int64
	err := tx.QueryRow(ctx, `SELECT role,is_blocked,auth_version FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&role, &blocked, &version)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (role != "admin" || blocked || version != user.AuthVersion)) {
		a.auth.authError(w, auth.ErrUnauthenticated)
		return false
	}
	if err != nil {
		handoffStorageError(w, err)
		return false
	}
	return true
}

func (a *HandoffAPI) getOwnership(w http.ResponseWriter, r *http.Request) {
	if !a.adminOnly(w, r) {
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_request", "Unexpected query parameters.")
		return
	}
	id, err := accountowner.Current(r.Context(), a.identityDB)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ownershipResponse{OwnerID: nullableUUID(id)})
}

func (a *HandoffAPI) transferOwnership(w http.ResponseWriter, r *http.Request) {
	if !a.adminOnly(w, r) {
		return
	}
	user := a.user(r)
	actor, _ := actorUUID(user)
	owner, err := accountowner.Current(r.Context(), a.identityDB)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if !owner.Valid || owner != actor {
		writeError(w, 403, "owner_required", "Only the current owner can transfer ownership.")
		return
	}
	var body struct {
		UserID   string `json:"user_id"`
		Password string `json:"password"`
	}
	if !decodeHandoffJSON(w, r, &body) {
		return
	}
	target, err := incidents.ParseID(body.UserID)
	if err != nil || target == actor || len(body.Password) < 1 || len(body.Password) > 128 {
		writeError(w, 400, "invalid_request", "Provide another administrator's user ID and your current password.")
		return
	}
	if !a.confirmAdminPassword(w, r, body.Password) {
		return
	}
	ctx := r.Context()
	tx, err := a.identityDB.Begin(ctx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if !a.lockCurrentAdmin(w, r, ctx, tx) {
		return
	}
	owner, err = accountowner.Current(ctx, tx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if !owner.Valid || owner != actor {
		writeError(w, 403, "owner_required", "Only the current owner can transfer ownership.")
		return
	}
	var eligible bool
	err = tx.QueryRow(ctx, `SELECT role='admin' AND NOT is_blocked FROM users WHERE id=$1 FOR UPDATE`, target).Scan(&eligible)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !eligible) {
		writeError(w, 409, "owner_target_ineligible", "Ownership can only be transferred to an active administrator.")
		return
	}
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if err := saveOwnershipTransfer(ctx, tx, actor, target); err != nil {
		handoffStorageError(w, err)
		return
	}
	if commitOrError(w, tx, ctx) {
		writeJSON(w, http.StatusOK, ownershipResponse{OwnerID: nullableUUID(target)})
	}
}

func (a *HandoffAPI) confirmAdminPassword(w http.ResponseWriter, r *http.Request, password string) bool {
	user := a.user(r)
	if allowed, retry := a.auth.ownerConfirmationLimiter.Allow(user.ID); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(retry.Seconds())))))
		writeError(w, http.StatusTooManyRequests, "confirmation_rate_limited", "Too many password confirmations. Try again later.")
		return false
	}
	verified, err := a.auth.service.Authenticate(r.Context(), user.Username, password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, 403, "invalid_confirmation", "Current password confirmation failed.")
		return false
	}
	if err != nil {
		a.auth.authError(w, err)
		return false
	}
	if verified.ID != user.ID || verified.AuthVersion != user.AuthVersion {
		a.auth.authError(w, auth.ErrUnauthenticated)
		return false
	}
	return true
}

func saveOwnershipTransfer(ctx context.Context, tx pgx.Tx, from, to pgtype.UUID) error {
	result, err := tx.Exec(ctx, `UPDATE account_ownership SET user_id=$2 WHERE singleton AND user_id=$1`, from, to)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("ownership changed before transfer")
	}
	_, err = tx.Exec(ctx, `INSERT INTO identity_audit(actor_id,subject_id,action) VALUES($1,$2,'ownership_transferred')`, from, to)
	return err
}
