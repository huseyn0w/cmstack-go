package menus

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

// Domain errors carried to the handler's error summary.
var (
	// ErrForbidden is returned when the actor lacks the menu permission.
	ErrForbidden = errors.New("menus: forbidden")
	// ErrNameRequired is returned when a menu create/rename has no usable name.
	ErrNameRequired = errors.New("menus: name is required")
	// ErrInvalidType is returned when an item's type is not one of the known set.
	ErrInvalidType = errors.New("menus: invalid item type")
	// ErrDefaultLocaleTranslation is returned when a label translation targets the
	// default locale (en). The default-locale label lives on the base item row.
	ErrDefaultLocaleTranslation = errors.New("menus: cannot store a translation for the default locale")
	// ErrUnsupportedLocale is returned when a label translation targets a locale
	// outside the supported set.
	ErrUnsupportedLocale = errors.New("menus: unsupported locale")
)

// Service holds ALL menu logic. It accesses data only through the repository and
// gates admin mutations through the authorizer. There is no per-author ownership:
// the menu permission grant alone gates every mutation.
//
// The service does NOT load referenced posts/pages/categories (to avoid
// cross-module coupling): for internal item types the CALLER (admin slice)
// resolves the reference into a RefID + rooted URL + default label before calling
// AddItem/UpdateItem; the service only validates the type and trims the label.
type Service struct {
	pool  Beginner
	repo  Repository
	authz Authorizer
}

// NewService constructs the menu Service with explicit dependencies.
func NewService(pool Beginner, repo Repository, authz Authorizer) *Service {
	return &Service{pool: pool, repo: repo, authz: authz}
}

// --- admin: menus ------------------------------------------------------------

// Menus returns every menu (admin listing). Requires read:menu.
func (s *Service) Menus(ctx context.Context, actorID uuid.UUID) ([]Menu, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectMenu) {
		return nil, ErrForbidden
	}
	return s.repo.ListMenus(ctx)
}

// CreateMenu makes a new menu. Name is required (trimmed); location is optional
// (trimmed; "" leaves it unassigned). Requires create:menu.
func (s *Service) CreateMenu(ctx context.Context, actorID uuid.UUID, name, location string) (Menu, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionCreate, accounts.SubjectMenu) {
		return Menu{}, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Menu{}, ErrNameRequired
	}
	return s.repo.CreateMenu(ctx, name, strings.TrimSpace(location))
}

// GetMenu returns a menu plus its items (ordered by position). Requires read:menu.
func (s *Service) GetMenu(ctx context.Context, actorID, id uuid.UUID) (Menu, []Item, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectMenu) {
		return Menu{}, nil, ErrForbidden
	}
	menu, err := s.repo.GetMenu(ctx, id)
	if err != nil {
		return Menu{}, nil, err
	}
	items, err := s.repo.ListItems(ctx, id)
	if err != nil {
		return Menu{}, nil, err
	}
	return menu, items, nil
}

// RenameMenu changes a menu's name (location unchanged). Requires update:menu.
func (s *Service) RenameMenu(ctx context.Context, actorID, id uuid.UUID, name string) (Menu, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectMenu) {
		return Menu{}, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Menu{}, ErrNameRequired
	}
	menu, err := s.repo.GetMenu(ctx, id)
	if err != nil {
		return Menu{}, err
	}
	return s.repo.UpdateMenu(ctx, id, name, menu.Location)
}

// AssignLocation sets a menu's location (name unchanged). Passing "" unassigns
// it. A collision with another menu's assigned location returns ErrLocationTaken.
// Requires update:menu.
func (s *Service) AssignLocation(ctx context.Context, actorID, id uuid.UUID, location string) (Menu, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectMenu) {
		return Menu{}, ErrForbidden
	}
	menu, err := s.repo.GetMenu(ctx, id)
	if err != nil {
		return Menu{}, err
	}
	return s.repo.UpdateMenu(ctx, id, menu.Name, strings.TrimSpace(location))
}

// DeleteMenu removes a menu (items + translations cascade). Requires delete:menu.
func (s *Service) DeleteMenu(ctx context.Context, actorID, id uuid.UUID) error {
	if !s.authz.Can(ctx, actorID, accounts.ActionDelete, accounts.SubjectMenu) {
		return ErrForbidden
	}
	if _, err := s.repo.GetMenu(ctx, id); err != nil {
		return err
	}
	return s.repo.DeleteMenu(ctx, id)
}

// --- admin: items ------------------------------------------------------------

// ItemInput is the validated add/update request for a menu item. For internal
// types (post/page/category) the CALLER has already resolved RefID, the rooted
// URL, and the default Label from the referenced content; the service validates
// the type, trims the label, and persists. Position on add appends to the end.
type ItemInput struct {
	ParentID *uuid.UUID
	Type     ItemType
	RefID    *uuid.UUID
	URL      string
	Label    string
}

// AddItem appends an item to a menu (position = current item count). It validates
// the type against the known set and trims the label. Requires update:menu.
func (s *Service) AddItem(ctx context.Context, actorID, menuID uuid.UUID, in ItemInput) (Item, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectMenu) {
		return Item{}, ErrForbidden
	}
	if !in.Type.Valid() {
		return Item{}, ErrInvalidType
	}
	if _, err := s.repo.GetMenu(ctx, menuID); err != nil {
		return Item{}, err
	}
	existing, err := s.repo.ListItems(ctx, menuID)
	if err != nil {
		return Item{}, err
	}
	return s.repo.AddItem(ctx, CreateItemData{
		MenuID:   menuID,
		ParentID: in.ParentID,
		Position: len(existing),
		Type:     in.Type,
		RefID:    in.RefID,
		URL:      strings.TrimSpace(in.URL),
		Label:    strings.TrimSpace(in.Label),
	})
}

