-- name: EnqueueOutbox :exec
INSERT INTO outbox (event_name, payload)
VALUES ($1, $2);

-- name: FetchUnprocessedOutbox :many
SELECT id, event_name, payload, created_at, processed_at
FROM outbox
WHERE processed_at IS NULL
ORDER BY created_at
LIMIT $1
FOR UPDATE SKIP LOCKED;

-- name: MarkOutboxProcessed :exec
UPDATE outbox
SET processed_at = now()
WHERE id = $1;
