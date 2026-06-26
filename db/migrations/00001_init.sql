-- +goose Up
-- +goose StatementBegin
CREATE TABLE schema_meta (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
INSERT INTO schema_meta (key, value) VALUES ('cmstack_version', 'm0');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE outbox (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_name    TEXT        NOT NULL,
    payload       JSONB       NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at  TIMESTAMPTZ
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX outbox_unprocessed_idx ON outbox (created_at) WHERE processed_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS outbox;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS schema_meta;
-- +goose StatementEnd
