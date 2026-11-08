-- name: CreateUser :one
INSERT INTO users (username, password_hash, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateDemoUser :one
INSERT INTO users (username, password_hash, role)
VALUES ($1, $2, $3)
ON CONFLICT (username) DO NOTHING
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: SetUserPassword :one
UPDATE users
SET password_hash = $2, auth_version = auth_version + 1, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetUserBlocked :one
UPDATE users
SET is_blocked = $2, auth_version = auth_version + 1, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetUserRole :one
UPDATE users
SET role = $2, auth_version = auth_version + 1, updated_at = now()
WHERE id = $1
RETURNING *;
