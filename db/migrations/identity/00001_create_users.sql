-- +goose Up
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL,
    is_blocked BOOLEAN NOT NULL DEFAULT FALSE,
    auth_version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_username_format CHECK (username ~ '^[a-z0-9][a-z0-9_.-]{2,63}$'),
    CONSTRAINT users_password_hash_format CHECK (
        password_hash LIKE '$argon2id$%' AND length(password_hash) <= 1024
    ),
    CONSTRAINT users_role_valid CHECK (role IN ('employee', 'analyst', 'admin')),
    CONSTRAINT users_auth_version_positive CHECK (auth_version > 0)
);

-- No DELETE: blocking preserves stable user references in the incident database.
GRANT SELECT, INSERT, UPDATE ON users TO go_identity;

-- +goose Down
DROP TABLE users;
