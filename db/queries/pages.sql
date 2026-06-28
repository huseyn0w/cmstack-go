-- name: CreatePage :one
INSERT INTO pages (
    title, slug, body, status, published_at, parent_id, template, reading_time
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: UpdatePage :one
UPDATE pages
SET title = $2,
    slug = $3,
    body = $4,
    status = $5,
    published_at = $6,
    parent_id = $7,
    template = $8,
    reading_time = $9,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: GetPageByID :one
SELECT * FROM pages WHERE id = $1;

-- name: GetActivePageByID :one
SELECT * FROM pages WHERE id = $1 AND deleted_at IS NULL;

-- name: GetPublishedPageBySlug :one
SELECT * FROM pages
WHERE slug = $1 AND status = 'PUBLISHED' AND deleted_at IS NULL;

-- name: CountPagesBySlug :one
SELECT count(*) FROM pages
WHERE slug = $1 AND id <> $2;

-- name: ListPages :many
SELECT * FROM pages
WHERE deleted_at IS NULL
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY title ASC
LIMIT $1 OFFSET $2;

-- name: CountPages :one
SELECT count(*) FROM pages
WHERE deleted_at IS NULL
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text);

-- name: ListAllActivePages :many
SELECT * FROM pages
WHERE deleted_at IS NULL
ORDER BY title ASC;

-- name: ListChildPages :many
SELECT * FROM pages
WHERE parent_id = $1 AND deleted_at IS NULL
ORDER BY title ASC;

-- name: ListTrashedPages :many
SELECT * FROM pages
WHERE deleted_at IS NOT NULL
ORDER BY deleted_at DESC
LIMIT $1 OFFSET $2;

-- name: CountTrashedPages :one
SELECT count(*) FROM pages WHERE deleted_at IS NOT NULL;

-- name: ListPublishedPages :many
SELECT * FROM pages
WHERE status = 'PUBLISHED' AND deleted_at IS NULL
ORDER BY title ASC
LIMIT $1 OFFSET $2;

-- name: CountPublishedPages :one
SELECT count(*) FROM pages WHERE status = 'PUBLISHED' AND deleted_at IS NULL;

-- name: TrashPage :exec
UPDATE pages SET deleted_at = now(), updated_at = now()
WHERE id = $1 AND deleted_at IS NULL;

-- name: RestorePage :exec
UPDATE pages SET deleted_at = NULL, updated_at = now()
WHERE id = $1 AND deleted_at IS NOT NULL;

-- name: PermanentDeletePage :exec
DELETE FROM pages WHERE id = $1;
