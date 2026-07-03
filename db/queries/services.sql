-- name: CreateService :one
INSERT INTO services (
    title, slug, summary, body, price, area_served, status, published_at, reading_time,
    meta_title, meta_description, canonical_url, noindex
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: UpdateService :one
UPDATE services
SET title = $2,
    slug = $3,
    summary = $4,
    body = $5,
    price = $6,
    area_served = $7,
    status = $8,
    published_at = $9,
    reading_time = $10,
    meta_title = $11,
    meta_description = $12,
    canonical_url = $13,
    noindex = $14,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: GetServiceByID :one
SELECT * FROM services WHERE id = $1;

-- name: GetActiveServiceByID :one
SELECT * FROM services WHERE id = $1 AND deleted_at IS NULL;

-- name: GetPublishedServiceBySlug :one
SELECT * FROM services
WHERE slug = $1 AND status = 'PUBLISHED' AND deleted_at IS NULL;

-- name: CountServicesBySlug :one
SELECT count(*) FROM services
WHERE slug = $1 AND id <> $2;

-- name: ListServices :many
SELECT * FROM services
WHERE deleted_at IS NULL
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountServices :one
SELECT count(*) FROM services
WHERE deleted_at IS NULL
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text);

-- name: ListTrashedServices :many
SELECT * FROM services
WHERE deleted_at IS NOT NULL
ORDER BY deleted_at DESC
LIMIT $1 OFFSET $2;

-- name: CountTrashedServices :one
SELECT count(*) FROM services WHERE deleted_at IS NOT NULL;

-- name: ListPublishedServices :many
SELECT * FROM services
WHERE status = 'PUBLISHED' AND deleted_at IS NULL
ORDER BY published_at DESC NULLS LAST, created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountPublishedServices :one
SELECT count(*) FROM services WHERE status = 'PUBLISHED' AND deleted_at IS NULL;

-- name: TrashService :exec
UPDATE services SET deleted_at = now(), updated_at = now()
WHERE id = $1 AND deleted_at IS NULL;

-- name: RestoreService :exec
UPDATE services SET deleted_at = NULL, updated_at = now()
WHERE id = $1 AND deleted_at IS NOT NULL;

-- name: PermanentDeleteService :exec
DELETE FROM services WHERE id = $1;

-- name: ListServiceFAQs :many
SELECT * FROM service_faqs
WHERE service_id = $1
ORDER BY position ASC, created_at ASC;

-- name: CreateServiceFAQ :one
INSERT INTO service_faqs (service_id, question, answer, position)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: DeleteServiceFAQs :exec
DELETE FROM service_faqs WHERE service_id = $1;
