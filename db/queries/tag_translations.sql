-- name: UpsertTagTranslation :one
-- Insert or update the translation row for (tag_id, locale). Callers pass a
-- NON-default locale (en content lives on the base tags row).
INSERT INTO tag_translations (tag_id, locale, name)
VALUES ($1, $2, $3)
ON CONFLICT (tag_id, locale) DO UPDATE
SET name = EXCLUDED.name,
    updated_at = now()
RETURNING *;

-- name: GetTagTranslation :one
SELECT * FROM tag_translations
WHERE tag_id = $1 AND locale = $2;

-- name: ListTagTranslations :many
-- All translation rows for a tag (every non-default locale that has one).
SELECT * FROM tag_translations
WHERE tag_id = $1
ORDER BY locale;

-- name: ListTagTranslationLocales :many
-- The set of locales that already have a translation row for a tag — used to mark
-- "has translation" indicators on the editor's locale tabs.
SELECT locale FROM tag_translations
WHERE tag_id = $1
ORDER BY locale;

-- name: DeleteTagTranslation :exec
DELETE FROM tag_translations
WHERE tag_id = $1 AND locale = $2;

-- name: GetTagInLocaleByID :one
-- Load a tag by id with its name OVERLAID by the given locale's translation where
-- present, falling back to the base row for an empty/absent name. NULLIF('')
-- makes an empty translation name fall through to COALESCE's base value.
-- Structural columns come straight from the base row. The projected row matches
-- the tags column shape so the repo maps it with the existing tagFromRow.
SELECT
    tg.id,
    COALESCE(NULLIF(t.name, ''), tg.name) AS name,
    tg.slug,
    tg.created_at,
    tg.updated_at
FROM tags tg
LEFT JOIN tag_translations t ON t.tag_id = tg.id AND t.locale = $2
WHERE tg.id = $1;

-- name: GetPublishedTagInLocaleBySlug :one
-- Public archive read: a tag by slug with its name overlaid by the given locale
-- (base fallback). Slug is shared so it is the same across locales.
SELECT
    tg.id,
    COALESCE(NULLIF(t.name, ''), tg.name) AS name,
    tg.slug,
    tg.created_at,
    tg.updated_at
FROM tags tg
LEFT JOIN tag_translations t ON t.tag_id = tg.id AND t.locale = $2
WHERE tg.slug = $1;