// UpdateItem writes an item's structural fields + label. Requires update:menu.
func (s *Service) UpdateItem(ctx context.Context, actorID, itemID uuid.UUID, in ItemInput) (Item, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectMenu) {
		return Item{}, ErrForbidden
	}
	if !in.Type.Valid() {
		return Item{}, ErrInvalidType
	}
	return s.repo.UpdateItem(ctx, itemID, UpdateItemData{
		ParentID: in.ParentID,
		Type:     in.Type,
		RefID:    in.RefID,
		URL:      strings.TrimSpace(in.URL),
		Label:    strings.TrimSpace(in.Label),
	})
}

// DeleteItem removes an item (children + translations cascade). Requires update:menu.
func (s *Service) DeleteItem(ctx context.Context, actorID, itemID uuid.UUID) error {
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectMenu) {
		return ErrForbidden
	}
	return s.repo.DeleteItem(ctx, itemID)
}

// Reorder assigns position = index for each id in orderedIDs (one transaction).
// Requires update:menu.
func (s *Service) Reorder(ctx context.Context, actorID, menuID uuid.UUID, orderedIDs []uuid.UUID) error {
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectMenu) {
		return ErrForbidden
	}
	if _, err := s.repo.GetMenu(ctx, menuID); err != nil {
		return err
	}
	return s.repo.SetPositions(ctx, menuID, orderedIDs)
}

// --- admin: per-locale labels ------------------------------------------------

// SaveItemTranslation upserts a NON-default locale's label for an item. The
// default locale is rejected (its label lives on the base item row, edited via
// AddItem/UpdateItem); unsupported locales are rejected. Requires update:menu.
func (s *Service) SaveItemTranslation(ctx context.Context, actorID, itemID uuid.UUID, locale i18n.Locale, label string) error {
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectMenu) {
		return ErrForbidden
	}
	if !i18n.IsSupported(locale) {
		return ErrUnsupportedLocale
	}
	if locale.IsDefault() {
		return ErrDefaultLocaleTranslation
	}
	return s.repo.UpsertItemTranslation(ctx, itemID, locale.String(), strings.TrimSpace(label))
}

// ItemTranslatedLocales returns the NON-default locales that already have a label
// for an item. Requires read:menu.
func (s *Service) ItemTranslatedLocales(ctx context.Context, actorID, itemID uuid.UUID) ([]i18n.Locale, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectMenu) {
		return nil, ErrForbidden
	}
	raw, err := s.repo.ItemTranslatedLocales(ctx, itemID)
	if err != nil {
		return nil, err
	}
	out := make([]i18n.Locale, 0, len(raw))
	for _, r := range raw {
		if l, ok := i18n.Parse(r); ok && !l.IsDefault() {
			out = append(out, l)
		}
	}
	return out, nil
}

// --- public resolve (NO auth) ------------------------------------------------

// ResolveForLocation loads the menu assigned to location and returns its items as
// a nested ResolvedItem tree in the active locale. An unassigned location returns
// nil (an empty menu is fine, not an error). Labels are overlaid by locale (the
// default/unsupported locale resolves to base labels). For each item the final
// URL is resolved: an "http"-prefixed URL is external and used as-is; otherwise
// it is treated as a rooted internal path and localized via i18n.LocalizePath.
// Internal-type items with an empty URL are skipped (nothing to link to).
func (s *Service) ResolveForLocation(ctx context.Context, location string, locale i18n.Locale) ([]ResolvedItem, error) {
	location = strings.TrimSpace(location)
	if location == "" {
		return nil, nil
	}
	menu, err := s.repo.MenuByLocation(ctx, location)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	loc := locale
	if !i18n.IsSupported(loc) {
		loc = i18n.Default()
	}

	items, err := s.repo.ListItemsInLocale(ctx, menu.ID, loc.String())
	if err != nil {
		return nil, err
	}

	return buildTree(items, loc, nil), nil
}

// buildTree assembles the nested ResolvedItem list under parent (nil == top
// level), preserving the position order the repo returned. It recurses per item,
// resolving each item's URL for loc and skipping internal items with no URL.
func buildTree(items []Item, loc i18n.Locale, parent *uuid.UUID) []ResolvedItem {
	var out []ResolvedItem
	for _, it := range items {
		if !sameParent(it.ParentID, parent) {
			continue
		}
		url, ok := resolveURL(it, loc)
		if !ok {
			continue
		}
		id := it.ID
		out = append(out, ResolvedItem{
			Label:    it.Label,
			URL:      url,
			Children: buildTree(items, loc, &id),
		})
	}
	return out
}

// resolveURL computes an item's final href for loc. External URLs (http/https)
// pass through unchanged; other non-empty values are treated as a rooted internal
// path and localized. An internal item with no URL yields ok=false (skip it).
func resolveURL(it Item, loc i18n.Locale) (string, bool) {
	url := strings.TrimSpace(it.URL)
	if strings.HasPrefix(url, "http") {
		return url, true
	}
	if url == "" {
		return "", false
	}
	return i18n.LocalizePath(loc, url), true
}

func sameParent(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
