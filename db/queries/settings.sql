-- name: GetSetting :one
SELECT value FROM site_settings WHERE key = $1;

-- name: UpsertSetting :exec
INSERT INTO site_settings (key, value)
VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE
SET value = EXCLUDED.value,
    updated_at = now();

-- name: ListSettings :many
SELECT key, value FROM site_settings
ORDER BY key;
