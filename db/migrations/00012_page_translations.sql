-- +goose Up

-- page_translations: the per-locale CONTENT overlay for pages (M7b-2). The base
-- `pages` row KEEPS its title/body as the DEFAULT-locale (en) content; this table
-- stores the translated title/body for NON-default locales (de/ru). Reads
-- resolve: active locale == default -> base row; else -> the translation row for
-- that locale, FALLING BACK to the base row for any missing (empty/absent)
-- translation field. Structural fields (slug/status/parent/template/schedule)
-- stay shared on the base row and are NOT per-locale.
--
--   TODO(M8): per-locale meta_title/meta_description will join here (SEO fields
--   translate alongside the content).
-- +goose StatementBegin
CREATE TABLE page_translations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    page_id    UUID NOT NULL REFERENCES pages (id) ON DELETE CASCADE,
    locale     TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (page_id, locale)
);
-- +goose StatementEnd

-- The overlay read + all-locales fetch join on page_id.
-- +goose StatementBegin
CREATE INDEX page_translations_page_idx ON page_translations (page_id);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS page_translations;
-- +goose StatementEnd
