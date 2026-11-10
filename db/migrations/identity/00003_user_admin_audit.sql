-- +goose Up
CREATE TABLE identity_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id UUID NOT NULL,
    subject_id UUID NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('user_created','user_role_changed','user_blocked','user_unblocked','user_password_reset')),
    details JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(details) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX identity_audit_latest ON identity_audit (created_at DESC, id DESC);
GRANT SELECT, INSERT ON identity_audit TO go_identity;
-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM identity_audit) THEN
        RAISE EXCEPTION 'identity audit history exists; refusing destructive downgrade';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP TABLE identity_audit;
