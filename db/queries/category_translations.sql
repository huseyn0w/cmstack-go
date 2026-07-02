-- name: UpsertCategoryTranslation :one
-- Insert or update the translation row for (category_id, locale). Callers pass a
-- NON-default locale (en content lives on the base categories row). Description
-- is sanitized by the service before it reaches here.
INSERT INTO category_translations (category_id, locale, name, description)
VALUES ($1, $2, $3, $4)
ON CONFLICT (category_id, locale) DO UPDATE
SET name = EXCLUDED.name,
    description = EXCLUDED.description,
    updated_at = now()
RETURNING *;

-- name: GetCategoryTranslation :one
SELECT * FROM category_translations
WHERE category_id = $1 AND locale = $2;

-- name: ListCategoryTranslations :many
-- All translation rows for a category (every non-default locale that has one).
SELECT * FROM category_translations
WHERE category_id = $1
ORDER BY locale;

-- name: ListCategoryTranslationLocales :many
-- The set of locales that already have a translation row for a category — used to
-- mark "has translation" indicators on the editor's locale tabs.
SELECT locale FROM category_translations
WHERE category_id = $1
ORDER BY locale;

-- name: DeleteCategoryTranslation :exec
DELETE FROM category_translations
WHERE category_id = $1 AND locale = $2;

-- name: GetCategoryInLocaleByID :one
-- Load a category by id with its name/description OVERLAID by the given locale's
-- translation where present, falling back to the base row for any empty/absent
-- translation field. NULLIF('') makes an empty translation field fall through to
-- COALESCE's base value. Structural columns come straight from the base row. The
-- projected row matches the categories column shape so the repo maps it with the
-- existing categoryFromRow.
SELECT
    c.id,
    COALESCE(NULLIF(t.name, ''), c.name)               AS name,
    c.slug,
    COALESCE(NULLIF(t.description, ''), c.description)  AS description,
    c.parent_id,
    c.created_at,
    c.updated_at
FROM categories c
LEFT JOIN category_translations t ON t.category_id = c.id AND t.locale = $2
WHERE c.id = $1;

-- name: GetPublishedCategoryInLocaleBySlug :one
-- Public archive read: a category by slug with its content overlaid by the given
-- locale (base fallback per field). Slug is shared so it is the same across
-- locales.
SELECT
    c.id,
    COALESCE(NULLIF(t.name, ''), c.name)               AS name,
    c.slug,
    COALESCE(NULLIF(t.description, ''), c.description)  AS description,
    c.parent_id,
    c.created_at,
    c.updated_at
FROM categories c
LEFT JOIN category_translations t ON t.category_id = c.id AND t.locale = $2
WHERE c.slug = $1;
