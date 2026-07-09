-- name: CreateAPIToken :one
INSERT INTO api_tokens (
    user_id, name, token_hash, last_four, expires_at
)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAPITokenByHash :one
SELECT * FROM api_tokens
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now());

-- name: TouchAPIToken :exec
UPDATE api_tokens
SET last_used_at = now()
WHERE id = $1;

-- name: ListAPITokensByUser :many
SELECT * FROM api_tokens
WHERE user_id = $1
ORDER BY created_at DESC, id DESC;

-- name: RevokeAPIToken :exec
UPDATE api_tokens
SET revoked_at = now()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;
