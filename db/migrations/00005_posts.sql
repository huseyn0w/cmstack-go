-- +goose Up

-- posts: the first content type. SEO-meta fields and taxonomy (categories/tags
-- m2m) are intentionally NOT here — they are documented seams for later
-- milestones:
--   TODO(M8 SEO fields): meta_title, meta_description, canonical_url, noindex.
--   TODO(M3 categories/tags M2M): post_categories / post_tags join tables.
-- +goose StatementBegin
CREATE TABLE posts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title         TEXT NOT NULL,
    slug          TEXT NOT NULL UNIQUE,
    excerpt       TEXT NOT NULL DEFAULT '',
    body          TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'DRAFT',
    published_at  TIMESTAMPTZ,
    scheduled_at  TIMESTAMPTZ,
    author_id     UUID NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    reading_time  INT NOT NULL DEFAULT 0,
    like_count    INT NOT NULL DEFAULT 0,
    deleted_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- Public reads filter on (status, deleted_at); author listings on author_id.
-- +goose StatementBegin
CREATE INDEX posts_status_idx ON posts (status) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX posts_author_idx ON posts (author_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- The scheduler scans DRAFT rows with a due scheduled_at.
-- +goose StatementBegin
CREATE INDEX posts_scheduled_due_idx ON posts (scheduled_at)
    WHERE status = 'DRAFT' AND scheduled_at IS NOT NULL AND deleted_at IS NULL;
-- +goose StatementEnd

-- post_likes: one like per (post, user). The pk on both columns makes a second
-- like a no-op (ON CONFLICT DO NOTHING) so liking is idempotent.
-- +goose StatementBegin
CREATE TABLE post_likes (
    post_id    UUID NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, user_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX post_likes_user_idx ON post_likes (user_id);
-- +goose StatementEnd

-- revisions: the generic, content-type-agnostic snapshot store (kernel). Any
-- content type that wants history writes a JSONB snapshot of the prior state
-- here, keyed by (entity_type, entity_id), before each update.
-- +goose StatementBegin
CREATE TABLE revisions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type TEXT NOT NULL,
    entity_id   UUID NOT NULL,
    snapshot    JSONB NOT NULL,
    author_id   UUID REFERENCES users (id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX revisions_entity_idx ON revisions (entity_type, entity_id, created_at DESC);
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm2', updated_at = now() WHERE key = 'cmstack_version';
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS revisions;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS post_likes;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS posts;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm1', updated_at = now() WHERE key = 'cmstack_version';
-- +goose StatementEnd
