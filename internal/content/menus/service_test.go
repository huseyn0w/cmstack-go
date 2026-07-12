package menus

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/i18n"
)

// --- fakes -------------------------------------------------------------------

// allowAuthz permits everything (the auth gate is exercised via denyAuthz).
type allowAuthz struct{}

func (allowAuthz) Can(context.Context, uuid.UUID, string, string) bool { return true }

// denyAuthz refuses one specific (action) and allows the rest, to prove the gate.
type denyAuthz struct{ deny string }

func (d denyAuthz) Can(_ context.Context, _ uuid.UUID, action, _ string) bool {
	return action != d.deny
}

// memRepo is an in-memory Repository fake mirroring the pages/service_test style.
type memRepo struct {
	mu           sync.Mutex
	menus        map[uuid.UUID]Menu
	items        map[uuid.UUID]Item
	translations map[uuid.UUID]map[string]string // itemID -> locale -> label
}

func newMemRepo() *memRepo {
	return &memRepo{
		menus:        map[uuid.UUID]Menu{},
		items:        map[uuid.UUID]Item{},
		translations: map[uuid.UUID]map[string]string{},
	}
}

func (m *memRepo) CreateMenu(_ context.Context, name, location string) (Menu, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if location != "" {
		for _, mn := range m.menus {
			if mn.Location == location {
				return Menu{}, ErrLocationTaken
			}
		}
	}
	menu := Menu{ID: uuid.New(), Name: name, Location: location}
	m.menus[menu.ID] = menu
	return menu, nil
}

func (m *memRepo) GetMenu(_ context.Context, id uuid.UUID) (Menu, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	menu, ok := m.menus[id]
	if !ok {
		return Menu{}, ErrNotFound
	}
	return menu, nil
}

func (m *memRepo) ListMenus(_ context.Context) ([]Menu, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Menu, 0, len(m.menus))
	for _, mn := range m.menus {
		out = append(out, mn)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *memRepo) UpdateMenu(_ context.Context, id uuid.UUID, name, location string) (Menu, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	menu, ok := m.menus[id]
	if !ok {
		return Menu{}, ErrNotFound
	}
	if location != "" {
		for oid, mn := range m.menus {
			if oid != id && mn.Location == location {
				return Menu{}, ErrLocationTaken
			}
		}
	}
	menu.Name = name
	menu.Location = location
	m.menus[id] = menu
	return menu, nil
}

func (m *memRepo) DeleteMenu(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.menus[id]; !ok {
		return ErrNotFound
	}
	delete(m.menus, id)
	for iid, it := range m.items {
		if it.MenuID == id {
			delete(m.items, iid)
			delete(m.translations, iid)
		}
	}
	return nil
}

func (m *memRepo) MenuByLocation(_ context.Context, location string) (Menu, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mn := range m.menus {
		if mn.Location != "" && mn.Location == location {
			return mn, nil
		}
	}
	return Menu{}, ErrNotFound
}

func (m *memRepo) AddItem(_ context.Context, in CreateItemData) (Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it := Item{
		ID:       uuid.New(),
		MenuID:   in.MenuID,
		ParentID: in.ParentID,
		Position: in.Position,
		Type:     in.Type,
		RefID:    in.RefID,
		URL:      in.URL,
		Label:    in.Label,
	}
	m.items[it.ID] = it
	return it, nil
}

func (m *memRepo) UpdateItem(_ context.Context, id uuid.UUID, in UpdateItemData) (Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	it.ParentID = in.ParentID
	it.Type = in.Type
	it.RefID = in.RefID
	it.URL = in.URL
	it.Label = in.Label
	m.items[id] = it
	return it, nil
}

func (m *memRepo) DeleteItem(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[id]; !ok {
		return ErrNotFound
	}
	delete(m.items, id)
	delete(m.translations, id)
	return nil
}

func (m *memRepo) listItems(menuID uuid.UUID) []Item {
	out := make([]Item, 0)
	for _, it := range m.items {
		if it.MenuID == menuID {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].ID.String() < out[j].ID.String()
	})
	return out
}

func (m *memRepo) ListItems(_ context.Context, menuID uuid.UUID) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listItems(menuID), nil
}

func (m *memRepo) SetPositions(_ context.Context, _ uuid.UUID, orderedIDs []uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, id := range orderedIDs {
		it, ok := m.items[id]
		if !ok {
			return ErrNotFound
		}
		it.Position = i
		m.items[id] = it
	}
	return nil
}

func (m *memRepo) ListItemsInLocale(_ context.Context, menuID uuid.UUID, locale string) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.listItems(menuID)
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if label := m.translations[it.ID][locale]; label != "" {
			it.Label = label
		}
		out = append(out, it)
	}
	return out, nil
}

