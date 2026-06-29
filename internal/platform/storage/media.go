package storage

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"

	// Register the image decoders we accept so image.DecodeConfig can probe
	// dimensions WITHOUT a full decode. JPEG/PNG/GIF are std; WebP comes from
	// golang.org/x/image. These blank imports only register format probers.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/gabriel-vasile/mimetype"
	_ "golang.org/x/image/webp"
)

// DefaultMaxMediaBytes caps a media upload at 10 MiB. Like the avatar cap it is
// enforced on bytes ACTUALLY read, never a client-declared Content-Length, so a
// lying header cannot bypass it. The cap is configurable per Validator.
const DefaultMaxMediaBytes = 10 << 20 // 10 MiB

// MaxImagePixels is the decompression-bomb guard threshold (django parity). The
// image's declared width*height (read from the header via DecodeConfig BEFORE any
// full decode) must not exceed this, so a tiny file declaring a 50000x50000
// canvas is rejected before it can allocate gigabytes during decode. 40 MP
// comfortably exceeds any legitimate web image (e.g. 6000x6000) while blocking
// the multi-hundred-megapixel bombs that exhaust memory.
const MaxImagePixels = 40_000_000 // 40 megapixels

// media validation errors. Sentinels so handlers map them to field messages
// without string matching.
var (
	// ErrMediaTooLarge is returned when the upload exceeds the size cap.
	ErrMediaTooLarge = errors.New("storage: file exceeds size limit")
	// ErrMediaEmpty is returned for a zero-byte upload.
	ErrMediaEmpty = errors.New("storage: file is empty")
	// ErrMediaType is returned when the magic-byte-detected type is not in the
	// allow-list (this is the SVG/polyglot rejection: an SVG sniffs as
	// image/svg+xml or text/* and is not allow-listed, so it is rejected).
	ErrMediaType = errors.New("storage: file type is not allowed")
	// ErrMediaDimensions is returned when a raster image's declared pixel area
	// exceeds MaxImagePixels (decompression-bomb guard) or its header is
	// unreadable.
	ErrMediaDimensions = errors.New("storage: image dimensions exceed the allowed maximum")
)

// MediaKind classifies an allow-listed asset so callers know whether to expect
// pixel dimensions / thumbnails (raster) or not (document).
type MediaKind int

const (
	// KindRaster is a bitmap image we can probe + thumbnail (jpg/png/gif/webp).
	KindRaster MediaKind = iota
	// KindDocument is a non-image asset stored as-is (pdf): no dimensions, no
	// thumbnail (a generic icon is shown in the UI).
	KindDocument
)

// allowedMediaType describes one allow-listed MIME: its canonical stored
// extension (derived from the VALIDATED MIME, never the client filename — the
// anti-polyglot rule) and its kind. SVG is deliberately ABSENT: it is an XML
// document that can carry script, so it is never acceptable.
type allowedMediaType struct {
	ext  string
	kind MediaKind
}

// allowedMediaTypes is the canonical allow-list (jpg/png/gif/webp/pdf), keyed by
// the sniffed MIME string. Anything not present here is rejected.
var allowedMediaTypes = map[string]allowedMediaType{
	"image/jpeg":      {ext: ".jpg", kind: KindRaster},
	"image/png":       {ext: ".png", kind: KindRaster},
	"image/gif":       {ext: ".gif", kind: KindRaster},
	"image/webp":      {ext: ".webp", kind: KindRaster},
	"application/pdf": {ext: ".pdf", kind: KindDocument},
}

// ValidatedMedia is the result of validating a media upload: the buffered bytes,
// the trusted (sniffed) MIME, the canonical extension, the asset kind, and —
// for raster images — the probed pixel dimensions. The service trusts these
// fields because validation produced them.
type ValidatedMedia struct {
	Data        []byte
	ContentType string
	Ext         string
	Kind        MediaKind
	Width       int // 0 for documents
	Height      int // 0 for documents
}

// IsRaster reports whether the asset is a bitmap image (has dimensions / can be
// thumbnailed).
func (m ValidatedMedia) IsRaster() bool { return m.Kind == KindRaster }

// Validator validates media uploads against an allow-list with a configurable
// size cap and decompression-bomb threshold. The zero value is NOT ready; use
// NewValidator.
type Validator struct {
	maxBytes  int64
	maxPixels int
}

// NewValidator constructs a media Validator. A non-positive maxBytes or
// maxPixels falls back to the defaults, so callers can pass 0 to accept them.
func NewValidator(maxBytes int64, maxPixels int) *Validator {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxMediaBytes
	}
	if maxPixels <= 0 {
		maxPixels = MaxImagePixels
	}
	return &Validator{maxBytes: maxBytes, maxPixels: maxPixels}
}

// MaxBytes returns the configured size cap (for the UI hint / form limit).
func (v *Validator) MaxBytes() int64 { return v.maxBytes }

// Validate reads at most maxBytes+1 from r, then enforces (in order): non-empty,
// within the size cap, an allow-listed MIME by MAGIC BYTES (not filename/declared
// type), and — for raster images — a header-probed pixel area within the bomb
// threshold. The stored extension is taken from the validated MIME. It returns
// the buffered bytes plus trusted metadata, or a sentinel error.
func (v *Validator) Validate(r io.Reader) (ValidatedMedia, error) {
	data, err := io.ReadAll(io.LimitReader(r, v.maxBytes+1))
	if err != nil {
		return ValidatedMedia{}, fmt.Errorf("read media: %w", err)
	}
	if len(data) == 0 {
		return ValidatedMedia{}, ErrMediaEmpty
	}
	if int64(len(data)) > v.maxBytes {
		return ValidatedMedia{}, ErrMediaTooLarge
	}

	mt := mimetype.Detect(data)
	allowed, ok := allowedMediaTypes[mt.String()]
	if !ok {
		return ValidatedMedia{}, fmt.Errorf("%w: got %s", ErrMediaType, mt.String())
	}

	out := ValidatedMedia{
		Data:        data,
		ContentType: mt.String(),
		Ext:         allowed.ext,
		Kind:        allowed.kind,
	}

	if allowed.kind == KindRaster {
		w, h, derr := probeDimensions(data, v.maxPixels)
		if derr != nil {
			return ValidatedMedia{}, derr
		}
		out.Width, out.Height = w, h
	}

	return out, nil
}

// probeDimensions reads ONLY the image header via image.DecodeConfig (no full
// decode, no pixel allocation) and rejects an image whose declared area exceeds
// maxPixels — the decompression-bomb guard. A header that cannot be parsed is
// also rejected (a raster MIME with an unreadable header is not a usable image).
func probeDimensions(data []byte, maxPixels int) (int, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, fmt.Errorf("%w: unreadable header: %v", ErrMediaDimensions, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, fmt.Errorf("%w: non-positive dimensions", ErrMediaDimensions)
	}
	// int64 multiply to avoid overflow before the comparison.
	if int64(cfg.Width)*int64(cfg.Height) > int64(maxPixels) {
		return 0, 0, fmt.Errorf("%w: %dx%d exceeds %d px", ErrMediaDimensions, cfg.Width, cfg.Height, maxPixels)
	}
	return cfg.Width, cfg.Height, nil
}
