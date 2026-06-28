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
	RoleID          uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
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
