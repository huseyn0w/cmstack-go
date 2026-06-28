-- +goose Up

-- Fix 3: per-user session invalidation on password reset/change. Sessions store
-- the password_changed_at they were minted under; the CurrentUser middleware
-- rejects any session older than the user's current value, forcing a global
-- logout after a credential change. Defaults to the row's creation time so all
-- existing sessions remain valid until the next change.
-- +goose StatementBegin
ALTER TABLE users
    ADD COLUMN password_changed_at TIMESTAMPTZ NOT NULL DEFAULT now();
-- +goose StatementEnd

-- Fix 5: poison-event isolation in the outbox relay. attempts counts dispatch
-- failures per row; last_error records the most recent failure; rows that
-- exhaust max attempts are dead-lettered by stamping processed_at with a
-- failed marker, so a single poison event cannot block its siblings or loop
-- forever.
-- +goose StatementBegin
ALTER TABLE outbox
    ADD COLUMN attempts   INT  NOT NULL DEFAULT 0,
    ADD COLUMN last_error TEXT,
    ADD COLUMN failed_at  TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm1.1', updated_at = now() WHERE key = 'cmstack_version';
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
ALTER TABLE outbox
    DROP COLUMN IF EXISTS failed_at,
    DROP COLUMN IF EXISTS last_error,
    DROP COLUMN IF EXISTS attempts;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE users
    DROP COLUMN IF EXISTS password_changed_at;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm1', updated_at = now() WHERE key = 'cmstack_version';
-- +goose StatementEnd