func (m *memRepo) UpsertItemTranslation(_ context.Context, itemID uuid.UUID, locale, label string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.translations[itemID] == nil {
		m.translations[itemID] = map[string]string{}
	}
	m.translations[itemID][locale] = label
	return nil
}

func (m *memRepo) ListItemTranslations(_ context.Context, itemID uuid.UUID) ([]ItemTranslation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ItemTranslation, 0, len(m.translations[itemID]))
	for loc, label := range m.translations[itemID] {
		out = append(out, ItemTranslation{Locale: loc, Label: label})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Locale < out[j].Locale })
	return out, nil
}

func (m *memRepo) ItemTranslatedLocales(_ context.Context, itemID uuid.UUID) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.translations[itemID]))
	for loc := range m.translations[itemID] {
		out = append(out, loc)
	}
	sort.Strings(out)
	return out, nil
}

// nilBeginner is never used by the fake repo's SetPositions (the fake overrides
// it), so a nil Beginner in the service is fine for these tests.
func newService(repo Repository) *Service {
	return NewService(nil, repo, allowAuthz{})
}

var actor = uuid.New()

// --- menu CRUD + location-unique --------------------------------------------

func TestCreateMenu_RequiresName(t *testing.T) {
	svc := newService(newMemRepo())
	if _, err := svc.CreateMenu(context.Background(), actor, "   ", ""); err != ErrNameRequired {
		t.Fatalf("want ErrNameRequired, got %v", err)
	}
}

func TestAssignLocation_Conflict(t *testing.T) {
	svc := newService(newMemRepo())
	ctx := context.Background()

	primary, err := svc.CreateMenu(ctx, actor, "Primary", "header")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	if primary.Location != "header" {
		t.Fatalf("want header, got %q", primary.Location)
	}

	other, err := svc.CreateMenu(ctx, actor, "Other", "")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	if _, err := svc.AssignLocation(ctx, actor, other.ID, "header"); err != ErrLocationTaken {
		t.Fatalf("want ErrLocationTaken, got %v", err)
	}
	// Unassigning is always allowed.
	if _, err := svc.AssignLocation(ctx, actor, other.ID, ""); err != nil {
		t.Fatalf("unassign: %v", err)
	}
}

