-- +goose Up

-- categories: a site-wide, HIERARCHICAL taxonomy (a self-referential tree,
-- exactly like pages). A category has a unique slug, an optional rich
-- description, and an optional parent (nullable, self-ref). Deleting a parent
-- detaches its children (ON DELETE SET NULL) rather than cascading, so a subtree
-- is never silently destroyed. Per-locale name/description is a documented seam:
--   TODO(M7 i18n): category_translations(category_id, locale, name, description).
-- +goose StatementBegin
CREATE TABLE categories (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    parent_id   UUID REFERENCES categories (id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- The tree view lists children of a parent; the parent picker walks ancestry.
-- +goose StatementBegin
CREATE INDEX categories_parent_idx ON categories (parent_id);
-- +goose StatementEnd

-- tags: a flat, site-wide taxonomy. Unique slug; no hierarchy. Per-locale name
-- is a documented seam:
--   TODO(M7 i18n): tag_translations(tag_id, locale, name).
-- +goose StatementBegin
CREATE TABLE tags (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- post_categories: the M2M join between posts and categories. The pk on both
-- columns makes a duplicate attach a no-op (ON CONFLICT DO NOTHING). Deleting
-- either side cascades the join rows (the association is meaningless without
-- both endpoints). This is the M3 seam the posts migration documented.
-- +goose StatementBegin
CREATE TABLE post_categories (
    post_id     UUID NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    category_id UUID NOT NULL REFERENCES categories (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, category_id)
);
-- +goose StatementEnd

-- Reverse lookups (posts-in-category) scan by category_id.
-- +goose StatementBegin
CREATE INDEX post_categories_category_idx ON post_categories (category_id);
-- +goose StatementEnd

-- post_tags: the M2M join between posts and tags. Same idempotent/cascade rules.
-- +goose StatementBegin
CREATE TABLE post_tags (
    post_id     UUID NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    tag_id      UUID NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, tag_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX post_tags_tag_idx ON post_tags (tag_id);
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm3', updated_at = now() WHERE key = 'cmstack_version';
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS post_tags;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS post_categories;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS tags;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS categories;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm2', updated_at = now() WHERE key = 'cmstack_version';
-- +goose StatementEnd
