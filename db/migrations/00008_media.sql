-- +goose Up

-- media: the M4 media-library asset table. One row per uploaded original.
-- storage_key is the opaque, sanitized object key under which the original is
-- stored via the Storage interface (local dir now, S3 drop-in). The extension
-- embedded in the key is ALWAYS derived from the VALIDATED (magic-byte sniffed)
-- MIME, never the client filename, so a polyglot cannot smuggle an active
-- extension onto disk. SVG is rejected at validation time and never reaches a row.
-- +goose StatementBegin
CREATE TABLE media (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    storage_key       TEXT NOT NULL UNIQUE,
    original_filename TEXT NOT NULL DEFAULT '',
    mime              TEXT NOT NULL,
    size_bytes        BIGINT NOT NULL DEFAULT 0,
    -- width/height are NULL for non-raster assets (PDF). Raster uploads carry
    -- their probed pixel dimensions.
    width             INT,
    height            INT,
    alt               TEXT NOT NULL DEFAULT '',
    title             TEXT NOT NULL DEFAULT '',
    caption           TEXT NOT NULL DEFAULT '',
    uploaded_by       UUID NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- The library lists newest-first; this index serves both the list and count.
-- +goose StatementBegin
CREATE INDEX media_created_idx ON media (created_at DESC);
-- +goose StatementEnd

-- media_thumbnails: generated raster variants of a media original (e.g. a "thumb"
-- and a "medium"). A SEPARATE TABLE (rather than fixed thumb_key/medium_key
-- columns on media) is the deliberate choice: variant SETS evolve over time
-- (add a "large" later, or per-format WebP variants) WITHOUT a schema migration,
-- each variant carries its own key + dimensions, and the delete path can simply
-- "list variants for media id" to remove every storage object. variant is a
-- short label ("thumb"|"medium"); (media_id, variant) is unique so re-generating
-- upserts rather than duplicates.
-- +goose StatementBegin
CREATE TABLE media_thumbnails (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    media_id    UUID NOT NULL REFERENCES media (id) ON DELETE CASCADE,
    variant     TEXT NOT NULL,
    storage_key TEXT NOT NULL,
    width       INT NOT NULL,
    height      INT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (media_id, variant)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX media_thumbnails_media_idx ON media_thumbnails (media_id);
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm4', updated_at = now() WHERE key = 'agentic_cms_version';
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE IF EXISTS media_thumbnails;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS media;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm3', updated_at = now() WHERE key = 'agentic_cms_version';
-- +goose StatementEnd
