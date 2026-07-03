-- name: CreatePost :one
INSERT INTO posts (
    title, slug, excerpt, body, status, published_at, scheduled_at,
    author_id, reading_time, meta_title, meta_description, canonical_url, noindex
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
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
    meta_title = $10,
    meta_description = $11,
    canonical_url = $12,
    noindex = $13,
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

-- name: GetPublishedPostsByIDs :many
-- Hydrate a set of post ids to their published, non-trashed rows. Used by the
-- taxonomy archives, which first resolve ordered ids then load the rows. Order
-- is re-applied in Go to preserve the archive's ranking.
SELECT * FROM posts
WHERE id = ANY(@ids::uuid[])
  AND status = 'PUBLISHED'
  AND deleted_at IS NULL;

-- name: ListRelatedPublishedPosts :many
-- Posts sharing >=1 category OR tag with the given post (laravel parity).
-- Self is excluded; only published, non-trashed posts are considered. Results
-- are ranked by the number of shared taxonomy terms (most-related first), then
-- recency, and limited.
SELECT p.*, count(*) AS shared_count
FROM posts p
JOIN (
    SELECT pc.post_id AS related_post_id
    FROM post_categories pc
    WHERE pc.category_id IN (
        SELECT pcs.category_id FROM post_categories pcs WHERE pcs.post_id = $1
    )
    UNION ALL
    SELECT pt.post_id AS related_post_id
    FROM post_tags pt
    WHERE pt.tag_id IN (
        SELECT pts.tag_id FROM post_tags pts WHERE pts.post_id = $1
    )
) rel ON rel.related_post_id = p.id
WHERE p.id <> $1
  AND p.status = 'PUBLISHED'
  AND p.deleted_at IS NULL
GROUP BY p.id
ORDER BY shared_count DESC, p.published_at DESC NULLS LAST, p.created_at DESC
LIMIT $2;

-- name: ListPublishedPostsFiltered :many
-- Public blog listing with optional, combinable category + tag slug filters. A
-- NULL slug param means "no constraint on that axis"; both set means the post
-- must match BOTH (intersection). Drafts/trashed are always excluded.
SELECT p.* FROM posts p
WHERE p.status = 'PUBLISHED'
  AND p.deleted_at IS NULL
  AND (
    sqlc.narg('category_slug')::text IS NULL OR EXISTS (
        SELECT 1 FROM post_categories pc
        JOIN categories c ON c.id = pc.category_id
        WHERE pc.post_id = p.id AND c.slug = sqlc.narg('category_slug')::text
    )
  )
  AND (
    sqlc.narg('tag_slug')::text IS NULL OR EXISTS (
        SELECT 1 FROM post_tags pt
        JOIN tags t ON t.id = pt.tag_id
        WHERE pt.post_id = p.id AND t.slug = sqlc.narg('tag_slug')::text
    )
  )
ORDER BY p.published_at DESC NULLS LAST, p.created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountPublishedPostsFiltered :one
SELECT count(*) FROM posts p
WHERE p.status = 'PUBLISHED'
  AND p.deleted_at IS NULL
  AND (
    sqlc.narg('category_slug')::text IS NULL OR EXISTS (
        SELECT 1 FROM post_categories pc
        JOIN categories c ON c.id = pc.category_id
        WHERE pc.post_id = p.id AND c.slug = sqlc.narg('category_slug')::text
    )
  )
  AND (
    sqlc.narg('tag_slug')::text IS NULL OR EXISTS (
        SELECT 1 FROM post_tags pt
        JOIN tags t ON t.id = pt.tag_id
        WHERE pt.post_id = p.id AND t.slug = sqlc.narg('tag_slug')::text
    )
  );

-- name: SitemapPosts :many
-- Lightweight enumeration for the sitemap/llms indexes: no body/heavy fields.
SELECT slug, title, meta_title, meta_description, excerpt, updated_at
FROM posts
WHERE status = 'PUBLISHED' AND deleted_at IS NULL
ORDER BY updated_at DESC;
