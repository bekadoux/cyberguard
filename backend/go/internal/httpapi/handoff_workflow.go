package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/incidents"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func (a *HandoffAPI) claim(w http.ResponseWriter, r *http.Request) {
	u := a.user(r)
	if !professional(u) {
		writeError(w, 403, "forbidden", "Professional role required.")
		return
	}
	id, ok := incidentPath(w, r)
	if !ok {
		return
	}
	actor, _ := actorUUID(u)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := a.incidentDB.Begin(ctx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	incident, err := lockIncident(ctx, tx, id)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if incident.assigned.Valid || incident.status != "submitted" {
		writeError(w, 409, "conflict", "Incident is not available for claiming.")
		return
	}
	if _, err = tx.Exec(ctx, `UPDATE incidents SET assigned_analyst_id=$2,status='triage',updated_at=now() WHERE id=$1`, id, actor); err != nil {
		handoffStorageError(w, err)
		return
	}
	if err = recordIncidentAction(ctx, tx, id, actor, "claimed", "", map[string]any{"status": "triage"}); err != nil {
		handoffStorageError(w, err)
		return
	}
	if err = notifyIncident(ctx, tx, incident.reporter, id, "incident_claimed", "An analyst has taken your incident."); err != nil {
		handoffStorageError(w, err)
		return
	}
	if !commitOrError(w, tx, ctx) {
		return
	}
	writeJSON(w, 200, map[string]any{"status": "triage", "assigned_analyst_id": actor.String()})
}

