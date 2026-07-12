package storage_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/storage"
)

// makePNG encodes a solid w x h PNG — a real raster the validator can probe.
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 120, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// makeNoisyPNG encodes a PNG of pseudo-random pixels so it does NOT compress
// well — useful when a test needs the encoded bytes to exceed a small size cap.
func makeNoisyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(2166136261)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			seed = seed*16777619 + uint32(x*31+y*7)
			img.Set(x, y, color.RGBA{R: uint8(seed), G: uint8(seed >> 8), B: uint8(seed >> 16), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode noisy png: %v", err)
	}
	return buf.Bytes()
}

func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func makeGIF(t *testing.T, w, h int) []byte {
	t.Helper()
	pal := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White})
	var buf bytes.Buffer
	if err := gif.Encode(&buf, pal, nil); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

// minimalPDF is a tiny but valid PDF header so mimetype detects application/pdf.
var minimalPDF = []byte("%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF")

func newValidator() *storage.Validator { return storage.NewValidator(0, 0) }

func TestValidate_GoodPNGPasses(t *testing.T) {
	got, err := newValidator().Validate(bytes.NewReader(makePNG(t, 64, 48)))
	if err != nil {
		t.Fatalf("png should pass: %v", err)
	}
	if got.ContentType != "image/png" || got.Ext != ".png" {
		t.Errorf("mime/ext = %q/%q", got.ContentType, got.Ext)
	}
	if !got.IsRaster() || got.Width != 64 || got.Height != 48 {
		t.Errorf("dims = %dx%d raster=%v", got.Width, got.Height, got.IsRaster())
	}
}

func TestValidate_GoodJPEGPasses(t *testing.T) {
	got, err := newValidator().Validate(bytes.NewReader(makeJPEG(t, 32, 32)))
	if err != nil {
		t.Fatalf("jpeg should pass: %v", err)
	}
	if got.Ext != ".jpg" {
		t.Errorf("ext = %q, want .jpg (derived from validated MIME, not filename)", got.Ext)
	}
}

func TestValidate_GoodGIFPasses(t *testing.T) {
	got, err := newValidator().Validate(bytes.NewReader(makeGIF(t, 16, 16)))
	if err != nil {
		t.Fatalf("gif should pass: %v", err)
	}
	if got.Ext != ".gif" {
		t.Errorf("ext = %q", got.Ext)
	}
}

func TestValidate_GoodWebPPasses(t *testing.T) {
	// A minimal valid lossy WebP (RIFF....WEBPVP8 ) header. mimetype sniffs by the
	// RIFF/WEBP magic; the decoder reads the VP8 dimensions.
	webpData := webpFixture()
	got, err := newValidator().Validate(bytes.NewReader(webpData))
	if err != nil {
		t.Fatalf("webp should pass: %v", err)
	}
	if got.ContentType != "image/webp" || got.Ext != ".webp" {
		t.Errorf("mime/ext = %q/%q", got.ContentType, got.Ext)
	}
}

func TestValidate_GoodPDFPasses(t *testing.T) {
	got, err := newValidator().Validate(bytes.NewReader(minimalPDF))
	if err != nil {
		t.Fatalf("pdf should pass: %v", err)
	}
	if got.ContentType != "application/pdf" || got.Ext != ".pdf" {
		t.Errorf("mime/ext = %q/%q", got.ContentType, got.Ext)
	}
	if got.IsRaster() {
		t.Error("pdf must be a document, not raster (no dimensions/thumbnail)")
	}
	if got.Width != 0 || got.Height != 0 {
		t.Error("pdf should carry no dimensions")
	}
}

func TestValidate_SVGRejected(t *testing.T) {
	svg := []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	_, err := newValidator().Validate(bytes.NewReader(svg))
	if !errors.Is(err, storage.ErrMediaType) {
		t.Fatalf("SVG must be rejected with ErrMediaType, got %v", err)
	}
}

