-- name: DemoIncidentBatchExists :one
SELECT EXISTS (SELECT 1 FROM demo_incident_batch);

-- name: ClaimDemoIncidentBatch :execrows
INSERT INTO demo_incident_batch (created_by) VALUES ($1)
ON CONFLICT (singleton) DO NOTHING;
