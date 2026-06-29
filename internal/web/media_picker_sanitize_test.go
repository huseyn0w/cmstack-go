package web

import (
	"strings"
	"testing"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
)

// TestEditorPickerInsertedImageSurvivesSanitization documents the M4 picker
// contract end-to-end at the security boundary: the picker inserts a plain
// <img src alt> into the editor, and the SAME server-side sanitizer that runs on
// every post/page/service save (kernel.SanitizeRichText) keeps that exact shape
// — while stripping the dangerous variants a tampered insertion could carry
// (onerror handlers, javascript:/data: src). So even though the inserted markup
// originates client-side, the saved body is always safe.
func TestEditorPickerInsertedImageSurvivesSanitization(t *testing.T) {
	// What the picker inserts (see editor.js insertImageFromPicker): a bare img
	// with a library src and alt, embedded in editor body HTML.
	inserted := `<p>Intro</p><img src="/uploads/media/2026/06/abc.png" alt="A sunset photo"><p>Outro</p>`
	out := kernel.SanitizeRichText(inserted)

	// The image and its src/alt are preserved (the legitimate insert path).
	for _, want := range []string{"<img", `src="/uploads/media/2026/06/abc.png"`, `alt="A sunset photo"`} {
		if !strings.Contains(out, want) {
			t.Errorf("sanitized body dropped %q:\n%s", want, out)
		}
	}

	// A tampered insertion carrying an event handler or a script/data src is
	// neutralized by the same sanitizer.
	for _, evil := range []string{
		`<img src="x" onerror="alert(1)">`,
		`<img src="javascript:alert(1)">`,
		`<img src="data:image/svg+xml,<svg onload=alert(1)>">`,
	} {
		got := kernel.SanitizeRichText(evil)
		if strings.Contains(got, "onerror") || strings.Contains(got, "javascript:") || strings.Contains(got, "onload") {
			t.Errorf("sanitizer failed to neutralize %q -> %q", evil, got)
		}
	}
}
