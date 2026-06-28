package storage_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/huseyn0w/cmstack-go/internal/platform/storage"
)

// pngMagic is a minimal valid PNG signature + IHDR so mimetype detects image/png.
var pngMagic = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk header
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89,
}

// gifMagic is a GIF89a header.
var gifMagic = append([]byte("GIF89a"), make([]byte, 16)...)

func TestValidateAvatar_GoodPNGPasses(t *testing.T) {
	got, err := storage.ValidateAvatar(bytes.NewReader(pngMagic))
	if err != nil {
		t.Fatalf("expected png to pass, got %v", err)
	}
	if got.ContentType != "image/png" {
		t.Errorf("content type = %q, want image/png", got.ContentType)
	}
	if got.Ext != ".png" {
		t.Errorf("ext = %q, want .png", got.Ext)
	}
	if !bytes.Equal(got.Data, pngMagic) {
		t.Error("buffered data should equal input")
	}
}

func TestValidateAvatar_GIFPasses(t *testing.T) {
	got, err := storage.ValidateAvatar(bytes.NewReader(gifMagic))
	if err != nil {
		t.Fatalf("expected gif to pass, got %v", err)
	}
	if got.Ext != ".gif" {
		t.Errorf("ext = %q, want .gif", got.Ext)
	}
}

func TestValidateAvatar_SVGRejected(t *testing.T) {
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	_, err := storage.ValidateAvatar(bytes.NewReader(svg))
	if !errors.Is(err, storage.ErrAvatarType) {
		t.Fatalf("expected ErrAvatarType for SVG, got %v", err)
	}
}

func TestValidateAvatar_WrongMagicRejected(t *testing.T) {
	// A plain-text payload with a lying ".png" intent must be rejected: detection
	// is by magic bytes, not by any declared type or filename.
	_, err := storage.ValidateAvatar(strings.NewReader("this is not an image, it is plain text content"))
	if !errors.Is(err, storage.ErrAvatarType) {
		t.Fatalf("expected ErrAvatarType for text, got %v", err)
	}
}

func TestValidateAvatar_OversizedRejected(t *testing.T) {
	big := make([]byte, storage.MaxAvatarBytes+1)
	copy(big, pngMagic)
	_, err := storage.ValidateAvatar(bytes.NewReader(big))
	if !errors.Is(err, storage.ErrAvatarTooLarge) {
		t.Fatalf("expected ErrAvatarTooLarge, got %v", err)
	}
}

func TestValidateAvatar_AtSizeLimitPasses(t *testing.T) {
	atLimit := make([]byte, storage.MaxAvatarBytes)
	copy(atLimit, pngMagic)
	if _, err := storage.ValidateAvatar(bytes.NewReader(atLimit)); err != nil {
		t.Fatalf("exactly-at-limit png should pass, got %v", err)
	}
}

func TestValidateAvatar_EmptyRejected(t *testing.T) {
	_, err := storage.ValidateAvatar(bytes.NewReader(nil))
	if !errors.Is(err, storage.ErrAvatarEmpty) {
		t.Fatalf("expected ErrAvatarEmpty, got %v", err)
	}
}

func TestObjectKey_DerivesFromValidatedExtOnly(t *testing.T) {
	key, err := storage.ObjectKey("avatars", "user-123", ".png")
	if err != nil {
		t.Fatalf("ObjectKey: %v", err)
	}
	if !strings.HasPrefix(key, "avatars/user-123/") || !strings.HasSuffix(key, ".png") {
		t.Errorf("unexpected key shape: %q", key)
	}
	// Two calls must differ (random component) so a re-upload busts caches.
	key2, _ := storage.ObjectKey("avatars", "user-123", ".png")
	if key == key2 {
		t.Error("object keys should be unique per call")
	}
}
