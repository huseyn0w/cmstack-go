package storage

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

// ThumbnailSpec names a variant and its bounding box. The image is scaled to FIT
// within MaxW x MaxH preserving aspect ratio (never upscaled), so the variant is
// at most that size on each axis.
type ThumbnailSpec struct {
	Variant string
	MaxW    int
	MaxH    int
}

// DefaultThumbnailSpecs are the variants generated for every raster upload: a
// small square-ish "thumb" for the grid card and a larger "medium" for previews.
// They are bounding boxes (fit, preserve aspect, no upscale).
var DefaultThumbnailSpecs = []ThumbnailSpec{
	{Variant: "thumb", MaxW: 320, MaxH: 320},
	{Variant: "medium", MaxW: 1024, MaxH: 1024},
}

// GeneratedThumbnail is one rendered variant ready to store: its label, encoded
// bytes, content type, extension, and actual pixel dimensions.
type GeneratedThumbnail struct {
	Variant     string
	Data        []byte
	ContentType string
	Ext         string
	Width       int
	Height      int
}

// GenerateThumbnails decodes the raster image bytes once and renders each spec
// via high-quality Catmull-Rom scaling (golang.org/x/image/draw — maintained,
// pure-Go; the unmaintained disintegration/imaging is deliberately avoided).
// Variants are encoded as PNG to preserve transparency and stay format-agnostic;
// the grid serves them with the same hardened nosniff handler as originals.
// A variant whose source is already smaller than the box is emitted at the
// source size (no upscaling). The source MIME selects the decoder.
func GenerateThumbnails(data []byte, sourceMIME string, specs []ThumbnailSpec) ([]GeneratedThumbnail, error) {
	src, err := decodeRaster(data, sourceMIME)
	if err != nil {
		return nil, fmt.Errorf("decode source for thumbnails: %w", err)
	}
	b := src.Bounds()
	srcW, srcH := b.Dx(), b.Dy()

	out := make([]GeneratedThumbnail, 0, len(specs))
	for _, spec := range specs {
		w, h := fitWithin(srcW, srcH, spec.MaxW, spec.MaxH)
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)

		var buf bytes.Buffer
		if err := png.Encode(&buf, dst); err != nil {
			return nil, fmt.Errorf("encode %s thumbnail: %w", spec.Variant, err)
		}
		out = append(out, GeneratedThumbnail{
			Variant:     spec.Variant,
			Data:        buf.Bytes(),
			ContentType: "image/png",
			Ext:         ".png",
			Width:       w,
			Height:      h,
		})
	}
	return out, nil
}

// decodeRaster decodes data using the decoder matching sourceMIME. WebP is
// decode-only (golang.org/x/image/webp, registered in media.go); GIF decodes the
// first frame, which is the correct thumbnail source.
func decodeRaster(data []byte, sourceMIME string) (image.Image, error) {
	switch sourceMIME {
	case "image/jpeg":
		return jpeg.Decode(bytes.NewReader(data))
	case "image/png":
		return png.Decode(bytes.NewReader(data))
	case "image/gif":
		return gif.Decode(bytes.NewReader(data))
	default:
		// image.Decode dispatches via the registered format probers (covers webp
		// and any of the above as a fallback).
		img, _, err := image.Decode(bytes.NewReader(data))
		return img, err
	}
}

// fitWithin returns the largest w x h that fits inside maxW x maxH while
// preserving the srcW:srcH aspect ratio. It never upscales: if the source is
// already within the box, the source size is returned. The result is at least
// 1x1 so encoding never fails on a degenerate ratio.
func fitWithin(srcW, srcH, maxW, maxH int) (int, int) {
	if srcW <= 0 || srcH <= 0 {
		return 1, 1
	}
	if srcW <= maxW && srcH <= maxH {
		return srcW, srcH
	}
	scaleW := float64(maxW) / float64(srcW)
	scaleH := float64(maxH) / float64(srcH)
	scale := scaleW
	if scaleH < scale {
		scale = scaleH
	}
	w := int(float64(srcW) * scale)
	h := int(float64(srcH) * scale)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return w, h
}
