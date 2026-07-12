-- +goose Up

-- comments (M5): threaded, moderated comments on posts. Guests supply
-- name+email; signed-in users are attributed via author_user_id (which enables
-- the author self-edit window). All comments start PENDING and are public only
-- once APPROVED. parent_id threads a reply under another comment on the SAME
-- post; the service enforces that a reply's parent is an APPROVED comment on the
-- same post. author_email / author_ip are PII and are NEVER serialized to the
-- public payload (the public serializer strips them).
-- +goose StatementBegin
CREATE TABLE comments (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id        UUID NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    parent_id      UUID REFERENCES comments (id) ON DELETE CASCADE,
    author_user_id UUID REFERENCES users (id) ON DELETE SET NULL,
    author_name    TEXT NOT NULL,
    author_email   TEXT NOT NULL DEFAULT '',
    author_ip      TEXT NOT NULL DEFAULT '',
    body           TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'PENDING',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    edited_at      TIMESTAMPTZ
);
-- +goose StatementEnd

-- Per-post approved fetch (the public thread) reads (post_id, status) ordered by
-- created_at; the partial index keeps the hot public read tight.
-- +goose StatementBegin
CREATE INDEX comments_post_approved_idx ON comments (post_id, created_at)
    WHERE status = 'APPROVED';
-- +goose StatementEnd

-- Moderation queue lists by status, newest first.
-- +goose StatementBegin
CREATE INDEX comments_status_idx ON comments (status, created_at DESC);
-- +goose StatementEnd

-- Reply lookups + cascade integrity walk parent_id.
-- +goose StatementBegin
CREATE INDEX comments_parent_idx ON comments (parent_id);
-- +goose StatementEnd

-- A signed-in user's own comments (self-edit window lookups).
-- +goose StatementBegin
CREATE INDEX comments_author_user_idx ON comments (author_user_id)
    WHERE author_user_id IS NOT NULL;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm5', updated_at = now() WHERE key = 'agentic_cms_version';
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS comments;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm4', updated_at = now() WHERE key = 'agentic_cms_version';
-- +goose StatementEnd
