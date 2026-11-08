-- +goose Up
-- Identity users live on another PostgreSQL instance: their UUIDs are stable
-- references, not cross-database foreign keys. No AI/job schema is defined here.
CREATE TABLE incidents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id UUID NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    affected_system TEXT,
    status TEXT NOT NULL DEFAULT 'submitted',
    category TEXT,
    severity TEXT,
    assigned_analyst_id UUID,
    resolution_note TEXT,
    archived_at TIMESTAMPTZ,
    archive_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT incidents_title_valid CHECK (
        char_length(title) BETWEEN 1 AND 200 AND
        title !~ E'^[[:space:]\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*$'
    ),
    CONSTRAINT incidents_description_valid CHECK (
        char_length(description) BETWEEN 1 AND 10000 AND
        description !~ E'^[[:space:]\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]*$'
    ),
    CONSTRAINT incidents_affected_system_length CHECK (char_length(affected_system) <= 500),
    CONSTRAINT incidents_status_valid CHECK (status IN (
        'submitted', 'triage', 'investigating', 'awaiting_reporter', 'resolved', 'closed', 'false_positive'
    )),
    CONSTRAINT incidents_category_valid CHECK (category IN (
        'phishing', 'malware', 'account_compromise', 'data_exposure', 'lost_device',
        'policy_violation', 'service_disruption', 'other'
    )),
    CONSTRAINT incidents_severity_valid CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    CONSTRAINT incidents_resolution_required CHECK (
        status NOT IN ('resolved', 'closed') OR
        (resolution_note IS NOT NULL AND btrim(resolution_note) <> '')
    ),
    CONSTRAINT incidents_archive_pair CHECK (
        (archived_at IS NULL AND archive_reason IS NULL) OR
        (archived_at IS NOT NULL AND archive_reason IS NOT NULL AND btrim(archive_reason) <> '')
    )
);

CREATE INDEX incidents_queue_idx ON incidents (created_at DESC, id DESC) WHERE archived_at IS NULL;
CREATE INDEX incidents_reporter_queue_idx ON incidents (reporter_id, created_at DESC, id DESC) WHERE archived_at IS NULL;

-- This increment has no update/delete API. Grant only submitted input columns;
-- later workflow migrations must explicitly grant their mutable columns.
GRANT SELECT ON incidents TO go_incidents;
GRANT INSERT (reporter_id, title, description, affected_system) ON incidents TO go_incidents;

-- +goose Down
DROP TABLE incidents;