func TestValidate_PolyglotRejected(t *testing.T) {
	// A GIF-magic prefix followed by an SVG/script payload. Detection is by the
	// leading magic bytes — this sniffs as image/gif — but it is NOT a decodable
	// raster, so the dimension probe rejects it. Either way it never becomes a row.
	poly := append([]byte("GIF89a"), []byte(`<svg onload="alert(1)"><script>x</script>`)...)
	_, err := newValidator().Validate(bytes.NewReader(poly))
	if err == nil {
		t.Fatal("polyglot must be rejected")
	}
	if !errors.Is(err, storage.ErrMediaDimensions) && !errors.Is(err, storage.ErrMediaType) {
		t.Fatalf("polyglot rejection should be a dimensions/type error, got %v", err)
	}
}

func TestValidate_WrongMagicRejected(t *testing.T) {
	_, err := newValidator().Validate(strings.NewReader("just some plain text, definitely not an allowed media file"))
	if !errors.Is(err, storage.ErrMediaType) {
		t.Fatalf("plain text must be rejected with ErrMediaType, got %v", err)
	}
}

func TestValidate_OversizedRejected(t *testing.T) {
	v := storage.NewValidator(1024, 0) // 1 KB cap
	big := makeNoisyPNG(t, 200, 200)   // random pixels -> incompressible, > 1 KB
	if len(big) <= 1024 {
		t.Fatalf("fixture should exceed 1 KB, got %d bytes", len(big))
	}
	_, err := v.Validate(bytes.NewReader(big))
	if !errors.Is(err, storage.ErrMediaTooLarge) {
		t.Fatalf("oversized must be rejected with ErrMediaTooLarge, got %v", err)
	}
}

func TestValidate_EmptyRejected(t *testing.T) {
	_, err := newValidator().Validate(bytes.NewReader(nil))
	if !errors.Is(err, storage.ErrMediaEmpty) {
		t.Fatalf("empty must be rejected with ErrMediaEmpty, got %v", err)
	}
}

func TestValidate_DecompressionBombRejected(t *testing.T) {
	// A real but huge-canvas PNG: tiny on disk (solid color compresses well) yet
	// declares an enormous pixel area. The header probe rejects it BEFORE a full
	// decode could allocate gigabytes — the django-parity bomb guard.
	v := storage.NewValidator(50<<20, 1_000_000) // 1 MP threshold
	bomb := makePNG(t, 4000, 4000)               // 16 MP >> 1 MP
	_, err := v.Validate(bytes.NewReader(bomb))
	if !errors.Is(err, storage.ErrMediaDimensions) {
		t.Fatalf("decompression bomb must be rejected with ErrMediaDimensions, got %v", err)
	}
}

func TestValidate_AtPixelLimitPasses(t *testing.T) {
	v := storage.NewValidator(50<<20, 64*48) // exactly the fixture area
	if _, err := v.Validate(bytes.NewReader(makePNG(t, 64, 48))); err != nil {
		t.Fatalf("image exactly at the pixel limit should pass, got %v", err)
	}
}

// webpFixture returns a minimal valid VP8 lossy WebP (8x8) so the webp decoder
// can read it. Hand-built so the test has no binary asset dependency.
func webpFixture() []byte {
	// This is a known-good 1x1 white lossy WebP produced by cwebp, base of the
	// fixture used widely in Go webp tests.
	return []byte{
		0x52, 0x49, 0x46, 0x46, 0x1a, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50,
		0x56, 0x50, 0x38, 0x20, 0x0e, 0x00, 0x00, 0x00, 0xb0, 0x01, 0x00, 0x9d,
		0x01, 0x2a, 0x01, 0x00, 0x01, 0x00, 0x02, 0x00, 0x34, 0x25, 0xa4, 0x00,
		0x03, 0x70, 0x00, 0xfe, 0xfb, 0xfd, 0x50, 0x00,
	}
}
