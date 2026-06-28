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
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/services"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
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

func (s *stubServiceAdmin) Update(context.Context, uuid.UUID, uuid.UUID, services.UpdateInput) (services.Service, error) {
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
