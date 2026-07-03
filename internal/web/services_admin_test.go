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
	"github.com/huseyn0w/cmstack-go/internal/content/services"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
)

// stubServiceAdmin is a controllable ServiceAdminService.
type stubServiceAdmin struct {
	list      []services.Service
	listTotal int
	get       services.Service
	getErr    error
	createErr error
	captured  *services.CreateInput

	// SEO editor (M8). updated captures the last base-row Update input so a handler
	// test can assert the SEO fields reached the service.
	updated *services.UpdateInput

	// M7b-2 per-locale fields.
	byLocale       map[i18n.Locale]services.Service
	translated     []i18n.Locale
	savedLocale    i18n.Locale
	savedInput     services.TranslationInput
	savedTranslate bool
	saveErr        error
}

func (s *stubServiceAdmin) GetInLocale(_ context.Context, _, _ uuid.UUID, locale i18n.Locale) (services.Service, error) {
	if s.getErr != nil {
		return services.Service{}, s.getErr
	}
	if svc, ok := s.byLocale[locale]; ok {
		return svc, nil
	}
	return s.get, nil // base (en) fallback
}

func (s *stubServiceAdmin) TranslatedLocales(context.Context, uuid.UUID, uuid.UUID) ([]i18n.Locale, error) {
	return s.translated, nil
}

func (s *stubServiceAdmin) SaveTranslation(_ context.Context, _, _ uuid.UUID, locale i18n.Locale, in services.TranslationInput) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.savedLocale = locale
	s.savedInput = in
	s.savedTranslate = true
	return nil
}

func (s *stubServiceAdmin) AdminList(context.Context, services.ListFilter) ([]services.Service, int, error) {
	return s.list, s.listTotal, nil
}

func (s *stubServiceAdmin) AdminTrashed(context.Context, int, int) ([]services.Service, int, error) {
	return nil, 0, nil
}

func (s *stubServiceAdmin) Get(context.Context, uuid.UUID, uuid.UUID) (services.Service, error) {
	return s.get, s.getErr
}

func (s *stubServiceAdmin) Create(_ context.Context, _ uuid.UUID, in services.CreateInput) (services.Service, error) {
	cp := in
	s.captured = &cp
	return services.Service{}, s.createErr
}

func (s *stubServiceAdmin) Update(_ context.Context, _, _ uuid.UUID, in services.UpdateInput) (services.Service, error) {
	cp := in
	s.updated = &cp
	return services.Service{}, nil
}

func (s *stubServiceAdmin) Trash(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (s *stubServiceAdmin) Restore(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (s *stubServiceAdmin) PermanentDelete(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (s *stubServiceAdmin) Revisions(context.Context, uuid.UUID, uuid.UUID) ([]kernel.Revision, error) {
	return nil, nil
}

func (s *stubServiceAdmin) RestoreRevision(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (services.Service, error) {
	return services.Service{}, nil
}

func (s *stubServiceAdmin) BulkTrash(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	return kernel.BulkResult{}, nil
}

func (s *stubServiceAdmin) BulkRestore(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	return kernel.BulkResult{}, nil
}

func (s *stubServiceAdmin) BulkPermanentDelete(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	return kernel.BulkResult{}, nil
}

func (s *stubServiceAdmin) BulkPublish(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	return kernel.BulkResult{}, nil
}

func (s *stubServiceAdmin) BulkUnpublish(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	return kernel.BulkResult{}, nil
}

func buildServicesAdminEnv(t *testing.T, svc ServiceAdminService, authz PermissionChecker) (http.Handler, *scs.SessionManager, *AuthMiddleware, accounts.User) {
	t.Helper()
	user := accounts.User{ID: uuid.New(), Email: "ed@example.com", Name: "Ed", PasswordChangedAt: time.Now()}
	sess := session.NewManager(false)
	users := fakeUsers{users: map[uuid.UUID]accounts.User{user.ID: user}}
	mw := NewAuthMiddleware(sess, users, authz)
	authH := accounts.NewHandler(stubAuthService{}, mw, security.Token, accounts.NewValidator())
	r := Router(Deps{
		Config:          config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health:          health.NewHandler(health.NewService(nil)),
		Session:         sess,
		Auth:            authH,
		AuthMW:          mw,
		CSRFFunc:        security.Token,
		Authz:           authz,
		Roles:           fakeRoles{role: accounts.Role{Key: "editor", Label: "Editor"}},
		ServiceAdminSvc: svc,
		Authors:         users,
	})
	return r, sess, mw, user
}

func TestServicesAdmin_DeniedPermissionIs403(t *testing.T) {
	r, sess, mw, user := buildServicesAdminEnv(t, &stubServiceAdmin{}, denyAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/services", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied /admin/services = %d, want 403", rec.Code)
	}
}

func TestServicesAdmin_ListRenders(t *testing.T) {
	svc := &stubServiceAdmin{
		list:      []services.Service{{ID: uuid.New(), Title: "SEO Audit", Slug: "seo-audit", Status: kernel.StatusPublished, Price: "From $499"}},
		listTotal: 1,
	}
	r, sess, mw, user := buildServicesAdminEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/services", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/admin/services = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`data-testid="services-table"`, "SEO Audit", "From $499"} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing %q", want)
		}
	}
}

