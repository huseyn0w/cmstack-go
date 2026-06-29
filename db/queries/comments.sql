-- name: CreateComment :one
INSERT INTO comments (
    post_id, parent_id, author_user_id, author_name, author_email,
    author_ip, body, status
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetCommentByID :one
SELECT * FROM comments WHERE id = $1;

-- name: GetApprovedCommentByID :one
SELECT * FROM comments
WHERE id = $1 AND post_id = $2 AND status = 'APPROVED';

-- name: ListApprovedCommentsForPost :many
SELECT * FROM comments
WHERE post_id = $1 AND status = 'APPROVED'
ORDER BY created_at ASC;

-- name: ListCommentsForModeration :many
SELECT * FROM comments
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountCommentsForModeration :one
SELECT count(*) FROM comments
WHERE (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text);

-- name: CountCommentsByStatus :many
SELECT status, count(*) AS total
FROM comments
GROUP BY status;

-- name: UpdateCommentStatus :one
UPDATE comments
SET status = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateCommentBody :one
UPDATE comments
SET body = $2,
    status = $3,
    edited_at = now(),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteComment :exec
DELETE FROM comments WHERE id = $1;
