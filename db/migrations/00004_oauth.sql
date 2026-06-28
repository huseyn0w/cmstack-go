-- +goose Up
-- Social login (M1-ext). The oauth_accounts table already exists (00002);
-- this migration adds the avatar_url source for provider-supplied avatars and
-- a NOT NULL guard nothing more — oauth_accounts(provider, provider_user_id)
-- already carries the unique index used by the lookup path.

-- avatar_url stores the absolute URL of a provider-supplied avatar. It is
-- distinct from avatar_path (a local upload, set in a later media milestone):
-- a provider avatar is a remote URL we never copy. Nullable: password accounts
-- and providers without an avatar leave it NULL.
-- +goose StatementBegin
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS avatar_url TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm1.2', updated_at = now() WHERE key = 'cmstack_version';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users
    DROP COLUMN IF EXISTS avatar_url;
-- +goose StatementEnd

-- +goose StatementBegin
UPDATE schema_meta SET value = 'm1.1', updated_at = now() WHERE key = 'cmstack_version';
-- +goose StatementEnd
