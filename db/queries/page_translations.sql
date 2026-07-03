-- name: UpsertPageTranslation :one
-- Insert or update the translation row for (page_id, locale). Callers pass a
-- NON-default locale (en content lives on the base pages row). Body is sanitized
-- by the service before it reaches here.
INSERT INTO page_translations (page_id, locale, title, body, meta_title, meta_description)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (page_id, locale) DO UPDATE
SET title = EXCLUDED.title,
    body = EXCLUDED.body,
    meta_title = EXCLUDED.meta_title,
    meta_description = EXCLUDED.meta_description,
    updated_at = now()
RETURNING *;

-- name: GetPageTranslation :one
SELECT * FROM page_translations
WHERE page_id = $1 AND locale = $2;

-- name: ListPageTranslations :many
-- All translation rows for a page (every non-default locale that has one).
SELECT * FROM page_translations
WHERE page_id = $1
ORDER BY locale;

-- name: ListPageTranslationLocales :many
-- The set of locales that already have a translation row for a page — used to
-- mark "has translation" indicators on the editor's locale tabs.
SELECT locale FROM page_translations
WHERE page_id = $1
ORDER BY locale;

-- name: DeletePageTranslation :exec
DELETE FROM page_translations
WHERE page_id = $1 AND locale = $2;

-- name: GetActivePageInLocaleByID :one
-- Load an ACTIVE (non-trashed) page by id with its title/body OVERLAID by the
-- given locale's translation where present, falling back to the base row for any
-- empty/absent translation field. NULLIF('') makes an empty translation field
-- fall through to COALESCE's base value. Structural columns come straight from
-- the base row. The projected row matches the pages column shape so the repo maps
-- it with the existing pageFromRow.
SELECT
    p.id,
    COALESCE(NULLIF(t.title, ''), p.title) AS title,
    p.slug,
    COALESCE(NULLIF(t.body, ''), p.body)   AS body,
    p.status,
    p.published_at,
    p.parent_id,
    p.template,
    p.reading_time,
    COALESCE(NULLIF(t.meta_title, ''), p.meta_title)             AS meta_title,
    COALESCE(NULLIF(t.meta_description, ''), p.meta_description) AS meta_description,
    p.canonical_url,
    p.noindex,
    p.deleted_at,
    p.created_at,
    p.updated_at
FROM pages p
LEFT JOIN page_translations t ON t.page_id = p.id AND t.locale = $2
WHERE p.id = $1 AND p.deleted_at IS NULL;

-- name: GetPublishedPageInLocaleBySlug :one
-- Public detail read: a published, non-trashed page by slug with its content
-- overlaid by the given locale (base fallback per field). Slug/status are shared
-- so the slug is the same across locales.
SELECT
    p.id,
    COALESCE(NULLIF(t.title, ''), p.title) AS title,
    p.slug,
    COALESCE(NULLIF(t.body, ''), p.body)   AS body,
    p.status,
    p.published_at,
    p.parent_id,
    p.template,
    p.reading_time,
    COALESCE(NULLIF(t.meta_title, ''), p.meta_title)             AS meta_title,
    COALESCE(NULLIF(t.meta_description, ''), p.meta_description) AS meta_description,
    p.canonical_url,
    p.noindex,
    p.deleted_at,
    p.created_at,
    p.updated_at
FROM pages p
LEFT JOIN page_translations t ON t.page_id = p.id AND t.locale = $2
WHERE p.slug = $1 AND p.status = 'PUBLISHED' AND p.deleted_at IS NULL;
