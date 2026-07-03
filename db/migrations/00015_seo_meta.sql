-- +goose Up

-- Per-content SEO metadata (M8-1). Two axes of fields land on the content types:
--   * meta_title / meta_description are TRANSLATABLE (per-locale overlay with base
--     fallback): they live on BOTH the base row (the default-locale en value) AND
--     the *_translations tables (the non-default overlay), resolved with the same
--     COALESCE(NULLIF(t.field,''), base.field) rule as the other content fields.
--   * canonical_url / noindex are STRUCTURAL (NOT per-locale): they live only on the
--     base row and are shared across every locale (passed straight through the
--     overlay reads from the base row).
-- All four default to their zero value so existing rows need no backfill.

-- +goose StatementBegin
ALTER TABLE posts
    ADD COLUMN meta_title       TEXT NOT NULL DEFAULT '',
    ADD COLUMN meta_description TEXT NOT NULL DEFAULT '',
    ADD COLUMN canonical_url    TEXT NOT NULL DEFAULT '',
    ADD COLUMN noindex          BOOLEAN NOT NULL DEFAULT false;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE pages
    ADD COLUMN meta_title       TEXT NOT NULL DEFAULT '',
    ADD COLUMN meta_description TEXT NOT NULL DEFAULT '',
    ADD COLUMN canonical_url    TEXT NOT NULL DEFAULT '',
    ADD COLUMN noindex          BOOLEAN NOT NULL DEFAULT false;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE services
    ADD COLUMN meta_title       TEXT NOT NULL DEFAULT '',
    ADD COLUMN meta_description TEXT NOT NULL DEFAULT '',
    ADD COLUMN canonical_url    TEXT NOT NULL DEFAULT '',
    ADD COLUMN noindex          BOOLEAN NOT NULL DEFAULT false;
-- +goose StatementEnd

-- The translation tables carry ONLY the translatable meta_* overlay fields.
-- +goose StatementBegin
ALTER TABLE post_translations
    ADD COLUMN meta_title       TEXT NOT NULL DEFAULT '',
    ADD COLUMN meta_description TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE page_translations
    ADD COLUMN meta_title       TEXT NOT NULL DEFAULT '',
    ADD COLUMN meta_description TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE service_translations
    ADD COLUMN meta_title       TEXT NOT NULL DEFAULT '',
    ADD COLUMN meta_description TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
ALTER TABLE service_translations
    DROP COLUMN meta_title,
    DROP COLUMN meta_description;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE page_translations
    DROP COLUMN meta_title,
    DROP COLUMN meta_description;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE post_translations
    DROP COLUMN meta_title,
    DROP COLUMN meta_description;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE services
    DROP COLUMN meta_title,
    DROP COLUMN meta_description,
    DROP COLUMN canonical_url,
    DROP COLUMN noindex;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE pages
    DROP COLUMN meta_title,
    DROP COLUMN meta_description,
    DROP COLUMN canonical_url,
    DROP COLUMN noindex;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE posts
    DROP COLUMN meta_title,
    DROP COLUMN meta_description,
    DROP COLUMN canonical_url,
    DROP COLUMN noindex;
-- +goose StatementEnd
