-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS citext;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pgcrypto;
-- +goose StatementEnd

-- Roles: one row per role key (administrator, editor, author, member).
-- +goose StatementBegin
CREATE TABLE roles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key         TEXT NOT NULL UNIQUE,
    label       TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- Permissions: a granular (action, subject) pair. manage = all actions.
-- +goose StatementBegin
CREATE TABLE permissions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    action      TEXT NOT NULL,
    subject     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (action, subject)
);
-- +goose StatementEnd

-- role_permissions: m:n join, pk on both columns.
-- +goose StatementBegin
CREATE TABLE role_permissions (
    role_id       UUID NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions (id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX role_permissions_permission_idx ON role_permissions (permission_id);
-- +goose StatementEnd

-- users: one role per user (m:1). Profile columns ship now; their edit UI is M1-ext.
-- +goose StatementBegin
CREATE TABLE users (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email             CITEXT NOT NULL UNIQUE,
    username          CITEXT UNIQUE,
    password_hash     TEXT NOT NULL,
    name              TEXT NOT NULL DEFAULT '',
    email_verified_at TIMESTAMPTZ,
    role_id           UUID NOT NULL REFERENCES roles (id) ON DELETE RESTRICT,
    bio               TEXT,
    avatar_path       TEXT,
    website           TEXT,
    social_links      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX users_role_idx ON users (role_id);
-- +goose StatementEnd

-- email_verification_tokens: store ONLY sha-256 hashes, single-use, expiring.
-- +goose StatementBegin
CREATE TABLE email_verification_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX email_verification_tokens_user_idx ON email_verification_tokens (user_id);
-- +goose StatementEnd

-- password_reset_tokens: store ONLY sha-256 hashes, single-use, expiring.
-- +goose StatementBegin
CREATE TABLE password_reset_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX password_reset_tokens_user_idx ON password_reset_tokens (user_id);
-- +goose StatementEnd

-- oauth_accounts: table only; provider wiring is M1-ext.
-- +goose StatementBegin
CREATE TABLE oauth_accounts (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider         TEXT NOT NULL,
    provider_user_id TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_user_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX oauth_accounts_user_idx ON oauth_accounts (user_id);
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm1', updated_at = now() WHERE key = 'agentic_cms_version';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS oauth_accounts;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS password_reset_tokens;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS email_verification_tokens;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS users;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS role_permissions;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS permissions;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS roles;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm0', updated_at = now() WHERE key = 'agentic_cms_version';
-- +goose StatementEnd
