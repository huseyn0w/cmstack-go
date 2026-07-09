-- +goose Up

-- API tokens (M17-1): bearer credentials for the REST API that a later MCP
-- server will consume. Only the SHA-256 hex of the plaintext token is stored;
-- the plaintext is shown to the operator exactly once at creation and never
-- persisted. last_four holds the trailing 4 characters of the plaintext for a
-- non-secret display hint. A token is valid when it is not revoked and either
-- never expires (expires_at IS NULL) or has not yet expired. last_used_at is a
-- best-effort touch stamp for auditing; a nil revoked_at/expires_at means
-- "active"/"never expires" respectively.

-- +goose StatementBegin
CREATE TABLE api_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    last_four    TEXT NOT NULL DEFAULT '',
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_api_tokens_token_hash ON api_tokens (token_hash);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_api_tokens_user_id ON api_tokens (user_id);
-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin
DROP TABLE api_tokens;
-- +goose StatementEnd
