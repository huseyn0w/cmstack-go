package web

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/content/categories"
	"github.com/huseyn0w/agentic-cms-go/internal/content/menus"
	"github.com/huseyn0w/agentic-cms-go/internal/content/pages"
	"github.com/huseyn0w/agentic-cms-go/internal/content/posts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/i18n"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

// MenuAdminService is the subset of *menus.Service the admin menu builder calls.
// Declaring it here keeps web decoupled from the menus package and trivially
// fakeable in tests.
type MenuAdminService interface {
	Menus(ctx context.Context, actorID uuid.UUID) ([]menus.Menu, error)
	CreateMenu(ctx context.Context, actorID uuid.UUID, name, location string) (menus.Menu, error)
	GetMenu(ctx context.Context, actorID, id uuid.UUID) (menus.Menu, []menus.Item, error)
	RenameMenu(ctx context.Context, actorID, id uuid.UUID, name string) (menus.Menu, error)
	AssignLocation(ctx context.Context, actorID, id uuid.UUID, location string) (menus.Menu, error)
	DeleteMenu(ctx context.Context, actorID, id uuid.UUID) error
	AddItem(ctx context.Context, actorID, menuID uuid.UUID, in menus.ItemInput) (menus.Item, error)
	UpdateItem(ctx context.Context, actorID, itemID uuid.UUID, in menus.ItemInput) (menus.Item, error)
	DeleteItem(ctx context.Context, actorID, itemID uuid.UUID) error
	Reorder(ctx context.Context, actorID, menuID uuid.UUID, orderedIDs []uuid.UUID) error
	SaveItemTranslation(ctx context.Context, actorID, itemID uuid.UUID, locale i18n.Locale, label string) error
}

// menuPostLister is the narrow reader the item picker uses to resolve posts:
// list them for the select, and (post→slug/title) for the add-item resolution.
// *posts.Service satisfies it via PublicList.
type menuPostLister interface {
	PublicList(ctx context.Context, limit, offset int) ([]posts.Post, int, error)
}

// menuPageLister is the narrow reader the item picker uses for pages.
// *pages.Service satisfies it via AdminList.
type menuPageLister interface {
	AdminList(ctx context.Context, f pages.ListFilter) ([]pages.Page, int, error)
}

// menuCategoryLister is the narrow reader the item picker uses for categories.
// *categories.Service satisfies it via AllFlat.
type menuCategoryLister interface {
	AllFlat(ctx context.Context) ([]categories.Category, error)
}

// menuPickerLimit caps how many posts/pages the item picker lists. The picker is
// a convenience selector, not a paginated browser.
const menuPickerLimit = 500

// MenuAdminHandler is the thin HTTP boundary for the admin menu builder. It owns
// no data access: menu CRUD is the menus service; the content pickers read
// through the narrow listers. For internal item types the handler resolves the
// chosen content into a rooted URL + default label BEFORE calling AddItem (the
// service does not load referenced content).
type MenuAdminHandler struct {
	svc   MenuAdminService
	posts menuPostLister
	pages menuPageLister
	cats  menuCategoryLister
	shell adminShellDeps
	csrf  func(*http.Request) string
}

// NewMenuAdminHandler constructs the admin menu builder handler.
func NewMenuAdminHandler(svc MenuAdminService, postSvc menuPostLister, pageSvc menuPageLister, catSvc menuCategoryLister, shell adminShellDeps, csrf func(*http.Request) string) *MenuAdminHandler {
	return &MenuAdminHandler{svc: svc, posts: postSvc, pages: pageSvc, cats: catSvc, shell: shell, csrf: csrf}
}

// List renders the menus index (all menus + the new-menu form).
func (h *MenuAdminHandler) List(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	all, err := h.svc.Menus(r.Context(), u.ID)
	if errors.Is(err, menus.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, webtempl.MenusListPage(h.listView(r, all, "", "")))
}

// Create handles the new-menu POST, then redirects to the editor. A location
// collision surfaces as a friendly inline error on the re-rendered list.
func (h *MenuAdminHandler) Create(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	_ = r.ParseForm()
	name := r.PostFormValue("name")
	location := r.PostFormValue("location")

	m, err := h.svc.CreateMenu(r.Context(), u.ID, name, location)
	if errors.Is(err, menus.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		all, _ := h.svc.Menus(r.Context(), u.ID)
		h.render(w, r, webtempl.MenusListPage(h.listView(r, all, name, menuHumanError(err))))
		return
	}
	http.Redirect(w, r, "/admin/menus/"+m.ID.String(), http.StatusSeeOther)
}

