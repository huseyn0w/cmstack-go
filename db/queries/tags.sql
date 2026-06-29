-- name: CreateTag :one
INSERT INTO tags (name, slug)
VALUES ($1, $2)
RETURNING *;

-- name: UpdateTag :one
UPDATE tags
SET name = $2,
    slug = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetTagByID :one
SELECT * FROM tags WHERE id = $1;

-- name: GetTagBySlug :one
SELECT * FROM tags WHERE slug = $1;

-- name: CountTagsBySlug :one
SELECT count(*) FROM tags
WHERE slug = $1 AND id <> $2;

-- name: ListAllTags :many
SELECT * FROM tags
ORDER BY name ASC;

-- name: ListTags :many
SELECT * FROM tags
ORDER BY name ASC
LIMIT $1 OFFSET $2;

-- name: CountTags :one
SELECT count(*) FROM tags;

-- name: DeleteTag :exec
DELETE FROM tags WHERE id = $1;

-- name: AttachPostTag :exec
INSERT INTO post_tags (post_id, tag_id)
VALUES ($1, $2)
ON CONFLICT (post_id, tag_id) DO NOTHING;

-- name: DetachAllPostTags :exec
DELETE FROM post_tags WHERE post_id = $1;

-- name: ListTagsForPost :many
SELECT t.* FROM tags t
JOIN post_tags pt ON pt.tag_id = t.id
WHERE pt.post_id = $1
ORDER BY t.name ASC;

-- name: ListPublishedPostsInTag :many
SELECT p.* FROM posts p
JOIN post_tags pt ON pt.post_id = p.id
WHERE pt.tag_id = $1
  AND p.status = 'PUBLISHED'
  AND p.deleted_at IS NULL
ORDER BY p.published_at DESC NULLS LAST, p.created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountPublishedPostsInTag :one
SELECT count(*) FROM posts p
JOIN post_tags pt ON pt.post_id = p.id
WHERE pt.tag_id = $1
  AND p.status = 'PUBLISHED'
  AND p.deleted_at IS NULL;
