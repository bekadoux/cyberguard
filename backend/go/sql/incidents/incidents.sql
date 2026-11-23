-- name: CreateIncident :one
INSERT INTO incidents (reporter_id, title, description, affected_system)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetVisibleIncident :one
SELECT * FROM incidents
WHERE id = sqlc.arg(id)
  AND archived_at IS NULL
  AND (reporter_id = sqlc.arg(actor_id) OR sqlc.arg(read_company)::boolean);

-- name: ListVisibleIncidents :many
-- Ownership is applied before pagination, never by filtering an unrestricted page.
-- List projections deliberately exclude long descriptions/resolution text.
SELECT i.id, i.reporter_id, i.title, i.status, i.category, i.severity,
       i.assigned_analyst_id, i.created_at, i.updated_at, j.status AS ai_assessment_status
FROM incidents AS i
LEFT JOIN assessment_jobs AS j
  ON sqlc.arg(read_company)::boolean AND j.incident_id = i.id AND j.kind = 'initial_assessment'
WHERE i.archived_at IS NULL
  AND (i.reporter_id = sqlc.arg(actor_id) OR sqlc.arg(read_company)::boolean)
  AND (sqlc.narg(status_filter)::text IS NULL OR i.status = sqlc.narg(status_filter)::text)
  AND (sqlc.narg(category_filter)::text IS NULL OR i.category = sqlc.narg(category_filter)::text)
  AND (sqlc.narg(before_created_at)::timestamptz IS NULL OR
       (i.created_at, i.id) < (sqlc.narg(before_created_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY i.created_at DESC, i.id DESC
LIMIT sqlc.arg(page_limit);
