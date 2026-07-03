-- +goose Up

-- Generic key/value site-settings store (M9-1). The first DB-backed settings
-- table: a single flat namespace of string values keyed by a stable string
-- (e.g. 'active_theme'). It is deliberately schema-light so later milestones
-- (M15 admin settings) can extend it without a migration per toggle. Values are
-- always non-null; an absent key means "unset" and resolves to a code default
-- in the application layer. updated_at tracks the last write for auditing.

-- +goose StatementBegin
CREATE TABLE site_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE site_settings;
-- +goose StatementEnd
