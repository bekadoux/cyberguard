package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/alexedwards/argon2id"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var adminUsernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{2,63}$`)

func validAdminRole(role string) bool {
	return role == "employee" || role == "analyst" || role == "admin"
}

func hashNewPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 128 || !validHandoffText(password, 128, true) {
		return "", errors.New("unsafe password")
	}
	return argon2id.CreateHash(password, &argon2id.Params{
		Memory: 64 * 1024, Iterations: 1, Parallelism: 2, SaltLength: 16, KeyLength: 32,
	})
}

func (a *HandoffAPI) createUser(w http.ResponseWriter, r *http.Request) {
	if !a.adminOnly(w, r) {
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !decodeHandoffJSON(w, r, &body) {
		return
	}
	username := strings.ToLower(strings.TrimSpace(body.Username))
	if !adminUsernamePattern.MatchString(username) || !validAdminRole(body.Role) {
		writeError(w, 400, "invalid_request", "Invalid username or role.")
		return
	}
	hash, err := hashNewPassword(body.Password)
	if err != nil {
		writeError(w, 400, "invalid_request", "Password must be 12 to 128 UTF-8 bytes/characters.")
		return
	}
	actor, _ := actorUUID(a.user(r))
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := a.identityDB.Begin(ctx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	var id pgtype.UUID
	err = tx.QueryRow(ctx, `INSERT INTO users(username,password_hash,role) VALUES($1,$2,$3)
        ON CONFLICT(username) DO NOTHING RETURNING id`, username, hash, body.Role).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 409, "conflict", "Username unavailable.")
		return
	}
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if _, err = tx.Exec(ctx, `INSERT INTO identity_audit(actor_id,subject_id,action,details)
        VALUES($1,$2,'user_created',jsonb_build_object('role',$3::text))`, actor, id, body.Role); err != nil {
		handoffStorageError(w, err)
		return
	}
	if !commitOrError(w, tx, ctx) {
		return
	}
	writeJSON(w, 201, map[string]any{"user": map[string]any{"id": id.String(), "username": username, "role": body.Role, "is_blocked": false}})
}

func (a *HandoffAPI) listUsers(w http.ResponseWriter, r *http.Request) {
	if !a.adminOnly(w, r) {
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_request", "Unexpected query parameters.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := a.identityDB.Query(ctx, `SELECT id,username,role,is_blocked,created_at,updated_at
      FROM users ORDER BY username LIMIT 500`)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer rows.Close()
	users := make([]any, 0)
	for rows.Next() {
		var id pgtype.UUID
		var name, role string
		var blocked bool
		var created, updated time.Time
		if err = rows.Scan(&id, &name, &role, &blocked, &created, &updated); err != nil {
			handoffStorageError(w, err)
			return
		}
		users = append(users, map[string]any{"id": id.String(), "username": name, "role": role, "is_blocked": blocked, "created_at": created, "updated_at": updated})
	}
	if err = rows.Err(); err != nil {
		handoffStorageError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"users": users})
}