func TestMutations_Forbidden(t *testing.T) {
	svc := NewService(nil, newMemRepo(), denyAuthz{deny: accounts.ActionCreate})
	if _, err := svc.CreateMenu(context.Background(), actor, "X", ""); err != ErrForbidden {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}

// --- items: add appends + reorder reassigns positions -----------------------

func TestAddItem_AppendsAndReorder(t *testing.T) {
	repo := newMemRepo()
	svc := newService(repo)
	ctx := context.Background()

	menu, _ := svc.CreateMenu(ctx, actor, "Primary", "header")

	a, err := svc.AddItem(ctx, actor, menu.ID, ItemInput{Type: ItemCustom, URL: "https://a.example", Label: " A "})
	if err != nil {
		t.Fatalf("add a: %v", err)
	}
	if a.Position != 0 {
		t.Fatalf("first item position want 0, got %d", a.Position)
	}
	if a.Label != "A" {
		t.Fatalf("label should be trimmed, got %q", a.Label)
	}
	b, _ := svc.AddItem(ctx, actor, menu.ID, ItemInput{Type: ItemCustom, URL: "https://b.example", Label: "B"})
	c, _ := svc.AddItem(ctx, actor, menu.ID, ItemInput{Type: ItemCustom, URL: "https://c.example", Label: "C"})
	if b.Position != 1 || c.Position != 2 {
		t.Fatalf("append positions want 1,2 got %d,%d", b.Position, c.Position)
	}

	// Reorder to c, a, b -> positions 0,1,2 respectively.
	if err := svc.Reorder(ctx, actor, menu.ID, []uuid.UUID{c.ID, a.ID, b.ID}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	items, _ := repo.ListItems(ctx, menu.ID)
	gotOrder := []string{items[0].Label, items[1].Label, items[2].Label}
	if gotOrder[0] != "C" || gotOrder[1] != "A" || gotOrder[2] != "B" {
		t.Fatalf("reorder failed, got %v", gotOrder)
	}
	for i, it := range items {
		if it.Position != i {
			t.Fatalf("position not reassigned to index: item %d has position %d", i, it.Position)
		}
	}
}

func TestAddItem_InvalidType(t *testing.T) {
	repo := newMemRepo()
	svc := newService(repo)
	ctx := context.Background()
	menu, _ := svc.CreateMenu(ctx, actor, "Primary", "")
	if _, err := svc.AddItem(ctx, actor, menu.ID, ItemInput{Type: "bogus"}); err != ErrInvalidType {
		t.Fatalf("want ErrInvalidType, got %v", err)
	}
}

// --- translations overlay + base fallback -----------------------------------

func TestSaveItemTranslation_RejectsDefaultAndUnsupported(t *testing.T) {
	svc := newService(newMemRepo())
	ctx := context.Background()
	id := uuid.New()
	if err := svc.SaveItemTranslation(ctx, actor, id, i18n.Default(), "x"); err != ErrDefaultLocaleTranslation {
		t.Fatalf("want ErrDefaultLocaleTranslation, got %v", err)
	}
	if err := svc.SaveItemTranslation(ctx, actor, id, i18n.Locale("fr"), "x"); err != ErrUnsupportedLocale {
		t.Fatalf("want ErrUnsupportedLocale, got %v", err)
	}
}

func TestResolveForLocation_OverlayAndFallback(t *testing.T) {
	repo := newMemRepo()
	svc := newService(repo)
	ctx := context.Background()

	menu, _ := svc.CreateMenu(ctx, actor, "Primary", "header")
	home, _ := svc.AddItem(ctx, actor, menu.ID, ItemInput{Type: ItemPage, RefID: ptr(uuid.New()), URL: "/", Label: "Home"})
	about, _ := svc.AddItem(ctx, actor, menu.ID, ItemInput{Type: ItemPage, RefID: ptr(uuid.New()), URL: "/about", Label: "About"})

	// German label only for "home"; "about" falls back to its base label.
	if err := svc.SaveItemTranslation(ctx, actor, home.ID, i18n.LocaleDE, "Startseite"); err != nil {
		t.Fatalf("save translation: %v", err)
	}
	_ = about

	resolved, err := svc.ResolveForLocation(ctx, "header", i18n.LocaleDE)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("want 2 items, got %d", len(resolved))
	}
	if resolved[0].Label != "Startseite" {
		t.Fatalf("overlay label want Startseite, got %q", resolved[0].Label)
	}
	if resolved[1].Label != "About" {
		t.Fatalf("fallback label want About, got %q", resolved[1].Label)
	}
	// Internal URLs localized under /de.
	if resolved[0].URL != "/de" {
		t.Fatalf("home localized url want /de, got %q", resolved[0].URL)
	}
	if resolved[1].URL != "/de/about" {
		t.Fatalf("about localized url want /de/about, got %q", resolved[1].URL)
	}
}

// --- resolve: tree, url rules, empty menu -----------------------------------

func TestResolveForLocation_NestedTreeAndURLRules(t *testing.T) {
	repo := newMemRepo()
	svc := newService(repo)
	ctx := context.Background()

	menu, _ := svc.CreateMenu(ctx, actor, "Primary", "header")
	parent, _ := svc.AddItem(ctx, actor, menu.ID, ItemInput{Type: ItemPage, RefID: ptr(uuid.New()), URL: "/services", Label: "Services"})
	external, _ := svc.AddItem(ctx, actor, menu.ID, ItemInput{Type: ItemCustom, URL: "https://external.example/x", Label: "Ext"})
	_ = external
	// Child of parent (internal path).
	child, _ := svc.AddItem(ctx, actor, menu.ID, ItemInput{ParentID: &parent.ID, Type: ItemPage, RefID: ptr(uuid.New()), URL: "/services/consulting", Label: "Consulting"})
	_ = child
	// Internal item with EMPTY url -> skipped.
	if _, err := svc.AddItem(ctx, actor, menu.ID, ItemInput{Type: ItemPage, RefID: ptr(uuid.New()), URL: "", Label: "Broken"}); err != nil {
		t.Fatalf("add broken: %v", err)
	}

	// Default locale (en): internal URLs unprefixed, external untouched.
	resolved, err := svc.ResolveForLocation(ctx, "header", i18n.Default())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// Top level: parent + external (broken skipped, child nested).
	if len(resolved) != 2 {
		t.Fatalf("want 2 top-level items, got %d: %+v", len(resolved), resolved)
	}
	if resolved[0].Label != "Services" || resolved[0].URL != "/services" {
		t.Fatalf("parent wrong: %+v", resolved[0])
	}
	if len(resolved[0].Children) != 1 || resolved[0].Children[0].URL != "/services/consulting" {
		t.Fatalf("child nesting wrong: %+v", resolved[0].Children)
	}
	if resolved[1].URL != "https://external.example/x" {
		t.Fatalf("external url must be untouched, got %q", resolved[1].URL)
	}
}

func TestResolveForLocation_UnassignedIsEmptyNotError(t *testing.T) {
	svc := newService(newMemRepo())
	resolved, err := svc.ResolveForLocation(context.Background(), "footer", i18n.Default())
	if err != nil {
		t.Fatalf("want nil error for unassigned location, got %v", err)
	}
	if resolved != nil {
		t.Fatalf("want nil resolved, got %+v", resolved)
	}
}

func ptr[T any](v T) *T { return &v }
