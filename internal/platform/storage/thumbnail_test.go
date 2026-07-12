package storage_test

import (
	"bytes"
	"image"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/storage"
)

func decodeDims(t *testing.T, data []byte) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode config: %v", err)
	}
	return cfg.Width, cfg.Height
}

func TestGenerateThumbnails_ScalesDownPreservingAspect(t *testing.T) {
	src := makePNG(t, 800, 400) // 2:1
	thumbs, err := storage.GenerateThumbnails(src, "image/png", []storage.ThumbnailSpec{
		{Variant: "thumb", MaxW: 100, MaxH: 100},
	})
	if err != nil {
		t.Fatalf("GenerateThumbnails: %v", err)
	}
	if len(thumbs) != 1 {
		t.Fatalf("want 1 variant, got %d", len(thumbs))
	}
	th := thumbs[0]
	if th.Variant != "thumb" || th.ContentType != "image/png" || th.Ext != ".png" {
		t.Errorf("variant meta = %+v", th)
	}
	// 2:1 fit into 100x100 => 100x50.
	if th.Width != 100 || th.Height != 50 {
		t.Errorf("scaled dims = %dx%d, want 100x50", th.Width, th.Height)
	}
	w, h := decodeDims(t, th.Data)
	if w != 100 || h != 50 {
		t.Errorf("encoded dims = %dx%d, want 100x50", w, h)
	}
}

func TestGenerateThumbnails_NeverUpscales(t *testing.T) {
	src := makePNG(t, 40, 30) // smaller than the box
	thumbs, err := storage.GenerateThumbnails(src, "image/png", []storage.ThumbnailSpec{
		{Variant: "thumb", MaxW: 320, MaxH: 320},
	})
	if err != nil {
		t.Fatalf("GenerateThumbnails: %v", err)
	}
	if thumbs[0].Width != 40 || thumbs[0].Height != 30 {
		t.Errorf("upscaled to %dx%d; should stay 40x30", thumbs[0].Width, thumbs[0].Height)
	}
}

func TestGenerateThumbnails_MultipleVariants(t *testing.T) {
	src := makeJPEG(t, 2000, 1000)
	thumbs, err := storage.GenerateThumbnails(src, "image/jpeg", storage.DefaultThumbnailSpecs)
	if err != nil {
		t.Fatalf("GenerateThumbnails: %v", err)
	}
	if len(thumbs) != len(storage.DefaultThumbnailSpecs) {
		t.Fatalf("want %d variants, got %d", len(storage.DefaultThumbnailSpecs), len(thumbs))
	}
	// thumb (<=320) and medium (<=1024) both fit by width (2:1).
	if thumbs[0].Width != 320 || thumbs[1].Width != 1024 {
		t.Errorf("variant widths = %d, %d", thumbs[0].Width, thumbs[1].Width)
	}
}
