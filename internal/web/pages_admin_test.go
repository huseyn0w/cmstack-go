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
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/pages"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
)

// stubPageAdmin is a controllable PageAdminService.
type stubPageAdmin struct {
	list      []pages.Page
	listTotal int
	all       []pages.Page
	get       pages.Page
	getErr    error
	createErr error

	// M7b-2 per-locale fields.
	byLocale   map[i18n.Locale]pages.Page
	translated []i18n.Locale
	saved      *savedPageTranslation // capture for SaveTranslation
	saveErr    error

	// SEO editor (M8). When set, savedUpdate captures the last base-row Update
	// input so a handler test can assert the SEO fields reached the service.
	savedUpdate *pages.UpdateInput
}

// savedPageTranslation captures a SaveTranslation call so a value-receiver stub
// can record persistence for assertions.
type savedPageTranslation struct {
	locale i18n.Locale
	input  pages.TranslationInput
	called bool
}

func (s stubPageAdmin) GetInLocale(_ context.Context, _, _ uuid.UUID, locale i18n.Locale) (pages.Page, error) {
	if s.getErr != nil {
		return pages.Page{}, s.getErr
	}
	if p, ok := s.byLocale[locale]; ok {
		return p, nil
	}
	return s.get, nil // base (en) fallback
}

func (s stubPageAdmin) TranslatedLocales(context.Context, uuid.UUID, uuid.UUID) ([]i18n.Locale, error) {
	return s.translated, nil
}

func (s stubPageAdmin) SaveTranslation(_ context.Context, _, _ uuid.UUID, locale i18n.Locale, in pages.TranslationInput) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	if s.saved != nil {
		s.saved.locale = locale
		s.saved.input = in
		s.saved.called = true
	}
	return nil
}

func (s stubPageAdmin) AdminList(context.Context, pages.ListFilter) ([]pages.Page, int, error) {
	return s.list, s.listTotal, nil
}
func (s stubPageAdmin) AllActive(context.Context) ([]pages.Page, error) { return s.all, nil }
func (s stubPageAdmin) AdminTrashed(context.Context, int, int) ([]pages.Page, int, error) {
	return nil, 0, nil
}

func (s stubPageAdmin) Get(context.Context, uuid.UUID, uuid.UUID) (pages.Page, error) {
	return s.get, s.getErr
}

func (s stubPageAdmin) Create(context.Context, uuid.UUID, pages.CreateInput) (pages.Page, error) {
	return pages.Page{}, s.createErr
}

func (s stubPageAdmin) Update(_ context.Context, _, _ uuid.UUID, in pages.UpdateInput) (pages.Page, error) {
	if s.savedUpdate != nil {
		*s.savedUpdate = in
	}
	return pages.Page{}, nil
}
func (s stubPageAdmin) Trash(context.Context, uuid.UUID, uuid.UUID) error           { return nil }
func (s stubPageAdmin) Restore(context.Context, uuid.UUID, uuid.UUID) error         { return nil }
func (s stubPageAdmin) PermanentDelete(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (s stubPageAdmin) Revisions(context.Context, uuid.UUID, uuid.UUID) ([]kernel.Revision, error) {
	return nil, nil
}

func (s stubPageAdmin) RestoreRevision(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (pages.Page, error) {
	return pages.Page{}, nil
}

func (s stubPageAdmin) BulkTrash(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	return kernel.BulkResult{}, nil
}

func (s stubPageAdmin) BulkRestore(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	return kernel.BulkResult{}, nil
}

func (s stubPageAdmin) BulkPermanentDelete(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	return kernel.BulkResult{}, nil
}

func (s stubPageAdmin) BulkPublish(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	return kernel.BulkResult{}, nil
}

func (s stubPageAdmin) BulkUnpublish(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	return kernel.BulkResult{}, nil
}

func buildPagesAdminEnv(t *testing.T, svc PageAdminService, authz PermissionChecker) (http.Handler, *scs.SessionManager, *AuthMiddleware, accounts.User) {
	t.Helper()
	user := accounts.User{ID: uuid.New(), Email: "ed@example.com", Name: "Ed", PasswordChangedAt: time.Now()}
	sess := session.NewManager(false)
	users := fakeUsers{users: map[uuid.UUID]accounts.User{user.ID: user}}
	mw := NewAuthMiddleware(sess, users, authz)
	authH := accounts.NewHandler(stubAuthService{}, mw, security.Token, accounts.NewValidator())
	r := Router(Deps{
		Config:       config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health:       health.NewHandler(health.NewService(nil)),
		Session:      sess,
		Auth:         authH,
		AuthMW:       mw,
		CSRFFunc:     security.Token,
		Authz:        authz,
		Roles:        fakeRoles{role: accounts.Role{Key: "editor", Label: "Editor"}},
		PageAdminSvc: svc,
		Authors:      users,
	})
	return r, sess, mw, user
}

func TestPagesAdmin_DeniedPermissionIs403(t *testing.T) {
	r, sess, mw, user := buildPagesAdminEnv(t, stubPageAdmin{}, denyAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/pages", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied /admin/pages = %d, want 403", rec.Code)
	}
}

func TestPagesAdmin_UnauthenticatedRedirects(t *testing.T) {
	r, _, _, _ := buildPagesAdminEnv(t, stubPageAdmin{}, allowAllAuthz{})
	req := httptest.NewRequest(http.MethodGet, "/admin/pages", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unauth /admin/pages = %d, want 303", rec.Code)
	}
}

func TestPagesAdmin_ListRendersTreeForPermittedUser(t *testing.T) {
	root := uuid.New()
	rootPage := pages.Page{ID: root, Title: "Root", Slug: "root", Status: kernel.StatusPublished, Template: "default"}
	child := pages.Page{ID: uuid.New(), Title: "Child", Slug: "child", Status: kernel.StatusDraft, Template: "default", ParentID: &root}
	svc := stubPageAdmin{list: []pages.Page{rootPage, child}, listTotal: 2}
	r, sess, mw, user := buildPagesAdminEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/pages", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/admin/pages = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`data-testid="pages-table"`, "Root", "Child", "padding-left:20px"} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing %q", want)
		}
	}
}

