-- name: UpsertServiceTranslation :one
-- Insert or update the translation row for (service_id, locale). Callers pass a
-- NON-default locale (en content lives on the base services row). Body is
-- sanitized by the service before it reaches here.
INSERT INTO service_translations (service_id, locale, title, summary, body)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (service_id, locale) DO UPDATE
SET title = EXCLUDED.title,
    summary = EXCLUDED.summary,
    body = EXCLUDED.body,
    updated_at = now()
RETURNING *;

-- name: GetServiceTranslation :one
SELECT * FROM service_translations
WHERE service_id = $1 AND locale = $2;

-- name: ListServiceTranslations :many
SELECT * FROM service_translations
WHERE service_id = $1
ORDER BY locale;

-- name: ListServiceTranslationLocales :many
-- The set of locales that already have a translation row for a service — used to
-- mark "has translation" indicators on the editor's locale tabs.
SELECT locale FROM service_translations
WHERE service_id = $1
ORDER BY locale;

-- name: DeleteServiceTranslation :exec
DELETE FROM service_translations
WHERE service_id = $1 AND locale = $2;

-- name: GetActiveServiceInLocaleByID :one
-- Load an ACTIVE (non-trashed) service by id with title/summary/body OVERLAID by
-- the given locale's translation where present, falling back to the base row for
-- any empty/absent translation field. Structural/citable columns come straight
-- from the base row. The projected row matches the services column shape so the
-- repo maps it with the existing serviceFromRow.
SELECT
    s.id,
    COALESCE(NULLIF(t.title, ''), s.title)     AS title,
    s.slug,
    COALESCE(NULLIF(t.summary, ''), s.summary) AS summary,
    COALESCE(NULLIF(t.body, ''), s.body)       AS body,
    s.price,
    s.area_served,
    s.status,
    s.published_at,
    s.reading_time,
    s.deleted_at,
    s.created_at,
    s.updated_at
FROM services s
LEFT JOIN service_translations t ON t.service_id = s.id AND t.locale = $2
WHERE s.id = $1 AND s.deleted_at IS NULL;

-- name: GetPublishedServiceInLocaleBySlug :one
-- Public detail read: a published, non-trashed service by slug with its content
-- overlaid by the given locale (base fallback per field).
SELECT
    s.id,
    COALESCE(NULLIF(t.title, ''), s.title)     AS title,
    s.slug,
    COALESCE(NULLIF(t.summary, ''), s.summary) AS summary,
    COALESCE(NULLIF(t.body, ''), s.body)       AS body,
    s.price,
    s.area_served,
    s.status,
    s.published_at,
    s.reading_time,
    s.deleted_at,
    s.created_at,
    s.updated_at
FROM services s
LEFT JOIN service_translations t ON t.service_id = s.id AND t.locale = $2
WHERE s.slug = $1 AND s.status = 'PUBLISHED' AND s.deleted_at IS NULL;

-- name: ListServiceFAQsInLocale :many
-- A service's FAQs (ordered by position) with question/answer OVERLAID by the
-- given locale's translation, base fallback per field. Structural order comes
-- from the base service_faqs rows.
SELECT
    f.id,
    f.service_id,
    COALESCE(NULLIF(t.question, ''), f.question) AS question,
    COALESCE(NULLIF(t.answer, ''), f.answer)     AS answer,
    f.position
FROM service_faqs f
LEFT JOIN service_faq_translations t ON t.faq_id = f.id AND t.locale = $2
WHERE f.service_id = $1
ORDER BY f.position ASC, f.created_at ASC;

-- name: UpsertServiceFAQTranslation :one
-- Insert or update the per-locale question/answer overlay for one base FAQ row.
INSERT INTO service_faq_translations (faq_id, locale, question, answer)
VALUES ($1, $2, $3, $4)
ON CONFLICT (faq_id, locale) DO UPDATE
SET question = EXCLUDED.question,
    answer = EXCLUDED.answer,
    updated_at = now()
RETURNING *;

-- name: GetServiceFAQTranslation :one
SELECT * FROM service_faq_translations
WHERE faq_id = $1 AND locale = $2;

-- name: DeleteServiceFAQTranslationsForService :exec
-- Remove every FAQ translation row for a service's FAQs in one locale — used when
-- the editor replaces a locale's FAQ overlay block (the whole set is rewritten,
-- mirroring the base ReplaceFAQs strategy).
DELETE FROM service_faq_translations
WHERE locale = $2
  AND faq_id IN (SELECT id FROM service_faqs WHERE service_id = $1);
