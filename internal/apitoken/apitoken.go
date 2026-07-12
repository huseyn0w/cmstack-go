// Package apitoken issues and verifies bearer tokens for the REST API (M17-1).
//
// A token is a cryptographically-random secret shown to the operator exactly
// once at creation. Only its SHA-256 hex digest is persisted (never the
// plaintext), so a database leak cannot reveal a usable credential. Verification
// hashes the presented plaintext and looks the digest up by its unique column,
// which is itself a constant-time-equivalent match (the plaintext is never
// compared byte-by-byte in application code).
//
// All business rules — validity (not revoked, not expired), best-effort
// last-used stamping — live in the Service. Data access is only through the Repo
// interface; the pg implementation is the sole layer touching generated SQL.
package apitoken

import (
	"time"

	"github.com/google/uuid"
)

// tokenPrefix is the human-recognisable, non-secret prefix every plaintext
// token carries. It lets operators and log scrubbers spot a Agentic CMS API token by
// shape without revealing anything about its secret bytes.
const tokenPrefix = "cmg_"

// randomBytes is the number of cryptographically-random bytes behind each token
// (256 bits of entropy), hex-encoded into the plaintext after the prefix.
const randomBytes = 32

// Token is the persisted, non-secret metadata for an issued API token. The
// plaintext secret is NEVER stored on it; Generate returns the plaintext
// separately, once.
type Token struct {
	// ID is the token's stable primary key.
	ID uuid.UUID
	// UserID is the owner the token authenticates as.
	UserID uuid.UUID
	// Name is the human label the operator gave the token.
	Name string
	// LastFour is the trailing 4 characters of the plaintext, a non-secret
	// display hint so an operator can recognise a token in a list.
	LastFour string
	// ExpiresAt is when the token stops being valid; nil means it never expires.
	ExpiresAt *time.Time
	// LastUsedAt is the best-effort timestamp of the last successful verify; nil
	// until first use.
	LastUsedAt *time.Time
	// RevokedAt is when the token was revoked; nil means active.
	RevokedAt *time.Time
	// CreatedAt is when the token was issued.
	CreatedAt time.Time
}
