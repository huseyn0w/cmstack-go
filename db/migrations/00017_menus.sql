-- +goose Up

-- Menus (M11-1): named navigation menus assigned to a location (header/footer),
-- with ordered, nestable items that reference internal content (post/page/
-- category) or a custom URL, plus a per-locale label overlay. The base item row
-- holds the DEFAULT-locale label; menu_item_translations stores the translated
-- label for NON-default locales (read via the COALESCE overlay). Structural
-- fields (type/ref/url/parent/position) are shared across locales and live on the
-- base item row only. This slice is data + service only — the admin builder UI and
-- the public header/footer rendering are separate later slices.

-- +goose StatementBegin
CREATE TABLE menus (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    location   TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- At most one menu per ASSIGNED location. Unassigned menus (location = '') are
-- exempt from the constraint, so any number may sit unassigned.
-- +goose StatementBegin
CREATE UNIQUE INDEX menus_location_uniq ON menus (location) WHERE location <> '';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE menu_items (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    menu_id    UUID NOT NULL REFERENCES menus (id) ON DELETE CASCADE,
    parent_id  UUID NULL REFERENCES menu_items (id) ON DELETE CASCADE,
    position   INT NOT NULL DEFAULT 0,
    type       TEXT NOT NULL,
    ref_id     UUID NULL,
    url        TEXT NOT NULL DEFAULT '',
    label      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- Items are read ordered within a menu; the index backs the ordered list read.
-- +goose StatementBegin
CREATE INDEX menu_items_menu_position_idx ON menu_items (menu_id, position);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE menu_item_translations (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id    UUID NOT NULL REFERENCES menu_items (id) ON DELETE CASCADE,
    locale     TEXT NOT NULL,
    label      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (item_id, locale)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX menu_item_translations_item_idx ON menu_item_translations (item_id);
-- +goose StatementEnd

-- +goose Down

-- Drop in dependency order (translations + items also cascade off menus, but the
-- explicit order keeps the intent clear and is safe if the FKs ever change).
-- +goose StatementBegin
DROP TABLE IF EXISTS menu_item_translations;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS menu_items;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS menus;
-- +goose StatementEnd
