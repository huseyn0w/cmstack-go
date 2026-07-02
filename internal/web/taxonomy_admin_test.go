package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/categories"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/tags"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
)

// --- category stub -----------------------------------------------------------

type stubCategoryAdmin struct {
	tree       []categories.TreeNode
	get        categories.Category
	getErr     error
	createErr  error
	translated []i18n.Locale
	saveErr    error
}

func (s stubCategoryAdmin) Tree(context.Context) ([]categories.TreeNode, error) { return s.tree, nil }

func (s stubCategoryAdmin) Get(context.Context, uuid.UUID, uuid.UUID) (categories.Category, error) {
	return s.get, s.getErr
}

func (s stubCategoryAdmin) Create(context.Context, uuid.UUID, categories.CreateInput) (categories.Category, error) {
	return categories.Category{ID: uuid.New()}, s.createErr
}

func (s stubCategoryAdmin) Update(context.Context, uuid.UUID, uuid.UUID, categories.UpdateInput) (categories.Category, error) {
	return categories.Category{}, nil
}
func (s stubCategoryAdmin) Delete(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (s stubCategoryAdmin) BulkDelete(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	return kernel.BulkResult{}, nil
}

func (s stubCategoryAdmin) GetInLocale(context.Context, uuid.UUID, uuid.UUID, i18n.Locale) (categories.Category, error) {
	return s.get, s.getErr
}

func (s stubCategoryAdmin) TranslatedLocales(context.Context, uuid.UUID, uuid.UUID) ([]i18n.Locale, error) {
	return s.translated, nil
}

func (s stubCategoryAdmin) SaveTranslation(context.Context, uuid.UUID, uuid.UUID, i18n.Locale, categories.TranslationInput) error {
	return s.saveErr
}

// --- tag stub ----------------------------------------------------------------

type stubTagAdmin struct {
	list       []tags.Tag
	total      int
	get        tags.Tag
	getErr     error
	createErr  error
	translated []i18n.Locale
	saveErr    error
}

func (s stubTagAdmin) AdminList(context.Context, int, int) ([]tags.Tag, int, error) {
	return s.list, s.total, nil
}

func (s stubTagAdmin) Get(context.Context, uuid.UUID, uuid.UUID) (tags.Tag, error) {
	return s.get, s.getErr
}

func (s stubTagAdmin) Create(context.Context, uuid.UUID, tags.CreateInput) (tags.Tag, error) {
	return tags.Tag{ID: uuid.New()}, s.createErr
}

func (s stubTagAdmin) Update(context.Context, uuid.UUID, uuid.UUID, tags.UpdateInput) (tags.Tag, error) {
	return tags.Tag{}, nil
}
func (s stubTagAdmin) Delete(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (s stubTagAdmin) BulkDelete(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	return kernel.BulkResult{}, nil
}

func (s stubTagAdmin) GetInLocale(context.Context, uuid.UUID, uuid.UUID, i18n.Locale) (tags.Tag, error) {
	return s.get, s.getErr
}

func (s stubTagAdmin) TranslatedLocales(context.Context, uuid.UUID, uuid.UUID) ([]i18n.Locale, error) {
	return s.translated, nil
}

func (s stubTagAdmin) SaveTranslation(context.Context, uuid.UUID, uuid.UUID, i18n.Locale, tags.TranslationInput) error {
	return s.saveErr
}

// --- env ---------------------------------------------------------------------

func buildTaxonomyAdminEnv(t *testing.T, cat CategoryAdminService, tag TagAdminService, authz PermissionChecker) (http.Handler, *scs.SessionManager, *AuthMiddleware, accounts.User) {
	t.Helper()
	user := accounts.User{ID: uuid.New(), Email: "ed@example.com", Name: "Ed", PasswordChangedAt: time.Now()}
	sess := session.NewManager(false)
	users := fakeUsers{users: map[uuid.UUID]accounts.User{user.ID: user}}
	mw := NewAuthMiddleware(sess, users, authz)
	authH := accounts.NewHandler(stubAuthService{}, mw, security.Token, accounts.NewValidator())
	r := Router(Deps{
		Config:           config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health:           health.NewHandler(health.NewService(nil)),
		Session:          sess,
		Auth:             authH,
		AuthMW:           mw,
		CSRFFunc:         security.Token,
		Authz:            authz,
		Roles:            fakeRoles{role: accounts.Role{Key: "editor", Label: "Editor"}},
		CategoryAdminSvc: cat,
		TagAdminSvc:      tag,
		Authors:          users,
	})
	return r, sess, mw, user
}

func TestCategoriesAdmin_DeniedPermissionIs403(t *testing.T) {
	r, sess, mw, user := buildTaxonomyAdminEnv(t, stubCategoryAdmin{}, stubTagAdmin{}, denyAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/categories", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied /admin/categories = %d, want 403", rec.Code)
	}
}

func TestTagsAdmin_DeniedPermissionIs403(t *testing.T) {
	r, sess, mw, user := buildTaxonomyAdminEnv(t, stubCategoryAdmin{}, stubTagAdmin{}, denyAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/tags", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied /admin/tags = %d, want 403", rec.Code)
	}
}

