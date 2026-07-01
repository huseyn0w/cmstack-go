-- M6 Search queries. A UNION across the three public content types (posts,
-- pages, services), scoped to published + non-trashed rows. Two strategies:
--   1. SearchFTS  — websearch_to_tsquery match, ranked by ts_rank (title>body via
--      the weighted search_vector), with a type/recency tie-break. ts_headline
--      builds a highlighted snippet from the body/excerpt.
--   2. SearchILIKE — parameterized substring scan (django parity) used as a
--      FALLBACK when FTS yields nothing (handles substrings/typos tsquery misses).
-- The query string is ALWAYS a bound parameter ($1) — never interpolated.
--
-- Drafts / trashed are excluded by (status = 'PUBLISHED' AND deleted_at IS NULL).
-- TODO(M7 locale scope): the 'english' regconfig is fixed; a locale param is the seam.
-- TODO(M8 noindex): add `AND noindex = false` once the flag lands.

-- name: SearchFTS :many
SELECT
    result_type,
    id,
    title,
    slug,
    snippet,
    published_at,
    rank
FROM (
    SELECT
        'post'::text AS result_type,
        p.id,
        p.title,
        p.slug,
        ts_headline('english', coalesce(nullif(p.excerpt, ''), p.body),
            websearch_to_tsquery('english', @query),
            'StartSel=<mark>, StopSel=</mark>, MaxWords=30, MinWords=8, ShortWord=2, MaxFragments=1') AS snippet,
        p.published_at,
        ts_rank(p.search_vector, websearch_to_tsquery('english', @query)) AS rank,
        2 AS type_order
    FROM posts p
    WHERE p.status = 'PUBLISHED' AND p.deleted_at IS NULL
      AND p.search_vector @@ websearch_to_tsquery('english', @query)

    UNION ALL

    SELECT
        'page'::text,
        pg.id,
        pg.title,
        pg.slug,
        ts_headline('english', pg.body,
            websearch_to_tsquery('english', @query),
            'StartSel=<mark>, StopSel=</mark>, MaxWords=30, MinWords=8, ShortWord=2, MaxFragments=1'),
        pg.published_at,
        ts_rank(pg.search_vector, websearch_to_tsquery('english', @query)),
        3 AS type_order
    FROM pages pg
    WHERE pg.status = 'PUBLISHED' AND pg.deleted_at IS NULL
      AND pg.search_vector @@ websearch_to_tsquery('english', @query)

    UNION ALL

    SELECT
        'service'::text,
        s.id,
        s.title,
        s.slug,
        ts_headline('english', coalesce(nullif(s.summary, ''), s.body),
            websearch_to_tsquery('english', @query),
            'StartSel=<mark>, StopSel=</mark>, MaxWords=30, MinWords=8, ShortWord=2, MaxFragments=1'),
        s.published_at,
        ts_rank(s.search_vector, websearch_to_tsquery('english', @query)),
        1 AS type_order
    FROM services s
    WHERE s.status = 'PUBLISHED' AND s.deleted_at IS NULL
      AND s.search_vector @@ websearch_to_tsquery('english', @query)
) hits
ORDER BY rank DESC, type_order ASC, published_at DESC NULLS LAST
LIMIT $1 OFFSET $2;

-- name: CountSearchFTS :one
SELECT count(*) FROM (
    SELECT p.id FROM posts p
    WHERE p.status = 'PUBLISHED' AND p.deleted_at IS NULL
      AND p.search_vector @@ websearch_to_tsquery('english', @query)
    UNION ALL
    SELECT pg.id FROM pages pg
    WHERE pg.status = 'PUBLISHED' AND pg.deleted_at IS NULL
      AND pg.search_vector @@ websearch_to_tsquery('english', @query)
    UNION ALL
    SELECT s.id FROM services s
    WHERE s.status = 'PUBLISHED' AND s.deleted_at IS NULL
      AND s.search_vector @@ websearch_to_tsquery('english', @query)
) hits;

-- name: SearchILIKE :many
-- Fallback substring scan. @pattern is the caller-built '%'||term||'%' literal,
-- bound as a parameter (never interpolated). Ranking degrades to a title-first,
-- recency tie-break since there is no ts_rank here.
SELECT
    result_type,
    id,
    title,
    slug,
    snippet,
    published_at,
    rank
FROM (
    SELECT
        'post'::text AS result_type,
        p.id,
        p.title,
        p.slug,
        left(coalesce(nullif(p.excerpt, ''), p.body), 200) AS snippet,
        p.published_at,
        CASE WHEN p.title ILIKE @pattern THEN 1.0 ELSE 0.5 END AS rank,
        2 AS type_order
    FROM posts p
    WHERE p.status = 'PUBLISHED' AND p.deleted_at IS NULL
      AND (p.title ILIKE @pattern OR p.excerpt ILIKE @pattern OR p.body ILIKE @pattern)

    UNION ALL

    SELECT
        'page'::text,
        pg.id,
        pg.title,
        pg.slug,
        left(pg.body, 200),
        pg.published_at,
        CASE WHEN pg.title ILIKE @pattern THEN 1.0 ELSE 0.5 END,
        3 AS type_order
    FROM pages pg
    WHERE pg.status = 'PUBLISHED' AND pg.deleted_at IS NULL
      AND (pg.title ILIKE @pattern OR pg.body ILIKE @pattern)

    UNION ALL

    SELECT
        'service'::text,
        s.id,
        s.title,
        s.slug,
        left(coalesce(nullif(s.summary, ''), s.body), 200),
        s.published_at,
        CASE WHEN s.title ILIKE @pattern THEN 1.0 ELSE 0.5 END,
        1 AS type_order
    FROM services s
    WHERE s.status = 'PUBLISHED' AND s.deleted_at IS NULL
      AND (s.title ILIKE @pattern OR s.summary ILIKE @pattern OR s.body ILIKE @pattern)
) hits
ORDER BY rank DESC, type_order ASC, published_at DESC NULLS LAST
LIMIT $1 OFFSET $2;

-- name: CountSearchILIKE :one
SELECT count(*) FROM (
    SELECT p.id FROM posts p
    WHERE p.status = 'PUBLISHED' AND p.deleted_at IS NULL
      AND (p.title ILIKE @pattern OR p.excerpt ILIKE @pattern OR p.body ILIKE @pattern)
    UNION ALL
    SELECT pg.id FROM pages pg
    WHERE pg.status = 'PUBLISHED' AND pg.deleted_at IS NULL
      AND (pg.title ILIKE @pattern OR pg.body ILIKE @pattern)
    UNION ALL
    SELECT s.id FROM services s
    WHERE s.status = 'PUBLISHED' AND s.deleted_at IS NULL
      AND (s.title ILIKE @pattern OR s.summary ILIKE @pattern OR s.body ILIKE @pattern)
) hits;
