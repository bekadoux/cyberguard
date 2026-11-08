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
SELECT id, reporter_id, title, status, category, severity, assigned_analyst_id, created_at, updated_at
FROM incidents
WHERE archived_at IS NULL
  AND (reporter_id = sqlc.arg(actor_id) OR sqlc.arg(read_company)::boolean)
  AND (sqlc.narg(status_filter)::text IS NULL OR status = sqlc.narg(status_filter)::text)
  AND (sqlc.narg(category_filter)::text IS NULL OR category = sqlc.narg(category_filter)::text)
  AND (sqlc.narg(before_created_at)::timestamptz IS NULL OR
       (created_at, id) < (sqlc.narg(before_created_at)::timestamptz, sqlc.narg(before_id)::uuid))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit);
