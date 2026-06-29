package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/categories"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// CategoryAdminService is the subset of *categories.Service the admin handler
// calls. The Bulk method makes the service a taxonomyBulkActor (delete-only).
type CategoryAdminService interface {
	Tree(ctx context.Context) ([]categories.TreeNode, error)
	Get(ctx context.Context, actorID, id uuid.UUID) (categories.Category, error)
	Create(ctx context.Context, actorID uuid.UUID, in categories.CreateInput) (categories.Category, error)
	Update(ctx context.Context, actorID, id uuid.UUID, in categories.UpdateInput) (categories.Category, error)
	Delete(ctx context.Context, actorID, id uuid.UUID) error
	BulkDelete(ctx context.Context, actorID uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error)
}

// CategoryAdminHandler is the thin HTTP boundary for the admin categories area.
type CategoryAdminHandler struct {
	svc   CategoryAdminService
	shell adminShellDeps
	csrf  func(*http.Request) string
}

// NewCategoryAdminHandler constructs the admin categories handler.
func NewCategoryAdminHandler(svc CategoryAdminService, shell adminShellDeps, csrf func(*http.Request) string) *CategoryAdminHandler {
	return &CategoryAdminHandler{svc: svc, shell: shell, csrf: csrf}
}

// List renders the indented category tree with bulk delete.
func (h *CategoryAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.svc.Tree(r.Context())
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	rows := make([]webtempl.CategoryRow, 0, len(nodes))
	for _, n := range nodes {
		rows = append(rows, webtempl.CategoryRow{
			ID:       n.Category.ID.String(),
			Name:     n.Category.Name,
			Slug:     n.Category.Slug,
			Depth:    n.Depth,
			EditURL:  "/admin/categories/" + n.Category.ID.String() + "/edit",
			PostsURL: "/categories/" + n.Category.Slug,
		})
	}
	view := webtempl.CategoryListView{
		Shell:     h.shell.buildShell(r, "Categories"),
		Rows:      rows,
		NewURL:    "/admin/categories/new",
		BulkURL:   "/admin/categories/bulk",
		Summary:   bulkSummaryFromQuery(r),
		CSRFToken: h.csrf(r),
	}
	h.render(w, r, webtempl.CategoryList(view))
}

// New renders the empty editor with a parent picker.
func (h *CategoryAdminHandler) New(w http.ResponseWriter, r *http.Request) {
	choices, err := h.parentChoices(r.Context(), uuid.Nil, "")
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	view := webtempl.CategoryFormView{
		Shell:         h.shell.buildShell(r, "New category"),
		IsNew:         true,
		ActionURL:     "/admin/categories",
		ParentChoices: choices,
		CSRFToken:     h.csrf(r),
		FieldErrors:   map[string]string{},
		BackURL:       "/admin/categories",
	}
	h.render(w, r, webtempl.CategoryEditor(view))
}

// Create handles the new-category POST.
func (h *CategoryAdminHandler) Create(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	_ = r.ParseForm()
	in := categories.CreateInput{
		Name:        r.PostFormValue("name"),
		Slug:        r.PostFormValue("slug"),
		Description: r.PostFormValue("description"),
		ParentID:    parseParentID(r.PostFormValue("parent_id")),
	}
	cat, err := h.svc.Create(r.Context(), u.ID, in)
	if err != nil {
		h.renderFormError(w, r, webtempl.CategoryFormView{
			Shell:       h.shell.buildShell(r, "New category"),
			IsNew:       true,
			Name:        in.Name,
			Slug:        in.Slug,
			Description: in.Description,
			ParentID:    r.PostFormValue("parent_id"),
			ActionURL:   "/admin/categories",
			CSRFToken:   h.csrf(r),
			FieldErrors: map[string]string{},
			BackURL:     "/admin/categories",
		}, uuid.Nil, err)
		return
	}
	http.Redirect(w, r, "/admin/categories/"+cat.ID.String()+"/edit", http.StatusSeeOther)
}

// Edit renders the editor for an existing category.
func (h *CategoryAdminHandler) Edit(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cat, err := h.svc.Get(r.Context(), u.ID, id)
	if errors.Is(err, categories.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, categories.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, webtempl.CategoryEditor(h.formView(r, cat)))
}

