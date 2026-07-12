package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/content/categories"
	"github.com/huseyn0w/agentic-cms-go/internal/content/menus"
	"github.com/huseyn0w/agentic-cms-go/internal/content/pages"
	"github.com/huseyn0w/agentic-cms-go/internal/content/posts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/i18n"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/security"
)

// --- fakes -------------------------------------------------------------------

// fakeMenuSvc is a spy over the menu admin surface. It records the last add /
// update / reorder / translation calls so tests can assert the resolved payloads.
type fakeMenuSvc struct {
	menus []menus.Menu
	items map[uuid.UUID][]menus.Item

	createErr error

	addCalls     []menus.ItemInput
	addMenuID    uuid.UUID
	updateCalls  []menus.ItemInput
	updateItemID uuid.UUID
	reorderIDs   []uuid.UUID
	transCalls   []fakeTransCall
}

type fakeTransCall struct {
	itemID uuid.UUID
	locale i18n.Locale
	label  string
}

func (f *fakeMenuSvc) Menus(context.Context, uuid.UUID) ([]menus.Menu, error) {
	return f.menus, nil
}

func (f *fakeMenuSvc) CreateMenu(_ context.Context, _ uuid.UUID, name, location string) (menus.Menu, error) {
	if f.createErr != nil {
		return menus.Menu{}, f.createErr
	}
	m := menus.Menu{ID: uuid.New(), Name: name, Location: location}
	f.menus = append(f.menus, m)
	return m, nil
}

func (f *fakeMenuSvc) GetMenu(_ context.Context, _, id uuid.UUID) (menus.Menu, []menus.Item, error) {
	for _, m := range f.menus {
		if m.ID == id {
			return m, f.items[id], nil
		}
	}
	return menus.Menu{}, nil, menus.ErrNotFound
}

func (f *fakeMenuSvc) RenameMenu(_ context.Context, _, _ uuid.UUID, _ string) (menus.Menu, error) {
	return menus.Menu{}, nil
}

func (f *fakeMenuSvc) AssignLocation(_ context.Context, _, _ uuid.UUID, _ string) (menus.Menu, error) {
	return menus.Menu{}, nil
}

