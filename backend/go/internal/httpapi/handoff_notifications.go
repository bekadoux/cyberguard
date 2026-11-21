package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Recipient identity alone does not imply continuing access to the incident.
// Both listing and acknowledgements use the role checked by protectedSession.
const notificationScope = `n.recipient_id=$1 AND (n.incident_id IS NULL OR EXISTS (
	SELECT 1 FROM incidents i WHERE i.id=n.incident_id AND i.archived_at IS NULL
	  AND ($2 OR i.reporter_id=n.recipient_id)))`

func (a *HandoffAPI) listNotifications(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_request", "Unexpected query.")
		return
	}
	u := a.user(r)
	actor, _ := actorUUID(u)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := a.incidentDB.Query(ctx, `SELECT n.id,n.incident_id,n.event_type,n.message,n.created_at,n.read_at
      FROM incident_notifications n WHERE `+notificationScope+`
      ORDER BY n.created_at DESC,n.id DESC LIMIT 100`, actor, professional(u))
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer rows.Close()
	out := make([]any, 0)
	for rows.Next() {
		var id, incident pgtype.UUID
		var event, message string
		var created time.Time
		var read *time.Time
		if err = rows.Scan(&id, &incident, &event, &message, &created, &read); err != nil {
			handoffStorageError(w, err)
			return
		}
		var incidentID any
		if incident.Valid {
			incidentID = incident.String()
		}
		out = append(out, map[string]any{"id": id.String(), "incident_id": incidentID, "event_type": event, "message": message, "created_at": created, "read_at": read})
	}
	if err = rows.Err(); err != nil {
		handoffStorageError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"notifications": out})
}

func (a *HandoffAPI) readNotification(w http.ResponseWriter, r *http.Request) {
	a.setNotificationRead(w, r, true)
}

func (a *HandoffAPI) unreadNotification(w http.ResponseWriter, r *http.Request) {
	a.setNotificationRead(w, r, false)
}

func (a *HandoffAPI) setNotificationRead(w http.ResponseWriter, r *http.Request, read bool) {
	if r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_request", "Unexpected query.")
		return
	}
	id, ok := uuidForPath(w, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	u := a.user(r)
	actor, _ := actorUUID(u)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var got pgtype.UUID
	err := a.incidentDB.QueryRow(ctx, `UPDATE incident_notifications n
		SET read_at=CASE WHEN $4 THEN COALESCE(n.read_at,now()) END
		WHERE n.id=$3 AND `+notificationScope+` RETURNING n.id`, actor, professional(u), id, read).Scan(&got)
	if err == pgx.ErrNoRows {
		writeError(w, 404, "not_found", "Notification not found.")
		return
	}
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"read": read})
}
