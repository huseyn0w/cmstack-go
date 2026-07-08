-- name: CreateMenu :one
INSERT INTO menus (name, location)
VALUES ($1, $2)
RETURNING *;

-- name: GetMenu :one
SELECT * FROM menus WHERE id = $1;

-- name: ListMenus :many
SELECT * FROM menus
ORDER BY name, created_at;

-- name: GetMenuByLocation :one
-- Public resolve entry: the single menu assigned to a location (partial-unique
-- guarantees at most one). Unassigned menus (location = '') are never returned.
SELECT * FROM menus
WHERE location = $1 AND location <> '';

-- name: UpdateMenu :one
UPDATE menus
SET name = $2,
    location = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteMenu :exec
DELETE FROM menus WHERE id = $1;

-- name: CreateMenuItem :one
INSERT INTO menu_items (menu_id, parent_id, position, type, ref_id, url, label)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateMenuItem :one
UPDATE menu_items
SET parent_id = $2,
    type = $3,
    ref_id = $4,
    url = $5,
    label = $6,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetMenuItemPosition :exec
-- Assign a single item's position; used in a loop within one transaction to
-- reorder a menu's items (position = index in the ordered id list).
UPDATE menu_items
SET position = $2,
    updated_at = now()
WHERE id = $1;

-- name: DeleteMenuItem :exec
DELETE FROM menu_items WHERE id = $1;

-- name: ListMenuItems :many
-- Every item in a menu, ordered by position (then created_at as a stable
-- tiebreak). Structural read — labels are the base default-locale values.
SELECT * FROM menu_items
WHERE menu_id = $1
ORDER BY position, created_at;

-- name: ListMenuItemsInLocale :many
-- Overlay read for the public resolve: every item in a menu with its LABEL
-- overlaid by the given locale's translation where present, falling back to the
-- base item label for any empty/absent translation. All other columns are
-- structural and come straight from the base item row. Ordered by position.
SELECT
    i.id,
    i.menu_id,
    i.parent_id,
    i.position,
    i.type,
    i.ref_id,
    i.url,
    COALESCE(NULLIF(t.label, ''), i.label) AS label,
    i.created_at,
    i.updated_at
FROM menu_items i
LEFT JOIN menu_item_translations t ON t.item_id = i.id AND t.locale = $2
WHERE i.menu_id = $1
ORDER BY i.position, i.created_at;

-- name: UpsertMenuItemTranslation :exec
-- Insert or update the per-locale label for (item_id, locale). Callers pass a
-- NON-default locale (the base item row holds the default-locale label).
INSERT INTO menu_item_translations (item_id, locale, label)
VALUES ($1, $2, $3)
ON CONFLICT (item_id, locale) DO UPDATE
SET label = EXCLUDED.label,
    updated_at = now();

-- name: ListMenuItemTranslations :many
SELECT locale, label FROM menu_item_translations
WHERE item_id = $1
ORDER BY locale;

-- name: ListMenuItemTranslationLocales :many
SELECT locale FROM menu_item_translations
WHERE item_id = $1
ORDER BY locale;
