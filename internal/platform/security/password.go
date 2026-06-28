package security

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// PasswordHasher hashes and verifies passwords using argon2id with OWASP
// recommended parameters. Hashes are encoded in the standard PHC string format
// ($argon2id$v=19$m=...,t=...,p=...$salt$hash) so parameters travel with the
// hash and can be tuned over time without breaking existing credentials.
type PasswordHasher struct {
	memory      uint32 // KiB
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

// NewPasswordHasher returns a PasswordHasher configured with the OWASP-recommended
// argon2id parameters (second configuration: 19 MiB, 2 iterations, 1 lane).
func NewPasswordHasher() *PasswordHasher {
	return &PasswordHasher{
		memory:      19 * 1024, // 19 MiB
		iterations:  2,
		parallelism: 1,
		saltLength:  16,
		keyLength:   32,
	}
}

// ErrInvalidHash is returned when an encoded hash cannot be parsed.
var ErrInvalidHash = errors.New("security: invalid argon2 hash encoding")

// ErrIncompatibleVersion is returned when the encoded hash uses an argon2
// version this binary does not support.
var ErrIncompatibleVersion = errors.New("security: incompatible argon2 version")

// Hash derives an argon2id hash of password and returns it in PHC string format.
func (h *PasswordHasher) Hash(password string) (string, error) {
	salt := make([]byte, h.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, h.iterations, h.memory, h.parallelism, h.keyLength)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(key)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.memory, h.iterations, h.parallelism, b64Salt, b64Hash,
	), nil
}

// Verify reports whether password matches the encoded argon2id hash. It is
// constant-time with respect to the derived key comparison. A malformed encoded
// hash returns (false, ErrInvalidHash).
func (h *PasswordHasher) Verify(password, encoded string) (bool, error) {
	params, salt, hash, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}

	other := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, uint32(len(hash)))

	if subtle.ConstantTimeEq(int32(len(hash)), int32(len(other))) == 0 {
		return false, nil
	}
	if subtle.ConstantTimeCompare(hash, other) == 1 {
		return true, nil
	}
	return false, nil
}

type hashParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func decodeHash(encoded string) (hashParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return hashParams{}, nil, nil, ErrInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return hashParams{}, nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return hashParams{}, nil, nil, ErrIncompatibleVersion
	}

	var p hashParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism); err != nil {
		return hashParams{}, nil, nil, ErrInvalidHash
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return hashParams{}, nil, nil, ErrInvalidHash
	}
	hash, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return hashParams{}, nil, nil, ErrInvalidHash
	}

	return p, salt, hash, nil
}