func TestPagesAdmin_NewRendersParentPicker(t *testing.T) {
	svc := stubPageAdmin{all: []pages.Page{{ID: uuid.New(), Title: "Existing", Slug: "existing"}}}
	r, sess, mw, user := buildPagesAdminEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/pages/new", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/admin/pages/new = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`data-testid="page-field-parent"`, `data-testid="page-field-template"`, "Existing"} {
		if !strings.Contains(body, want) {
			t.Errorf("new editor missing %q", want)
		}
	}
}

func TestPagesAdmin_CreateValidationError(t *testing.T) {
	svc := stubPageAdmin{createErr: pages.ErrTitleRequired}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{role: accounts.Role{Label: "Editor"}}, csrf: security.Token, siteURL: "/"}
	h := NewPageAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{"title": {""}, "body": {"<p>x</p>"}, "status": {"DRAFT"}, "template": {"default"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/pages", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New(), Name: "Ed"}))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create validation = %d, want 200 re-render", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Title is required") {
		t.Errorf("expected title error in re-rendered editor")
	}
}

// TestPagesAdmin_EditRendersLocaleTabs asserts the editor for an existing page
// renders the per-locale tab strip (M7b-2).
func TestPagesAdmin_EditRendersLocaleTabs(t *testing.T) {
	id := uuid.New()
	page := pages.Page{ID: id, Title: "About", Slug: "about", Body: "<p>en</p>", Status: kernel.StatusPublished, Template: "default"}
	svc := stubPageAdmin{get: page, translated: []i18n.Locale{i18n.LocaleDE}}
	r, sess, mw, user := buildPagesAdminEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/pages/"+id.String()+"/edit", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="locale-tabs"`,
		`role="tablist"`,
		`data-testid="locale-tab-en"`,
		`data-testid="locale-tab-de"`,
		`data-testid="locale-dot-de"`, // de has a translation
		`data-testid="page-field-status"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit editor missing %q", want)
		}
	}
}

// TestPagesAdmin_EditDeTabHidesStructural asserts the de tab hides structural
// fields and shows the translation note.
func TestPagesAdmin_EditDeTabHidesStructural(t *testing.T) {
	id := uuid.New()
	en := pages.Page{ID: id, Title: "About", Slug: "about", Body: "<p>en</p>", Status: kernel.StatusPublished, Template: "default"}
	de := en
	de.Title = "Ueber uns"
	de.Body = "<p>de</p>"
	svc := stubPageAdmin{get: en, byLocale: map[i18n.Locale]pages.Page{i18n.LocaleDE: de}, translated: []i18n.Locale{i18n.LocaleDE}}
	r, sess, mw, user := buildPagesAdminEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/pages/"+id.String()+"/edit?language=de", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit de = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Ueber uns") {
		t.Errorf("de tab did not render de title")
	}
	if !strings.Contains(body, `data-testid="page-translation-note"`) {
		t.Errorf("de tab missing translation note")
	}
	for _, absent := range []string{`data-testid="page-field-status"`, `data-testid="page-field-slug"`, `data-testid="page-action-publish"`} {
		if strings.Contains(body, absent) {
			t.Errorf("de translation tab should hide %q", absent)
		}
	}
}

// TestPagesAdmin_UpdateDePersistsTranslation asserts posting with locale=de
// dispatches to SaveTranslation rather than the base Update.
func TestPagesAdmin_UpdateDePersistsTranslation(t *testing.T) {
	id := uuid.New()
	saved := &savedPageTranslation{}
	svc := stubPageAdmin{get: pages.Page{ID: id, Title: "About", Slug: "about"}, saved: saved}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewPageAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{"title": {"Ueber uns"}, "body": {"<p>de body</p>"}, "locale": {"de"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/pages/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id.String())
	req = req.WithContext(withUser(context.WithValue(req.Context(), chi.RouteCtxKey, rctx), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("de update = %d, want 303\n%s", rec.Code, rec.Body.String())
	}
	if !saved.called {
		t.Fatal("SaveTranslation was not called for a de update")
	}
	if saved.locale != i18n.LocaleDE || saved.input.Title != "Ueber uns" || saved.input.Body != "<p>de body</p>" {
		t.Errorf("SaveTranslation got %+v / %+v", saved.locale, saved.input)
	}
}

// TestPagesAdmin_EditRendersSEOFields asserts the SEO panel renders on the
// default tab: translatable meta title/description + structural canonical/
// noindex (M8).
func TestPagesAdmin_EditRendersSEOFields(t *testing.T) {
	id := uuid.New()
	page := pages.Page{
		ID: id, Title: "About", Slug: "about", Body: "<p>en</p>", Template: "default",
		MetaTitle: "Custom Meta", MetaDescription: "Meta desc", CanonicalURL: "https://x.test/about", NoIndex: true,
	}
	svc := stubPageAdmin{get: page}
	r, sess, mw, user := buildPagesAdminEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/pages/"+id.String()+"/edit", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="field-meta-title"`,
		`data-testid="field-meta-description"`,
		`data-testid="field-canonical-url"`,
		`data-testid="field-noindex"`,
		`value="Custom Meta"`,
		`value="https://x.test/about"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("SEO editor missing %q", want)
		}
	}
}

// TestPagesAdmin_EditDeTabHidesStructuralSEO asserts the de tab keeps the
// translatable meta title but hides the structural canonical/noindex fields.
func TestPagesAdmin_EditDeTabHidesStructuralSEO(t *testing.T) {
	id := uuid.New()
	en := pages.Page{ID: id, Title: "About", Slug: "about", Body: "<p>en</p>", Template: "default"}
	svc := stubPageAdmin{get: en, byLocale: map[i18n.Locale]pages.Page{i18n.LocaleDE: en}, translated: []i18n.Locale{i18n.LocaleDE}}
	r, sess, mw, user := buildPagesAdminEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/pages/"+id.String()+"/edit?language=de", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit de = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="field-meta-title"`) {
		t.Error("de tab should keep the translatable meta title field")
	}
	if strings.Contains(body, `data-testid="field-canonical-url"`) {
		t.Error("de tab must hide the structural canonical URL field")
	}
	if strings.Contains(body, `data-testid="field-noindex"`) {
		t.Error("de tab must hide the structural noindex field")
	}
}

