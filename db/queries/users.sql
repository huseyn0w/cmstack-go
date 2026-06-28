-- name: CreateUser :one
INSERT INTO users (email, username, password_hash, name, role_id, email_verified_at, avatar_url)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: CountUsersByEmail :one
SELECT count(*) FROM users WHERE email = $1;

-- name: CountUsersByUsername :one
SELECT count(*) FROM users WHERE username = $1;

-- name: UpdateUserProfile :one
UPDATE users
SET name = $2,
    bio = $3,
    website = $4,
    social_links = $5,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetUserAvatarPath :one
UPDATE users
SET avatar_path = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetUserPassword :exec
UPDATE users
SET password_hash = $2, password_changed_at = now(), updated_at = now()
WHERE id = $1;

-- name: MarkEmailVerified :exec
UPDATE users
SET email_verified_at = now(), updated_at = now()
WHERE id = $1;
