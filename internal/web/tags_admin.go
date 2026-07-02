package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/categories"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/tags"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// TagAdminService is the subset of *tags.Service the admin handler calls.
type TagAdminService interface {
	AdminList(ctx context.Context, limit, offset int) ([]tags.Tag, int, error)
	Get(ctx context.Context, actorID, id uuid.UUID) (tags.Tag, error)
	Create(ctx context.Context, actorID uuid.UUID, in tags.CreateInput) (tags.Tag, error)
	Update(ctx context.Context, actorID, id uuid.UUID, in tags.UpdateInput) (tags.Tag, error)
	Delete(ctx context.Context, actorID, id uuid.UUID) error
	BulkDelete(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)

	// Per-locale content overlay (M7b-3): GetInLocale loads the editor content
	// overlaid by the active tab's locale (base fallback); TranslatedLocales marks
	// which tabs already have a translation; SaveTranslation upserts a de/ru tab's
	// name without touching the shared slug.
	GetInLocale(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale) (tags.Tag, error)
	TranslatedLocales(ctx context.Context, actorID, id uuid.UUID) ([]i18n.Locale, error)
	SaveTranslation(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale, in tags.TranslationInput) error
}

// TagAdminHandler is the thin HTTP boundary for the admin tags area.
type TagAdminHandler struct {
	svc   TagAdminService
	shell adminShellDeps
	csrf  func(*http.Request) string
}

// NewTagAdminHandler constructs the admin tags handler.
func NewTagAdminHandler(svc TagAdminService, shell adminShellDeps, csrf func(*http.Request) string) *TagAdminHandler {
	return &TagAdminHandler{svc: svc, shell: shell, csrf: csrf}
}

// List renders the admin tags table with pagination + bulk delete.
func (h *TagAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	items, total, err := h.svc.AdminList(r.Context(), adminPageSize, (page-1)*adminPageSize)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	rows := make([]webtempl.TagRow, 0, len(items))
	for _, t := range items {
		rows = append(rows, webtempl.TagRow{
			ID:       t.ID.String(),
			Name:     t.Name,
			Slug:     t.Slug,
			EditURL:  "/admin/tags/" + t.ID.String() + "/edit",
			PostsURL: "/tags/" + t.Slug,
		})
	}
	view := webtempl.TagListView{
		Shell:     h.shell.buildShell(r, "Tags"),
		Rows:      rows,
		Pager:     pager(page, adminPageSize, total, "/admin/tags", ""),
		NewURL:    "/admin/tags/new",
		BulkURL:   "/admin/tags/bulk",
		Summary:   bulkSummaryFromQuery(r),
		CSRFToken: h.csrf(r),
	}
	h.render(w, r, webtempl.TagList(view))
}

// New renders the empty editor.
func (h *TagAdminHandler) New(w http.ResponseWriter, r *http.Request) {
	view := webtempl.TagFormView{
		Shell:       h.shell.buildShell(r, "New tag"),
		IsNew:       true,
		ActionURL:   "/admin/tags",
		CSRFToken:   h.csrf(r),
		FieldErrors: map[string]string{},
		BackURL:     "/admin/tags",
	}
	h.render(w, r, webtempl.TagEditor(view))
}

// Create handles the new-tag POST.
func (h *TagAdminHandler) Create(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	_ = r.ParseForm()
	in := tags.CreateInput{Name: r.PostFormValue("name"), Slug: r.PostFormValue("slug")}
	tag, err := h.svc.Create(r.Context(), u.ID, in)
	if err != nil {
		view := webtempl.TagFormView{
			Shell:       h.shell.buildShell(r, "New tag"),
			IsNew:       true,
			Name:        in.Name,
			Slug:        in.Slug,
			ActionURL:   "/admin/tags",
			CSRFToken:   h.csrf(r),
			FieldErrors: map[string]string{},
			BackURL:     "/admin/tags",
		}
		h.renderFormError(w, r, view, err)
		return
	}
	http.Redirect(w, r, "/admin/tags/"+tag.ID.String()+"/edit", http.StatusSeeOther)
}

// Edit renders the editor for an existing tag.
func (h *TagAdminHandler) Edit(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// The active editor tab is chosen by ?language=xx (django-parler parity); it
	// defaults to en (the base row). Content is loaded overlaid by that locale.
	locale := editorLocale(r)
	tag, err := h.svc.GetInLocale(r.Context(), u.ID, id, locale)
	if errors.Is(err, tags.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, tags.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, webtempl.TagEditor(h.formView(r, tag, locale)))
}

