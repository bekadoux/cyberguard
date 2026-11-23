-- name: LockDemoIncidentSelection :exec
SELECT pg_advisory_xact_lock(12473, 9002);

-- name: ListUsedDemoScenarios :many
SELECT scenario_id FROM demo_incident_scenarios;

-- name: UseDemoScenario :exec
INSERT INTO demo_incident_scenarios (scenario_id, incident_id) VALUES ($1, $2);
