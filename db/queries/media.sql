-- name: CreateMedia :one
INSERT INTO media (
    storage_key, original_filename, mime, size_bytes,
    width, height, alt, title, caption, uploaded_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetMediaByID :one
SELECT * FROM media WHERE id = $1;

-- name: ListMedia :many
SELECT * FROM media
ORDER BY created_at DESC, id DESC
LIMIT $1 OFFSET $2;

-- name: CountMedia :one
SELECT count(*) FROM media;

-- name: UpdateMediaMetadata :one
UPDATE media
SET alt = $2,
    title = $3,
    caption = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteMedia :exec
DELETE FROM media WHERE id = $1;

-- name: CreateThumbnail :one
INSERT INTO media_thumbnails (media_id, variant, storage_key, width, height)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (media_id, variant)
DO UPDATE SET storage_key = EXCLUDED.storage_key,
              width = EXCLUDED.width,
              height = EXCLUDED.height
RETURNING *;

-- name: ListThumbnailsForMedia :many
SELECT * FROM media_thumbnails
WHERE media_id = $1
ORDER BY variant;

-- name: ListThumbnailsForMediaIDs :many
SELECT * FROM media_thumbnails
WHERE media_id = ANY(@media_ids::uuid[])
ORDER BY media_id, variant;
