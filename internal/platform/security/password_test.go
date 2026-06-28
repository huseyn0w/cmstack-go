package security

import (
	"strings"
	"testing"
)

func TestPasswordHashVerifyRoundTrip(t *testing.T) {
	h := NewPasswordHasher()
	encoded, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
		t.Fatalf("encoded hash has unexpected prefix: %q", encoded)
	}

	ok, err := h.Verify("correct horse battery staple", encoded)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Error("Verify returned false for the correct password")
	}
}

func TestPasswordVerifyWrongPassword(t *testing.T) {
	h := NewPasswordHasher()
	encoded, err := h.Hash("s3cret-password")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	ok, err := h.Verify("wrong-password", encoded)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if ok {
		t.Error("Verify returned true for an incorrect password")
	}
}

func TestPasswordHashesAreSalted(t *testing.T) {
	h := NewPasswordHasher()
	a, _ := h.Hash("same-password")
	b, _ := h.Hash("same-password")
	if a == b {
		t.Error("two hashes of the same password are identical; salt not applied")
	}
}

func TestPasswordVerifyInvalidEncoding(t *testing.T) {
	h := NewPasswordHasher()
	tests := []string{
		"",
		"not-a-hash",
		"$argon2id$v=19$bad-params$c2FsdA$aGFzaA",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA", // wrong variant
	}
	for _, enc := range tests {
		if _, err := h.Verify("x", enc); err == nil {
			t.Errorf("Verify(%q) expected an error, got nil", enc)
		}
	}
}

func TestPasswordVerifyIncompatibleVersion(t *testing.T) {
	h := NewPasswordHasher()
	if _, err := h.Verify("x", "$argon2id$v=18$m=19456,t=2,p=1$c2FsdA$aGFzaA"); err != ErrIncompatibleVersion {
		t.Errorf("expected ErrIncompatibleVersion, got %v", err)
	}
}
