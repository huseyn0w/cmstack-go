-- +goose Up

-- category_translations: the per-locale CONTENT overlay for categories (M7b-3).
-- The base `categories` row KEEPS its name/description as the DEFAULT-locale (en)
-- content; this table stores the translated name/description for NON-default
-- locales (de/ru). Reads resolve: active locale == default -> base row; else ->
-- the translation row for that locale, FALLING BACK to the base row for any
-- missing (empty/absent) translation field. Structural fields (slug/parent) stay
-- shared on the base row and are NOT per-locale.
-- +goose StatementBegin
CREATE TABLE category_translations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id UUID NOT NULL REFERENCES categories (id) ON DELETE CASCADE,
    locale      TEXT NOT NULL,
    name        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (category_id, locale)
);
-- +goose StatementEnd

-- The overlay read + all-locales fetch join on category_id.
-- +goose StatementBegin
CREATE INDEX category_translations_category_idx ON category_translations (category_id);
-- +goose StatementEnd

-- tag_translations: the per-locale CONTENT overlay for tags (M7b-3). The base
-- `tags` row KEEPS its name as the DEFAULT-locale (en) content; this table stores
-- the translated name for NON-default locales (de/ru). Reads resolve: active
-- locale == default -> base row; else -> the translation row for that locale,
-- FALLING BACK to the base row for a missing (empty/absent) name. The slug stays
-- shared on the base row and is NOT per-locale.
-- +goose StatementBegin
CREATE TABLE tag_translations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tag_id     UUID NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    locale     TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tag_id, locale)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX tag_translations_tag_idx ON tag_translations (tag_id);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS tag_translations;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS category_translations;
-- +goose StatementEnd
