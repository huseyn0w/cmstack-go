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
	tag, err := h.svc.Get(r.Context(), u.ID, id)
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
	h.render(w, r, webtempl.TagEditor(h.formView(r, tag)))
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

func (h *TagAdminHandler) formView(r *http.Request, t tags.Tag) webtempl.TagFormView {
	return webtempl.TagFormView{
		Shell:       h.shell.buildShell(r, "Edit tag"),
		IsNew:       false,
		ID:          t.ID.String(),
		Name:        t.Name,
		Slug:        t.Slug,
		ActionURL:   "/admin/tags/" + t.ID.String(),
		CSRFToken:   h.csrf(r),
		FieldErrors: map[string]string{},
		BackURL:     "/admin/tags",
	}
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
