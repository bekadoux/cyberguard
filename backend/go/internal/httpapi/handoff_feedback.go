package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bekadoux/cyberguard/backend/go/internal/incidents"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// reviewAIFeedback commits the analyst's decision and confirmed category in
// the same incident database transaction. Raw model output remains advisory.
// One review per assessment job is enforced by the incident_actions index.
func (a *HandoffAPI) reviewAIFeedback(w http.ResponseWriter, r *http.Request) {
	u := a.user(r)
	if !professional(u) {
		writeError(w, http.StatusForbidden, "forbidden", "Professional role required.")
		return
	}
	id, ok := incidentPath(w, r)
	if !ok {
		return
	}
	var body struct {
		JobID    string  `json:"job_id"`
		Decision string  `json:"decision"`
		Category *string `json:"category"`
		Reason   string  `json:"reason"`
	}
	if !decodeHandoffJSON(w, r, &body) {
		return
	}
	job, err := incidents.ParseID(body.JobID)
	if err != nil || !validHandoffText(body.Reason, 2000, false) ||
		!validFeedbackInput(body.Decision, body.Category, body.Reason) {
		writeError(w, 400, "invalid_request", "Invalid AI feedback input.")
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
		writeError(w, 409, "conflict", "Incident is already terminal.")
		return
	}
	var source string
	var suggested *string
	// Ensure the job belongs to this incident and has a stored result. The
	// model's suggested_category is read only for equality checking; it is
	// never silently applied to the incident.
	err = tx.QueryRow(ctx, `SELECT ar.source_kind, ar.assessment #>> '{triage,suggested_category}'
        FROM assessment_results ar JOIN assessment_jobs aj ON aj.id=ar.job_id
        WHERE ar.job_id=$1 AND ar.incident_id=$2 AND aj.incident_id=$2`, job, id).Scan(&source, &suggested)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "not_found", "Assessment result not found.")
		return
	}
	if err != nil {
		handoffStorageError(w, err)
		return
	}
	if source == "fake" {
		writeError(w, 409, "conflict", "Synthetic assessments cannot be approved.")
		return
	}
	if body.Decision == "accepted" && (suggested == nil || body.Category == nil || *suggested != *body.Category) {
		writeError(w, 400, "invalid_request", "Accepted category must match model suggestion.")
		return
	}
	if body.Decision == "corrected" && suggested != nil && body.Category != nil && *suggested == *body.Category {
		writeError(w, 400, "invalid_request", "Use accepted when the category matches.")
		return
	}
	if body.Decision != "rejected" {
		if _, err = tx.Exec(ctx, `UPDATE incidents SET category=$2,updated_at=now() WHERE id=$1`, id, body.Category); err != nil {
			handoffStorageError(w, err)
			return
		}
	}
	err = recordIncidentAction(ctx, tx, id, actor, "ai_feedback_submitted", body.Reason, map[string]any{
		"job_id": body.JobID, "decision": body.Decision, "confirmed_category": body.Category,
	})
	if err != nil {
		var pgerr *pgconn.PgError
		if errors.As(err, &pgerr) && pgerr.Code == "23505" {
			writeError(w, 409, "conflict", "Feedback for this assessment was already saved.")
		} else {
			handoffStorageError(w, err)
		}
		return
	}
	if !commitOrError(w, tx, ctx) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"recorded": true})
}

func validFeedbackInput(decision string, category *string, reason string) bool {
	if decision != "accepted" && decision != "corrected" && decision != "rejected" {
		return false
	}
	if decision == "rejected" {
		return category == nil && strings.TrimSpace(reason) != ""
	}
	if category == nil || !incidents.ValidCategory(*category) {
		return false
	}
	return decision != "corrected" || strings.TrimSpace(reason) != ""
}
