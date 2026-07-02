-- +goose Up

-- service_translations: the per-locale CONTENT overlay for services (M7b-2). The
-- base `services` row KEEPS its title/summary/body as the DEFAULT-locale (en)
-- content; this table stores the translated title/summary/body for NON-default
-- locales (de/ru). Reads resolve: active locale == default -> base row; else ->
-- the translation row for that locale, FALLING BACK to the base row for any
-- missing (empty/absent) translation field. Structural/citable fields
-- (slug/status/price/area_served/schedule) stay shared on the base row and are
-- NOT per-locale.
--
--   TODO(M8): per-locale meta_title/meta_description will join here.
-- +goose StatementBegin
CREATE TABLE service_translations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id UUID NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    locale     TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    summary    TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (service_id, locale)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX service_translations_service_idx ON service_translations (service_id);
-- +goose StatementEnd

-- service_faq_translations: the per-locale overlay for a service's ORDERED FAQ
-- rows (M7b-2). The base `service_faqs` row keeps its question/answer as the
-- default-locale (en) content; this table stores the translated question/answer
-- for non-default locales. Position/order stays structural on the base FAQ row
-- and is shared across locales. Deleting the FAQ (or its service) cascades.
-- +goose StatementBegin
CREATE TABLE service_faq_translations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    faq_id     UUID NOT NULL REFERENCES service_faqs (id) ON DELETE CASCADE,
    locale     TEXT NOT NULL,
    question   TEXT NOT NULL DEFAULT '',
    answer     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (faq_id, locale)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX service_faq_translations_faq_idx ON service_faq_translations (faq_id);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS service_faq_translations;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS service_translations;
-- +goose StatementEnd
