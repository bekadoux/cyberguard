package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MountAIWorkerContext provides only the context of a persisted job to a
// credentialed private service. Never expose /internal/* through Caddy.
func MountAIWorkerContext(router chi.Router, pool *pgxpool.Pool, serviceToken string) {
	if serviceToken == "" {
		return
	}
	router.Get("/internal/ai/jobs/{id}/context", func(w http.ResponseWriter, r *http.Request) {
		supplied := r.Header.Get("Authorization")
		expected := "Bearer " + serviceToken
		if len(supplied) != len(expected) ||
			subtle.ConstantTimeCompare([]byte(supplied), []byte(expected)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Service authentication is required.")
			return
		}
		id := chi.URLParam(r, "id")
		var uuid pgtype.UUID
		if len(id) != 36 || uuid.Scan(id) != nil || !uuid.Valid || uuid.String() != id {
			writeError(w, http.StatusNotFound, "not_found", "Job not found.")
			return
		}
		ctx, cancel := contextWithTimeout(r, 3*time.Second)
		defer cancel()
		var incidentID pgtype.UUID
		var title, description string
		var affected *string
		var jobRevision, currentRevision int64
		err := pool.QueryRow(ctx, `
            SELECT i.id, i.title, i.description, i.affected_system,
                   j.incident_revision, i.context_revision
            FROM assessment_jobs j JOIN incidents i ON i.id = j.incident_id
            WHERE j.id = $1 AND i.archived_at IS NULL`, uuid).Scan(
			&incidentID, &title, &description, &affected, &jobRevision, &currentRevision)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Job not found.")
			return
		}
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "incidents_unavailable", "Incident context unavailable.")
			return
		}
		if currentRevision != jobRevision {
			writeError(w, http.StatusConflict, "stale_context", "Incident context changed since scheduling.")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"job_id": id, "incident_id": incidentID.String(),
			"incident_revision": jobRevision, "title": title,
			"description": description, "affected_system": affected,
		})
	})
}

func contextWithTimeout(r *http.Request, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), timeout)
}