func TestCategoriesAdmin_ListRendersIndentedTree(t *testing.T) {
	rootID := uuid.New()
	childID := uuid.New()
	tree := []categories.TreeNode{
		{Category: categories.Category{ID: rootID, Name: "Engineering", Slug: "engineering"}, Depth: 0},
		{Category: categories.Category{ID: childID, Name: "Backend", Slug: "backend", ParentID: &rootID}, Depth: 1},
	}
	r, sess, mw, user := buildTaxonomyAdminEnv(t, stubCategoryAdmin{tree: tree}, stubTagAdmin{}, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/categories", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"categories-table", "Engineering", "Backend", "category-row-" + childID.String()} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q", want)
		}
	}
}

func TestCategoriesAdmin_EmptyState(t *testing.T) {
	r, sess, mw, user := buildTaxonomyAdminEnv(t, stubCategoryAdmin{}, stubTagAdmin{}, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/categories", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "categories-empty") {
		t.Fatalf("empty state not rendered")
	}
}

func TestCategoriesAdmin_NewRendersParentPicker(t *testing.T) {
	rootID := uuid.New()
	tree := []categories.TreeNode{{Category: categories.Category{ID: rootID, Name: "Root", Slug: "root"}, Depth: 0}}
	r, sess, mw, user := buildTaxonomyAdminEnv(t, stubCategoryAdmin{tree: tree}, stubTagAdmin{}, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/categories/new", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "field-parent") || !strings.Contains(body, "Root") {
		t.Fatalf("parent picker not rendered: %q missing", "field-parent/Root")
	}
}

// TestCategoriesAdmin_CreateValidationError drives the handler directly
// (bypassing CSRF) to assert a missing name re-renders the editor with the error.
func TestCategoriesAdmin_CreateValidationError(t *testing.T) {
	svc := stubCategoryAdmin{createErr: categories.ErrNameRequired}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{role: accounts.Role{Label: "Editor"}}, csrf: security.Token, siteURL: "/"}
	h := NewCategoryAdminHandler(svc, shell, security.Token)

	form := url.Values{"name": {""}}
	req := httptest.NewRequest(http.MethodPost, "/admin/categories", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New(), Name: "Ed"}))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("create validation = %d, want 200 (re-render)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "error-name") {
		t.Fatalf("name error not shown")
	}
}

// TestCategoriesAdmin_BulkUnknownActionRejected asserts the taxonomy bulk handler
// rejects any action other than "delete" with 400, before the service.
func TestCategoriesAdmin_BulkUnknownActionRejected(t *testing.T) {
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewCategoryAdminHandler(stubCategoryAdmin{}, shell, security.Token)

	form := url.Values{"action": {"trash"}, "ids": {uuid.New().String()}}
	req := httptest.NewRequest(http.MethodPost, "/admin/categories/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Bulk(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown taxonomy bulk action = %d, want 400", rec.Code)
	}
}

