-- +goose Up

-- pages: a standalone, optionally HIERARCHICAL content type (About, Contact, ...).
-- Mirrors the django Page: a self-referential parent (nullable), a named template
-- selector validated against a server-side allow-list, revisions via the shared
-- kernel table (entity_type='page'), and soft-delete. SEO-meta fields are a
-- documented seam:
--   TODO(M8 SEO fields): meta_title, meta_description, canonical_url, noindex.
-- +goose StatementBegin
CREATE TABLE pages (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title         TEXT NOT NULL,
    slug          TEXT NOT NULL UNIQUE,
    body          TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'DRAFT',
    published_at  TIMESTAMPTZ,
    parent_id     UUID REFERENCES pages (id) ON DELETE SET NULL,
    template      TEXT NOT NULL DEFAULT 'default',
    reading_time  INT NOT NULL DEFAULT 0,
    deleted_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- Public reads filter on (status, deleted_at); the tree view lists children of a
-- parent.
-- +goose StatementBegin
CREATE INDEX pages_status_idx ON pages (status) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX pages_parent_idx ON pages (parent_id) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- services: the GEO content type. A definitional `summary`, a rich `body`,
-- citable facts (`price`, `area_served`), an ordered FAQ block (own table), and
-- soft-delete. Revisions via the shared kernel table (entity_type='service').
-- SEO-meta fields are a documented seam:
--   TODO(M8 SEO fields): meta_title, meta_description, canonical_url, noindex.
--   TODO(M8 JSON-LD): Service + FAQPage schema is serialized at M8 from a typed seam.
-- +goose StatementBegin
CREATE TABLE services (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title         TEXT NOT NULL,
    slug          TEXT NOT NULL UNIQUE,
    summary       TEXT NOT NULL DEFAULT '',
    body          TEXT NOT NULL DEFAULT '',
    price         TEXT NOT NULL DEFAULT '',
    area_served   TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'DRAFT',
    published_at  TIMESTAMPTZ,
    reading_time  INT NOT NULL DEFAULT 0,
    deleted_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX services_status_idx ON services (status) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- service_faqs: the ordered Q&A list for a service (one row per question). The
-- (service_id, position) pair drives the public accordion order; deleting a
-- service cascades its FAQs.
-- +goose StatementBegin
CREATE TABLE service_faqs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    service_id  UUID NOT NULL REFERENCES services (id) ON DELETE CASCADE,
    question    TEXT NOT NULL,
    answer      TEXT NOT NULL DEFAULT '',
    position    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX service_faqs_service_idx ON service_faqs (service_id, position);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS service_faqs;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS services;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS pages;
-- +goose StatementEnd
