-- name: CreateUser :one
INSERT INTO users (email, username, password_hash, name, role_id, email_verified_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: CountUsersByEmail :one
SELECT count(*) FROM users WHERE email = $1;

-- name: SetUserPassword :exec
UPDATE users
SET password_hash = $2, updated_at = now()
WHERE id = $1;

-- name: MarkEmailVerified :exec
UPDATE users
SET email_verified_at = now(), updated_at = now()
WHERE id = $1;
