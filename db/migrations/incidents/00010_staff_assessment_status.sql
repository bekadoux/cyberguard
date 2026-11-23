-- +goose Up
-- The Go list reads only initial job status. No grants for result data or
-- operational fields (lease, claim token, or error code) are added.
GRANT SELECT (kind, status) ON assessment_jobs TO go_incidents;

-- +goose Down
REVOKE SELECT (kind, status) ON assessment_jobs FROM go_incidents;
