package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/bekadoux/cyberguard/backend/go/internal/auth"
	"github.com/bekadoux/cyberguard/backend/go/internal/incidents"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HandoffAPI extends the existing, session-protected Go API. The two pools
// belong to distinct PostgreSQL instances; never pretend their writes form
// one cross-database transaction.
type HandoffAPI struct {
	incidentDB *pgxpool.Pool
	identityDB *pgxpool.Pool
	auth       *Authentication
}

func (a *HandoffAPI) mountIncidentRoutes(r chi.Router) {
	r.Post("/{id}/claim", a.claim)
	r.Post("/{id}/assign", a.assign)
	r.Patch("/{id}/fields", a.updateFields)
	r.Post("/{id}/transition", a.transition)
	r.Post("/{id}/archive", a.archive)
	r.Get("/{id}/comments", a.listComments)
	r.Post("/{id}/comments", a.addComment)
	r.Get("/{id}/history", a.listHistory)
}

func (a *HandoffAPI) mountAuxiliaryRoutes(router chi.Router) {
	router.Route("/notifications", func(r chi.Router) {
		r.Use(a.auth.browserPolicy, a.auth.withDeadline, a.auth.protectedSession)
		r.Get("/", a.listNotifications)
		r.Post("/{id}/read", a.readNotification)
	})
	router.Route("/admin", func(r chi.Router) {
		r.Use(a.auth.browserPolicy, a.auth.withDeadline, a.auth.protectedSession)
		r.Get("/users", a.listUsers)
		r.Post("/users", a.createUser)
		r.Post("/users/{id}/role", a.changeRole)
		r.Post("/users/{id}/block", a.setBlocked)
		r.Post("/users/{id}/password", a.resetPassword)
		r.Get("/audit", a.listAudit)
	})
}

func (a *HandoffAPI) user(r *http.Request) auth.User {
	return r.Context().Value(userContextKey{}).(auth.User)
}

func professional(u auth.User) bool { return u.Role == "analyst" || u.Role == "admin" }

func (a *HandoffAPI) adminOnly(w http.ResponseWriter, r *http.Request) bool {
	if a.user(r).Role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "Admin role is required.")
		return false
	}
	return true
}

func decodeHandoffJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	if r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_request", "Unexpected query parameters.")
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, 415, "unsupported_media_type", "Content-Type must be application/json.")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		writeError(w, 400, "invalid_request", "Invalid request body.")
		return false
	}
	var rest any
	if err := dec.Decode(&rest); err != io.EOF {
		writeError(w, 400, "invalid_request", "Expected one JSON object.")
		return false
	}
	return true
}

func validHandoffText(s string, max int, required bool) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && utf8.RuneCountInString(s) <= max && (!required || strings.TrimSpace(s) != "")
}

func uuidForPath(w http.ResponseWriter, value string) (pgtype.UUID, bool) {
	id, err := incidents.ParseID(value)
	if err != nil {
		writeError(w, 404, "not_found", "Resource not found.")
		return pgtype.UUID{}, false
	}
	return id, true
}

func incidentPath(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	return uuidForPath(w, chi.URLParam(r, "id"))
}

func actorUUID(u auth.User) (pgtype.UUID, error) { return incidents.ParseID(u.ID) }

func handoffStorageError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "not_found", "Resource not found.")
		return
	}
	writeError(w, 503, "service_unavailable", "Service temporarily unavailable.")
}

// Incident state is read FOR UPDATE before any mutation. This serializes
// assignment and state transitions even when multiple analysts race.
type lockedIncident struct {
	reporter   pgtype.UUID
	assigned   pgtype.UUID
	status     string
	category   *string
	severity   *string
	resolution *string
	affected   *string
}

func lockIncident(ctx context.Context, tx pgx.Tx, id pgtype.UUID) (lockedIncident, error) {
	var v lockedIncident
	err := tx.QueryRow(ctx, `
        SELECT reporter_id, assigned_analyst_id, status, category, severity, resolution_note, affected_system
        FROM incidents WHERE id=$1 AND archived_at IS NULL FOR UPDATE`, id).Scan(
		&v.reporter, &v.assigned, &v.status, &v.category, &v.severity, &v.resolution, &v.affected)
	return v, err
}

func recordIncidentAction(ctx context.Context, tx pgx.Tx, id, actor pgtype.UUID, action, reason string, details any) error {
	_, err := tx.Exec(ctx, `INSERT INTO incident_actions(incident_id, actor_id, action, reason, details)
        VALUES ($1,$2,$3,NULLIF($4,''),$5)`, id, actor, action, reason, details)
	return err
}

func notifyIncident(ctx context.Context, tx pgx.Tx, recipient, incident pgtype.UUID, event, message string) error {
	if !recipient.Valid {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO incident_notifications(recipient_id, incident_id, event_type, message)
        VALUES ($1,$2,$3,$4)`, recipient, incident, event, message)
	return err
}

func commitOrError(w http.ResponseWriter, tx pgx.Tx, ctx context.Context) bool {
	if err := tx.Commit(ctx); err != nil {
		handoffStorageError(w, err)
		return false
	}
	return true
}