// TestPagesAdmin_UpdateSavesSEOFields asserts a base-row POST forwards the SEO
// form values into the UpdateInput reaching the service (M8).
func TestPagesAdmin_UpdateSavesSEOFields(t *testing.T) {
	id := uuid.New()
	upd := pages.UpdateInput{}
	svc := stubPageAdmin{get: pages.Page{ID: id, Title: "About", Slug: "about"}, savedUpdate: &upd}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewPageAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{
		"title":            {"About"},
		"body":             {"<p>x</p>"},
		"status":           {"DRAFT"},
		"template":         {"default"},
		"meta_title":       {"SEO Title"},
		"meta_description": {"SEO Description"},
		"canonical_url":    {"https://x.test/about"},
		"noindex":          {"on"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/pages/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id.String())
	req = req.WithContext(withUser(context.WithValue(req.Context(), chi.RouteCtxKey, rctx), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update = %d, want 303\n%s", rec.Code, rec.Body.String())
	}
	if upd.MetaTitle == nil || *upd.MetaTitle != "SEO Title" {
		t.Errorf("MetaTitle = %v, want SEO Title", upd.MetaTitle)
	}
	if upd.CanonicalURL == nil || *upd.CanonicalURL != "https://x.test/about" {
		t.Errorf("CanonicalURL = %v, want https://x.test/about", upd.CanonicalURL)
	}
	if upd.NoIndex == nil || !*upd.NoIndex {
		t.Errorf("NoIndex = %v, want true", upd.NoIndex)
	}
}

func TestPagesAdmin_CreateCycleErrorRendered(t *testing.T) {
	svc := stubPageAdmin{createErr: pages.ErrParentCycle}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewPageAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{"title": {"X"}, "body": {"y"}, "status": {"DRAFT"}, "template": {"default"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/pages", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cycle error = %d, want 200 re-render", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "loop in the page hierarchy") {
		t.Errorf("expected cycle error message in re-rendered editor:\n%s", rec.Body.String())
	}
}