func (a *HandoffAPI) assign(w http.ResponseWriter, r *http.Request) {
	if !a.adminOnly(w, r) {
		return
	}
	id, ok := incidentPath(w, r)
	if !ok {
		return
	}
	var body struct {
		AnalystID string `json:"analyst_id"`
	}
	if !decodeHandoffJSON(w, r, &body) {
		return
	}
	target, err := incidents.ParseID(body.AnalystID)
	if err != nil {
		writeError(w, 400, "invalid_request", "Invalid analyst ID.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var role string
	var blocked bool
	if err = a.identityDB.QueryRow(ctx, `SELECT role,is_blocked FROM users WHERE id=$1`, target).Scan(&role, &blocked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 400, "invalid_request", "Analyst not found.")
		} else {
			handoffStorageError(w, err)
		}
		return
	}
	if role != "analyst" || blocked {
		writeError(w, 400, "invalid_request", "Target must be an active analyst.")
		return
	}
	actor, _ := actorUUID(a.user(r))
	tx, err := a.incidentDB.Begin(ctx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	current, err := lockIncident(ctx, tx, id)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if current.status == "closed" || current.status == "false_positive" || current.status == "resolved" {
		writeError(w, 409, "conflict", "Incident cannot be reassigned in this status.")
		return
	}
	if _, err = tx.Exec(ctx, `UPDATE incidents SET assigned_analyst_id=$2,
        status=CASE WHEN status='submitted' THEN 'triage' ELSE status END,updated_at=now() WHERE id=$1`, id, target); err != nil {
		handoffStorageError(w, err)
		return
	}
	if err = recordIncidentAction(ctx, tx, id, actor, "assigned", "", map[string]any{"analyst_id": target.String()}); err != nil {
		handoffStorageError(w, err)
		return
	}
	if err = notifyIncident(ctx, tx, target, id, "incident_assigned", "An incident has been assigned to you."); err != nil {
		handoffStorageError(w, err)
		return
	}
	if err = notifyIncident(ctx, tx, current.reporter, id, "incident_assigned", "An analyst has been assigned to your incident."); err != nil {
		handoffStorageError(w, err)
		return
	}
	if !commitOrError(w, tx, ctx) {
		return
	}
	writeJSON(w, 200, map[string]any{"assigned_analyst_id": target.String()})
}

// updateFields accepts only explicitly authorized fields. No blanket UPDATE
// over request JSON or incident columns; original submission is immutable.
func (a *HandoffAPI) updateFields(w http.ResponseWriter, r *http.Request) {
	u := a.user(r)
	if !professional(u) {
		writeError(w, 403, "forbidden", "Professional role required.")
		return
	}
	id, ok := incidentPath(w, r)
	if !ok {
		return
	}
	var fields map[string]json.RawMessage
	if !decodeHandoffJSON(w, r, &fields) {
		return
	}
	if len(fields) == 0 {
		writeError(w, 400, "invalid_request", "Provide fields to update.")
		return
	}
	for name := range fields {
		switch name {
		case "category", "severity", "affected_system", "resolution_note":
		default:
			writeError(w, 400, "invalid_request", "Unknown or immutable field.")
			return
		}
	}
	actor, _ := actorUUID(u)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := a.incidentDB.Begin(ctx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	current, err := lockIncident(ctx, tx, id)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if u.Role != "admin" && (!current.assigned.Valid || current.assigned != actor) {
		writeError(w, 403, "forbidden", "Assigned analyst required.")
		return
	}
	if current.status == "closed" || current.status == "false_positive" {
		writeError(w, 409, "conflict", "Closed incident cannot be edited.")
		return
	}
	category, severity, affected, resolution := current.category, current.severity, current.affected, current.resolution
	targets := map[string]**string{"category": &category, "severity": &severity, "affected_system": &affected, "resolution_note": &resolution}
	for name, raw := range fields {
		var value *string
		if err = json.Unmarshal(raw, &value); err != nil {
			writeError(w, 400, "invalid_request", "Fields must be strings or null.")
			return
		}
		if value != nil {
			limit := 500
			if name == "resolution_note" {
				limit = 10000
			}
			if !validHandoffText(*value, limit, name != "affected_system") {
				writeError(w, 400, "invalid_request", "Invalid field length or text.")
				return
			}
			if name == "category" && !incidents.ValidCategory(*value) {
				writeError(w, 400, "invalid_request", "Unsupported category.")
				return
			}
			if name == "severity" && *value != "low" && *value != "medium" && *value != "high" && *value != "critical" {
				writeError(w, 400, "invalid_request", "Unsupported severity.")
				return
			}
		}
		*targets[name] = value
	}
	if (current.status == "resolved") && (resolution == nil || strings.TrimSpace(*resolution) == "") {
		writeError(w, 409, "conflict", "Resolution is required.")
		return
	}
	if _, err = tx.Exec(ctx, `UPDATE incidents SET category=$2,severity=$3,affected_system=$4,resolution_note=$5,updated_at=now() WHERE id=$1`, id, category, severity, affected, resolution); err != nil {
		handoffStorageError(w, err)
		return
	}
	// Record only the names of changed fields; incident contents may be sensitive.
	names := make([]string, 0, len(fields))
	for k := range fields {
		names = append(names, k)
	}
	if err = recordIncidentAction(ctx, tx, id, actor, "fields_updated", "", map[string]any{"fields": names}); err != nil {
		handoffStorageError(w, err)
		return
	}
	if !commitOrError(w, tx, ctx) {
		return
	}
	writeJSON(w, 200, map[string]any{"updated": true})
}

// allowedTransition implements the agreed domain state machine. Status changes
// are serialized by SELECT FOR UPDATE and guarded by current role and ownership.
func allowedTransition(from, to string, uRole string, reporter, assigned, actor pgtype.UUID) bool {
	reporterOwn := reporter == actor
	assignedOwn := assigned.Valid && assigned == actor
	professionalWrite := uRole == "admin" || (uRole == "analyst" && assignedOwn)
	switch {
	case from == "triage" && (to == "investigating" || to == "false_positive"):
		return professionalWrite
	case from == "investigating" && (to == "awaiting_reporter" || to == "resolved"):
		return professionalWrite
	case from == "resolved" && (to == "closed" || to == "investigating"):
		return reporterOwn
	case (from == "closed" || from == "false_positive") && to == "investigating":
		return uRole == "admin"
	}
	return false
}

func (a *HandoffAPI) transition(w http.ResponseWriter, r *http.Request) {
	id, ok := incidentPath(w, r)
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if !decodeHandoffJSON(w, r, &body) {
		return
	}
	if !incidents.ValidStatus(body.Status) || !validHandoffText(body.Reason, 2000, false) {
		writeError(w, 400, "invalid_request", "Invalid status or reason.")
		return
	}
	u := a.user(r)
	actor, _ := actorUUID(u)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := a.incidentDB.Begin(ctx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	current, err := lockIncident(ctx, tx, id)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	// Preserve employee non-enumeration: inaccessible and missing incidents are both 404.
	if u.Role == "employee" && current.reporter != actor {
		writeError(w, 404, "not_found", "Incident not found.")
		return
	}
	if !allowedTransition(current.status, body.Status, u.Role, current.reporter, current.assigned, actor) {
		writeError(w, 403, "forbidden", "Transition not allowed.")
		return
	}
	reasonRequired := body.Status == "false_positive" || (current.status == "closed" || current.status == "false_positive") || (current.status == "resolved" && body.Status == "investigating")
	if reasonRequired && strings.TrimSpace(body.Reason) == "" {
		writeError(w, 400, "invalid_request", "A reason is required.")
		return
	}
	if body.Status == "investigating" && current.status == "triage" && (current.category == nil || current.severity == nil) {
		writeError(w, 409, "conflict", "Category and severity are required before investigation.")
		return
	}
	if body.Status == "resolved" && (current.resolution == nil || strings.TrimSpace(*current.resolution) == "") {
		writeError(w, 409, "conflict", "Resolution note is required.")
		return
	}
	if body.Status == "awaiting_reporter" {
		var found bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM incident_comments
            WHERE incident_id=$1 AND visibility='public' AND author_id=$2)`, id, actor).Scan(&found)
		if err != nil {
			handoffStorageError(w, err)
			return
		}
		if !found {
			writeError(w, 409, "conflict", "Add a public question before requesting reporter information.")
			return
		}
	}
	if current.status == "resolved" && body.Status == "investigating" {
		if _, err = tx.Exec(ctx, `INSERT INTO incident_comments(incident_id,author_id,visibility,body)
           VALUES($1,$2,'public',$3)`, id, actor, body.Reason); err != nil {
			handoffStorageError(w, err)
			return
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE incidents SET status=$2,updated_at=now() WHERE id=$1`, id, body.Status); err != nil {
		handoffStorageError(w, err)
		return
	}
	if body.Status == "resolved" {
		if _, err = tx.Exec(ctx, `INSERT INTO incident_resolutions(incident_id,resolved_at) VALUES($1,now())
          ON CONFLICT(incident_id) DO UPDATE SET resolved_at=EXCLUDED.resolved_at`, id); err != nil {
			handoffStorageError(w, err)
			return
		}
	} else if body.Status == "investigating" {
		if _, err = tx.Exec(ctx, `UPDATE incident_resolutions SET resolved_at=NULL WHERE incident_id=$1`, id); err != nil {
			handoffStorageError(w, err)
			return
		}
	}

	if err = recordIncidentAction(ctx, tx, id, actor, "status_changed", body.Reason, map[string]any{"from": current.status, "to": body.Status}); err != nil {
		handoffStorageError(w, err)
		return
	}
	if err = notifyIncident(ctx, tx, current.reporter, id, "status_changed", "Your incident status has changed."); err != nil {
		handoffStorageError(w, err)
		return
	}
	if !commitOrError(w, tx, ctx) {
		return
	}
	writeJSON(w, 200, map[string]any{"status": body.Status})
}

func (a *HandoffAPI) archive(w http.ResponseWriter, r *http.Request) {
	if !a.adminOnly(w, r) {
		return
	}
	id, ok := incidentPath(w, r)
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if !decodeHandoffJSON(w, r, &body) {
		return
	}
	if !validHandoffText(body.Reason, 2000, true) {
		writeError(w, 400, "invalid_request", "Archive reason is required.")
		return
	}
	actor, _ := actorUUID(a.user(r))
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := a.incidentDB.Begin(ctx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	_, err = lockIncident(ctx, tx, id)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if _, err = tx.Exec(ctx, `UPDATE incidents SET archived_at=now(),archive_reason=$2,updated_at=now() WHERE id=$1`, id, body.Reason); err != nil {
		handoffStorageError(w, err)
		return
	}
	if err = recordIncidentAction(ctx, tx, id, actor, "archived", body.Reason, map[string]any{}); err != nil {
		handoffStorageError(w, err)
		return
	}
	if !commitOrError(w, tx, ctx) {
		return
	}
	writeJSON(w, 200, map[string]any{"archived": true})
}

func (a *HandoffAPI) addComment(w http.ResponseWriter, r *http.Request) {
	id, ok := incidentPath(w, r)
	if !ok {
		return
	}
	var body struct {
		Text       string `json:"text"`
		Visibility string `json:"visibility"`
	}
	if !decodeHandoffJSON(w, r, &body) {
		return
	}
	if !validHandoffText(body.Text, 5000, true) || (body.Visibility != "public" && body.Visibility != "internal") {
		writeError(w, 400, "invalid_request", "Invalid comment.")
		return
	}
	u := a.user(r)
	actor, _ := actorUUID(u)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	tx, err := a.incidentDB.Begin(ctx)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	current, err := lockIncident(ctx, tx, id)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	reporterOwn := current.reporter == actor
	if !professional(u) && !reporterOwn {
		writeError(w, 404, "not_found", "Incident not found.")
		return
	}
	if body.Visibility == "internal" && !professional(u) {
		writeError(w, 403, "forbidden", "Internal comments require professional access.")
		return
	}
	if body.Visibility == "public" && !reporterOwn && u.Role != "admin" && !(u.Role == "analyst" && current.assigned.Valid && current.assigned == actor) {
		writeError(w, 403, "forbidden", "Assigned analyst required for public comments.")
		return
	}
	var commentID pgtype.UUID
	err = tx.QueryRow(ctx, `INSERT INTO incident_comments(incident_id,author_id,visibility,body)
        VALUES($1,$2,$3,$4) RETURNING id`, id, actor, body.Visibility, body.Text).Scan(&commentID)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if err = recordIncidentAction(ctx, tx, id, actor, "comment_added", "", map[string]any{"visibility": body.Visibility, "comment_id": commentID.String()}); err != nil {
		handoffStorageError(w, err)
		return
	}
	if body.Visibility == "public" && reporterOwn && current.status == "awaiting_reporter" {
		if _, err = tx.Exec(ctx, `UPDATE incidents SET status='investigating',updated_at=now() WHERE id=$1`, id); err != nil {
			handoffStorageError(w, err)
			return
		}
		if err = recordIncidentAction(ctx, tx, id, actor, "status_changed", "Reporter replied", map[string]any{"from": "awaiting_reporter", "to": "investigating"}); err != nil {
			handoffStorageError(w, err)
			return
		}
		if err = notifyIncident(ctx, tx, current.assigned, id, "reporter_replied", "The reporter replied to an information request."); err != nil {
			handoffStorageError(w, err)
			return
		}
	} else if body.Visibility == "public" && !reporterOwn {
		if err = notifyIncident(ctx, tx, current.reporter, id, "new_comment", "A public comment was added to your incident."); err != nil {
			handoffStorageError(w, err)
			return
		}
	} else if body.Visibility == "public" && reporterOwn {
		if err = notifyIncident(ctx, tx, current.assigned, id, "reporter_replied", "The reporter added a comment."); err != nil {
			handoffStorageError(w, err)
			return
		}
	}
	if !commitOrError(w, tx, ctx) {
		return
	}
	writeJSON(w, 201, map[string]any{"id": commentID.String(), "visibility": body.Visibility})
}

func (a *HandoffAPI) listComments(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_request", "Unexpected query.")
		return
	}
	id, ok := incidentPath(w, r)
	if !ok {
		return
	}
	u := a.user(r)
	actor, _ := actorUUID(u)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var reporter pgtype.UUID
	err := a.incidentDB.QueryRow(ctx, `SELECT reporter_id FROM incidents WHERE id=$1 AND archived_at IS NULL`, id).Scan(&reporter)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if !professional(u) && reporter != actor {
		writeError(w, 404, "not_found", "Incident not found.")
		return
	}
	rows, err := a.incidentDB.Query(ctx, `SELECT id,author_id,visibility,body,created_at FROM incident_comments
        WHERE incident_id=$1 AND ($2 OR visibility='public') ORDER BY created_at,id LIMIT 500`, id, professional(u))
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer rows.Close()
	out := make([]any, 0)
	for rows.Next() {
		var cid, author pgtype.UUID
		var vis, text string
		var at time.Time
		if err = rows.Scan(&cid, &author, &vis, &text, &at); err != nil {
			handoffStorageError(w, err)
			return
		}
		out = append(out, map[string]any{"id": cid.String(), "author_id": author.String(), "visibility": vis, "text": text, "created_at": at})
	}
	if err = rows.Err(); err != nil {
		handoffStorageError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"comments": out})
}

func (a *HandoffAPI) listHistory(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeError(w, 400, "invalid_request", "Unexpected query.")
		return
	}
	u := a.user(r)
	if !professional(u) {
		writeError(w, 403, "forbidden", "Professional role required.")
		return
	}
	id, ok := incidentPath(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var exists bool
	if err := a.incidentDB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM incidents WHERE id=$1 AND (archived_at IS NULL OR $2))`, id, u.Role == "admin").Scan(&exists); err != nil {
		handoffStorageError(w, err)
		return
	}
	if !exists {
		writeError(w, 404, "not_found", "Incident not found.")
		return
	}
	rows, err := a.incidentDB.Query(ctx, `SELECT id,actor_id,action,reason,details,created_at
        FROM incident_actions WHERE incident_id=$1 ORDER BY created_at,id LIMIT 500`, id)
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	defer rows.Close()
	out := make([]any, 0)
	for rows.Next() {
		var aid pgtype.UUID
		var actor pgtype.UUID
		var action string
		var reason *string
		var details []byte
		var at time.Time
		if err = rows.Scan(&aid, &actor, &action, &reason, &details, &at); err != nil {
			handoffStorageError(w, err)
			return
		}
		var actorID any
		if actor.Valid {
			actorID = actor.String()
		}
		out = append(out, map[string]any{"id": aid.String(), "actor_id": actorID, "action": action, "reason": reason, "details": json.RawMessage(details), "created_at": at})
	}
	if err = rows.Err(); err != nil {
		handoffStorageError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"history": out})
}

// RunAutoClose is a separately triggered maintenance action. It is deliberately
// not tied to any HTTP request or browser session. Only overdue resolved cases
// are closed; the entire batch (including action history) commits atomically.
func RunAutoClose(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT i.id,i.reporter_id FROM incidents i
       JOIN incident_resolutions r ON r.incident_id=i.id
       WHERE i.status='resolved' AND i.archived_at IS NULL
         AND r.resolved_at IS NOT NULL AND r.resolved_at<=now()-INTERVAL '7 days'
       ORDER BY r.resolved_at LIMIT 100 FOR UPDATE OF i SKIP LOCKED`)
	if err != nil {
		return err
	}
	type pending struct{ id, reporter pgtype.UUID }
	items := make([]pending, 0)
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.id, &p.reporter); err != nil {
			rows.Close()
			return err
		}
		items = append(items, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range items {
		if _, err = tx.Exec(ctx, `UPDATE incidents SET status='closed',updated_at=now() WHERE id=$1`, p.id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO incident_actions(incident_id,actor_id,action,reason,details)
            VALUES($1,NULL,'status_changed','Automatic closure after seven days',$2)`, p.id, `{"from":"resolved","to":"closed"}`); err != nil {
			return err
		}
		if err = notifyIncident(ctx, tx, p.reporter, p.id, "status_changed", "Your incident was automatically closed."); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
