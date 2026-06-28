package accounts

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// tokenBytes is the entropy of a generated token (32 bytes = 256 bits).
const tokenBytes = 32

// generateToken returns a URL-safe random plaintext token and its sha-256 hex
// hash. The plaintext is delivered to the user (in an email link); only the
// hash is ever persisted, so a database read cannot reconstruct usable tokens.
func generateToken() (plaintext, hash string, err error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	plaintext = base64.RawURLEncoding.EncodeToString(b)
	return plaintext, hashToken(plaintext), nil
}

// hashToken returns the hex-encoded sha-256 of a plaintext token. Lookups hash
// the incoming token and compare against the stored hash.
func hashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
