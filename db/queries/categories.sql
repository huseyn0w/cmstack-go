-- name: CreateCategory :one
INSERT INTO categories (name, slug, description, parent_id)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateCategory :one
UPDATE categories
SET name = $2,
    slug = $3,
    description = $4,
    parent_id = $5,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetCategoryByID :one
SELECT * FROM categories WHERE id = $1;

-- name: GetCategoryBySlug :one
SELECT * FROM categories WHERE slug = $1;

-- name: CountCategoriesBySlug :one
SELECT count(*) FROM categories
WHERE slug = $1 AND id <> $2;

-- name: ListAllCategories :many
SELECT * FROM categories
ORDER BY name ASC;

-- name: ListChildCategories :many
SELECT * FROM categories
WHERE parent_id = $1
ORDER BY name ASC;

-- name: ListCategories :many
SELECT * FROM categories
ORDER BY name ASC
LIMIT $1 OFFSET $2;

-- name: CountCategories :one
SELECT count(*) FROM categories;

-- name: DeleteCategory :exec
DELETE FROM categories WHERE id = $1;

-- name: AttachPostCategory :exec
INSERT INTO post_categories (post_id, category_id)
VALUES ($1, $2)
ON CONFLICT (post_id, category_id) DO NOTHING;

-- name: DetachAllPostCategories :exec
DELETE FROM post_categories WHERE post_id = $1;

-- name: ListCategoriesForPost :many
SELECT c.* FROM categories c
JOIN post_categories pc ON pc.category_id = c.id
WHERE pc.post_id = $1
ORDER BY c.name ASC;

-- name: ListPublishedPostsInCategory :many
SELECT p.* FROM posts p
JOIN post_categories pc ON pc.post_id = p.id
WHERE pc.category_id = $1
  AND p.status = 'PUBLISHED'
  AND p.deleted_at IS NULL
ORDER BY p.published_at DESC NULLS LAST, p.created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountPublishedPostsInCategory :one
SELECT count(*) FROM posts p
JOIN post_categories pc ON pc.post_id = p.id
WHERE pc.category_id = $1
  AND p.status = 'PUBLISHED'
  AND p.deleted_at IS NULL;