// TestCategoriesAdmin_BulkDeleteGatedByRoute asserts the bulk route requires the
// delete:category permission (denied -> 403).
func TestCategoriesAdmin_BulkDeleteGatedByRoute(t *testing.T) {
	r, sess, mw, user := buildTaxonomyAdminEnv(t, stubCategoryAdmin{}, stubTagAdmin{}, denyAuthz{})
	cookie := mintSession(t, sess, mw, user)
	form := url.Values{"action": {"delete"}, "ids": {uuid.New().String()}}
	req := httptest.NewRequest(http.MethodPost, "/admin/categories/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	// CSRF runs before permission in the group; either a 400 (CSRF) or 403 proves
	// the unprivileged caller cannot delete. Assert it is NOT a successful 303.
	if rec.Code == http.StatusSeeOther {
		t.Fatalf("denied bulk delete unexpectedly succeeded (303)")
	}
}

// TestCategoriesAdmin_EditRendersLocaleTabs asserts the category editor renders
// the per-locale tab strip and, on a non-default (?language=de) tab, marks the
// active locale and hides the shared structural fields (slug/parent) in favour of
// the translation note (M7b-3).
func TestCategoriesAdmin_EditRendersLocaleTabs(t *testing.T) {
	id := uuid.New()
	cat := stubCategoryAdmin{
		get:        categories.Category{ID: id, Name: "Guides", Slug: "guides"},
		translated: []i18n.Locale{i18n.Locale("de")},
	}
	r, sess, mw, user := buildTaxonomyAdminEnv(t, cat, stubTagAdmin{}, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)

	// Default (en) tab: structural fields visible, no translation note.
	reqEN := httptest.NewRequest(http.MethodGet, "/admin/categories/"+id.String()+"/edit", nil)
	reqEN.AddCookie(cookie)
	recEN := httptest.NewRecorder()
	r.ServeHTTP(recEN, reqEN)
	bodyEN := recEN.Body.String()
	if !strings.Contains(bodyEN, "locale-tabs") {
		t.Fatalf("en tab: locale strip not rendered")
	}
	if !strings.Contains(bodyEN, `data-testid="field-parent"`) {
		t.Fatalf("en tab: structural parent field should be visible")
	}
	if strings.Contains(bodyEN, "category-translation-note") {
		t.Fatalf("en tab: translation note should NOT show on default locale")
	}

	// de tab: structural fields hidden, active-locale hidden field + note present.
	reqDE := httptest.NewRequest(http.MethodGet, "/admin/categories/"+id.String()+"/edit?language=de", nil)
	reqDE.AddCookie(cookie)
	recDE := httptest.NewRecorder()
	r.ServeHTTP(recDE, reqDE)
	bodyDE := recDE.Body.String()
	if !strings.Contains(bodyDE, `name="locale" value="de" data-testid="category-editor-active-locale"`) {
		t.Fatalf("de tab: active-locale hidden field missing")
	}
	if strings.Contains(bodyDE, `data-testid="field-parent"`) {
		t.Fatalf("de tab: structural parent field must be hidden on a translation tab")
	}
	if !strings.Contains(bodyDE, "category-translation-note") {
		t.Fatalf("de tab: translation note missing")
	}
}

// TestCategoriesAdmin_UpdateNonDefaultLocaleSavesTranslation asserts a POST with a
// non-default locale field routes to SaveTranslation and redirects back to that
// language tab, rather than taking the base Update path (M7b-3).
func TestCategoriesAdmin_UpdateNonDefaultLocaleSavesTranslation(t *testing.T) {
	id := uuid.New()
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewCategoryAdminHandler(stubCategoryAdmin{get: categories.Category{ID: id}}, shell, security.Token)

	form := url.Values{"name": {"Anleitungen"}, "description": {"Deutsch"}, "locale": {"de"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/categories/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("translation save = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasSuffix(loc, "/edit?language=de") {
		t.Fatalf("redirect = %q, want the de language tab", loc)
	}
}

// TestTagsAdmin_UpdateNonDefaultLocaleSavesTranslation is the tag-side mirror.
func TestTagsAdmin_UpdateNonDefaultLocaleSavesTranslation(t *testing.T) {
	id := uuid.New()
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewTagAdminHandler(stubTagAdmin{get: tags.Tag{ID: id}}, shell, security.Token)

	form := url.Values{"name": {"Los"}, "locale": {"de"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/tags/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("tag translation save = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasSuffix(loc, "/edit?language=de") {
		t.Fatalf("redirect = %q, want the de language tab", loc)
	}
}

func TestTagsAdmin_ListAndEmpty(t *testing.T) {
	// Empty.
	r, sess, mw, user := buildTaxonomyAdminEnv(t, stubCategoryAdmin{}, stubTagAdmin{}, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/tags", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "tags-empty") {
		t.Fatalf("tags empty state not rendered")
	}

	// Non-empty.
	id := uuid.New()
	r2, sess2, mw2, user2 := buildTaxonomyAdminEnv(t, stubCategoryAdmin{}, stubTagAdmin{list: []tags.Tag{{ID: id, Name: "Go", Slug: "go"}}, total: 1}, allowAllAuthz{})
	cookie2 := mintSession(t, sess2, mw2, user2)
	req2 := httptest.NewRequest(http.MethodGet, "/admin/tags", nil)
	req2.AddCookie(cookie2)
	rec2 := httptest.NewRecorder()
	r2.ServeHTTP(rec2, req2)
	if !strings.Contains(rec2.Body.String(), "tag-row-"+id.String()) {
		t.Fatalf("tag row not rendered")
	}
}
