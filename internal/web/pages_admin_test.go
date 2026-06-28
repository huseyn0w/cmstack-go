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
	"github.com/huseyn0w/cmstack-go/internal/content/pages"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
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

func (s stubPageAdmin) Update(context.Context, uuid.UUID, uuid.UUID, pages.UpdateInput) (pages.Page, error) {
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