// Edit renders the menu editor for an existing menu.
func (h *MenuAdminHandler) Edit(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	m, items, err := h.svc.GetMenu(r.Context(), u.ID, id)
	if errors.Is(err, menus.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, menus.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, webtempl.MenuEditorPage(h.editorView(r, m, items, "")))
}

// UpdateSettings handles the menu settings POST: it renames the menu and assigns
// its location (both in one submit). A location collision surfaces inline.
func (h *MenuAdminHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	name := r.PostFormValue("name")
	location := r.PostFormValue("location")

	if _, err = h.svc.RenameMenu(r.Context(), u.ID, id, name); err != nil {
		h.editorError(w, r, u.ID, id, err)
		return
	}
	if _, err = h.svc.AssignLocation(r.Context(), u.ID, id, location); err != nil {
		h.editorError(w, r, u.ID, id, err)
		return
	}
	http.Redirect(w, r, "/admin/menus/"+id.String(), http.StatusSeeOther)
}

// Delete removes the whole menu, then redirects to the list.
func (h *MenuAdminHandler) Delete(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = h.svc.DeleteMenu(r.Context(), u.ID, id)
	if errors.Is(err, menus.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if errors.Is(err, menus.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/menus", http.StatusSeeOther)
}

// AddItem resolves the chosen content (for internal types) into a rooted URL +
// default label, then appends it to the menu. For custom items it uses the
// entered URL + label directly.
func (h *MenuAdminHandler) AddItem(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	menuID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()

	in, err := h.resolveItemInput(r)
	if err != nil {
		h.editorError(w, r, u.ID, menuID, err)
		return
	}

	if _, err = h.svc.AddItem(r.Context(), u.ID, menuID, in); err != nil {
		h.editorError(w, r, u.ID, menuID, err)
		return
	}
	http.Redirect(w, r, "/admin/menus/"+menuID.String(), http.StatusSeeOther)
}

// MoveItem swaps an item with its up/down neighbour and persists the new full
// order via Reorder. The up/down buttons are the reliable, tested reorder path.
func (h *MenuAdminHandler) MoveItem(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	menuID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	itemID, err := uuid.Parse(chi.URLParam(r, "itemID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	dir := r.PostFormValue("dir")

	_, items, err := h.svc.GetMenu(r.Context(), u.ID, menuID)
	if errors.Is(err, menus.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	ordered := swapNeighbour(items, itemID, dir)
	if err = h.svc.Reorder(r.Context(), u.ID, menuID, ordered); err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/menus/"+menuID.String(), http.StatusSeeOther)
}

// DeleteItemHandler removes a single item from the menu, then redirects back to
// the editor.
func (h *MenuAdminHandler) DeleteItemHandler(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	menuID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	itemID, err := uuid.Parse(chi.URLParam(r, "itemID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	err = h.svc.DeleteItem(r.Context(), u.ID, itemID)
	if errors.Is(err, menus.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil && !errors.Is(err, menus.ErrNotFound) {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/menus/"+menuID.String(), http.StatusSeeOther)
}

// EditItem renders the per-item editor (base label + per-locale labels + custom
// URL). Per-locale inputs are left blank when no label read is available.
func (h *MenuAdminHandler) EditItem(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	menuID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	itemID, err := uuid.Parse(chi.URLParam(r, "itemID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, items, err := h.svc.GetMenu(r.Context(), u.ID, menuID)
	if errors.Is(err, menus.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, menus.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	item, ok := findItem(items, itemID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h.render(w, r, webtempl.MenuItemEditPage(h.itemEditView(r, menuID, item, "")))
}

// UpdateItem writes the base (en) label (+ URL for custom items) and upserts each
// non-empty non-default-locale label, then redirects back to the editor.
func (h *MenuAdminHandler) UpdateItem(w http.ResponseWriter, r *http.Request) {
	u, _ := UserFromContext(r.Context())
	menuID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	itemID, err := uuid.Parse(chi.URLParam(r, "itemID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, items, err := h.svc.GetMenu(r.Context(), u.ID, menuID)
	if errors.Is(err, menus.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	item, ok := findItem(items, itemID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	_ = r.ParseForm()
	label := r.PostFormValue("label")

	// Structural fields (type/ref/parent) are unchanged; only the label and — for
	// custom items — the URL are editable here.
	url := item.URL
	if item.Type == menus.ItemCustom {
		url = r.PostFormValue("url")
	}
	in := menus.ItemInput{
		ParentID: item.ParentID,
		Type:     item.Type,
		RefID:    item.RefID,
		URL:      url,
		Label:    label,
	}
	if _, err = h.svc.UpdateItem(r.Context(), u.ID, itemID, in); err != nil {
		view := h.itemEditView(r, menuID, item, menuHumanError(err))
		view.Label = label
		view.URL = url
		h.render(w, r, webtempl.MenuItemEditPage(view))
		return
	}

	// Per-locale labels: upsert each non-default locale that has a non-empty value.
	for _, loc := range i18n.All() {
		if loc.IsDefault() {
			continue
		}
		val := strings.TrimSpace(r.PostFormValue("label_" + loc.String()))
		if val == "" {
			continue
		}
		if err = h.svc.SaveItemTranslation(r.Context(), u.ID, itemID, loc, val); err != nil {
			view := h.itemEditView(r, menuID, item, menuHumanError(err))
			view.Label = label
			h.render(w, r, webtempl.MenuItemEditPage(view))
			return
		}
	}
	http.Redirect(w, r, "/admin/menus/"+menuID.String(), http.StatusSeeOther)
}

// --- helpers -----------------------------------------------------------------

// resolveItemInput builds the ItemInput from the add-item form. For internal
// types it looks up the chosen content by id to derive the rooted URL + default
// label (a blank label override wins nothing; a provided one overrides the
// title). Custom items use the entered URL + label directly.
func (h *MenuAdminHandler) resolveItemInput(r *http.Request) (menus.ItemInput, error) {
	itemType := menus.ItemType(r.PostFormValue("type"))
	override := strings.TrimSpace(r.PostFormValue("label"))

	switch itemType {
	case menus.ItemCustom:
		return menus.ItemInput{
			Type:  menus.ItemCustom,
			URL:   r.PostFormValue("url"),
			Label: r.PostFormValue("custom_label"),
		}, nil
	case menus.ItemPost:
		return h.resolvePost(r.Context(), r.PostFormValue("ref_post"), override)
	case menus.ItemPage:
		return h.resolvePage(r.Context(), r.PostFormValue("ref_page"), override)
	case menus.ItemCategory:
		return h.resolveCategory(r.Context(), r.PostFormValue("ref_category"), override)
	default:
		return menus.ItemInput{}, menus.ErrInvalidType
	}
}

func (h *MenuAdminHandler) resolvePost(ctx context.Context, rawID, override string) (menus.ItemInput, error) {
	id, err := uuid.Parse(rawID)
	if err != nil {
		return menus.ItemInput{}, menus.ErrInvalidType
	}
	list, _, err := h.posts.PublicList(ctx, menuPickerLimit, 0)
	if err != nil {
		return menus.ItemInput{}, err
	}
	for _, p := range list {
		if p.ID == id {
			return menus.ItemInput{
				Type:  menus.ItemPost,
				RefID: &id,
				URL:   "/blog/" + p.Slug,
				Label: pickLabel(override, p.Title),
			}, nil
		}
	}
	return menus.ItemInput{}, menus.ErrInvalidType
}

func (h *MenuAdminHandler) resolvePage(ctx context.Context, rawID, override string) (menus.ItemInput, error) {
	id, err := uuid.Parse(rawID)
	if err != nil {
		return menus.ItemInput{}, menus.ErrInvalidType
	}
	list, _, err := h.pages.AdminList(ctx, pages.ListFilter{Limit: menuPickerLimit})
	if err != nil {
		return menus.ItemInput{}, err
	}
	for _, p := range list {
		if p.ID == id {
			return menus.ItemInput{
				Type:  menus.ItemPage,
				RefID: &id,
				URL:   "/p/" + p.Slug,
				Label: pickLabel(override, p.Title),
			}, nil
		}
	}
	return menus.ItemInput{}, menus.ErrInvalidType
}

func (h *MenuAdminHandler) resolveCategory(ctx context.Context, rawID, override string) (menus.ItemInput, error) {
	id, err := uuid.Parse(rawID)
	if err != nil {
		return menus.ItemInput{}, menus.ErrInvalidType
	}
	list, err := h.cats.AllFlat(ctx)
	if err != nil {
		return menus.ItemInput{}, err
	}
	for _, c := range list {
		if c.ID == id {
			return menus.ItemInput{
				Type:  menus.ItemCategory,
				RefID: &id,
				URL:   "/categories/" + c.Slug,
				Label: pickLabel(override, c.Name),
			}, nil
		}
	}
	return menus.ItemInput{}, menus.ErrInvalidType
}

func (h *MenuAdminHandler) listView(r *http.Request, all []menus.Menu, newName, errMsg string) webtempl.MenusListView {
	rows := make([]webtempl.MenuRow, 0, len(all))
	for _, m := range all {
		rows = append(rows, webtempl.MenuRow{
			ID:        m.ID.String(),
			Name:      m.Name,
			Location:  m.Location,
			ItemCount: h.itemCount(r, m.ID),
			EditURL:   "/admin/menus/" + m.ID.String(),
		})
	}
	return webtempl.MenusListView{
		Shell:     h.shell.buildShell(r, "Menus"),
		Rows:      rows,
		CreateURL: "/admin/menus",
		Locations: locationOptions(""),
		NewName:   newName,
		Error:     errMsg,
		CSRFToken: h.csrf(r),
	}
}

// itemCount returns a menu's item count for the list. Best-effort: a read error
// yields 0 rather than failing the whole list render.
func (h *MenuAdminHandler) itemCount(r *http.Request, id uuid.UUID) int {
	u, _ := UserFromContext(r.Context())
	if _, items, err := h.svc.GetMenu(r.Context(), u.ID, id); err == nil {
		return len(items)
	}
	return 0
}

func (h *MenuAdminHandler) editorView(r *http.Request, m menus.Menu, items []menus.Item, errMsg string) webtempl.MenuEditorView {
	rows := make([]webtempl.MenuItemRow, 0, len(items))
	for i, it := range items {
		rows = append(rows, webtempl.MenuItemRow{
			ID:        it.ID.String(),
			Label:     it.Label,
			URL:       it.URL,
			Type:      it.Type.String(),
			EditURL:   "/admin/menus/" + m.ID.String() + "/items/" + it.ID.String() + "/edit",
			MoveURL:   "/admin/menus/" + m.ID.String() + "/items/" + it.ID.String() + "/move",
			DeleteURL: "/admin/menus/" + m.ID.String() + "/items/" + it.ID.String() + "/delete",
			IsFirst:   i == 0,
			IsLast:    i == len(items)-1,
		})
	}
	return webtempl.MenuEditorView{
		Shell:       h.shell.buildShell(r, "Edit menu"),
		ID:          m.ID.String(),
		Name:        m.Name,
		Location:    m.Location,
		Locations:   locationOptions(m.Location),
		Items:       rows,
		Posts:       h.postChoices(r.Context()),
		Pages:       h.pageChoices(r.Context()),
		Categories:  h.categoryChoices(r.Context()),
		SettingsURL: "/admin/menus/" + m.ID.String(),
		DeleteURL:   "/admin/menus/" + m.ID.String() + "/delete",
		AddItemURL:  "/admin/menus/" + m.ID.String() + "/items",
		Error:       errMsg,
		CSRFToken:   h.csrf(r),
	}
}

func (h *MenuAdminHandler) itemEditView(r *http.Request, menuID uuid.UUID, item menus.Item, errMsg string) webtempl.MenuItemEditView {
	inputs := make([]webtempl.MenuItemLabelInput, 0)
	for _, loc := range i18n.All() {
		if loc.IsDefault() {
			continue
		}
		inputs = append(inputs, webtempl.MenuItemLabelInput{
			Locale: loc.String(),
			Label:  localeDisplayName(loc),
			Value:  "", // no per-locale label read is exposed by the service; left blank.
		})
	}
	return webtempl.MenuItemEditView{
		Shell:        h.shell.buildShell(r, "Edit item"),
		MenuID:       menuID.String(),
		ItemID:       item.ID.String(),
		Label:        item.Label,
		URL:          item.URL,
		IsCustom:     item.Type == menus.ItemCustom,
		LocaleInputs: inputs,
		ActionURL:    "/admin/menus/" + menuID.String() + "/items/" + item.ID.String() + "/edit",
		BackURL:      "/admin/menus/" + menuID.String(),
		Error:        errMsg,
		CSRFToken:    h.csrf(r),
	}
}

func (h *MenuAdminHandler) postChoices(ctx context.Context) []webtempl.MenuContentChoice {
	if h.posts == nil {
		return nil
	}
	list, _, err := h.posts.PublicList(ctx, menuPickerLimit, 0)
	if err != nil {
		return nil
	}
	out := make([]webtempl.MenuContentChoice, 0, len(list))
	for _, p := range list {
		out = append(out, webtempl.MenuContentChoice{Value: p.ID.String(), Label: p.Title})
	}
	return out
}

func (h *MenuAdminHandler) pageChoices(ctx context.Context) []webtempl.MenuContentChoice {
	if h.pages == nil {
		return nil
	}
	list, _, err := h.pages.AdminList(ctx, pages.ListFilter{Limit: menuPickerLimit})
	if err != nil {
		return nil
	}
	out := make([]webtempl.MenuContentChoice, 0, len(list))
	for _, p := range list {
		out = append(out, webtempl.MenuContentChoice{Value: p.ID.String(), Label: p.Title})
	}
	return out
}

func (h *MenuAdminHandler) categoryChoices(ctx context.Context) []webtempl.MenuContentChoice {
	if h.cats == nil {
		return nil
	}
	list, err := h.cats.AllFlat(ctx)
	if err != nil {
		return nil
	}
	out := make([]webtempl.MenuContentChoice, 0, len(list))
	for _, c := range list {
		out = append(out, webtempl.MenuContentChoice{Value: c.ID.String(), Label: c.Name})
	}
	return out
}

// editorError re-renders the editor with a friendly inline error. A forbidden or
// not-found error short-circuits to the matching HTTP status instead.
func (h *MenuAdminHandler) editorError(w http.ResponseWriter, r *http.Request, actorID, menuID uuid.UUID, cause error) {
	if errors.Is(cause, menus.ErrForbidden) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if errors.Is(cause, menus.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	m, items, err := h.svc.GetMenu(r.Context(), actorID, menuID)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	h.render(w, r, webtempl.MenuEditorPage(h.editorView(r, m, items, menuHumanError(cause))))
}

func (h *MenuAdminHandler) render(w http.ResponseWriter, r *http.Request, c webtempl.Component) {
	if err := render.Component(r.Context(), w, http.StatusOK, c); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// swapNeighbour returns the full ordered id list with the item at itemID swapped
// with its up (previous) or down (next) neighbour. When the move is a no-op (item
// missing, or already at the relevant end) the original order is returned.
func swapNeighbour(items []menus.Item, itemID uuid.UUID, dir string) []uuid.UUID {
	ids := make([]uuid.UUID, len(items))
	idx := -1
	for i, it := range items {
		ids[i] = it.ID
		if it.ID == itemID {
			idx = i
		}
	}
	if idx == -1 {
		return ids
	}
	switch dir {
	case "up":
		if idx > 0 {
			ids[idx-1], ids[idx] = ids[idx], ids[idx-1]
		}
	case "down":
		if idx < len(ids)-1 {
			ids[idx+1], ids[idx] = ids[idx], ids[idx+1]
		}
	}
	return ids
}

func findItem(items []menus.Item, id uuid.UUID) (menus.Item, bool) {
	for _, it := range items {
		if it.ID == id {
			return it, true
		}
	}
	return menus.Item{}, false
}

// pickLabel returns the trimmed override when non-empty, else the content title.
func pickLabel(override, title string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	return title
}

// locationOptions builds the shared none/header/footer <select> options with the
// current value selected.
func locationOptions(current string) []webtempl.LocationOption {
	specs := []struct{ value, label string }{
		{"", "— None (unassigned) —"},
		{"header", "Header"},
		{"footer", "Footer"},
	}
	out := make([]webtempl.LocationOption, 0, len(specs))
	for _, s := range specs {
		out = append(out, webtempl.LocationOption{
			Value:    s.value,
			Label:    s.label,
			Selected: s.value == current,
		})
	}
	return out
}

// localeDisplayName returns the human display name for a locale, falling back to
// its code. It reuses the shared localeDisplayNames map used by the editors.
func localeDisplayName(loc i18n.Locale) string {
	if name := localeDisplayNames[loc]; name != "" {
		return name
	}
	return loc.String()
}

// menuHumanError maps a menus domain/repo error to a friendly inline message.
func menuHumanError(err error) string {
	switch {
	case errors.Is(err, menus.ErrLocationTaken):
		return "That location already has a menu."
	case errors.Is(err, menus.ErrNameRequired):
		return "Name is required."
	case errors.Is(err, menus.ErrInvalidType):
		return "Please choose a valid item to add."
	case errors.Is(err, menus.ErrUnsupportedLocale):
		return "That language is not supported."
	default:
		return "Something went wrong. Please try again."
	}
}
