package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/media"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	"github.com/huseyn0w/cmstack-go/internal/platform/storage"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// mediaPageSize is the media library grid page size.
const mediaPageSize = 24

// maxFilesPerUpload bounds how many files a single multipart upload may carry,
// so one request can't fan out into an unbounded number of validate+store passes.
const maxFilesPerUpload = 8

// MediaAdminService is the subset of *media.Service the admin handler calls.
type MediaAdminService interface {
	List(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]media.Media, int, error)
	Get(ctx context.Context, actorID, id uuid.UUID) (media.Media, error)
	Upload(ctx context.Context, actorID uuid.UUID, in media.UploadInput) (media.Media, error)
	UpdateMetadata(ctx context.Context, actorID, id uuid.UUID, alt, title, caption string) (media.Media, error)
	Delete(ctx context.Context, actorID, id uuid.UUID) error
	URL(key string) string
	MaxUploadBytes() int64
}

// MediaAdminHandler is the thin HTTP boundary for the admin media library. It
// decodes, calls the service, and renders/redirects — no business logic.
type MediaAdminHandler struct {
	svc   MediaAdminService
	shell adminShellDeps
	csrf  func(*http.Request) string
}

// NewMediaAdminHandler constructs the media admin handler.
func NewMediaAdminHandler(svc MediaAdminService, shell adminShellDeps, csrf func(*http.Request) string) *MediaAdminHandler {
	return &MediaAdminHandler{svc: svc, shell: shell, csrf: csrf}
}

// List renders the media library grid with pagination + dropzone.
func (h *MediaAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	u, _ := UserFromContext(r.Context())
	items, total, err := h.svc.List(r.Context(), u.ID, mediaPageSize, (page-1)*mediaPageSize)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	view := h.listView(r, items, total, page)
	if q := r.URL.Query().Get("error"); q != "" {
		view.UploadError = q
	}
	h.render(w, r, webtempl.MediaLibrary(view))
}

// Upload handles a multipart upload. It supports BOTH the JS-enhanced XHR path
// (one file per request, used by the dropzone) and a plain non-JS form POST
// (one or more files). On the XHR path it returns a tiny 201; on the native path
// it redirects back to the library. Validation failures map to a friendly
// message (422 for XHR, redirect-with-error for native).
func (h *MediaAdminHandler) Upload(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())

	maxBytes := h.svc.MaxUploadBytes()
	formCap := maxBytes + (1 << 16) // slack for multipart headers/boundaries
	r.Body = http.MaxBytesReader(w, r.Body, formCap*8)
	if err := r.ParseMultipartForm(formCap); err != nil {
		h.uploadFailure(w, r, "Upload was too large or malformed.", http.StatusRequestEntityTooLarge)
		return
	}
	if r.MultipartForm == nil || len(r.MultipartForm.File["file"]) == 0 {
		h.uploadFailure(w, r, "Choose a file to upload.", http.StatusUnprocessableEntity)
		return
	}
	// Bound files-per-request so one multipart body can't fan out into an
	// unbounded number of validate+store passes (each file is still capped at
	// maxBytes). Matches the ~8x body budget above.
	if len(r.MultipartForm.File["file"]) > maxFilesPerUpload {
		h.uploadFailure(w, r, "Too many files in one upload.", http.StatusRequestEntityTooLarge)
		return
	}

	var lastErr error
	uploaded := 0
	for _, fh := range r.MultipartForm.File["file"] {
		file, err := fh.Open()
		if err != nil {
			lastErr = err
			continue
		}
		_, err = h.svc.Upload(r.Context(), u.ID, media.UploadInput{Reader: file, Filename: fh.Filename})
		_ = file.Close()
		if err != nil {
			lastErr = err
			continue
		}
		uploaded++
	}

	if uploaded == 0 && lastErr != nil {
		if errors.Is(lastErr, media.ErrForbidden) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		h.uploadFailure(w, r, mediaErrorMessage(lastErr), http.StatusUnprocessableEntity)
		return
	}

	if isXHR(r) {
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, "%d uploaded", uploaded)
		return
	}
	http.Redirect(w, r, "/admin/media", http.StatusSeeOther)
}

// Detail renders the per-asset metadata panel (htmx-loaded into the modal).
func (h *MediaAdminHandler) Detail(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	m, err := h.svc.Get(r.Context(), u.ID, id)
	if errors.Is(err, media.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, media.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, webtempl.MediaDetailPanel(h.detailView(r, m, false)))
}