func TestServicesAdmin_NewRendersFAQEditor(t *testing.T) {
	r, sess, mw, user := buildServicesAdminEnv(t, &stubServiceAdmin{}, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/services/new", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/admin/services/new = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="faq-editor"`) {
		t.Error("new editor missing FAQ editor")
	}
}

// TestServicesAdmin_CreateParsesFAQArrays asserts the handler pairs the repeated
// faq_question[]/faq_answer[] form fields into ordered FAQ inputs.
func TestServicesAdmin_CreateParsesFAQArrays(t *testing.T) {
	svc := &stubServiceAdmin{}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewServiceAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{
		"title":          {"SEO Audit"},
		"status":         {"DRAFT"},
		"faq_question[]": {"How long?", "How much?"},
		"faq_answer[]":   {"A week.", "From $499."},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/services", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if svc.captured == nil {
		t.Fatal("Create was not called")
	}
	if len(svc.captured.FAQs) != 2 {
		t.Fatalf("parsed %d FAQs, want 2", len(svc.captured.FAQs))
	}
	if svc.captured.FAQs[0].Question != "How long?" || svc.captured.FAQs[1].Answer != "From $499." {
		t.Errorf("FAQ pairing wrong: %+v", svc.captured.FAQs)
	}
}

// TestServicesAdmin_EditRendersLocaleTabs asserts the editor for an existing
// service renders the per-locale tab strip (M7b-2).
func TestServicesAdmin_EditRendersLocaleTabs(t *testing.T) {
	id := uuid.New()
	svcRow := services.Service{ID: id, Title: "SEO Audit", Slug: "seo-audit", Body: "<p>en</p>", Status: kernel.StatusPublished}
	svc := &stubServiceAdmin{get: svcRow, translated: []i18n.Locale{i18n.LocaleDE}}
	r, sess, mw, user := buildServicesAdminEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/services/"+id.String()+"/edit", nil)
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
		`data-testid="locale-dot-de"`,
		`data-testid="service-field-price"`, // structural on en
		`data-testid="faq-editor"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("edit editor missing %q", want)
		}
	}
}

// TestServicesAdmin_EditDeTabHidesStructural asserts the de tab hides
// structural/citable fields + FAQ and shows the translation note.
func TestServicesAdmin_EditDeTabHidesStructural(t *testing.T) {
	id := uuid.New()
	en := services.Service{ID: id, Title: "SEO Audit", Slug: "seo-audit", Body: "<p>en</p>", Price: "$499", Status: kernel.StatusPublished}
	de := en
	de.Title = "SEO Pruefung"
	de.Body = "<p>de</p>"
	svc := &stubServiceAdmin{get: en, byLocale: map[i18n.Locale]services.Service{i18n.LocaleDE: de}, translated: []i18n.Locale{i18n.LocaleDE}}
	r, sess, mw, user := buildServicesAdminEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/services/"+id.String()+"/edit?language=de", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit de = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "SEO Pruefung") {
		t.Errorf("de tab did not render de title")
	}
	if !strings.Contains(body, `data-testid="service-translation-note"`) {
		t.Errorf("de tab missing translation note")
	}
	for _, absent := range []string{`data-testid="service-field-price"`, `data-testid="service-field-status"`, `data-testid="faq-editor"`, `data-testid="service-action-publish"`} {
		if strings.Contains(body, absent) {
			t.Errorf("de translation tab should hide %q", absent)
		}
	}
}

// TestServicesAdmin_UpdateDePersistsTranslation asserts posting with locale=de
// dispatches to SaveTranslation rather than the base Update.
func TestServicesAdmin_UpdateDePersistsTranslation(t *testing.T) {
	id := uuid.New()
	svc := &stubServiceAdmin{get: services.Service{ID: id, Title: "SEO Audit", Slug: "seo-audit"}}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewServiceAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{"title": {"SEO Pruefung"}, "summary": {"de summary"}, "body": {"<p>de body</p>"}, "locale": {"de"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/services/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id.String())
	req = req.WithContext(withUser(context.WithValue(req.Context(), chi.RouteCtxKey, rctx), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("de update = %d, want 303\n%s", rec.Code, rec.Body.String())
	}
	if !svc.savedTranslate {
		t.Fatal("SaveTranslation was not called for a de update")
	}
	if svc.savedLocale != i18n.LocaleDE || svc.savedInput.Title != "SEO Pruefung" || svc.savedInput.Body != "<p>de body</p>" {
		t.Errorf("SaveTranslation got %+v / %+v", svc.savedLocale, svc.savedInput)
	}
}

// TestServicesAdmin_EditRendersSEOFields asserts the SEO panel renders on the
// default tab: translatable meta title/description + structural canonical/
// noindex (M8).
func TestServicesAdmin_EditRendersSEOFields(t *testing.T) {
	id := uuid.New()
	svcRow := services.Service{
		ID: id, Title: "SEO Audit", Slug: "seo-audit", Body: "<p>en</p>",
		MetaTitle: "Custom Meta", MetaDescription: "Meta desc", CanonicalURL: "https://x.test/seo-audit", NoIndex: true,
	}
	svc := &stubServiceAdmin{get: svcRow}
	r, sess, mw, user := buildServicesAdminEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/services/"+id.String()+"/edit", nil)
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
		`value="https://x.test/seo-audit"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("SEO editor missing %q", want)
		}
	}
}

