package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/categories"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/content/tags"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// CategoryReader supplies the post editor's category tree + a post's current
// category associations (M3). *categories.Service satisfies it. Optional: when
// nil the editor shows no category selector and the list no category pills.
type CategoryReader interface {
	Tree(ctx context.Context) ([]categories.TreeNode, error)
	IDsForPost(ctx context.Context, postID uuid.UUID) ([]uuid.UUID, error)
	CategoriesForPost(ctx context.Context, postID uuid.UUID) ([]categories.Category, error)
}

// TagReader supplies the post editor's tag set + a post's current tag
// associations (M3). *tags.Service satisfies it. Optional like CategoryReader.
type TagReader interface {
	AllFlat(ctx context.Context) ([]tags.Tag, error)
	IDsForPost(ctx context.Context, postID uuid.UUID) ([]uuid.UUID, error)
	TagsForPost(ctx context.Context, postID uuid.UUID) ([]tags.Tag, error)
}

// adminPageSize is the admin list page size.
const adminPageSize = 20

// PostAdminService is the subset of *posts.Service the admin handler calls.
type PostAdminService interface {
	AdminList(ctx context.Context, f posts.ListFilter) ([]posts.Post, int, error)
	AdminTrashed(ctx context.Context, limit, offset int) ([]posts.Post, int, error)
	Get(ctx context.Context, actorID, id uuid.UUID) (posts.Post, error)
	Create(ctx context.Context, authorID uuid.UUID, in posts.CreateInput) (posts.Post, error)
	Update(ctx context.Context, actorID, id uuid.UUID, in posts.UpdateInput) (posts.Post, error)
	Trash(ctx context.Context, actorID, id uuid.UUID) error
	Restore(ctx context.Context, actorID, id uuid.UUID) error
	PermanentDelete(ctx context.Context, actorID, id uuid.UUID) error
	Revisions(ctx context.Context, actorID, id uuid.UUID) ([]kernel.Revision, error)
	RestoreRevision(ctx context.Context, actorID, id, revisionID uuid.UUID) (posts.Post, error)

	// Per-locale content overlay (M7b-1). GetInLocale loads the editor's content
	// overlaid by the active tab's locale (base fallback); TranslatedLocales marks
	// which tabs already have a translation; SaveTranslation upserts a de/ru tab's
	// content. The default locale (en) still edits the base row via Update.
	GetInLocale(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale) (posts.Post, error)
	TranslatedLocales(ctx context.Context, actorID, id uuid.UUID) ([]i18n.Locale, error)
	SaveTranslation(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale, in posts.TranslationInput) error

	// Bulk list actions (M2c). Each reuses the matching single-item op per id, so
	// per-post ownership/permission/events stay correct; the concrete *posts.Service
	// satisfies these directly. The set also makes the service a bulkActor.
	BulkTrash(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkRestore(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkPermanentDelete(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkPublish(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkUnpublish(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
}

// AuthorNamer resolves an author's display name for the admin list (best effort).
type AuthorNamer interface {
	GetByID(ctx context.Context, id uuid.UUID) (accounts.User, error)
}

// PostAdminHandler is the thin HTTP boundary for the admin posts area. It
// decodes, validates, calls the service, and renders/redirects — ZERO logic.
type PostAdminHandler struct {
	svc        PostAdminService
	shell      adminShellDeps
	authors    AuthorNamer
	categories CategoryReader // optional (M3 taxonomy)
	tags       TagReader      // optional (M3 taxonomy)
	csrf       func(*http.Request) string
}

// NewPostAdminHandler constructs the admin posts handler.
func NewPostAdminHandler(svc PostAdminService, shell adminShellDeps, authors AuthorNamer, csrf func(*http.Request) string) *PostAdminHandler {
	return &PostAdminHandler{svc: svc, shell: shell, authors: authors, csrf: csrf}
}

// WithTaxonomy attaches the optional category + tag readers so the editor offers
// taxonomy selectors and the list shows term pills (M3). Returns the receiver
// for chaining at wire time.
func (h *PostAdminHandler) WithTaxonomy(cats CategoryReader, tagSvc TagReader) *PostAdminHandler {
	h.categories = cats
	h.tags = tagSvc
	return h
}

// List renders the admin posts table with status-filter tabs and pagination.
func (h *PostAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	statusParam := r.URL.Query().Get("status")

	f := posts.ListFilter{Limit: adminPageSize, Offset: (page - 1) * adminPageSize}
	if statusParam == "DRAFT" || statusParam == "PUBLISHED" {
		s := kernel.Status(statusParam)
		f.Status = &s
	}

	items, total, err := h.svc.AdminList(r.Context(), f)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	view := webtempl.PostListView{
		Shell:     h.shell.buildShell(r, "Posts"),
		Rows:      h.rows(r.Context(), items),
		Tabs:      h.statusTabs(statusParam),
		Pager:     pager(page, adminPageSize, total, "/admin/posts", statusQuery(statusParam)),
		NewURL:    "/admin/posts/new",
		BulkURL:   "/admin/posts/bulk",
		Summary:   bulkSummaryFromQuery(r),
		CSRFToken: h.csrf(r),
	}
	h.render(w, r, webtempl.PostList(view))
}

// Bulk dispatches an allow-listed bulk action over the submitted post ids,
// reusing the shared handleBulk driver. Per-post ownership is enforced inside
// the service (an Author's bulk only touches their own posts); the route gate
// already required the coarse grant.
func (h *PostAdminHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	handleBulk(w, r, h.svc, "/admin/posts")
}

// New renders the empty editor.
func (h *PostAdminHandler) New(w http.ResponseWriter, r *http.Request) {
	cats, tagChoices := h.taxonomyChoices(r.Context(), uuid.Nil)
	view := webtempl.PostFormView{
		Shell:           h.shell.buildShell(r, "New post"),
		IsNew:           true,
		Status:          webtempl.PostStatusDraft,
		ActionURL:       "/admin/posts",
		CSRFToken:       h.csrf(r),
		FieldErrors:     map[string]string{},
		BackURL:         "/admin/posts",
		CategoryChoices: cats,
		TagChoices:      tagChoices,
	}
	h.render(w, r, webtempl.PostEditor(view))
}

// Create handles the new-post POST.
func (h *PostAdminHandler) Create(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	in, status := h.decodeForm(r)

	post, err := h.svc.Create(r.Context(), u.ID, posts.CreateInput{
		Title:       in.title,
		Slug:        in.slug,
		Excerpt:     in.excerpt,
		Body:        in.body,
		Status:      status,
		ScheduledAt: in.scheduledAt,
		CategoryIDs: in.categoryIDs,
		TagIDs:      in.tagIDs,
	})
	if err != nil {
		h.renderCreateError(w, r, in, status, err)
		return
	}
	http.Redirect(w, r, "/admin/posts/"+post.ID.String()+"/edit", http.StatusSeeOther)
}

// Edit renders the editor for an existing post.
func (h *PostAdminHandler) Edit(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// The active editor tab is chosen by ?language=xx (django-parler parity); it
	// defaults to en (the base row). Content is loaded overlaid by that locale so
	// the tab shows the translation (with base fallback) it will edit.
	locale := editorLocale(r)
	post, err := h.svc.GetInLocale(r.Context(), u.ID, id, locale)
	if errors.Is(err, posts.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, posts.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, webtempl.PostEditor(h.formView(r, post, locale)))
}

// editorLocale resolves the active editor tab locale from ?language=xx, falling
// back to the default locale (en) for an empty/unknown value.
func editorLocale(r *http.Request) i18n.Locale {
	if loc, ok := i18n.Parse(r.URL.Query().Get("language")); ok {
		return loc
	}
	return i18n.Default()
}

// Update handles the edit POST (save/publish/schedule via the action button).
func (h *PostAdminHandler) Update(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	in, status := h.decodeForm(r)

	// Per-locale save (M7b-1): a non-default `locale` field means the editor is on
	// a de/ru translation tab, so the translatable content is upserted to the
	// overlay rather than the base row. en (default/empty) takes the unchanged base
	// Update path below.
	if loc, ok := i18n.Parse(r.PostFormValue("locale")); ok && !loc.IsDefault() {
		err = h.svc.SaveTranslation(r.Context(), u.ID, id, loc, posts.TranslationInput{
			Title:   in.title,
			Excerpt: in.excerpt,
			Body:    in.body,
		})
		if errors.Is(err, posts.ErrForbidden) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		if errors.Is(err, posts.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			post, _ := h.svc.GetInLocale(r.Context(), u.ID, id, loc)
			view := h.formView(r, post, loc)
			view.Error = humanError(err)
			h.render(w, r, webtempl.PostEditor(view))
			return
		}
		http.Redirect(w, r, "/admin/posts/"+id.String()+"/edit?language="+loc.String(), http.StatusSeeOther)
		return
	}

	upd := posts.UpdateInput{
		Title:       &in.title,
		Slug:        &in.slug,
		Excerpt:     &in.excerpt,
		Body:        &in.body,
		Status:      &status,
		SetTaxonomy: true,
		CategoryIDs: in.categoryIDs,
		TagIDs:      in.tagIDs,
	}
	// The clicked action button refines intent.
	switch r.PostFormValue("action") {
	case "publish":
		published := kernel.StatusPublished
		upd.Status = &published
	case "schedule":
		draft := kernel.StatusDraft
		upd.Status = &draft
		upd.ScheduledAt = in.scheduledAt
	default:
		if status == kernel.StatusDraft && in.scheduledAt != nil {
			upd.ScheduledAt = in.scheduledAt
		}
	}

	_, err = h.svc.Update(r.Context(), u.ID, id, upd)
	if errors.Is(err, posts.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if errors.Is(err, posts.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		// Re-render the editor with the error (base/en tab).
		post, _ := h.svc.Get(r.Context(), u.ID, id)
		view := h.formView(r, post, i18n.Default())
		view.Error = humanError(err)
		h.render(w, r, webtempl.PostEditor(view))
		return
	}
	http.Redirect(w, r, "/admin/posts/"+id.String()+"/edit", http.StatusSeeOther)
}

// Trash soft-deletes a post then redirects to the list.
func (h *PostAdminHandler) Trash(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(actor, id uuid.UUID) error { return h.svc.Trash(r.Context(), actor, id) }, "/admin/posts")
}

// RestoreTrashed restores a trashed post then redirects to trash.
func (h *PostAdminHandler) RestoreTrashed(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(actor, id uuid.UUID) error { return h.svc.Restore(r.Context(), actor, id) }, "/admin/posts/trash")
}

// PermanentDelete hard-deletes a trashed post then redirects to trash.
func (h *PostAdminHandler) PermanentDelete(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(actor, id uuid.UUID) error { return h.svc.PermanentDelete(r.Context(), actor, id) }, "/admin/posts/trash")
}

// Trashed renders the trash list.
func (h *PostAdminHandler) Trashed(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	items, total, err := h.svc.AdminTrashed(r.Context(), adminPageSize, (page-1)*adminPageSize)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	rows := make([]webtempl.TrashRow, 0, len(items))
	for _, p := range items {
		rows = append(rows, webtempl.TrashRow{
			ID:         p.ID.String(),
			Title:      p.Title,
			DeletedAt:  formatTime(p.DeletedAt),
			RestoreURL: "/admin/posts/trash/" + p.ID.String() + "/restore",
			DeleteURL:  "/admin/posts/trash/" + p.ID.String() + "/delete",
		})
	}
	view := webtempl.TrashView{
		Shell:     h.shell.buildShell(r, "Trash"),
		Rows:      rows,
		Pager:     pager(page, adminPageSize, total, "/admin/posts/trash", ""),
		CSRFToken: h.csrf(r),
	}
	h.render(w, r, webtempl.PostTrash(view))
}

// Revisions renders the revision history for a post.
func (h *PostAdminHandler) Revisions(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	post, err := h.svc.Get(r.Context(), u.ID, id)
	if errors.Is(err, posts.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, posts.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	revs, err := h.svc.Revisions(r.Context(), u.ID, id)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	view := webtempl.RevisionsView{
		Shell:     h.shell.buildShell(r, "Revisions"),
		PostTitle: post.Title,
		PostID:    post.ID.String(),
		Current:   webtempl.RevisionRow{Title: post.Title, Body: post.Body},
		Rows:      h.revisionRows(r.Context(), id, revs),
		BackURL:   "/admin/posts/" + id.String() + "/edit",
		CSRFToken: h.csrf(r),
	}
	h.render(w, r, webtempl.PostRevisions(view))
}

// RestoreRevision applies a snapshot then redirects to the editor.
func (h *PostAdminHandler) RestoreRevision(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	revID, err := uuid.Parse(chi.URLParam(r, "rev"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, err = h.svc.RestoreRevision(r.Context(), u.ID, id, revID)
	if errors.Is(err, posts.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/posts/"+id.String()+"/edit", http.StatusSeeOther)
}

// --- helpers -----------------------------------------------------------------

type postForm struct {
	title       string
	slug        string
	excerpt     string
	body        string
	scheduledAt *time.Time
	categoryIDs []uuid.UUID
	tagIDs      []uuid.UUID
}

func (h *PostAdminHandler) decodeForm(r *http.Request) (postForm, kernel.Status) {
	_ = r.ParseForm()
	f := postForm{
		title:       r.PostFormValue("title"),
		slug:        r.PostFormValue("slug"),
		excerpt:     r.PostFormValue("excerpt"),
		body:        r.PostFormValue("body"),
		categoryIDs: parseBulkIDs(r.PostForm["category_ids"]),
		tagIDs:      parseBulkIDs(r.PostForm["tag_ids"]),
	}
	status := kernel.ParseStatus(r.PostFormValue("status"))
	if raw := r.PostFormValue("scheduled_at"); raw != "" {
		// datetime-local has no zone; interpret as UTC.
		if t, err := time.Parse("2006-01-02T15:04", raw); err == nil {
			f.scheduledAt = &t
		} else if t, err := time.Parse(time.RFC3339, raw); err == nil {
			f.scheduledAt = &t
		}
	}
	return f, status
}

func (h *PostAdminHandler) renderCreateError(w http.ResponseWriter, r *http.Request, in postForm, status kernel.Status, err error) {
	if errors.Is(err, posts.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	view := webtempl.PostFormView{
		Shell:       h.shell.buildShell(r, "New post"),
		IsNew:       true,
		Title:       in.title,
		Slug:        in.slug,
		Excerpt:     in.excerpt,
		Body:        in.body,
		Status:      statusView(status),
		ActionURL:   "/admin/posts",
		CSRFToken:   h.csrf(r),
		FieldErrors: map[string]string{},
		Error:       humanError(err),
		BackURL:     "/admin/posts",
	}
	if errors.Is(err, posts.ErrTitleRequired) {
		view.FieldErrors["title"] = "Title is required."
	}
	h.render(w, r, webtempl.PostEditor(view))
}

func (h *PostAdminHandler) mutate(w http.ResponseWriter, r *http.Request, fn func(actor, id uuid.UUID) error, redirect string) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = fn(u.ID, id)
	if errors.Is(err, posts.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if errors.Is(err, posts.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (h *PostAdminHandler) formView(r *http.Request, p posts.Post, locale i18n.Locale) webtempl.PostFormView {
	cats, tagChoices := h.taxonomyChoices(r.Context(), p.ID)
	return webtempl.PostFormView{
		Shell:           h.shell.buildShell(r, "Edit post"),
		IsNew:           false,
		ID:              p.ID.String(),
		Title:           p.Title,
		Slug:            p.Slug,
		Excerpt:         p.Excerpt,
		Body:            p.Body,
		Status:          statusView(p.Status),
		ScheduledAt:     scheduleValue(p.ScheduledAt),
		ActionURL:       "/admin/posts/" + p.ID.String(),
		CSRFToken:       h.csrf(r),
		FieldErrors:     map[string]string{},
		RevisionsURL:    "/admin/posts/" + p.ID.String() + "/revisions",
		BackURL:         "/admin/posts",
		CategoryChoices: cats,
		TagChoices:      tagChoices,
		LocaleTabs:      h.localeTabs(r.Context(), r, p.ID, locale),
		ActiveLocale:    locale.String(),
		IsDefaultLocale: locale.IsDefault(),
	}
}

// localeDisplayNames maps a locale to its editor-tab display label. The admin
// area is en, so English endonyms keep the strip legible for admins.
var localeDisplayNames = map[i18n.Locale]string{
	i18n.LocaleEN: "English",
	i18n.LocaleDE: "Deutsch",
	i18n.LocaleRU: "Русский",
}

// localeTabs builds the editor's per-locale tab strip: one tab per supported
// locale, the active one selected, de/ru tabs marked when a translation row
// already exists. Best-effort: a translated-locales read error yields no dots
// (the strip still renders). Each tab links to the editor with ?language=xx.
func (h *PostAdminHandler) localeTabs(ctx context.Context, r *http.Request, postID uuid.UUID, active i18n.Locale) []webtempl.LocaleTab {
	u, _ := UserFromContext(ctx)
	has := map[i18n.Locale]bool{}
	if locs, err := h.svc.TranslatedLocales(r.Context(), u.ID, postID); err == nil {
		for _, l := range locs {
			has[l] = true
		}
	}
	base := "/admin/posts/" + postID.String() + "/edit"
	tabs := make([]webtempl.LocaleTab, 0, len(i18n.All()))
	for _, loc := range i18n.All() {
		href := base
		if !loc.IsDefault() {
			href += "?language=" + loc.String()
		}
		label := localeDisplayNames[loc]
		if label == "" {
			label = loc.String()
		}
		tabs = append(tabs, webtempl.LocaleTab{
			Label:          label,
			Code:           loc.String(),
			Href:           href,
			Active:         loc == active,
			HasTranslation: has[loc],
		})
	}
	return tabs
}

// taxonomyChoices builds the editor's category (tree, pre-selected) + tag (flat,
// pre-selected) choice lists for a post. postID is uuid.Nil on the new-post path
// (nothing pre-selected). Returns empty slices when taxonomy is not wired.
func (h *PostAdminHandler) taxonomyChoices(ctx context.Context, postID uuid.UUID) (cats, tagChoices []webtempl.TaxonomyChoice) {
	if h.categories != nil {
		selected := map[uuid.UUID]bool{}
		if postID != uuid.Nil {
			if ids, err := h.categories.IDsForPost(ctx, postID); err == nil {
				for _, id := range ids {
					selected[id] = true
				}
			}
		}
		if nodes, err := h.categories.Tree(ctx); err == nil {
			for _, n := range nodes {
				cats = append(cats, webtempl.TaxonomyChoice{
					ID:       n.Category.ID.String(),
					Label:    n.Category.Name,
					Depth:    n.Depth,
					Selected: selected[n.Category.ID],
				})
			}
		}
	}
	if h.tags != nil {
		selected := map[uuid.UUID]bool{}
		if postID != uuid.Nil {
			if ids, err := h.tags.IDsForPost(ctx, postID); err == nil {
				for _, id := range ids {
					selected[id] = true
				}
			}
		}
		if all, err := h.tags.AllFlat(ctx); err == nil {
			for _, t := range all {
				tagChoices = append(tagChoices, webtempl.TaxonomyChoice{
					ID:       t.ID.String(),
					Label:    t.Name,
					Selected: selected[t.ID],
				})
			}
		}
	}
	return cats, tagChoices
}

func (h *PostAdminHandler) rows(ctx context.Context, items []posts.Post) []webtempl.PostRow {
	rows := make([]webtempl.PostRow, 0, len(items))
	for _, p := range items {
		rows = append(rows, webtempl.PostRow{
			ID:         p.ID.String(),
			Title:      p.Title,
			Slug:       p.Slug,
			AuthorName: h.authorName(ctx, p.AuthorID),
			Status:     statusView(p.Status),
			Scheduled:  p.Scheduled(),
			Date:       postDisplayDate(p),
			EditURL:    "/admin/posts/" + p.ID.String() + "/edit",
			Taxonomy:   h.rowTaxonomy(ctx, p.ID),
		})
	}
	return rows
}

// rowTaxonomy returns a short category+tag label list for a post's admin row.
// Best-effort: any read error yields no labels rather than failing the list.
func (h *PostAdminHandler) rowTaxonomy(ctx context.Context, postID uuid.UUID) []string {
	var out []string
	if h.categories != nil {
		if cats, err := h.categories.CategoriesForPost(ctx, postID); err == nil {
			for _, c := range cats {
				out = append(out, c.Name)
			}
		}
	}
	if h.tags != nil {
		if ts, err := h.tags.TagsForPost(ctx, postID); err == nil {
			for _, t := range ts {
				out = append(out, "#"+t.Name)
			}
		}
	}
	return out
}

func (h *PostAdminHandler) revisionRows(ctx context.Context, _ uuid.UUID, revs []kernel.Revision) []webtempl.RevisionRow {
	rows := make([]webtempl.RevisionRow, 0, len(revs))
	for _, rev := range revs {
		snap := decodeRevisionSnapshot(rev.Snapshot)
		author := "System"
		if rev.AuthorID != nil {
			author = h.authorName(ctx, *rev.AuthorID)
		}
		rows = append(rows, webtempl.RevisionRow{
			ID:         rev.ID.String(),
			AuthorName: author,
			CreatedAt:  rev.CreatedAt.Format("Jan 2, 2006 15:04"),
			Title:      snap.Title,
			Body:       snap.Body,
			RestoreURL: "/admin/posts/" + rev.EntityID.String() + "/revisions/" + rev.ID.String() + "/restore",
		})
	}
	return rows
}

func (h *PostAdminHandler) authorName(ctx context.Context, id uuid.UUID) string {
	if h.authors == nil {
		return ""
	}
	u, err := h.authors.GetByID(ctx, id)
	if err != nil {
		return ""
	}
	if u.Name != "" {
		return u.Name
	}
	return u.Email
}

func (h *PostAdminHandler) statusTabs(active string) []webtempl.StatusTab {
	mk := func(label, value string) webtempl.StatusTab {
		href := "/admin/posts"
		if value != "" {
			href += "?status=" + value
		}
		return webtempl.StatusTab{Label: label, Value: value, Href: href, Active: active == value}
	}
	return []webtempl.StatusTab{mk("All", ""), mk("Published", "PUBLISHED"), mk("Draft", "DRAFT")}
}

func (h *PostAdminHandler) render(w http.ResponseWriter, r *http.Request, c webtempl.Component) {
	if err := render.Component(r.Context(), w, http.StatusOK, c); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func statusView(s kernel.Status) webtempl.PostStatus {
	if s == kernel.StatusPublished {
		return webtempl.PostStatusPublished
	}
	return webtempl.PostStatusDraft
}

func postDisplayDate(p posts.Post) string {
	if p.PublishedAt != nil {
		return p.PublishedAt.Format("Jan 2, 2006")
	}
	if p.Scheduled() {
		return "→ " + p.ScheduledAt.Format("Jan 2, 2006")
	}
	return p.UpdatedAt.Format("Jan 2, 2006")
}

func scheduleValue(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02T15:04")
}

func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("Jan 2, 2006 15:04")
}

func humanError(err error) string {
	switch {
	case errors.Is(err, posts.ErrTitleRequired):
		return "Title is required."
	case errors.Is(err, posts.ErrForbidden):
		return "You do not have permission to do that."
	default:
		return "Something went wrong. Please try again."
	}
}

func pageParam(r *http.Request) int {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	return page
}

func statusQuery(status string) string {
	if status == "DRAFT" || status == "PUBLISHED" {
		return "status=" + status
	}
	return ""
}

func pager(page, size, total int, basePath, extraQuery string) webtempl.Pagination {
	p := webtempl.Pagination{Page: page, PageSize: size, Total: total}
	build := func(n int) string {
		q := "?page=" + strconv.Itoa(n)
		if extraQuery != "" {
			q += "&" + extraQuery
		}
		return basePath + q
	}
	if page > 1 {
		p.PrevURL = build(page - 1)
	}
	if page < p.TotalPages() {
		p.NextURL = build(page + 1)
	}
	return p
}

// snapshotView is the decoded revision snapshot for display.
type snapshotView struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}
