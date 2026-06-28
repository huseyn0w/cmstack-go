// Package accounts implements the authentication and authorization domain:
// users, roles, permissions, password/email tokens, and the auth service. All
// business logic lives in the service; handlers are thin HTTP boundaries and
// data access happens only through the repository interfaces defined here.
package accounts

import (
	"time"

	"github.com/google/uuid"
)

// Role keys. Member is the lowest-privilege default for new accounts.
const (
	RoleAdministrator = "administrator"
	RoleEditor        = "editor"
	RoleAuthor        = "author"
	RoleMember        = "member"
)

// User is the domain representation of an account. It never carries the
// password hash beyond what the service needs for verification.
type User struct {
	ID              uuid.UUID
	Email           string
	Username        string // empty when unset
	PasswordHash    string
	Name            string
	EmailVerifiedAt *time.Time
	// AvatarURL is an absolute URL of a provider-supplied avatar (social login).
	// Empty for password accounts and providers that expose no avatar.
	AvatarURL string
	// Bio is a short free-text description shown on the profile/author page.
	Bio string
	// AvatarPath is the storage key of a self-uploaded avatar (Storage.URL turns
	// it into a public URL). Empty when the user has not uploaded one.
	AvatarPath string
	// Website is the user's personal/site URL (validated http(s) on save).
	Website string
	// SocialLinks maps a known network key (twitter/github/linkedin/mastodon) to a
	// profile URL. Normalized and validated on save.
	SocialLinks map[string]string
	RoleID      uuid.UUID
	// PasswordChangedAt is bumped on every password reset/change. Sessions store
	// the value they were minted under; the middleware rejects sessions older
	// than this, enforcing a global logout after a credential change.
	PasswordChangedAt time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// EmailVerified reports whether the user's email has been confirmed.
func (u User) EmailVerified() bool { return u.EmailVerifiedAt != nil }

// Role is a named bundle of permissions.
type Role struct {
	ID    uuid.UUID
	Key   string
	Label string
}

// Permission is a single (action, subject) grant.
type Permission struct {
	Action  string
	Subject string
}

// OAuthAccount is a linked third-party identity (social login). The unique
// (Provider, ProviderUserID) pair maps a provider account to a local user.
type OAuthAccount struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	Provider       string
	ProviderUserID string
	CreatedAt      time.Time
}

// Token is the persisted, hashed form of an email-verification or
// password-reset token. The plaintext token is never stored.
type Token struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	TokenHash  string
	ExpiresAt  time.Time
	ConsumedAt *time.Time
}

// Usable reports whether the token is unconsumed and not yet expired at now.
func (t Token) Usable(now time.Time) bool {
	return t.ConsumedAt == nil && now.Before(t.ExpiresAt)
}