// TestServicesAdmin_EditDeTabHidesStructuralSEO asserts the de tab keeps the
// translatable meta title but hides the structural canonical/noindex fields.
func TestServicesAdmin_EditDeTabHidesStructuralSEO(t *testing.T) {
	id := uuid.New()
	en := services.Service{ID: id, Title: "SEO Audit", Slug: "seo-audit", Body: "<p>en</p>"}
	svc := &stubServiceAdmin{get: en, byLocale: map[i18n.Locale]services.Service{i18n.LocaleDE: en}, translated: []i18n.Locale{i18n.LocaleDE}}
	r, sess, mw, user := buildServicesAdminEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/services/"+id.String()+"/edit?language=de", nil)
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

// TestServicesAdmin_UpdateSavesSEOFields asserts a base-row POST forwards the
// SEO form values into the UpdateInput reaching the service (M8).
func TestServicesAdmin_UpdateSavesSEOFields(t *testing.T) {
	id := uuid.New()
	svc := &stubServiceAdmin{get: services.Service{ID: id, Title: "SEO Audit", Slug: "seo-audit"}}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewServiceAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{
		"title":            {"SEO Audit"},
		"body":             {"<p>x</p>"},
		"status":           {"DRAFT"},
		"meta_title":       {"SEO Title"},
		"meta_description": {"SEO Description"},
		"canonical_url":    {"https://x.test/seo-audit"},
		"noindex":          {"on"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/services/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id.String())
	req = req.WithContext(withUser(context.WithValue(req.Context(), chi.RouteCtxKey, rctx), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update = %d, want 303\n%s", rec.Code, rec.Body.String())
	}
	if svc.updated == nil {
		t.Fatal("Update was not called")
	}
	if svc.updated.MetaTitle == nil || *svc.updated.MetaTitle != "SEO Title" {
		t.Errorf("MetaTitle = %v, want SEO Title", svc.updated.MetaTitle)
	}
	if svc.updated.CanonicalURL == nil || *svc.updated.CanonicalURL != "https://x.test/seo-audit" {
		t.Errorf("CanonicalURL = %v, want https://x.test/seo-audit", svc.updated.CanonicalURL)
	}
	if svc.updated.NoIndex == nil || !*svc.updated.NoIndex {
		t.Errorf("NoIndex = %v, want true", svc.updated.NoIndex)
	}
}

func TestServicesAdmin_CreateValidationError(t *testing.T) {
	svc := &stubServiceAdmin{createErr: services.ErrTitleRequired}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewServiceAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{"title": {""}, "status": {"DRAFT"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/services", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create validation = %d, want 200 re-render", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Title is required") {
		t.Errorf("expected title error in re-rendered editor")
	}
}
