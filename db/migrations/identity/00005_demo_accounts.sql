-- +goose Up
-- Written only by explicit provisioning. Never infer provenance from usernames.
CREATE TABLE demo_accounts (
    user_id UUID PRIMARY KEY REFERENCES users(id)
);
GRANT SELECT ON demo_accounts TO go_identity;

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM demo_accounts) THEN
        RAISE EXCEPTION 'demo account provenance exists; refusing destructive downgrade';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP TABLE demo_accounts;