func (a *HandoffAPI) changeRole(w http.ResponseWriter, r *http.Request) {
	if !a.adminOnly(w, r) {
		return
	}
	target, ok := uuidForPath(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var body struct {
		Role   string `json:"role"`
		Reason string `json:"reason"`
	}
	if !decodeHandoffJSON(w, r, &body) {
		return
	}
	if !validAdminRole(body.Role) {
		writeError(w, 400, "invalid_request", "Invalid role.")
		return
	}
	a.changeUser(w, r, target, "role", body.Role, false, "", body.Reason)
}
func (a *HandoffAPI) setBlocked(w http.ResponseWriter, r *http.Request) {
	if !a.adminOnly(w, r) {
		return
	}
	target, ok := uuidForPath(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var body struct {
		Blocked *bool  `json:"blocked"`
		Reason  string `json:"reason"`
	}
	if !decodeHandoffJSON(w, r, &body) {
		return
	}
	if body.Blocked == nil {
		writeError(w, 400, "invalid_request", "Provide blocked boolean.")
		return
	}
	a.changeUser(w, r, target, "blocked", "", *body.Blocked, "", body.Reason)
}
func (a *HandoffAPI) resetPassword(w http.ResponseWriter, r *http.Request) {
	if !a.adminOnly(w, r) {
		return
	}
	target, ok := uuidForPath(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var body struct {
		Password string `json:"password"`
		Reason   string `json:"reason"`
	}
	if !decodeHandoffJSON(w, r, &body) {
		return
	}
	hash, err := hashNewPassword(body.Password)
	if err != nil {
		writeError(w, 400, "invalid_request", "Invalid password length.")
		return
	}
	a.changeUser(w, r, target, "password", "", false, hash, body.Reason)
}

func (a *HandoffAPI) changeUser(w http.ResponseWriter, r *http.Request, target pgtype.UUID, action, newRole string, blocked bool, hash, reason string) {
	if !validHandoffText(reason, 1000, false) {
		writeError(w, 400, "invalid_request", "Invalid reason.")
		return
	}
	actor, _ := actorUUID(a.user(r))
	if target == actor && (action == "role" || action == "blocked") {
		writeError(w, 409, "conflict", "Cannot change own role or blocked status.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := a.identityDB.Begin(ctx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	// Serialize administration changes so two admins cannot simultaneously
	// demote/block each other and leave the application without an admin.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(12473, 9001)`); err != nil {
		handoffStorageError(w, err)
		return
	}
	var oldRole string
	var oldBlocked bool
	if err = tx.QueryRow(ctx, `SELECT role,is_blocked FROM users WHERE id=$1 FOR UPDATE`, target).Scan(&oldRole, &oldBlocked); err != nil {
		handoffStorageError(w, err)
		return
	}
	if oldRole == "admin" && !oldBlocked && ((action == "role" && newRole != "admin") || (action == "blocked" && blocked)) {
		var remaining int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE id<>$1 AND role='admin' AND NOT is_blocked`, target).Scan(&remaining); err != nil {
			handoffStorageError(w, err)
			return
		}
		if remaining == 0 {
			writeError(w, 409, "conflict", "Cannot remove the last active administrator.")
			return
		}
	}
	// auth_version invalidates every prior session even after unblocking.
	event := ""
	var details any = map[string]any{}
	switch action {
	case "role":
		_, err = tx.Exec(ctx, `UPDATE users SET role=$2,auth_version=auth_version+1,updated_at=now() WHERE id=$1`, target, newRole)
		event = "user_role_changed"
		details = map[string]any{"previous": oldRole, "new": newRole}
	case "blocked":
		_, err = tx.Exec(ctx, `UPDATE users SET is_blocked=$2,auth_version=auth_version+1,updated_at=now() WHERE id=$1`, target, blocked)
		if blocked {
			event = "user_blocked"
		} else {
			event = "user_unblocked"
		}
		details = map[string]any{"previous": oldBlocked, "new": blocked}
	case "password":
		_, err = tx.Exec(ctx, `UPDATE users SET password_hash=$2,auth_version=auth_version+1,updated_at=now() WHERE id=$1`, target, hash)
		event = "user_password_reset"
	}
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if reason != "" {
		if attrs, ok := details.(map[string]any); ok {
			attrs["reason"] = reason
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO identity_audit(actor_id,subject_id,action,details) VALUES($1,$2,$3,$4)`, actor, target, event, details); err != nil {
		handoffStorageError(w, err)
		return
	}
	if !commitOrError(w, tx, ctx) {
		return
	}
	writeJSON(w, 200, map[string]any{"updated": true, "user_id": target.String()})
}

func (a *HandoffAPI) listAudit(w http.ResponseWriter, r *http.Request) {
	if !a.adminOnly(w, r) {
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_request", "Unexpected query.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := a.identityDB.Query(ctx, `SELECT id,actor_id,subject_id,action,details,created_at
         FROM identity_audit ORDER BY created_at DESC,id DESC LIMIT 500`)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer rows.Close()
	out := make([]any, 0)
	for rows.Next() {
		var id, actor, subject pgtype.UUID
		var action string
		var details []byte
		var at time.Time
		if err = rows.Scan(&id, &actor, &subject, &action, &details, &at); err != nil {
			handoffStorageError(w, err)
			return
		}
		out = append(out, map[string]any{"id": id.String(), "actor_id": actor.String(), "subject_id": subject.String(), "action": action, "details": json.RawMessage(details), "created_at": at})
	}
	if err = rows.Err(); err != nil {
		handoffStorageError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"audit": out})
}