// Update handles the edit POST.
func (h *TagAdminHandler) Update(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	name := r.PostFormValue("name")
	slug := r.PostFormValue("slug")

	// Per-locale save (M7b-3): a non-default `locale` field means the editor is on
	// a de/ru translation tab, so the translatable name is upserted to the overlay
	// rather than the base row. en (default/empty) takes the base Update path below.
	if loc, ok := i18n.Parse(r.PostFormValue("locale")); ok && !loc.IsDefault() {
		err = h.svc.SaveTranslation(r.Context(), u.ID, id, loc, tags.TranslationInput{Name: name})
		if errors.Is(err, tags.ErrForbidden) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		if errors.Is(err, tags.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			tag, _ := h.svc.GetInLocale(r.Context(), u.ID, id, loc)
			view := h.formView(r, tag, loc)
			view.Name = name
			view.Error = taxonomyHumanError(err)
			if errors.Is(err, tags.ErrNameRequired) {
				view.FieldErrors["name"] = "Name is required."
			}
			h.render(w, r, webtempl.TagEditor(view))
			return
		}
		http.Redirect(w, r, "/admin/tags/"+id.String()+"/edit?language="+loc.String(), http.StatusSeeOther)
		return
	}

	_, err = h.svc.Update(r.Context(), u.ID, id, tags.UpdateInput{Name: &name, Slug: &slug})
	if errors.Is(err, tags.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if errors.Is(err, tags.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		view := webtempl.TagFormView{
			Shell:       h.shell.buildShell(r, "Edit tag"),
			IsNew:       false,
			ID:          id.String(),
			Name:        name,
			Slug:        slug,
			ActionURL:   "/admin/tags/" + id.String(),
			CSRFToken:   h.csrf(r),
			FieldErrors: map[string]string{},
			BackURL:     "/admin/tags",
		}
		h.renderFormError(w, r, view, err)
		return
	}
	http.Redirect(w, r, "/admin/tags/"+id.String()+"/edit", http.StatusSeeOther)
}

// Delete hard-deletes a tag then redirects to the list.
func (h *TagAdminHandler) Delete(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = h.svc.Delete(r.Context(), u.ID, id)
	if errors.Is(err, tags.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if errors.Is(err, tags.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/tags", http.StatusSeeOther)
}

// Bulk dispatches the delete-only bulk action over the submitted tag ids.
func (h *TagAdminHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	handleTaxonomyBulk(w, r, h.svc.BulkDelete, "/admin/tags")
}

// --- helpers -----------------------------------------------------------------

func (h *TagAdminHandler) formView(r *http.Request, t tags.Tag, locale i18n.Locale) webtempl.TagFormView {
	return webtempl.TagFormView{
		Shell:           h.shell.buildShell(r, "Edit tag"),
		IsNew:           false,
		ID:              t.ID.String(),
		Name:            t.Name,
		Slug:            t.Slug,
		ActionURL:       "/admin/tags/" + t.ID.String(),
		CSRFToken:       h.csrf(r),
		FieldErrors:     map[string]string{},
		BackURL:         "/admin/tags",
		LocaleTabs:      h.localeTabs(r, t.ID, locale),
		ActiveLocale:    locale.String(),
		IsDefaultLocale: locale.IsDefault(),
	}
}

// localeTabs builds the tag editor's per-locale tab strip (see the categories
// editor for the shared shape). Best-effort translated-locale dots.
func (h *TagAdminHandler) localeTabs(r *http.Request, tagID uuid.UUID, active i18n.Locale) []webtempl.LocaleTab {
	u, _ := UserFromContext(r.Context())
	has := map[i18n.Locale]bool{}
	if locs, err := h.svc.TranslatedLocales(r.Context(), u.ID, tagID); err == nil {
		for _, l := range locs {
			has[l] = true
		}
	}
	return buildLocaleTabs("/admin/tags/"+tagID.String()+"/edit", active, has)
}

func (h *TagAdminHandler) renderFormError(w http.ResponseWriter, r *http.Request, view webtempl.TagFormView, err error) {
	if errors.Is(err, tags.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	view.Error = taxonomyHumanError(err)
	if errors.Is(err, tags.ErrNameRequired) {
		view.FieldErrors["name"] = "Name is required."
	}
	h.render(w, r, webtempl.TagEditor(view))
}

func (h *TagAdminHandler) render(w http.ResponseWriter, r *http.Request, c webtempl.Component) {
	if err := render.Component(r.Context(), w, http.StatusOK, c); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// taxonomyHumanError maps a category/tag domain error to a user-facing message.
func taxonomyHumanError(err error) string {
	switch {
	case errors.Is(err, categories.ErrNameRequired), errors.Is(err, tags.ErrNameRequired):
		return "Name is required."
	case errors.Is(err, categories.ErrParentCycle):
		return "That parent would create a cycle. Choose a different parent."
	case errors.Is(err, categories.ErrParentNotFound):
		return "The chosen parent no longer exists."
	default:
		return "Something went wrong. Please try again."
	}
}