// UpdateMetadata saves the alt/title/caption form then re-renders the panel
// (when XHR) or redirects back to the library.
func (h *MediaAdminHandler) UpdateMetadata(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	m, err := h.svc.UpdateMetadata(r.Context(), u.ID, id,
		r.PostFormValue("alt"), r.PostFormValue("title"), r.PostFormValue("caption"))
	if errors.Is(err, media.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if errors.Is(err, media.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	if isXHR(r) {
		h.render(w, r, webtempl.MediaDetailPanel(h.detailView(r, m, true)))
		return
	}
	http.Redirect(w, r, "/admin/media", http.StatusSeeOther)
}

// Delete removes an asset then redirects to the library.
func (h *MediaAdminHandler) Delete(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = h.svc.Delete(r.Context(), u.ID, id)
	if errors.Is(err, media.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if errors.Is(err, media.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/media", http.StatusSeeOther)
}

// Bulk handles the bulk-delete POST: it deletes each selected id (the service
// re-checks delete:media), counts applied/missing, then redirects with a
// summary query the list surfaces via aria-live.
func (h *MediaAdminHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if r.PostFormValue("action") != "delete" {
		http.Error(w, "unknown bulk action", http.StatusBadRequest)
		return
	}
	ids := parseBulkIDs(r.PostForm["ids"])
	if len(ids) == 0 {
		http.Redirect(w, r, "/admin/media", http.StatusSeeOther)
		return
	}

	applied, missing, skipped := 0, 0, 0
	for _, id := range ids {
		err := h.svc.Delete(r.Context(), u.ID, id)
		switch {
		case err == nil:
			applied++
		case errors.Is(err, media.ErrNotFound):
			missing++
		case errors.Is(err, media.ErrForbidden):
			skipped++
		default:
			http.Error(w, "error", http.StatusInternalServerError)
			return
		}
	}
	q := mediaBulkSummaryQuery(applied, skipped, missing)
	http.Redirect(w, r, "/admin/media?"+q, http.StatusSeeOther)
}

// Picker renders the editor's media-picker grid fragment: a paginated set of
// selectable IMAGE assets (documents are excluded — an <img> of a PDF is
// meaningless). It is htmx-loaded into the rich-text editor's picker modal.
// Gated by read:media like the rest of the read routes.
func (h *MediaAdminHandler) Picker(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	u, _ := UserFromContext(r.Context())
	items, total, err := h.svc.List(r.Context(), u.ID, mediaPageSize, (page-1)*mediaPageSize)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	picks := make([]webtempl.MediaPickerItem, 0, len(items))
	for _, m := range items {
		if !m.IsImage() {
			continue // only raster images are insertable as <img>
		}
		card := h.card(m)
		pick := webtempl.MediaPickerItem{
			ID:       card.ID,
			Src:      card.FullURL,
			ThumbURL: card.ThumbURL,
			Alt:      card.Alt,
			Title:    card.Title,
		}
		// IsImage() guarantees non-nil dimensions; carry them so the editor can
		// stamp width/height onto the inserted <img> (CLS avoidance).
		if m.Width != nil && m.Height != nil {
			pick.Width = *m.Width
			pick.Height = *m.Height
		}
		picks = append(picks, pick)
	}
	pages := (total + mediaPageSize - 1) / mediaPageSize
	if pages < 1 {
		pages = 1
	}
	view := webtempl.MediaPickerView{Items: picks, Page: page, Pages: pages}
	if page > 1 {
		view.PrevURL = fmt.Sprintf("/admin/media/picker?page=%d", page-1)
	}
	if page < pages {
		view.NextURL = fmt.Sprintf("/admin/media/picker?page=%d", page+1)
	}
	h.render(w, r, webtempl.MediaPickerGrid(view))
}

// --- helpers -----------------------------------------------------------------

func (h *MediaAdminHandler) listView(r *http.Request, items []media.Media, total, page int) webtempl.MediaListView {
	cards := make([]webtempl.MediaCard, 0, len(items))
	for _, m := range items {
		cards = append(cards, h.card(m))
	}
	return webtempl.MediaListView{
		Shell:      h.shell.buildShell(r, "Media"),
		Cards:      cards,
		Pager:      pager(page, mediaPageSize, total, "/admin/media", ""),
		UploadURL:  "/admin/media",
		BulkURL:    "/admin/media/bulk",
		CSRFToken:  h.csrf(r),
		MaxBytes:   h.svc.MaxUploadBytes(),
		AcceptHint: "JPG, PNG, GIF, WebP, PDF",
		AcceptAttr: "image/jpeg,image/png,image/gif,image/webp,application/pdf",
		Summary:    bulkSummaryFromQuery(r),
	}
}

func (h *MediaAdminHandler) detailView(r *http.Request, m media.Media, saved bool) webtempl.MediaDetailView {
	return webtempl.MediaDetailView{
		Card:        h.card(m),
		UpdateURL:   "/admin/media/" + m.ID.String(),
		CSRFToken:   h.csrf(r),
		FieldErrors: map[string]string{},
		Saved:       saved,
	}
}

// card maps a domain Media to its grid/detail view-model, resolving the
// thumbnail URL (prefer the "thumb" variant; fall back to the original for
// raster assets) and the original URL via the storage backend.
func (h *MediaAdminHandler) card(m media.Media) webtempl.MediaCard {
	fullURL := h.svc.URL(m.StorageKey)
	thumbURL := ""
	if m.IsImage() {
		if key := m.ThumbnailKey("thumb"); key != "" {
			thumbURL = h.svc.URL(key)
		} else {
			thumbURL = fullURL
		}
	}
	return webtempl.MediaCard{
		ID:         m.ID.String(),
		Title:      m.DisplayTitle(),
		RawTitle:   m.Title,
		Alt:        m.Alt,
		Caption:    m.Caption,
		ThumbURL:   thumbURL,
		FullURL:    fullURL,
		IsImage:    m.IsImage(),
		MIMELabel:  mimeLabel(m.MIME),
		Dimensions: webtempl.DimensionsLabel(m.Width, m.Height),
		SizeLabel:  webtempl.HumanBytes(m.SizeBytes),
		EditURL:    "/admin/media/" + m.ID.String() + "/detail",
		DeleteURL:  "/admin/media/" + m.ID.String() + "/delete",
		Uploaded:   m.CreatedAt.Format("Jan 2, 2006 15:04"),
	}
}

func (h *MediaAdminHandler) uploadFailure(w http.ResponseWriter, r *http.Request, msg string, status int) {
	if isXHR(r) {
		http.Error(w, msg, status)
		return
	}
	http.Redirect(w, r, "/admin/media?error="+strings.ReplaceAll(msg, " ", "+"), http.StatusSeeOther)
}

func (h *MediaAdminHandler) render(w http.ResponseWriter, r *http.Request, c webtempl.Component) {
	if err := render.Component(r.Context(), w, http.StatusOK, c); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// isXHR reports whether the request came from the JS upload/detail path (the
// island sets X-Requested-With). Used to pick a fragment/JSON-ish response over
// a full redirect.
func isXHR(r *http.Request) bool {
	return r.Header.Get("X-Requested-With") == "XMLHttpRequest"
}

// mediaErrorMessage maps a media/storage validation error to a user message.
func mediaErrorMessage(err error) string {
	switch {
	case errors.Is(err, storage.ErrMediaTooLarge):
		return "File is too large."
	case errors.Is(err, storage.ErrMediaType):
		return "That file type is not allowed. Use JPG, PNG, GIF, WebP or PDF."
	case errors.Is(err, storage.ErrMediaDimensions):
		return "Image dimensions are too large."
	case errors.Is(err, storage.ErrMediaEmpty):
		return "The uploaded file was empty."
	default:
		return "That file could not be processed."
	}
}

// mimeLabel returns a short uppercase label for a MIME (e.g. "image/png" ->
// "PNG", "application/pdf" -> "PDF").
func mimeLabel(mime string) string {
	if i := strings.LastIndex(mime, "/"); i >= 0 {
		sub := mime[i+1:]
		if sub == "jpeg" {
			sub = "jpg"
		}
		return strings.ToUpper(sub)
	}
	return strings.ToUpper(mime)
}

// mediaBulkSummaryQuery encodes a bulk-delete outcome for the redirect.
func mediaBulkSummaryQuery(applied, skipped, missing int) string {
	q := "bulk=delete&applied=" + itoa(applied)
	if skipped > 0 {
		q += "&skipped=" + itoa(skipped)
	}
	if missing > 0 {
		q += "&missing=" + itoa(missing)
	}
	return q
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
