package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/content/pages"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/i18n"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

// PageAdminService is the subset of *pages.Service the admin handler calls.
type PageAdminService interface {
	AdminList(ctx context.Context, f pages.ListFilter) ([]pages.Page, int, error)
	AllActive(ctx context.Context) ([]pages.Page, error)
	AdminTrashed(ctx context.Context, limit, offset int) ([]pages.Page, int, error)
	Get(ctx context.Context, actorID, id uuid.UUID) (pages.Page, error)
	Create(ctx context.Context, actorID uuid.UUID, in pages.CreateInput) (pages.Page, error)
	Update(ctx context.Context, actorID, id uuid.UUID, in pages.UpdateInput) (pages.Page, error)
	Trash(ctx context.Context, actorID, id uuid.UUID) error
	Restore(ctx context.Context, actorID, id uuid.UUID) error
	PermanentDelete(ctx context.Context, actorID, id uuid.UUID) error
	Revisions(ctx context.Context, actorID, id uuid.UUID) ([]kernel.Revision, error)
	RestoreRevision(ctx context.Context, actorID, id, revisionID uuid.UUID) (pages.Page, error)

	// Per-locale content overlay (M7b-2). GetInLocale loads the editor's content
	// overlaid by the active tab's locale (base fallback); TranslatedLocales marks
	// which tabs already have a translation; SaveTranslation upserts a de/ru tab's
	// content. The default locale (en) still edits the base row via Update.
	GetInLocale(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale) (pages.Page, error)
	TranslatedLocales(ctx context.Context, actorID, id uuid.UUID) ([]i18n.Locale, error)
	SaveTranslation(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale, in pages.TranslationInput) error

	// Bulk list actions (M2c). The concrete *pages.Service satisfies these; the
	// set also makes the service a bulkActor.
	BulkTrash(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkRestore(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkPermanentDelete(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkPublish(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkUnpublish(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
}

// PageAdminHandler is the thin HTTP boundary for the admin pages area.
type PageAdminHandler struct {
	svc     PageAdminService
	authors AuthorNamer
	shell   adminShellDeps
	csrf    func(*http.Request) string
}

// NewPageAdminHandler constructs the admin pages handler.
func NewPageAdminHandler(svc PageAdminService, shell adminShellDeps, authors AuthorNamer, csrf func(*http.Request) string) *PageAdminHandler {
	return &PageAdminHandler{svc: svc, shell: shell, authors: authors, csrf: csrf}
}

// List renders the admin pages tree with status tabs and pagination.
func (h *PageAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	statusParam := r.URL.Query().Get("status")

	f := pages.ListFilter{Limit: adminPageSize, Offset: (page - 1) * adminPageSize}
	if statusParam == "DRAFT" || statusParam == "PUBLISHED" {
		s := kernel.Status(statusParam)
		f.Status = &s
	}

	items, total, err := h.svc.AdminList(r.Context(), f)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	view := webtempl.PageListView{
		Shell:     h.shell.buildShell(r, "Pages"),
		Rows:      h.rows(items),
		Tabs:      h.statusTabs(statusParam),
		Pager:     pager(page, adminPageSize, total, "/admin/pages", statusQuery(statusParam)),
		NewURL:    "/admin/pages/new",
		BulkURL:   "/admin/pages/bulk",
		Summary:   bulkSummaryFromQuery(r),
		CSRFToken: h.csrf(r),
	}
	h.render(w, r, webtempl.PageList(view))
}

// Bulk dispatches an allow-listed bulk action over the submitted page ids via
// the shared handleBulk driver. Pages have no per-author ownership; the route
// gate already required the coarse (action, page) grant.
func (h *PageAdminHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	handleBulk(w, r, h.svc, "/admin/pages")
}

// New renders the empty editor.
func (h *PageAdminHandler) New(w http.ResponseWriter, r *http.Request) {
	parents, _ := h.parentOptions(r.Context(), uuid.Nil)
	view := webtempl.PageFormView{
		Shell:        h.shell.buildShell(r, "New page"),
		IsNew:        true,
		Status:       webtempl.PostStatusDraft,
		Template:     pages.TemplateDefault,
		Parents:      parents,
		TemplateOpts: templateOptions(),
		ActionURL:    "/admin/pages",
		CSRFToken:    h.csrf(r),
		FieldErrors:  map[string]string{},
		BackURL:      "/admin/pages",
	}
	h.render(w, r, webtempl.PageEditor(view))
}

// Create handles the new-page POST.
func (h *PageAdminHandler) Create(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	in := h.decodeForm(r)

	p, err := h.svc.Create(r.Context(), u.ID, pages.CreateInput{
		Title:    in.title,
		Slug:     in.slug,
		Body:     in.body,
		Status:   in.status,
		ParentID: in.parentID,
		Template: in.template,

		MetaTitle:       in.metaTitle,
		MetaDescription: in.metaDescription,
		CanonicalURL:    in.canonicalURL,
		NoIndex:         in.noindex,
	})
	if err != nil {
		h.renderCreateError(w, r, in, err)
		return
	}
	http.Redirect(w, r, "/admin/pages/"+p.ID.String()+"/edit", http.StatusSeeOther)
}

// Edit renders the editor for an existing page.
func (h *PageAdminHandler) Edit(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// The active editor tab is chosen by ?language=xx (django-parler parity); it
	// defaults to en (the base row). Content is loaded overlaid by that locale.
	locale := editorLocale(r)
	p, err := h.svc.GetInLocale(r.Context(), u.ID, id, locale)
	if errors.Is(err, pages.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, pages.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, webtempl.PageEditor(h.formView(r, p, locale)))
}

// Update handles the edit POST (save/publish via the action button).
func (h *PageAdminHandler) Update(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	in := h.decodeForm(r)

	// Per-locale save (M7b-2): a non-default `locale` field means the editor is on
	// a de/ru translation tab, so the translatable content is upserted to the
	// overlay rather than the base row. en (default/empty) takes the base Update
	// path below.
	if loc, ok := i18n.Parse(r.PostFormValue("locale")); ok && !loc.IsDefault() {
		err = h.svc.SaveTranslation(r.Context(), u.ID, id, loc, pages.TranslationInput{
			Title:           in.title,
			Body:            in.body,
			MetaTitle:       in.metaTitle,
			MetaDescription: in.metaDescription,
		})
		if errors.Is(err, pages.ErrForbidden) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		if errors.Is(err, pages.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			p, _ := h.svc.GetInLocale(r.Context(), u.ID, id, loc)
			view := h.formView(r, p, loc)
			view.Error = pageHumanError(err)
			h.render(w, r, webtempl.PageEditor(view))
			return
		}
		http.Redirect(w, r, "/admin/pages/"+id.String()+"/edit?language="+loc.String(), http.StatusSeeOther)
		return
	}

	upd := pages.UpdateInput{
		Title:     &in.title,
		Slug:      &in.slug,
		Body:      &in.body,
		Status:    &in.status,
		Template:  &in.template,
		SetParent: true,
		ParentID:  in.parentID,

		MetaTitle:       &in.metaTitle,
		MetaDescription: &in.metaDescription,
		CanonicalURL:    &in.canonicalURL,
		NoIndex:         &in.noindex,
	}
	if r.PostFormValue("action") == "publish" {
		published := kernel.StatusPublished
		upd.Status = &published
	}

	_, err = h.svc.Update(r.Context(), u.ID, id, upd)
	if errors.Is(err, pages.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if errors.Is(err, pages.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		p, _ := h.svc.Get(r.Context(), u.ID, id)
		view := h.formView(r, p, i18n.Default())
		view.Error = pageHumanError(err)
		h.render(w, r, webtempl.PageEditor(view))
		return
	}
	http.Redirect(w, r, "/admin/pages/"+id.String()+"/edit", http.StatusSeeOther)
}

// Trash soft-deletes a page then redirects to the list.
func (h *PageAdminHandler) Trash(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(actor, id uuid.UUID) error { return h.svc.Trash(r.Context(), actor, id) }, "/admin/pages")
}

// RestoreTrashed restores a trashed page then redirects to trash.
func (h *PageAdminHandler) RestoreTrashed(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(actor, id uuid.UUID) error { return h.svc.Restore(r.Context(), actor, id) }, "/admin/pages/trash")
}

// PermanentDelete hard-deletes a trashed page then redirects to trash.
func (h *PageAdminHandler) PermanentDelete(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(actor, id uuid.UUID) error { return h.svc.PermanentDelete(r.Context(), actor, id) }, "/admin/pages/trash")
}

// Trashed renders the trash list.
func (h *PageAdminHandler) Trashed(w http.ResponseWriter, r *http.Request) {
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
			RestoreURL: "/admin/pages/trash/" + p.ID.String() + "/restore",
			DeleteURL:  "/admin/pages/trash/" + p.ID.String() + "/delete",
		})
	}
	view := webtempl.PageTrashView{
		Shell:     h.shell.buildShell(r, "Trash"),
		Rows:      rows,
		Pager:     pager(page, adminPageSize, total, "/admin/pages/trash", ""),
		CSRFToken: h.csrf(r),
	}
	h.render(w, r, webtempl.PageTrash(view))
}

