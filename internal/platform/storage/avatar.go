package storage

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/gabriel-vasile/mimetype"
)

// MaxAvatarBytes caps an avatar upload at 2 MiB. The cap is enforced on the
// number of bytes actually read, not a client-supplied Content-Length, so a
// lying header cannot bypass it.
const MaxAvatarBytes = 2 << 20 // 2 MiB

// avatar validation errors. They are sentinel values so handlers can map them
// to user-facing field messages without string matching.
var (
	// ErrAvatarTooLarge is returned when the upload exceeds MaxAvatarBytes.
	ErrAvatarTooLarge = errors.New("storage: avatar exceeds size limit")
	// ErrAvatarType is returned when the detected content type is not an allowed
	// raster image (or is an SVG, which is rejected outright).
	ErrAvatarType = errors.New("storage: avatar must be a PNG, JPEG, WebP or GIF image")
	// ErrAvatarEmpty is returned for a zero-byte upload.
	ErrAvatarEmpty = errors.New("storage: avatar is empty")
)

// allowedAvatarTypes maps an allow-listed image MIME to the canonical extension
// we store it under. The extension is ALWAYS derived from the sniffed MIME, never
// from the client filename, so a ".png" wrapper around a script cannot smuggle a
// dangerous extension onto disk. SVG is deliberately excluded: it is an XML
// document that can carry script, so it is never an acceptable avatar.
var allowedAvatarTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// ValidatedAvatar is the result of validating an avatar upload: the fully
// buffered bytes, the trusted (sniffed) content type, and the canonical
// extension to store under.
type ValidatedAvatar struct {
	Data        []byte
	ContentType string // sniffed, allow-listed MIME
	Ext         string // canonical extension derived from ContentType
}

// ValidateAvatar reads at most MaxAvatarBytes+1 from r, then verifies by MAGIC
// BYTES (not filename, not the request's Content-Type header) that the content
// is an allow-listed raster image within the size cap. It returns the buffered
// bytes plus the trusted MIME/extension, or a sentinel error.
//
// Reading one byte past the cap lets us distinguish "exactly at the limit" from
// "over the limit" without trusting any client-declared length.
func ValidateAvatar(r io.Reader) (ValidatedAvatar, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxAvatarBytes+1))
	if err != nil {
		return ValidatedAvatar{}, fmt.Errorf("read avatar: %w", err)
	}
	if len(data) == 0 {
		return ValidatedAvatar{}, ErrAvatarEmpty
	}
	if len(data) > MaxAvatarBytes {
		return ValidatedAvatar{}, ErrAvatarTooLarge
	}

	mt := mimetype.Detect(data)
	ext, ok := allowedAvatarTypes[mt.String()]
	if !ok {
		return ValidatedAvatar{}, fmt.Errorf("%w: got %s", ErrAvatarType, mt.String())
	}

	return ValidatedAvatar{Data: data, ContentType: mt.String(), Ext: ext}, nil
}

// ObjectKey builds a collision-resistant, path-safe storage key for a user's
// avatar: "avatars/<userID>/<random>.<ext>". The random component lets a new
// upload land at a fresh key (so caches never serve a stale image) while the old
// object is deleted explicitly by the service. ext MUST be a validated extension
// (it comes from ValidateAvatar), so no user input reaches the path.
func ObjectKey(prefix, userID, ext string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("storage: generate object key: %w", err)
	}
	return fmt.Sprintf("%s/%s/%s%s", prefix, userID, hex.EncodeToString(b[:]), ext), nil
}
