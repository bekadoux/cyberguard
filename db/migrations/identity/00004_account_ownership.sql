-- +goose Up
-- The singleton is deliberately empty on upgrade. An operator must explicitly
-- initialize it. The composite FK prevents the owner from ceasing to be active
-- or an admin even if a future user-management caller forgets that check.
ALTER TABLE users ADD CONSTRAINT users_owner_eligibility_key UNIQUE (id, role, is_blocked);

CREATE TABLE account_ownership (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    user_id UUID NOT NULL,
    required_role TEXT NOT NULL DEFAULT 'admin' CHECK (required_role = 'admin'),
    required_blocked BOOLEAN NOT NULL DEFAULT FALSE CHECK (NOT required_blocked),
    FOREIGN KEY (user_id, required_role, required_blocked) REFERENCES users(id, role, is_blocked)
);
GRANT SELECT, UPDATE (user_id) ON account_ownership TO go_identity;

ALTER TABLE identity_audit DROP CONSTRAINT identity_audit_action_check;
ALTER TABLE identity_audit ADD CONSTRAINT identity_audit_action_check CHECK (action IN (
    'user_created', 'user_role_changed', 'user_blocked', 'user_unblocked', 'user_password_reset',
    'ownership_initialized', 'ownership_transferred'
));

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM account_ownership)
       OR EXISTS (SELECT 1 FROM identity_audit WHERE action IN ('ownership_initialized', 'ownership_transferred')) THEN
        RAISE EXCEPTION 'ownership history exists; refusing destructive downgrade';
    END IF;
END;
$$;
-- +goose StatementEnd
ALTER TABLE identity_audit DROP CONSTRAINT identity_audit_action_check;
ALTER TABLE identity_audit ADD CONSTRAINT identity_audit_action_check CHECK (action IN (
    'user_created', 'user_role_changed', 'user_blocked', 'user_unblocked', 'user_password_reset'
));
DROP TABLE account_ownership;
ALTER TABLE users DROP CONSTRAINT users_owner_eligibility_key;