// Update handles the edit POST.
func (h *CategoryAdminHandler) Update(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	name := r.PostFormValue("name")
	slug := r.PostFormValue("slug")
	desc := r.PostFormValue("description")
	parent := parseParentID(r.PostFormValue("parent_id"))
	in := categories.UpdateInput{
		Name:        &name,
		Slug:        &slug,
		Description: &desc,
		SetParent:   true,
		ParentID:    parent,
	}
	_, err = h.svc.Update(r.Context(), u.ID, id, in)
	if errors.Is(err, categories.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if errors.Is(err, categories.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		cat, _ := h.svc.Get(r.Context(), u.ID, id)
		view := h.formView(r, cat)
		view.Name = name
		view.Slug = slug
		view.Description = desc
		view.Error = taxonomyHumanError(err)
		if errors.Is(err, categories.ErrNameRequired) {
			view.FieldErrors["name"] = "Name is required."
		}
		h.render(w, r, webtempl.CategoryEditor(view))
		return
	}
	http.Redirect(w, r, "/admin/categories/"+id.String()+"/edit", http.StatusSeeOther)
}

// Delete hard-deletes a category then redirects to the list.
func (h *CategoryAdminHandler) Delete(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = h.svc.Delete(r.Context(), u.ID, id)
	if errors.Is(err, categories.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if errors.Is(err, categories.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/categories", http.StatusSeeOther)
}

// Bulk dispatches the delete-only bulk action over the submitted category ids.
func (h *CategoryAdminHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	handleTaxonomyBulk(w, r, h.svc.BulkDelete, "/admin/categories")
}

// --- helpers -----------------------------------------------------------------

func (h *CategoryAdminHandler) formView(r *http.Request, c categories.Category) webtempl.CategoryFormView {
	parentID := ""
	if c.ParentID != nil {
		parentID = c.ParentID.String()
	}
	choices, _ := h.parentChoices(r.Context(), c.ID, parentID)
	return webtempl.CategoryFormView{
		Shell:         h.shell.buildShell(r, "Edit category"),
		IsNew:         false,
		ID:            c.ID.String(),
		Name:          c.Name,
		Slug:          c.Slug,
		Description:   c.Description,
		ParentID:      parentID,
		ParentChoices: choices,
		ActionURL:     "/admin/categories/" + c.ID.String(),
		CSRFToken:     h.csrf(r),
		FieldErrors:   map[string]string{},
		BackURL:       "/admin/categories",
	}
}

// parentChoices builds the indented parent-picker options. selfID and its
// descendants are DISABLED so a parent assignment can never create a cycle (the
// service rejects it too; this keeps the UI honest).
func (h *CategoryAdminHandler) parentChoices(ctx context.Context, selfID uuid.UUID, selectedID string) ([]webtempl.ParentOption, error) {
	nodes, err := h.svc.Tree(ctx)
	if err != nil {
		return nil, err
	}
	descendants := taxonomyDescendants(nodes, selfID)
	opts := make([]webtempl.ParentOption, 0, len(nodes))
	for _, n := range nodes { //nolint:varnamelen
		idStr := n.Category.ID.String()
		disabled := n.Category.ID == selfID || descendants[n.Category.ID]
		opts = append(opts, webtempl.ParentOption{
			ID:       idStr,
			Label:    indentLabel(n.Depth) + n.Category.Name,
			Selected: idStr == selectedID,
			Disabled: disabled,
		})
	}
	return opts, nil
}

func (h *CategoryAdminHandler) renderFormError(w http.ResponseWriter, r *http.Request, view webtempl.CategoryFormView, selfID uuid.UUID, err error) {
	if errors.Is(err, categories.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	view.ParentChoices, _ = h.parentChoices(r.Context(), selfID, view.ParentID)
	view.Error = taxonomyHumanError(err)
	if errors.Is(err, categories.ErrNameRequired) {
		view.FieldErrors["name"] = "Name is required."
	}
	h.render(w, r, webtempl.CategoryEditor(view))
}

func (h *CategoryAdminHandler) render(w http.ResponseWriter, r *http.Request, c webtempl.Component) {
	if err := render.Component(r.Context(), w, http.StatusOK, c); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// taxonomyDescendants returns the set of category ids that descend from rootID,
// derived from the depth-ordered tree (a pre-order traversal: descendants are
// the contiguous run after root with greater depth). When rootID is Nil (create
// path) the set is empty — nothing to disable.
func taxonomyDescendants(nodes []categories.TreeNode, rootID uuid.UUID) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	if rootID == uuid.Nil {
		return out
	}
	in := false
	rootDepth := 0
	for _, n := range nodes {
		if n.Category.ID == rootID {
			in = true
			rootDepth = n.Depth
			continue
		}
		if in {
			if n.Depth > rootDepth {
				out[n.Category.ID] = true
			} else {
				break
			}
		}
	}
	return out
}

// indentLabel renders an em-space indent for a parent-picker option.
func indentLabel(depth int) string {
	out := ""
	for i := 0; i < depth; i++ {
		out += "— " // em dash + en space
	}
	return out
}

func parseParentID(raw string) *uuid.UUID {
	if raw == "" {
		return nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	return &id
}
