package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/services"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// ServiceAdminService is the subset of *services.Manager the admin handler calls.
type ServiceAdminService interface {
	AdminList(ctx context.Context, f services.ListFilter) ([]services.Service, int, error)
	AdminTrashed(ctx context.Context, limit, offset int) ([]services.Service, int, error)
	Get(ctx context.Context, actorID, id uuid.UUID) (services.Service, error)
	Create(ctx context.Context, actorID uuid.UUID, in services.CreateInput) (services.Service, error)
	Update(ctx context.Context, actorID, id uuid.UUID, in services.UpdateInput) (services.Service, error)
	Trash(ctx context.Context, actorID, id uuid.UUID) error
	Restore(ctx context.Context, actorID, id uuid.UUID) error
	PermanentDelete(ctx context.Context, actorID, id uuid.UUID) error
	Revisions(ctx context.Context, actorID, id uuid.UUID) ([]kernel.Revision, error)
	RestoreRevision(ctx context.Context, actorID, id, revisionID uuid.UUID) (services.Service, error)

	// Per-locale content overlay (M7b-2). GetInLocale loads the editor's content
	// overlaid by the active tab's locale (base fallback); TranslatedLocales marks
	// which tabs already have a translation; SaveTranslation upserts a de/ru tab's
	// content. The default locale (en) still edits the base row via Update.
	GetInLocale(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale) (services.Service, error)
	TranslatedLocales(ctx context.Context, actorID, id uuid.UUID) ([]i18n.Locale, error)
	SaveTranslation(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale, in services.TranslationInput) error

	// Bulk list actions (M2c). The concrete *services.Manager satisfies these; the
	// set also makes the manager a bulkActor.
	BulkTrash(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkRestore(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkPermanentDelete(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkPublish(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
	BulkUnpublish(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
}

// ServiceAdminHandler is the thin HTTP boundary for the admin services area.
type ServiceAdminHandler struct {
	svc     ServiceAdminService
	authors AuthorNamer
	shell   adminShellDeps
	csrf    func(*http.Request) string
}

// NewServiceAdminHandler constructs the admin services handler.
func NewServiceAdminHandler(svc ServiceAdminService, shell adminShellDeps, authors AuthorNamer, csrf func(*http.Request) string) *ServiceAdminHandler {
	return &ServiceAdminHandler{svc: svc, shell: shell, authors: authors, csrf: csrf}
}

// List renders the admin services table.
func (h *ServiceAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	statusParam := r.URL.Query().Get("status")

	f := services.ListFilter{Limit: adminPageSize, Offset: (page - 1) * adminPageSize}
	if statusParam == "DRAFT" || statusParam == "PUBLISHED" {
		s := kernel.Status(statusParam)
		f.Status = &s
	}

	items, total, err := h.svc.AdminList(r.Context(), f)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	view := webtempl.ServiceListView{
		Shell:     h.shell.buildShell(r, "Services"),
		Rows:      h.rows(items),
		Tabs:      h.statusTabs(statusParam),
		Pager:     pager(page, adminPageSize, total, "/admin/services", statusQuery(statusParam)),
		NewURL:    "/admin/services/new",
		BulkURL:   "/admin/services/bulk",
		Summary:   bulkSummaryFromQuery(r),
		CSRFToken: h.csrf(r),
	}
	h.render(w, r, webtempl.ServiceList(view))
}

// Bulk dispatches an allow-listed bulk action over the submitted service ids via
// the shared handleBulk driver. Services have no per-author ownership; the route
// gate already required the coarse (action, service) grant.
func (h *ServiceAdminHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	handleBulk(w, r, h.svc, "/admin/services")
}

// New renders the empty editor.
func (h *ServiceAdminHandler) New(w http.ResponseWriter, r *http.Request) {
	view := webtempl.ServiceFormView{
		Shell:       h.shell.buildShell(r, "New service"),
		IsNew:       true,
		Status:      webtempl.PostStatusDraft,
		FAQs:        []webtempl.ServiceFAQField{},
		ActionURL:   "/admin/services",
		CSRFToken:   h.csrf(r),
		FieldErrors: map[string]string{},
		BackURL:     "/admin/services",
	}
	h.render(w, r, webtempl.ServiceEditor(view))
}

// Create handles the new-service POST.
func (h *ServiceAdminHandler) Create(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	in := h.decodeForm(r)

	svc, err := h.svc.Create(r.Context(), u.ID, services.CreateInput{
		Title:      in.title,
		Slug:       in.slug,
		Summary:    in.summary,
		Body:       in.body,
		Price:      in.price,
		AreaServed: in.areaServed,
		Status:     in.status,
		FAQs:       in.faqs,

		MetaTitle:       in.metaTitle,
		MetaDescription: in.metaDescription,
		CanonicalURL:    in.canonicalURL,
		NoIndex:         in.noindex,
	})
	if err != nil {
		h.renderCreateError(w, r, in, err)
		return
	}
	http.Redirect(w, r, "/admin/services/"+svc.ID.String()+"/edit", http.StatusSeeOther)
}

// Edit renders the editor for an existing service.
func (h *ServiceAdminHandler) Edit(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// The active editor tab is chosen by ?language=xx (django-parler parity); it
	// defaults to en (the base row). Content is loaded overlaid by that locale.
	locale := editorLocale(r)
	svc, err := h.svc.GetInLocale(r.Context(), u.ID, id, locale)
	if errors.Is(err, services.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, services.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, webtempl.ServiceEditor(h.formView(r, svc, locale)))
}

// Update handles the edit POST (save/publish via the action button).
func (h *ServiceAdminHandler) Update(w http.ResponseWriter, r *http.Request) {
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
		err = h.svc.SaveTranslation(r.Context(), u.ID, id, loc, services.TranslationInput{
			Title:           in.title,
			Summary:         in.summary,
			Body:            in.body,
			MetaTitle:       in.metaTitle,
			MetaDescription: in.metaDescription,
		})
		if errors.Is(err, services.ErrForbidden) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		if errors.Is(err, services.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			svc, _ := h.svc.GetInLocale(r.Context(), u.ID, id, loc)
			view := h.formView(r, svc, loc)
			view.Error = serviceHumanError(err)
			h.render(w, r, webtempl.ServiceEditor(view))
			return
		}
		http.Redirect(w, r, "/admin/services/"+id.String()+"/edit?language="+loc.String(), http.StatusSeeOther)
		return
	}

	upd := services.UpdateInput{
		Title:      &in.title,
		Slug:       &in.slug,
		Summary:    &in.summary,
		Body:       &in.body,
		Price:      &in.price,
		AreaServed: &in.areaServed,
		Status:     &in.status,
		SetFAQs:    true,
		FAQs:       in.faqs,

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
	if errors.Is(err, services.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if errors.Is(err, services.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		svc, _ := h.svc.Get(r.Context(), u.ID, id)
		view := h.formView(r, svc, i18n.Default())
		view.Error = serviceHumanError(err)
		h.render(w, r, webtempl.ServiceEditor(view))
		return
	}
	http.Redirect(w, r, "/admin/services/"+id.String()+"/edit", http.StatusSeeOther)
}

// Trash soft-deletes a service then redirects to the list.
func (h *ServiceAdminHandler) Trash(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(actor, id uuid.UUID) error { return h.svc.Trash(r.Context(), actor, id) }, "/admin/services")
}

// RestoreTrashed restores a trashed service then redirects to trash.
func (h *ServiceAdminHandler) RestoreTrashed(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(actor, id uuid.UUID) error { return h.svc.Restore(r.Context(), actor, id) }, "/admin/services/trash")
}

// PermanentDelete hard-deletes a trashed service then redirects to trash.
func (h *ServiceAdminHandler) PermanentDelete(w http.ResponseWriter, r *http.Request) {
	h.mutate(w, r, func(actor, id uuid.UUID) error { return h.svc.PermanentDelete(r.Context(), actor, id) }, "/admin/services/trash")
}

// Trashed renders the trash list.
func (h *ServiceAdminHandler) Trashed(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	items, total, err := h.svc.AdminTrashed(r.Context(), adminPageSize, (page-1)*adminPageSize)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	rows := make([]webtempl.TrashRow, 0, len(items))
	for _, s := range items {
		rows = append(rows, webtempl.TrashRow{
			ID:         s.ID.String(),
			Title:      s.Title,
			DeletedAt:  formatTime(s.DeletedAt),
			RestoreURL: "/admin/services/trash/" + s.ID.String() + "/restore",
			DeleteURL:  "/admin/services/trash/" + s.ID.String() + "/delete",
		})
	}
	view := webtempl.ServiceTrashView{
		Shell:     h.shell.buildShell(r, "Trash"),
		Rows:      rows,
		Pager:     pager(page, adminPageSize, total, "/admin/services/trash", ""),
		CSRFToken: h.csrf(r),
	}
	h.render(w, r, webtempl.ServiceTrash(view))
}

// Revisions renders the revision history for a service.
func (h *ServiceAdminHandler) Revisions(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	svc, err := h.svc.Get(r.Context(), u.ID, id)
	if errors.Is(err, services.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, services.ErrForbidden) {
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
	view := webtempl.ServiceRevisionsView{
		Shell:        h.shell.buildShell(r, "Revisions"),
		ServiceTitle: svc.Title,
		ServiceID:    svc.ID.String(),
		Current:      webtempl.RevisionRow{Title: svc.Title, Body: svc.Body},
		Rows:         h.revisionRows(r.Context(), revs),
		BackURL:      "/admin/services/" + id.String() + "/edit",
		CSRFToken:    h.csrf(r),
	}
	h.render(w, r, webtempl.ServiceRevisions(view))
}

// RestoreRevision applies a snapshot then redirects to the editor.
func (h *ServiceAdminHandler) RestoreRevision(w http.ResponseWriter, r *http.Request) {
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
	if errors.Is(err, services.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/services/"+id.String()+"/edit", http.StatusSeeOther)
}

// --- helpers -----------------------------------------------------------------

type serviceForm struct {
	title           string
	slug            string
	summary         string
	body            string
	price           string
	areaServed      string
	status          kernel.Status
	faqs            []services.FAQInput
	metaTitle       string
	metaDescription string
	canonicalURL    string
	noindex         bool
}

func (h *ServiceAdminHandler) decodeForm(r *http.Request) serviceForm {
	_ = r.ParseForm()
	questions := r.PostForm["faq_question[]"]
	answers := r.PostForm["faq_answer[]"]
	faqs := make([]services.FAQInput, 0, len(questions))
	for i := range questions {
		ans := ""
		if i < len(answers) {
			ans = answers[i]
		}
		faqs = append(faqs, services.FAQInput{Question: questions[i], Answer: ans})
	}
	return serviceForm{
		title:           r.PostFormValue("title"),
		slug:            r.PostFormValue("slug"),
		summary:         r.PostFormValue("summary"),
		body:            r.PostFormValue("body"),
		price:           r.PostFormValue("price"),
		areaServed:      r.PostFormValue("area_served"),
		status:          kernel.ParseStatus(r.PostFormValue("status")),
		faqs:            faqs,
		metaTitle:       r.PostFormValue("meta_title"),
		metaDescription: r.PostFormValue("meta_description"),
		canonicalURL:    r.PostFormValue("canonical_url"),
		noindex:         r.PostFormValue("noindex") != "",
	}
}

func (h *ServiceAdminHandler) renderCreateError(w http.ResponseWriter, r *http.Request, in serviceForm, err error) {
	if errors.Is(err, services.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	view := webtempl.ServiceFormView{
		Shell:       h.shell.buildShell(r, "New service"),
		IsNew:       true,
		Title:       in.title,
		Slug:        in.slug,
		Summary:     in.summary,
		Body:        in.body,
		Price:       in.price,
		AreaServed:  in.areaServed,
		Status:      statusView(in.status),
		FAQs:        faqFields(in.faqs),
		ActionURL:   "/admin/services",
		CSRFToken:   h.csrf(r),
		FieldErrors: map[string]string{},
		Error:       serviceHumanError(err),
		BackURL:     "/admin/services",

		MetaTitle:       in.metaTitle,
		MetaDescription: in.metaDescription,
		CanonicalURL:    in.canonicalURL,
		NoIndex:         in.noindex,
	}
	if errors.Is(err, services.ErrTitleRequired) {
		view.FieldErrors["title"] = "Title is required."
	}
	h.render(w, r, webtempl.ServiceEditor(view))
}

func (h *ServiceAdminHandler) mutate(w http.ResponseWriter, r *http.Request, fn func(actor, id uuid.UUID) error, redirect string) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = fn(u.ID, id)
	if errors.Is(err, services.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if errors.Is(err, services.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (h *ServiceAdminHandler) formView(r *http.Request, s services.Service, locale i18n.Locale) webtempl.ServiceFormView {
	faqs := make([]webtempl.ServiceFAQField, 0, len(s.FAQs))
	for _, f := range s.FAQs {
		faqs = append(faqs, webtempl.ServiceFAQField{Question: f.Question, Answer: f.Answer})
	}
	return webtempl.ServiceFormView{
		Shell:           h.shell.buildShell(r, "Edit service"),
		IsNew:           false,
		ID:              s.ID.String(),
		Title:           s.Title,
		Slug:            s.Slug,
		Summary:         s.Summary,
		Body:            s.Body,
		Price:           s.Price,
		AreaServed:      s.AreaServed,
		Status:          statusView(s.Status),
		FAQs:            faqs,
		ActionURL:       "/admin/services/" + s.ID.String(),
		CSRFToken:       h.csrf(r),
		FieldErrors:     map[string]string{},
		RevisionsURL:    "/admin/services/" + s.ID.String() + "/revisions",
		BackURL:         "/admin/services",
		MetaTitle:       s.MetaTitle,
		MetaDescription: s.MetaDescription,
		CanonicalURL:    s.CanonicalURL,
		NoIndex:         s.NoIndex,
		LocaleTabs:      h.localeTabs(r, s.ID, locale),
		ActiveLocale:    locale.String(),
		IsDefaultLocale: locale.IsDefault(),
	}
}

// localeTabs builds the service editor's per-locale tab strip: one tab per
// supported locale, the active one selected, de/ru tabs marked when a
// translation row already exists. Best-effort: a read error yields no dots. Each
// tab links to the editor with ?language=xx.
func (h *ServiceAdminHandler) localeTabs(r *http.Request, serviceID uuid.UUID, active i18n.Locale) []webtempl.LocaleTab {
	u, _ := UserFromContext(r.Context())
	has := map[i18n.Locale]bool{}
	if locs, err := h.svc.TranslatedLocales(r.Context(), u.ID, serviceID); err == nil {
		for _, l := range locs {
			has[l] = true
		}
	}
	base := "/admin/services/" + serviceID.String() + "/edit"
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

func (h *ServiceAdminHandler) rows(items []services.Service) []webtempl.ServiceRow {
	rows := make([]webtempl.ServiceRow, 0, len(items))
	for _, s := range items {
		rows = append(rows, webtempl.ServiceRow{
			ID:      s.ID.String(),
			Title:   s.Title,
			Slug:    s.Slug,
			Status:  statusView(s.Status),
			Price:   s.Price,
			Date:    s.UpdatedAt.Format("Jan 2, 2006"),
			EditURL: "/admin/services/" + s.ID.String() + "/edit",
		})
	}
	return rows
}

func (h *ServiceAdminHandler) revisionRows(ctx context.Context, revs []kernel.Revision) []webtempl.RevisionRow {
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
			RestoreURL: "/admin/services/" + rev.EntityID.String() + "/revisions/" + rev.ID.String() + "/restore",
		})
	}
	return rows
}

func (h *ServiceAdminHandler) authorName(ctx context.Context, id uuid.UUID) string {
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

func (h *ServiceAdminHandler) statusTabs(active string) []webtempl.StatusTab {
	mk := func(label, value string) webtempl.StatusTab {
		href := "/admin/services"
		if value != "" {
			href += "?status=" + value
		}
		return webtempl.StatusTab{Label: label, Value: value, Href: href, Active: active == value}
	}
	return []webtempl.StatusTab{mk("All", ""), mk("Published", "PUBLISHED"), mk("Draft", "DRAFT")}
}

func (h *ServiceAdminHandler) render(w http.ResponseWriter, r *http.Request, c webtempl.Component) {
	if err := render.Component(r.Context(), w, http.StatusOK, c); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

func faqFields(in []services.FAQInput) []webtempl.ServiceFAQField {
	out := make([]webtempl.ServiceFAQField, 0, len(in))
	for _, f := range in {
		out = append(out, webtempl.ServiceFAQField{Question: f.Question, Answer: f.Answer})
	}
	return out
}

func serviceHumanError(err error) string {
	switch {
	case errors.Is(err, services.ErrTitleRequired):
		return "Title is required."
	case errors.Is(err, services.ErrForbidden):
		return "You do not have permission to do that."
	default:
		return "Something went wrong. Please try again."
	}
}

// compile-time assertion that the service subject constant exists.
var _ = accounts.SubjectService
