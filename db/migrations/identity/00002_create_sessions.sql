-- +goose Up
-- Exact storage schema required by SCS/pgxstore. SCS owns session serialization.
CREATE TABLE sessions (
    token TEXT PRIMARY KEY,
    data BYTEA NOT NULL,
    expiry TIMESTAMPTZ NOT NULL
);

CREATE INDEX sessions_expiry_idx ON sessions (expiry);
GRANT SELECT, INSERT, UPDATE, DELETE ON sessions TO go_identity;

-- +goose Down
DROP TABLE sessions;
