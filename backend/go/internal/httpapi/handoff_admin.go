package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/accountowner"
	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/db/identity"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var adminUsernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{2,63}$`)

func validAdminRole(role string) bool {
	return role == "employee" || role == "analyst" || role == "admin"
}

func (a *HandoffAPI) createUser(w http.ResponseWriter, r *http.Request) {
	if !a.adminOnly(w, r) {
		return
	}
	var body struct {
		Username string  `json:"username"`
		Password string  `json:"password"`
		Role     *string `json:"role"`
	}
	if !decodeHandoffJSON(w, r, &body) {
		return
	}
	username, valid := auth.NewAccountUsername(body.Username)
	if !valid || (body.Role != nil && *body.Role != "employee") {
		writeError(w, 400, "invalid_request", "Only employee accounts can be created; change the role afterward.")
		return
	}
	hash, err := a.auth.service.HashNewPassword(r.Context(), body.Password)
	if err != nil {
		a.newPasswordError(w, err)
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
	if !a.lockCurrentAdmin(w, r, ctx, tx) {
		return
	}
	created, err := identity.New(tx).RegisterEmployee(ctx, identity.RegisterEmployeeParams{Username: username, PasswordHash: hash})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 409, "conflict", "Username unavailable.")
		return
	}
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	id := created.ID
	if _, err = tx.Exec(ctx, `INSERT INTO identity_audit(actor_id,subject_id,action,details)
        VALUES($1,$2,'user_created','{"role":"employee"}'::jsonb)`, actor, id); err != nil {
		handoffStorageError(w, err)
		return
	}
	if !commitOrError(w, tx, ctx) {
		return
	}
	writeJSON(w, 201, map[string]any{"user": map[string]any{"id": id.String(), "username": username, "role": "employee", "is_blocked": false}})
}

type directoryQuery struct {
	eligibleOnly bool
	prefix       string
	after        string
	limit        int
}

var directoryPrefixPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

func parseDirectoryQuery(raw string) (directoryQuery, bool) {
	options := directoryQuery{limit: 500}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return options, false
	}
	for key, items := range values {
		if len(items) != 1 {
			return options, false
		}
		value := items[0]
		switch key {
		case "eligible":
			if value != "analyst" {
				return options, false
			}
			options.eligibleOnly = true
		case "q":
			options.prefix = strings.ToLower(value)
			if value != "" && !directoryPrefixPattern.MatchString(options.prefix) {
				return options, false
			}
		case "cursor":
			if !adminUsernamePattern.MatchString(value) {
				return options, false
			}
			options.after = value
		case "limit":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 || n > 500 {
				return options, false
			}
			options.limit = n
		default:
			return options, false
		}
	}
	return options, true
}

func (a *HandoffAPI) listUsers(w http.ResponseWriter, r *http.Request) {
	if !a.adminOnly(w, r) {
		return
	}
	options, ok := parseDirectoryQuery(r.URL.RawQuery)
	if !ok {
		writeError(w, 400, "invalid_request", "Invalid user directory query.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := a.identityDB.Query(ctx, `SELECT id,username,role,is_blocked,created_at,updated_at
      FROM users WHERE (NOT $1::boolean OR (role='analyst' AND NOT is_blocked))
        AND starts_with(username, $2) AND username > $3
      ORDER BY username LIMIT $4`, options.eligibleOnly, options.prefix, options.after, options.limit+1)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer rows.Close()
	users := make([]any, 0)
	var nextCursor *string
	var lastUsername string
	for rows.Next() {
		if len(users) == options.limit {
			nextCursor = &lastUsername
			break
		}
		var id pgtype.UUID
		var name, role string
		var blocked bool
		var created, updated time.Time
		if err = rows.Scan(&id, &name, &role, &blocked, &created, &updated); err != nil {
			handoffStorageError(w, err)
			return
		}
		users = append(users, map[string]any{"id": id.String(), "username": name, "role": role, "is_blocked": blocked, "created_at": created, "updated_at": updated})
		lastUsername = name
	}
	if err = rows.Err(); err != nil {
		handoffStorageError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"users": users, "next_cursor": nextCursor})
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
	a.changeUser(w, r, target, "role", body.Role, false, "", body.Reason, false)
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
	a.changeUser(w, r, target, "blocked", "", *body.Blocked, "", body.Reason, false)
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
		Password        string `json:"password"`
		Reason          string `json:"reason"`
		CurrentPassword string `json:"current_password"`
	}
	if !decodeHandoffJSON(w, r, &body) {
		return
	}
	actor, _ := actorUUID(a.user(r))
	confirmed := false
	if target == actor && body.CurrentPassword != "" {
		if !a.confirmAdminPassword(w, r, body.CurrentPassword) {
			return
		}
		confirmed = true
	}
	hash, err := a.auth.service.HashNewPassword(r.Context(), body.Password)
	if err != nil {
		a.newPasswordError(w, err)
		return
	}
	a.changeUser(w, r, target, "password", "", false, hash, body.Reason, confirmed)
}

func (a *HandoffAPI) newPasswordError(w http.ResponseWriter, err error) {
	if errors.Is(err, auth.ErrInvalidAccount) {
		writeError(w, 400, "invalid_request", "Password must be nonblank and 12 to 128 UTF-8 bytes.")
		return
	}
	a.auth.authError(w, err)
}

func (a *HandoffAPI) changeUser(w http.ResponseWriter, r *http.Request, target pgtype.UUID, action, newRole string, blocked bool, hash, reason string, confirmed bool) {
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
	if !a.lockCurrentAdmin(w, r, ctx, tx) {
		return
	}
	owner, err := accountowner.Current(ctx, tx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if owner.Valid && target == owner {
		if target != actor {
			writeError(w, 403, "owner_protected", "Other administrators cannot change the owner's role, blocked state or password.")
			return
		}
		if action == "password" && !confirmed {
			writeError(w, 403, "invalid_confirmation", "The owner must confirm their current password before changing it.")
			return
		}
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
