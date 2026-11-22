-- +goose Up
-- The singleton is both the write-once batch claim and its administrative audit.
CREATE TABLE demo_incident_batch (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
GRANT SELECT, INSERT ON demo_incident_batch TO go_incidents;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM demo_incident_batch) THEN
        RAISE EXCEPTION 'demo incident audit exists; refusing destructive downgrade';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP TABLE demo_incident_batch;
