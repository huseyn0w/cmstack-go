package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/content/categories"
	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/i18n"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
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

	// Per-locale content overlay (M7b-3): GetInLocale loads the editor content
	// overlaid by the active tab's locale (base fallback); TranslatedLocales marks
	// which tabs already have a translation; SaveTranslation upserts a de/ru tab's
	// name/description without touching the shared structural fields.
	GetInLocale(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale) (categories.Category, error)
	TranslatedLocales(ctx context.Context, actorID, id uuid.UUID) ([]i18n.Locale, error)
	SaveTranslation(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale, in categories.TranslationInput) error
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
	// The active editor tab is chosen by ?language=xx (django-parler parity); it
	// defaults to en (the base row). Content is loaded overlaid by that locale.
	locale := editorLocale(r)
	cat, err := h.svc.GetInLocale(r.Context(), u.ID, id, locale)
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
	h.render(w, r, webtempl.CategoryEditor(h.formView(r, cat, locale)))
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

	// Per-locale save (M7b-3): a non-default `locale` field means the editor is on
	// a de/ru translation tab, so the translatable name/description are upserted to
	// the overlay rather than the base row. en (default/empty) takes the base
	// Update path below.
	if loc, ok := i18n.Parse(r.PostFormValue("locale")); ok && !loc.IsDefault() {
		err = h.svc.SaveTranslation(r.Context(), u.ID, id, loc, categories.TranslationInput{
			Name:        name,
			Description: desc,
		})
		if errors.Is(err, categories.ErrForbidden) {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		if errors.Is(err, categories.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			cat, _ := h.svc.GetInLocale(r.Context(), u.ID, id, loc)
			view := h.formView(r, cat, loc)
			view.Name = name
			view.Description = desc
			view.Error = taxonomyHumanError(err)
			if errors.Is(err, categories.ErrNameRequired) {
				view.FieldErrors["name"] = "Name is required."
			}
			h.render(w, r, webtempl.CategoryEditor(view))
			return
		}
		http.Redirect(w, r, "/admin/categories/"+id.String()+"/edit?language="+loc.String(), http.StatusSeeOther)
		return
	}

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
		view := h.formView(r, cat, i18n.Default())
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

func (h *CategoryAdminHandler) formView(r *http.Request, c categories.Category, locale i18n.Locale) webtempl.CategoryFormView {
	parentID := ""
	if c.ParentID != nil {
		parentID = c.ParentID.String()
	}
	choices, _ := h.parentChoices(r.Context(), c.ID, parentID)
	return webtempl.CategoryFormView{
		Shell:           h.shell.buildShell(r, "Edit category"),
		IsNew:           false,
		ID:              c.ID.String(),
		Name:            c.Name,
		Slug:            c.Slug,
		Description:     c.Description,
		ParentID:        parentID,
		ParentChoices:   choices,
		ActionURL:       "/admin/categories/" + c.ID.String(),
		CSRFToken:       h.csrf(r),
		FieldErrors:     map[string]string{},
		BackURL:         "/admin/categories",
		LocaleTabs:      h.localeTabs(r, c.ID, locale),
		ActiveLocale:    locale.String(),
		IsDefaultLocale: locale.IsDefault(),
	}
}

// localeTabs builds the category editor's per-locale tab strip: one tab per
// supported locale, the active one selected, de/ru tabs marked when a translation
// row already exists. Best-effort: a translated-locales read error yields no
// dots. Each tab links to the editor with ?language=xx.
func (h *CategoryAdminHandler) localeTabs(r *http.Request, categoryID uuid.UUID, active i18n.Locale) []webtempl.LocaleTab {
	u, _ := UserFromContext(r.Context())
	has := map[i18n.Locale]bool{}
	if locs, err := h.svc.TranslatedLocales(r.Context(), u.ID, categoryID); err == nil {
		for _, l := range locs {
			has[l] = true
		}
	}
	base := "/admin/categories/" + categoryID.String() + "/edit"
	return buildLocaleTabs(base, active, has)
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

// buildLocaleTabs assembles the editor's per-locale tab strip from a base editor
// URL, the active locale, and the set of locales that already have a translation
// row. The default (en) tab links to the bare base URL; de/ru tabs append
// ?language=xx. Shared by the categories and tags editors (M7b-3).
func buildLocaleTabs(base string, active i18n.Locale, has map[i18n.Locale]bool) []webtempl.LocaleTab {
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
