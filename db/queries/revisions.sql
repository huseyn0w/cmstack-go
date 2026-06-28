-- name: CreateRevision :one
INSERT INTO revisions (entity_type, entity_id, snapshot, author_id)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListRevisions :many
SELECT * FROM revisions
WHERE entity_type = $1 AND entity_id = $2
ORDER BY created_at DESC;

-- name: GetRevision :one
SELECT * FROM revisions WHERE id = $1;