func (f *fakeMenuSvc) DeleteMenu(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (f *fakeMenuSvc) AddItem(_ context.Context, _, menuID uuid.UUID, in menus.ItemInput) (menus.Item, error) {
	f.addCalls = append(f.addCalls, in)
	f.addMenuID = menuID
	return menus.Item{ID: uuid.New()}, nil
}

func (f *fakeMenuSvc) UpdateItem(_ context.Context, _, itemID uuid.UUID, in menus.ItemInput) (menus.Item, error) {
	f.updateCalls = append(f.updateCalls, in)
	f.updateItemID = itemID
	return menus.Item{ID: itemID}, nil
}

func (f *fakeMenuSvc) DeleteItem(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (f *fakeMenuSvc) Reorder(_ context.Context, _, _ uuid.UUID, orderedIDs []uuid.UUID) error {
	f.reorderIDs = orderedIDs
	return nil
}

func (f *fakeMenuSvc) SaveItemTranslation(_ context.Context, _, itemID uuid.UUID, locale i18n.Locale, label string) error {
	f.transCalls = append(f.transCalls, fakeTransCall{itemID: itemID, locale: locale, label: label})
	return nil
}

// content listers.
type fakeMenuPosts struct{ list []posts.Post }

func (f fakeMenuPosts) PublicList(context.Context, int, int) ([]posts.Post, int, error) {
	return f.list, len(f.list), nil
}

type fakeMenuPages struct{ list []pages.Page }

func (f fakeMenuPages) AdminList(context.Context, pages.ListFilter) ([]pages.Page, int, error) {
	return f.list, len(f.list), nil
}

type fakeMenuCats struct{ list []categories.Category }

func (f fakeMenuCats) AllFlat(context.Context) ([]categories.Category, error) {
	return f.list, nil
}

func menuShell() adminShellDeps {
	return adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
}

func newMenuHandler(svc *fakeMenuSvc, p fakeMenuPosts, pg fakeMenuPages, c fakeMenuCats) *MenuAdminHandler {
	return NewMenuAdminHandler(svc, p, pg, c, menuShell(), security.Token)
}

func menuReq(method, target string, form url.Values) *http.Request {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return req.WithContext(withUser(context.Background(), accounts.User{ID: uuid.New()}))
}

// chiReq attaches URL params so chi.URLParam resolves inside the handler.
func chiReq(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// --- tests -------------------------------------------------------------------

func TestMenus_ListRendersMenus(t *testing.T) {
	m := menus.Menu{ID: uuid.New(), Name: "Main", Location: "header"}
	svc := &fakeMenuSvc{menus: []menus.Menu{m}, items: map[uuid.UUID][]menus.Item{m.ID: {{ID: uuid.New()}}}}
	h := newMenuHandler(svc, fakeMenuPosts{}, fakeMenuPages{}, fakeMenuCats{})

	rec := httptest.NewRecorder()
	h.List(rec, menuReq(http.MethodGet, "/admin/menus", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("List = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "menu-row-"+m.ID.String()) {
		t.Error("list missing the seeded menu row")
	}
	if !strings.Contains(body, "menu-create-form") {
		t.Error("list missing the new-menu form")
	}
}

func TestMenus_CreateRedirectsToEditor(t *testing.T) {
	svc := &fakeMenuSvc{}
	h := newMenuHandler(svc, fakeMenuPosts{}, fakeMenuPages{}, fakeMenuCats{})

	form := url.Values{"name": {"Footer nav"}, "location": {"footer"}}
	rec := httptest.NewRecorder()
	h.Create(rec, menuReq(http.MethodPost, "/admin/menus", form))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("Create = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/admin/menus/") || loc == "/admin/menus/" {
		t.Fatalf("Create redirect = %q, want /admin/menus/{id}", loc)
	}
}

func TestMenus_CreateLocationTakenSurfacesInlineError(t *testing.T) {
	svc := &fakeMenuSvc{createErr: menus.ErrLocationTaken}
	h := newMenuHandler(svc, fakeMenuPosts{}, fakeMenuPages{}, fakeMenuCats{})

	form := url.Values{"name": {"Dup"}, "location": {"header"}}
	rec := httptest.NewRecorder()
	h.Create(rec, menuReq(http.MethodPost, "/admin/menus", form))

	if rec.Code != http.StatusOK {
		t.Fatalf("Create(taken) = %d, want 200 (re-render)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "menus-error") || !strings.Contains(body, "That location already has a menu") {
		t.Error("location-taken did not surface a friendly inline error")
	}
}

func TestMenus_EditorRendersItemsInOrderWithMoveControls(t *testing.T) {
	m := menus.Menu{ID: uuid.New(), Name: "Main"}
	i1 := menus.Item{ID: uuid.New(), Label: "First", URL: "/blog/a", Type: menus.ItemPost}
	i2 := menus.Item{ID: uuid.New(), Label: "Second", URL: "/p/b", Type: menus.ItemPage}
	svc := &fakeMenuSvc{menus: []menus.Menu{m}, items: map[uuid.UUID][]menus.Item{m.ID: {i1, i2}}}
	h := newMenuHandler(svc, fakeMenuPosts{}, fakeMenuPages{}, fakeMenuCats{})

	req := chiReq(menuReq(http.MethodGet, "/admin/menus/"+m.ID.String(), nil), map[string]string{"id": m.ID.String()})
	rec := httptest.NewRecorder()
	h.Edit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Edit = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "menu-item-"+i1.ID.String()) || !strings.Contains(body, "menu-item-"+i2.ID.String()) {
		t.Error("editor missing item rows")
	}
	// First row's up button is disabled; down present. Order: First appears before Second.
	if strings.Index(body, "menu-item-"+i1.ID.String()) > strings.Index(body, "menu-item-"+i2.ID.String()) {
		t.Error("items rendered out of order")
	}
	if !strings.Contains(body, "item-move-up-"+i1.ID.String()) || !strings.Contains(body, "item-move-down-"+i1.ID.String()) {
		t.Error("editor missing move up/down controls")
	}
}

func TestMenus_AddPostItemResolvesSlugToURL(t *testing.T) {
	m := menus.Menu{ID: uuid.New(), Name: "Main"}
	p := posts.Post{ID: uuid.New(), Title: "Hello World", Slug: "hello-world"}
	svc := &fakeMenuSvc{menus: []menus.Menu{m}, items: map[uuid.UUID][]menus.Item{m.ID: {}}}
	h := newMenuHandler(svc, fakeMenuPosts{list: []posts.Post{p}}, fakeMenuPages{}, fakeMenuCats{})

	form := url.Values{"type": {"post"}, "ref_post": {p.ID.String()}, "label": {""}}
	req := chiReq(menuReq(http.MethodPost, "/admin/menus/"+m.ID.String()+"/items", form), map[string]string{"id": m.ID.String()})
	rec := httptest.NewRecorder()
	h.AddItem(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("AddItem = %d, want 303", rec.Code)
	}
	if len(svc.addCalls) != 1 {
		t.Fatalf("AddItem calls = %d, want 1", len(svc.addCalls))
	}
	got := svc.addCalls[0]
	if got.Type != menus.ItemPost {
		t.Errorf("type = %q, want post", got.Type)
	}
	if got.URL != "/blog/hello-world" {
		t.Errorf("resolved URL = %q, want /blog/hello-world", got.URL)
	}
	if got.Label != "Hello World" {
		t.Errorf("default label = %q, want the post title", got.Label)
	}
	if got.RefID == nil || *got.RefID != p.ID {
		t.Errorf("RefID = %v, want %v", got.RefID, p.ID)
	}
}

func TestMenus_MoveUpReordersWithSwappedOrder(t *testing.T) {
	m := menus.Menu{ID: uuid.New(), Name: "Main"}
	i1 := menus.Item{ID: uuid.New(), Label: "First"}
	i2 := menus.Item{ID: uuid.New(), Label: "Second"}
	svc := &fakeMenuSvc{menus: []menus.Menu{m}, items: map[uuid.UUID][]menus.Item{m.ID: {i1, i2}}}
	h := newMenuHandler(svc, fakeMenuPosts{}, fakeMenuPages{}, fakeMenuCats{})

	form := url.Values{"dir": {"up"}}
	req := chiReq(menuReq(http.MethodPost, "/admin/menus/"+m.ID.String()+"/items/"+i2.ID.String()+"/move", form),
		map[string]string{"id": m.ID.String(), "itemID": i2.ID.String()})
	rec := httptest.NewRecorder()
	h.MoveItem(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("MoveItem = %d, want 303", rec.Code)
	}
	want := []uuid.UUID{i2.ID, i1.ID}
	if len(svc.reorderIDs) != 2 || svc.reorderIDs[0] != want[0] || svc.reorderIDs[1] != want[1] {
		t.Errorf("Reorder order = %v, want %v", svc.reorderIDs, want)
	}
}

func TestMenus_UpdateItemSavesBaseLabelAndDeTranslation(t *testing.T) {
	m := menus.Menu{ID: uuid.New(), Name: "Main"}
	it := menus.Item{ID: uuid.New(), Label: "Old", Type: menus.ItemPost}
	svc := &fakeMenuSvc{menus: []menus.Menu{m}, items: map[uuid.UUID][]menus.Item{m.ID: {it}}}
	h := newMenuHandler(svc, fakeMenuPosts{}, fakeMenuPages{}, fakeMenuCats{})

	form := url.Values{"label": {"Home"}, "label_de": {"Startseite"}}
	req := chiReq(menuReq(http.MethodPost, "/admin/menus/"+m.ID.String()+"/items/"+it.ID.String()+"/edit", form),
		map[string]string{"id": m.ID.String(), "itemID": it.ID.String()})
	rec := httptest.NewRecorder()
	h.UpdateItem(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("UpdateItem = %d, want 303", rec.Code)
	}
	if len(svc.updateCalls) != 1 || svc.updateCalls[0].Label != "Home" {
		t.Fatalf("UpdateItem base label = %v, want Home", svc.updateCalls)
	}
	found := false
	for _, c := range svc.transCalls {
		if c.locale == i18n.LocaleDE && c.label == "Startseite" && c.itemID == it.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("de translation not saved: %+v", svc.transCalls)
	}
}