// Revisions renders the revision history for a page.
func (h *PageAdminHandler) Revisions(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := h.svc.Get(r.Context(), u.ID, id)
	if errors.Is(err, pages.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, pages.ErrForbidden) {
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
	view := webtempl.PageRevisionsView{
		Shell:     h.shell.buildShell(r, "Revisions"),
		PageTitle: p.Title,
		PageID:    p.ID.String(),
		Current:   webtempl.RevisionRow{Title: p.Title, Body: p.Body},
		Rows:      h.revisionRows(r.Context(), revs),
		BackURL:   "/admin/pages/" + id.String() + "/edit",
		CSRFToken: h.csrf(r),
	}
	h.render(w, r, webtempl.PageRevisions(view))
}

// RestoreRevision applies a snapshot then redirects to the editor.
func (h *PageAdminHandler) RestoreRevision(w http.ResponseWriter, r *http.Request) {
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
	if errors.Is(err, pages.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/pages/"+id.String()+"/edit", http.StatusSeeOther)
}

// --- helpers -----------------------------------------------------------------

type pageForm struct {
	title           string
	slug            string
	body            string
	status          kernel.Status
	parentID        *uuid.UUID
	template        string
	metaTitle       string
	metaDescription string
	canonicalURL    string
	noindex         bool
}

func (h *PageAdminHandler) decodeForm(r *http.Request) pageForm {
	f := pageForm{
		title:           r.PostFormValue("title"),
		slug:            r.PostFormValue("slug"),
		body:            r.PostFormValue("body"),
		status:          kernel.ParseStatus(r.PostFormValue("status")),
		template:        r.PostFormValue("template"),
		metaTitle:       r.PostFormValue("meta_title"),
		metaDescription: r.PostFormValue("meta_description"),
		canonicalURL:    r.PostFormValue("canonical_url"),
		noindex:         r.PostFormValue("noindex") != "",
	}
	if raw := r.PostFormValue("parent_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			f.parentID = &id
		}
	}
	return f
}

func (h *PageAdminHandler) renderCreateError(w http.ResponseWriter, r *http.Request, in pageForm, err error) {
	if errors.Is(err, pages.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	parents, _ := h.parentOptions(r.Context(), uuid.Nil)
	view := webtempl.PageFormView{
		Shell:        h.shell.buildShell(r, "New page"),
		IsNew:        true,
		Title:        in.title,
		Slug:         in.slug,
		Body:         in.body,
		Status:       statusView(in.status),
		Template:     in.template,
		Parents:      parents,
		TemplateOpts: templateOptions(),
		ActionURL:    "/admin/pages",
		CSRFToken:    h.csrf(r),
		FieldErrors:  map[string]string{},
		Error:        pageHumanError(err),
		BackURL:      "/admin/pages",

		MetaTitle:       in.metaTitle,
		MetaDescription: in.metaDescription,
		CanonicalURL:    in.canonicalURL,
		NoIndex:         in.noindex,
	}
	if in.parentID != nil {
		view.ParentID = in.parentID.String()
	}
	if errors.Is(err, pages.ErrTitleRequired) {
		view.FieldErrors["title"] = "Title is required."
	}
	h.render(w, r, webtempl.PageEditor(view))
}

func (h *PageAdminHandler) mutate(w http.ResponseWriter, r *http.Request, fn func(actor, id uuid.UUID) error, redirect string) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = fn(u.ID, id)
	if errors.Is(err, pages.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if errors.Is(err, pages.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (h *PageAdminHandler) formView(r *http.Request, p pages.Page, locale i18n.Locale) webtempl.PageFormView {
	parents, _ := h.parentOptions(r.Context(), p.ID)
	parentID := ""
	if p.ParentID != nil {
		parentID = p.ParentID.String()
	}
	return webtempl.PageFormView{
		Shell:           h.shell.buildShell(r, "Edit page"),
		IsNew:           false,
		ID:              p.ID.String(),
		Title:           p.Title,
		Slug:            p.Slug,
		Body:            p.Body,
		Status:          statusView(p.Status),
		ParentID:        parentID,
		Template:        p.Template,
		Parents:         parents,
		TemplateOpts:    templateOptions(),
		ActionURL:       "/admin/pages/" + p.ID.String(),
		CSRFToken:       h.csrf(r),
		FieldErrors:     map[string]string{},
		RevisionsURL:    "/admin/pages/" + p.ID.String() + "/revisions",
		BackURL:         "/admin/pages",
		MetaTitle:       p.MetaTitle,
		MetaDescription: p.MetaDescription,
		CanonicalURL:    p.CanonicalURL,
		NoIndex:         p.NoIndex,
		LocaleTabs:      h.localeTabs(r, p.ID, locale),
		ActiveLocale:    locale.String(),
		IsDefaultLocale: locale.IsDefault(),
	}
}

// localeTabs builds the page editor's per-locale tab strip: one tab per supported
// locale, the active one selected, de/ru tabs marked when a translation row
// already exists. Best-effort: a translated-locales read error yields no dots.
// Each tab links to the editor with ?language=xx.
func (h *PageAdminHandler) localeTabs(r *http.Request, pageID uuid.UUID, active i18n.Locale) []webtempl.LocaleTab {
	u, _ := UserFromContext(r.Context())
	has := map[i18n.Locale]bool{}
	if locs, err := h.svc.TranslatedLocales(r.Context(), u.ID, pageID); err == nil {
		for _, l := range locs {
			has[l] = true
		}
	}
	base := "/admin/pages/" + pageID.String() + "/edit"
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

func (h *PageAdminHandler) rows(items []pages.Page) []webtempl.PageRow {
	// Build a parent->children tree from the page list so the table shows the
	// hierarchy. Items in a status-filtered list may have parents not present in
	// the slice; those are treated as roots (depth 0) so nothing is dropped.
	byID := make(map[uuid.UUID]pages.Page, len(items))
	children := make(map[uuid.UUID][]pages.Page)
	var roots []pages.Page
	for _, p := range items {
		byID[p.ID] = p
	}
	for _, p := range items {
		if p.ParentID != nil {
			if _, ok := byID[*p.ParentID]; ok {
				children[*p.ParentID] = append(children[*p.ParentID], p)
				continue
			}
		}
		roots = append(roots, p)
	}
	var out []webtempl.PageRow
	var walk func(p pages.Page, depth int)
	walk = func(p pages.Page, depth int) {
		out = append(out, webtempl.PageRow{
			ID:       p.ID.String(),
			Title:    p.Title,
			Slug:     p.Slug,
			Status:   statusView(p.Status),
			Template: p.Template,
			Depth:    depth,
			Date:     p.UpdatedAt.Format("Jan 2, 2006"),
			EditURL:  "/admin/pages/" + p.ID.String() + "/edit",
		})
		for _, c := range children[p.ID] {
			walk(c, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return out
}

func (h *PageAdminHandler) revisionRows(ctx context.Context, revs []kernel.Revision) []webtempl.RevisionRow {
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
			RestoreURL: "/admin/pages/" + rev.EntityID.String() + "/revisions/" + rev.ID.String() + "/restore",
		})
	}
	return rows
}

// parentOptions returns the tree-indented parent-picker options, EXCLUDING the
// page being edited and its descendants (so the picker cannot create a cycle).
func (h *PageAdminHandler) parentOptions(ctx context.Context, excludeID uuid.UUID) ([]webtempl.PageParentOption, error) {
	all, err := h.svc.AllActive(ctx)
	if err != nil {
		return nil, err
	}
	children := make(map[uuid.UUID][]pages.Page)
	var roots []pages.Page
	byID := make(map[uuid.UUID]pages.Page, len(all))
	for _, p := range all {
		byID[p.ID] = p
	}
	for _, p := range all {
		if p.ParentID != nil {
			if _, ok := byID[*p.ParentID]; ok {
				children[*p.ParentID] = append(children[*p.ParentID], p)
				continue
			}
		}
		roots = append(roots, p)
	}
	var out []webtempl.PageParentOption
	var walk func(p pages.Page, depth int)
	walk = func(p pages.Page, depth int) {
		if p.ID == excludeID {
			return // skip self AND its whole subtree to prevent cycles
		}
		out = append(out, webtempl.PageParentOption{ID: p.ID.String(), Label: p.Title, Indent: depth})
		for _, c := range children[p.ID] {
			walk(c, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return out, nil
}

func (h *PageAdminHandler) authorName(ctx context.Context, id uuid.UUID) string {
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

func (h *PageAdminHandler) statusTabs(active string) []webtempl.StatusTab {
	mk := func(label, value string) webtempl.StatusTab {
		href := "/admin/pages"
		if value != "" {
			href += "?status=" + value
		}
		return webtempl.StatusTab{Label: label, Value: value, Href: href, Active: active == value}
	}
	return []webtempl.StatusTab{mk("All", ""), mk("Published", "PUBLISHED"), mk("Draft", "DRAFT")}
}

func (h *PageAdminHandler) render(w http.ResponseWriter, r *http.Request, c webtempl.Component) {
	if err := render.Component(r.Context(), w, http.StatusOK, c); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func templateOptions() []webtempl.PageTemplateOption {
	opts := make([]webtempl.PageTemplateOption, 0)
	for _, t := range pages.Templates() {
		opts = append(opts, webtempl.PageTemplateOption{Value: t.Value, Label: t.Label})
	}
	return opts
}

func pageHumanError(err error) string {
	switch {
	case errors.Is(err, pages.ErrTitleRequired):
		return "Title is required."
	case errors.Is(err, pages.ErrParentCycle):
		return "That parent would create a loop in the page hierarchy."
	case errors.Is(err, pages.ErrParentNotFound):
		return "The selected parent page no longer exists."
	case errors.Is(err, pages.ErrForbidden):
		return "You do not have permission to do that."
	default:
		return "Something went wrong. Please try again."
	}
}

// compile-time assertion that accounts subjects referenced exist.
var _ = accounts.SubjectPage
