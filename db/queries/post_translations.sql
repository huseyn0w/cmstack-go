-- name: UpsertPostTranslation :one
-- Insert or update the translation row for (post_id, locale). Callers pass a
-- NON-default locale (en content lives on the base posts row). Body is sanitized
-- by the service before it reaches here.
INSERT INTO post_translations (post_id, locale, title, excerpt, body)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (post_id, locale) DO UPDATE
SET title = EXCLUDED.title,
    excerpt = EXCLUDED.excerpt,
    body = EXCLUDED.body,
    updated_at = now()
RETURNING *;

-- name: GetPostTranslation :one
SELECT * FROM post_translations
WHERE post_id = $1 AND locale = $2;

-- name: ListPostTranslations :many
-- All translation rows for a post (every non-default locale that has one).
SELECT * FROM post_translations
WHERE post_id = $1
ORDER BY locale;

-- name: ListPostTranslationLocales :many
-- The set of locales that already have a translation row for a post — used to
-- mark "has translation" indicators on the editor's locale tabs.
SELECT locale FROM post_translations
WHERE post_id = $1
ORDER BY locale;

-- name: DeletePostTranslation :exec
DELETE FROM post_translations
WHERE post_id = $1 AND locale = $2;

-- name: GetActivePostInLocaleByID :one
-- Load an ACTIVE (non-trashed) post by id with its title/excerpt/body OVERLAID
-- by the given locale's translation where present, falling back to the base row
-- for any empty/absent translation field. NULLIF('') makes an empty translation
-- field fall through to COALESCE's base value. Structural columns come straight
-- from the base row. The projected row matches the posts column shape so the
-- repo maps it with the existing postFromRow.
SELECT
    p.id,
    COALESCE(NULLIF(t.title, ''), p.title)     AS title,
    p.slug,
    COALESCE(NULLIF(t.excerpt, ''), p.excerpt) AS excerpt,
    COALESCE(NULLIF(t.body, ''), p.body)       AS body,
    p.status,
    p.published_at,
    p.scheduled_at,
    p.author_id,
    p.reading_time,
    p.like_count,
    p.deleted_at,
    p.created_at,
    p.updated_at
FROM posts p
LEFT JOIN post_translations t ON t.post_id = p.id AND t.locale = $2
WHERE p.id = $1 AND p.deleted_at IS NULL;

-- name: GetPublishedPostInLocaleBySlug :one
-- Public detail read: a published, non-trashed post by slug with its content
-- overlaid by the given locale (base fallback per field). Slug/status are shared
-- so the slug is the same across locales.
SELECT
    p.id,
    COALESCE(NULLIF(t.title, ''), p.title)     AS title,
    p.slug,
    COALESCE(NULLIF(t.excerpt, ''), p.excerpt) AS excerpt,
    COALESCE(NULLIF(t.body, ''), p.body)       AS body,
    p.status,
    p.published_at,
    p.scheduled_at,
    p.author_id,
    p.reading_time,
    p.like_count,
    p.deleted_at,
    p.created_at,
    p.updated_at
FROM posts p
LEFT JOIN post_translations t ON t.post_id = p.id AND t.locale = $2
WHERE p.slug = $1 AND p.status = 'PUBLISHED' AND p.deleted_at IS NULL;

-- name: ListPublishedPostsInLocale :many
-- Public blog index in a locale: published posts (newest first) with content
-- overlaid by the locale, base fallback per field.
SELECT
    p.id,
    COALESCE(NULLIF(t.title, ''), p.title)     AS title,
    p.slug,
    COALESCE(NULLIF(t.excerpt, ''), p.excerpt) AS excerpt,
    COALESCE(NULLIF(t.body, ''), p.body)       AS body,
    p.status,
    p.published_at,
    p.scheduled_at,
    p.author_id,
    p.reading_time,
    p.like_count,
    p.deleted_at,
    p.created_at,
    p.updated_at
FROM posts p
LEFT JOIN post_translations t ON t.post_id = p.id AND t.locale = $1
WHERE p.status = 'PUBLISHED' AND p.deleted_at IS NULL
ORDER BY p.published_at DESC NULLS LAST, p.created_at DESC
LIMIT $2 OFFSET $3;
