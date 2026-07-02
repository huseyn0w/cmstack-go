-- +goose Up

-- post_translations: the per-locale CONTENT overlay for posts (M7b-1). The base
-- `posts` row KEEPS its title/excerpt/body as the DEFAULT-locale (en) content;
-- this table stores the translated title/excerpt/body for NON-default locales
-- (de/ru). Reads resolve: active locale == default -> base row; else -> the
-- translation row for that locale, FALLING BACK to the base row for any missing
-- (empty/absent) translation field. Structural fields (slug/status/author/
-- schedule/taxonomy) stay shared on the base row and are NOT per-locale.
--
--   TODO(M8): per-locale meta_title/meta_description will join here (SEO fields
--   translate alongside the content).
-- +goose StatementBegin
CREATE TABLE post_translations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id    UUID NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    locale     TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    excerpt    TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (post_id, locale)
);
-- +goose StatementEnd

-- The overlay read + all-locales fetch join on post_id.
-- +goose StatementBegin
CREATE INDEX post_translations_post_idx ON post_translations (post_id);
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm7b', updated_at = now() WHERE key = 'cmstack_version';
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS post_translations;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm7a', updated_at = now() WHERE key = 'cmstack_version';
-- +goose StatementEnd
