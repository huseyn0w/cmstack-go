package templ

import "fmt"

// MediaCard is one asset rendered in the library grid. ThumbURL is the small
// preview for raster images; for documents (PDF) it is empty and a file-type
// icon is shown instead. IsImage drives the icon-vs-thumbnail branch.
type MediaCard struct {
	ID         string
	Title      string // display title (title|filename|id)
	RawTitle   string // the stored title field (may be empty), for the edit form
	Alt        string
	Caption    string
	ThumbURL   string // grid thumbnail URL ("" for non-images)
	FullURL    string // original object URL
	IsImage    bool
	MIMELabel  string // short type label, e.g. "PNG", "PDF"
	Dimensions string // "800 × 600" or "" for documents
	SizeLabel  string // human size, e.g. "1.2 MB"
	EditURL    string // metadata detail/edit panel URL (htmx)
	DeleteURL  string // POST delete target
	Uploaded   string // formatted upload date
}

// MediaListView is the media library page view-model.
type MediaListView struct {
	Shell       AdminShell
	Cards       []MediaCard
	Pager       Pagination
	UploadURL   string // POST multipart upload target
	BulkURL     string // POST bulk-delete target
	CSRFToken   string
	MaxBytes    int64  // size cap for the dropzone hint
	AcceptHint  string // accepted-types hint, e.g. "JPG, PNG, GIF, WebP, PDF"
	AcceptAttr  string // <input accept> value
	Summary     BulkSummary
	UploadError string // surfaced when a synchronous upload failed
}

// MaxBytesLabel renders the size cap as a human string for the hint.
func (v MediaListView) MaxBytesLabel() string { return HumanBytes(v.MaxBytes) }

// MediaPickerView is the editor's media-picker grid fragment: a paginated set of
// selectable image assets the rich-text editor inserts as <img src alt>. Only
// raster images are offered (an <img> of a PDF is meaningless). PrevURL/NextURL
// drive the htmx-loaded pager inside the modal.
type MediaPickerView struct {
	Items   []MediaPickerItem
	PrevURL string
	NextURL string
	Page    int
	Pages   int
}

// MediaPickerItem is one selectable image in the picker grid. Src is the URL
// inserted into the editor; Alt seeds the inserted img's alt text. Width/Height
// are the intrinsic pixel dimensions stamped onto the inserted <img> so content
// images reserve layout space (avoiding CLS); 0 when unknown.
type MediaPickerItem struct {
	ID       string
	Src      string // original/full URL used for the inserted <img src>
	ThumbURL string // grid preview
	Alt      string
	Title    string
	Width    int
	Height   int
}

// MediaDetailView is the per-asset metadata edit panel (modal/detail), loaded
// into the library when an asset is selected.
type MediaDetailView struct {
	Card        MediaCard
	UpdateURL   string
	CSRFToken   string
	FieldErrors map[string]string
	Saved       bool
}

// mediaCardIDs returns the selectable ids for the grid's bulk select-all set.
func mediaCardIDs(cards []MediaCard) []string {
	ids := make([]string, 0, len(cards))
	for _, c := range cards {
		ids = append(ids, c.ID)
	}
	return ids
}

// mediaBulkActions is the action set on the media library: delete-only (assets
// have no draft/publish lifecycle). It is destructive, so it opens the confirm
// modal before submitting.
func mediaBulkActions() []BulkActionSpec {
	return []BulkActionSpec{
		{Action: "delete", Label: "Delete", Destructive: true, Consequence: "The selected files will be permanently deleted, including their thumbnails. This cannot be undone."},
	}
}

// HumanBytes renders a byte count as a compact human string (e.g. "1.2 MB").
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
