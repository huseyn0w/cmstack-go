-- +goose Up

-- M6 Search: full-text search (FTS) over the three public content types. Each
-- gets a GENERATED ALWAYS AS (...) STORED tsvector column, weighted by field
-- importance (title = 'A', excerpt/summary = 'B', body = 'C'), plus a GIN index
-- on that column. The generated expression uses to_tsvector('english', ...) with
-- a HARD-CODED regconfig, which is IMMUTABLE — a requirement for a generated
-- column. setweight(...) || setweight(...) composes the per-field weights inside
-- the single generated expression (a generated column may reference multiple
-- columns of the SAME row, which this does).
--
-- Drafts / trashed rows keep their vector too (indexing is cheap); the search
-- QUERIES exclude them via (status = 'PUBLISHED' AND deleted_at IS NULL). An
-- ILIKE fallback (handled in the query layer, not here) covers substrings the
-- tsquery misses (django parity).
--
-- TODO(M7 locale scope): the regconfig is pinned to 'english'; a locale column
-- + per-locale config (or 'simple') is the seam for multilingual search.
-- TODO(M8 noindex): once posts/pages/services carry a noindex flag, the search
-- queries add `AND noindex = false` so hidden pages never surface here.

-- posts: title 'A', excerpt 'B', body 'C'.
-- +goose StatementBegin
ALTER TABLE posts
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(excerpt, '')), 'B') ||
        setweight(to_tsvector('english', coalesce(body, '')), 'C')
    ) STORED;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX posts_search_vector_idx ON posts USING GIN (search_vector);
-- +goose StatementEnd

-- pages: title 'A', body 'C' (pages have no excerpt/summary).
-- +goose StatementBegin
ALTER TABLE pages
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(body, '')), 'C')
    ) STORED;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX pages_search_vector_idx ON pages USING GIN (search_vector);
-- +goose StatementEnd

-- services: title 'A', summary 'B', body 'C'.
-- +goose StatementBegin
ALTER TABLE services
    ADD COLUMN search_vector tsvector
    GENERATED ALWAYS AS (
        setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
        setweight(to_tsvector('english', coalesce(summary, '')), 'B') ||
        setweight(to_tsvector('english', coalesce(body, '')), 'C')
    ) STORED;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX services_search_vector_idx ON services USING GIN (search_vector);
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm6', updated_at = now() WHERE key = 'agentic_cms_version';
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP INDEX IF EXISTS services_search_vector_idx;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE services DROP COLUMN IF EXISTS search_vector;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX IF EXISTS pages_search_vector_idx;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE pages DROP COLUMN IF EXISTS search_vector;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX IF EXISTS posts_search_vector_idx;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE posts DROP COLUMN IF EXISTS search_vector;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm5', updated_at = now() WHERE key = 'agentic_cms_version';
-- +goose StatementEnd
