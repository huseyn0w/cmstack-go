-- name: CreatePost :one
INSERT INTO posts (
    title, slug, excerpt, body, status, published_at, scheduled_at,
    author_id, reading_time
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpdatePost :one
UPDATE posts
SET title = $2,
    slug = $3,
    excerpt = $4,
    body = $5,
    status = $6,
    published_at = $7,
    scheduled_at = $8,
    reading_time = $9,
    updated_at = now()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: GetPostByID :one
SELECT * FROM posts WHERE id = $1;

-- name: GetActivePostByID :one
SELECT * FROM posts WHERE id = $1 AND deleted_at IS NULL;

-- name: GetPublishedPostBySlug :one
SELECT * FROM posts
WHERE slug = $1 AND status = 'PUBLISHED' AND deleted_at IS NULL;

-- name: CountPostsBySlug :one
SELECT count(*) FROM posts
WHERE slug = $1 AND id <> $2;

-- name: ListPosts :many
SELECT * FROM posts
WHERE deleted_at IS NULL
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('author_id')::uuid IS NULL OR author_id = sqlc.narg('author_id')::uuid)
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountPosts :one
SELECT count(*) FROM posts
WHERE deleted_at IS NULL
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('author_id')::uuid IS NULL OR author_id = sqlc.narg('author_id')::uuid);

-- name: ListTrashedPosts :many
SELECT * FROM posts
WHERE deleted_at IS NOT NULL
ORDER BY deleted_at DESC
LIMIT $1 OFFSET $2;

-- name: CountTrashedPosts :one
SELECT count(*) FROM posts WHERE deleted_at IS NOT NULL;

-- name: ListPublishedPosts :many
SELECT * FROM posts
WHERE status = 'PUBLISHED' AND deleted_at IS NULL
ORDER BY published_at DESC NULLS LAST, created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountPublishedPosts :one
SELECT count(*) FROM posts WHERE status = 'PUBLISHED' AND deleted_at IS NULL;

-- name: ListPublishedPostsByAuthor :many
SELECT * FROM posts
WHERE author_id = $1 AND status = 'PUBLISHED' AND deleted_at IS NULL
ORDER BY published_at DESC NULLS LAST, created_at DESC;

-- name: TrashPost :exec
UPDATE posts SET deleted_at = now(), updated_at = now()
WHERE id = $1 AND deleted_at IS NULL;

-- name: RestorePost :exec
UPDATE posts SET deleted_at = NULL, updated_at = now()
WHERE id = $1 AND deleted_at IS NOT NULL;

-- name: PermanentDeletePost :exec
DELETE FROM posts WHERE id = $1;

-- name: ListDueScheduledPostIDs :many
SELECT id FROM posts
WHERE status = 'DRAFT'
  AND scheduled_at IS NOT NULL
  AND scheduled_at <= $1
  AND deleted_at IS NULL
ORDER BY scheduled_at;

-- name: LikePost :execrows
INSERT INTO post_likes (post_id, user_id)
VALUES ($1, $2)
ON CONFLICT (post_id, user_id) DO NOTHING;

-- name: UnlikePost :execrows
DELETE FROM post_likes WHERE post_id = $1 AND user_id = $2;

-- name: HasLiked :one
SELECT EXISTS (
    SELECT 1 FROM post_likes WHERE post_id = $1 AND user_id = $2
);

-- name: SetPostLikeCount :exec
UPDATE posts SET like_count = (
    SELECT count(*) FROM post_likes WHERE post_id = $1
)
WHERE id = $1;
