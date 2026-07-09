package apitoken

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Repo is the persistence port for API tokens. The pg implementation
// (RepoPG) is the only layer touching generated SQL.
type Repo interface {
	// Create inserts a new token row and returns the stored metadata.
	Create(ctx context.Context, in CreateParams) (Token, error)
	// GetByHash returns the VALID token (not revoked, not expired) whose
	// token_hash equals hash, or ErrNotFound when none matches.
	GetByHash(ctx context.Context, hash string) (Token, error)
	// Touch stamps last_used_at = now() for the token id (best-effort).
	Touch(ctx context.Context, id uuid.UUID) error
	// ListByUser returns every token owned by userID, newest first.
	ListByUser(ctx context.Context, userID uuid.UUID) ([]Token, error)
	// Revoke marks the token revoked, scoped to its owner so a caller can only
	// revoke its own tokens.
	Revoke(ctx context.Context, id, userID uuid.UUID) error
}

// ErrNotFound is the sentinel a Repo returns when no matching token exists.
var ErrNotFound = errors.New("apitoken: not found")

// CreateParams is the fully-prepared row the repo inserts. The service has
// already generated the secret, computed its hash, and extracted last_four.
type CreateParams struct {
	UserID    uuid.UUID
	Name      string
	TokenHash string
	LastFour  string
	ExpiresAt *time.Time
}

// Service holds all token issue/verify logic over the Repo port.
type Service struct {
	repo Repo
}

// NewService constructs the token Service with its persistence port.
func NewService(repo Repo) *Service { return &Service{repo: repo} }

// Generate mints a new API token for userID: it draws cryptographically-random
// bytes, formats the plaintext as "cmg_<hex>", stores only its SHA-256 hex
// digest plus the plaintext's last 4 characters, and returns the PLAINTEXT once
// (the caller must display it immediately — it is never recoverable afterwards).
// A nil expiresAt issues a non-expiring token.
func (s *Service) Generate(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time) (string, Token, error) {
	raw := make([]byte, randomBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", Token{}, err
	}
	plaintext := tokenPrefix + hex.EncodeToString(raw)

	tok, err := s.repo.Create(ctx, CreateParams{
		UserID:    userID,
		Name:      strings.TrimSpace(name),
		TokenHash: hashToken(plaintext),
		LastFour:  lastFour(plaintext),
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return "", Token{}, err
	}
	return plaintext, tok, nil
}

// Verify resolves a presented plaintext token to its owner's user id. It hashes
// the input and looks the digest up among the VALID (non-revoked, non-expired)
// tokens; on a hit it best-effort stamps last_used_at (a touch failure never
// changes the auth decision) and returns (userID, true, nil). An empty or
// malformed input, or no match, yields (uuid.Nil, false, nil) with no error, so
// the caller can fail closed without distinguishing "absent" from "wrong".
func (s *Service) Verify(ctx context.Context, plaintext string) (uuid.UUID, bool, error) {
	plaintext = strings.TrimSpace(plaintext)
	if plaintext == "" || !strings.HasPrefix(plaintext, tokenPrefix) {
		return uuid.Nil, false, nil
	}
	tok, err := s.repo.GetByHash(ctx, hashToken(plaintext))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, err
	}
	// Best-effort usage stamp; its failure must not affect the auth decision.
	_ = s.repo.Touch(ctx, tok.ID)
	return tok.UserID, true, nil
}

// List returns every token owned by userID, newest first (admin/management use).
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Token, error) {
	return s.repo.ListByUser(ctx, userID)
}

// Revoke marks a token revoked, scoped to its owner so a caller may only revoke
// its own tokens. Revoking an already-revoked or foreign token is a no-op.
func (s *Service) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.Revoke(ctx, id, userID)
}

// hashToken returns the lowercase SHA-256 hex digest of the plaintext token —
// the only representation persisted or matched against.
func hashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// lastFour returns the trailing 4 characters of the plaintext (or the whole
// string when shorter) for a non-secret display hint.
func lastFour(plaintext string) string {
	if len(plaintext) <= 4 {
		return plaintext
	}
	return plaintext[len(plaintext)-4:]
}
