package templ_test

import (
	"testing"

	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

func sampleImageCard(id, title string) webtempl.MediaCard {
	return webtempl.MediaCard{
		ID: id, Title: title, RawTitle: title, Alt: "alt", IsImage: true,
		ThumbURL: "/uploads/media/thumb-" + id + ".png", FullURL: "/uploads/media/" + id + ".png",
		MIMELabel: "PNG", Dimensions: "800 × 600", SizeLabel: "2.0 KB",
		EditURL: "/admin/media/" + id + "/detail", DeleteURL: "/admin/media/" + id + "/delete",
		Uploaded: "Jan 1, 2026 10:00",
	}
}

func TestMediaLibrary_GridAndDropzone(t *testing.T) {
	v := webtempl.MediaListView{
		Cards:      []webtempl.MediaCard{sampleImageCard("m1", "Sunset")},
		Pager:      webtempl.Pagination{Page: 1, PageSize: 24, Total: 1},
		UploadURL:  "/admin/media",
		BulkURL:    "/admin/media/bulk",
		MaxBytes:   10 << 20,
		AcceptHint: "JPG, PNG, GIF, WebP, PDF",
		AcceptAttr: "image/png,application/pdf",
	}
	html := renderStr(t, webtempl.MediaLibrary(v))
	mustContain(
		t, html,
		`data-testid="media-library"`,
		`data-testid="media-grid"`,
		`data-testid="media-card-m1"`,
		"Sunset",
		// Dropzone + accessible native file input.
		`data-testid="media-dropzone"`,
		`data-testid="media-file-input"`,
		`type="file"`,
		`multiple`,
		`accept="image/png,application/pdf"`,
		// Accepted-types/size hints.
		`data-testid="media-accept-hint"`,
		"JPG, PNG, GIF, WebP, PDF",
		"10.0 MB",
		// Per-file progress queue announced via aria-live.
		`data-testid="media-upload-queue"`,
		`aria-live="polite"`,
		// noscript fallback so upload works without JS.
		"<noscript>",
		// Delete + detail modals present.
		`data-testid="media-delete-modal"`,
		`data-testid="media-detail-modal"`,
	)
}

func TestMediaLibrary_DropzoneAccessibility(t *testing.T) {
	v := webtempl.MediaListView{UploadURL: "/admin/media", AcceptHint: "PNG", AcceptAttr: "image/png", MaxBytes: 1024}
	html := renderStr(t, webtempl.MediaLibrary(v))
	mustContain(
		t, html,
		`role="button"`,
		`tabindex="0"`,
		`aria-label="Upload files: choose files or drag and drop"`,
	)
}

func TestMediaLibrary_EmptyState(t *testing.T) {
	v := webtempl.MediaListView{UploadURL: "/admin/media", AcceptHint: "PNG", AcceptAttr: "image/png", MaxBytes: 1024}
	html := renderStr(t, webtempl.MediaLibrary(v))
	mustContain(t, html, `data-testid="media-empty"`, "No media yet")
}

func TestMediaDetailPanel_MetadataForm(t *testing.T) {
	v := webtempl.MediaDetailView{
		Card:      sampleImageCard("m1", "Sunset"),
		UpdateURL: "/admin/media/m1",
		Saved:     true,
	}
	html := renderStr(t, webtempl.MediaDetailPanel(v))
	mustContain(
		t, html,
		`data-testid="media-detail"`,
		`data-testid="media-field-alt"`,
		`data-testid="media-field-title"`,
		`data-testid="media-field-caption"`,
		`data-testid="media-save"`,
		`data-testid="media-saved"`,
		// Image preview + dimensions/size metadata.
		"800 × 600",
		"2.0 KB",
	)
}

func TestMediaPickerGrid_OffersImagesWithSrcAlt(t *testing.T) {
	v := webtempl.MediaPickerView{
		Items: []webtempl.MediaPickerItem{
			{ID: "m1", Src: "/uploads/media/m1.png", ThumbURL: "/uploads/media/thumb-m1.png", Alt: "A sunset", Title: "Sunset"},
		},
		Page: 1, Pages: 2, NextURL: "/admin/media/picker?page=2",
	}
	html := renderStr(t, webtempl.MediaPickerGrid(v))
	mustContain(
		t, html,
		`data-testid="media-picker-grid"`,
		`data-testid="media-pick-m1"`,
		// The pick button carries the src+alt the editor inserts as <img>.
		`data-src="/uploads/media/m1.png"`,
		`data-alt="A sunset"`,
		// Pagination inside the modal.
		`data-testid="media-picker-pager"`,
		`data-testid="media-picker-next"`,
	)
}

func TestMediaPickerGrid_EmptyState(t *testing.T) {
	html := renderStr(t, webtempl.MediaPickerGrid(webtempl.MediaPickerView{Page: 1, Pages: 1}))
	mustContain(t, html, `data-testid="media-picker-empty"`)
}
