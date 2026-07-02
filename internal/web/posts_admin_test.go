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
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/health"
	"github.com/huseyn0w/cmstack-go/internal/platform/config"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
	"github.com/huseyn0w/cmstack-go/internal/platform/session"
)

// stubPostAdmin is a controllable PostAdminService.
type stubPostAdmin struct {
	list      []posts.Post
	listTotal int
	get       posts.Post
	getErr    error
	createErr error
	created   posts.Post

	// bulkCalls records the verb each Bulk* method received, so a handler test can
	// assert an allow-listed action reached the service (and that an unknown action
	// did NOT — the handler rejects it before any service call).
	bulkCalls *[]string

	// Per-locale translation (M7b-1). translated is the set of locales the stub
	// reports as already having a translation (drives has-translation tab markers);
	// savedTranslation records the last SaveTranslation call so a handler test can
	// assert the de/ru save reached the service with the right locale + content.
	translated       []i18n.Locale
	savedTranslation *savedTranslation
}

type savedTranslation struct {
	locale i18n.Locale
	in     posts.TranslationInput
}

func (s stubPostAdmin) record(verb string) {
	if s.bulkCalls != nil {
		*s.bulkCalls = append(*s.bulkCalls, verb)
	}
}

func (s stubPostAdmin) BulkTrash(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	s.record("trash")
	return kernel.BulkResult{}, nil
}

func (s stubPostAdmin) BulkRestore(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	s.record("restore")
	return kernel.BulkResult{}, nil
}

func (s stubPostAdmin) BulkPermanentDelete(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	s.record("delete")
	return kernel.BulkResult{}, nil
}

func (s stubPostAdmin) BulkPublish(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	s.record("publish")
	return kernel.BulkResult{}, nil
}

func (s stubPostAdmin) BulkUnpublish(context.Context, uuid.UUID, []uuid.UUID) (kernel.BulkResult, error) {
	s.record("unpublish")
	return kernel.BulkResult{}, nil
}

func (s stubPostAdmin) AdminList(context.Context, posts.ListFilter) ([]posts.Post, int, error) {
	return s.list, s.listTotal, nil
}

func (s stubPostAdmin) AdminTrashed(context.Context, int, int) ([]posts.Post, int, error) {
	return nil, 0, nil
}

func (s stubPostAdmin) Get(context.Context, uuid.UUID, uuid.UUID) (posts.Post, error) {
	return s.get, s.getErr
}

func (s stubPostAdmin) Create(context.Context, uuid.UUID, posts.CreateInput) (posts.Post, error) {
	return s.created, s.createErr
}

func (s stubPostAdmin) Update(context.Context, uuid.UUID, uuid.UUID, posts.UpdateInput) (posts.Post, error) {
	return posts.Post{}, nil
}
func (s stubPostAdmin) Trash(context.Context, uuid.UUID, uuid.UUID) error           { return nil }
func (s stubPostAdmin) Restore(context.Context, uuid.UUID, uuid.UUID) error         { return nil }
func (s stubPostAdmin) PermanentDelete(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (s stubPostAdmin) Revisions(context.Context, uuid.UUID, uuid.UUID) ([]kernel.Revision, error) {
	return nil, nil
}

func (s stubPostAdmin) RestoreRevision(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (posts.Post, error) {
	return posts.Post{}, nil
}

func (s stubPostAdmin) GetInLocale(context.Context, uuid.UUID, uuid.UUID, i18n.Locale) (posts.Post, error) {
	return s.get, s.getErr
}

func (s stubPostAdmin) TranslatedLocales(context.Context, uuid.UUID, uuid.UUID) ([]i18n.Locale, error) {
	return s.translated, nil
}

func (s stubPostAdmin) SaveTranslation(_ context.Context, _, _ uuid.UUID, locale i18n.Locale, in posts.TranslationInput) error {
	if s.savedTranslation != nil {
		*s.savedTranslation = savedTranslation{locale: locale, in: in}
	}
	return nil
}

// denyAuthz denies every permission.
type denyAuthz struct{}

func (denyAuthz) Can(context.Context, uuid.UUID, string, string) bool { return false }

func buildPostsAdminEnv(t *testing.T, svc PostAdminService, authz PermissionChecker) (http.Handler, *scs.SessionManager, *AuthMiddleware, accounts.User) {
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
		PostAdminSvc: svc,
		Authors:      users,
	})
	return r, sess, mw, user
}

func TestPostsAdmin_UnauthenticatedRedirectsToLogin(t *testing.T) {
	r, _, _, _ := buildPostsAdminEnv(t, stubPostAdmin{}, allowAllAuthz{})
	req := httptest.NewRequest(http.MethodGet, "/admin/posts", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unauth /admin/posts = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("redirect to %q, want /login", loc)
	}
}

func TestPostsAdmin_DeniedPermissionIs403(t *testing.T) {
	r, sess, mw, user := buildPostsAdminEnv(t, stubPostAdmin{}, denyAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/posts", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied /admin/posts = %d, want 403", rec.Code)
	}
}

func TestPostsAdmin_ListRendersForPermittedUser(t *testing.T) {
	svc := stubPostAdmin{
		list: []posts.Post{
			{ID: uuid.New(), Title: "My Post", Slug: "my-post", Status: kernel.StatusPublished, AuthorID: uuid.New()},
		},
		listTotal: 1,
	}
	r, sess, mw, user := buildPostsAdminEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/posts", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/admin/posts = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`data-testid="posts-table"`, "My Post", `data-testid="status-tabs"`} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing %q", want)
		}
	}
}

func TestPostsAdmin_NewRendersEditor(t *testing.T) {
	r, sess, mw, user := buildPostsAdminEnv(t, stubPostAdmin{}, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/posts/new", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/admin/posts/new = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="editor-toolbar"`) {
		t.Error("new editor missing toolbar")
	}
}

// TestPostsAdmin_CreateValidationError drives the handler directly (bypassing
// CSRF) to assert a missing title re-renders the editor with the field error.
func TestPostsAdmin_CreateValidationError(t *testing.T) {
	svc := stubPostAdmin{createErr: posts.ErrTitleRequired}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{role: accounts.Role{Label: "Editor"}}, csrf: security.Token, siteURL: "/"}
	h := NewPostAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{"title": {""}, "body": {"<p>x</p>"}, "status": {"DRAFT"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/posts", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New(), Name: "Ed"}))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("create validation = %d, want 200 re-render", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="error-title"`) && !strings.Contains(body, "Title is required") {
		t.Errorf("expected title field error in re-rendered editor:\n%s", body)
	}
}

// TestPostsAdmin_BulkUnknownActionRejected asserts the bulk handler's allow-list
// rejects an action that is not one of the five recognized verbs with 400,
// BEFORE any service call (a tampered form must never reach the service).
func TestPostsAdmin_BulkUnknownActionRejected(t *testing.T) {
	calls := []string{}
	svc := stubPostAdmin{bulkCalls: &calls}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewPostAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{"action": {"nuke"}, "ids": {uuid.New().String()}}
	req := httptest.NewRequest(http.MethodPost, "/admin/posts/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Bulk(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown bulk action = %d, want 400", rec.Code)
	}
	if len(calls) != 0 {
		t.Errorf("unknown action reached the service: %v", calls)
	}
}

// TestPostsAdmin_BulkDispatchesAllowedAction asserts an allow-listed verb reaches
// the matching service method and redirects back with a summary query.
func TestPostsAdmin_BulkDispatchesAllowedAction(t *testing.T) {
	calls := []string{}
	svc := stubPostAdmin{bulkCalls: &calls}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewPostAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{"action": {"trash"}, "ids": {uuid.New().String(), uuid.New().String()}}
	req := httptest.NewRequest(http.MethodPost, "/admin/posts/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Bulk(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("bulk trash = %d, want 303", rec.Code)
	}
	if len(calls) != 1 || calls[0] != "trash" {
		t.Fatalf("service bulk calls = %v, want [trash]", calls)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/admin/posts?") || !strings.Contains(loc, "bulk=trash") {
		t.Errorf("redirect = %q, want /admin/posts?...bulk=trash", loc)
	}
}

// TestPostsAdmin_CreateForbiddenIs403 asserts the service ownership/permission
// denial surfaces as 403 from the handler.
func TestPostsAdmin_CreateForbiddenIs403(t *testing.T) {
	svc := stubPostAdmin{createErr: posts.ErrForbidden}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewPostAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{"title": {"X"}, "body": {"y"}, "status": {"DRAFT"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/posts", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("forbidden create = %d, want 403", rec.Code)
	}
}

// withPostID installs a chi route context carrying the post id param plus an
// authenticated user, so an admin handler can be driven directly (bypassing the
// router's CSRF middleware) in a unit test.
func withPostID(req *http.Request, id uuid.UUID, user accounts.User) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id.String())
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = withUser(ctx, user)
	return req.WithContext(ctx)
}

// TestPostsAdmin_EditRendersLocaleTabs asserts the editor renders the per-locale
// tab strip, marking a locale that already has a translation (M7b-1).
func TestPostsAdmin_EditRendersLocaleTabs(t *testing.T) {
	id := uuid.New()
	user := accounts.User{ID: uuid.New(), Name: "Ed"}
	svc := stubPostAdmin{
		get:        posts.Post{ID: id, Title: "Hello", Slug: "hello", Status: kernel.StatusPublished, AuthorID: user.ID},
		translated: []i18n.Locale{i18n.LocaleDE},
	}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{role: accounts.Role{Label: "Editor"}}, csrf: security.Token, siteURL: "/"}
	h := NewPostAdminHandler(svc, shell, nil, security.Token)

	req := httptest.NewRequest(http.MethodGet, "/admin/posts/"+id.String()+"/edit", nil)
	req = withPostID(req, id, user)
	rec := httptest.NewRecorder()
	h.Edit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("edit = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="locale-tabs"`,
		`data-testid="locale-tab-en"`,
		`data-testid="locale-tab-de"`,
		`data-testid="locale-tab-ru"`,
		`data-testid="locale-dot-de"`, // de already translated
	} {
		if !strings.Contains(body, want) {
			t.Errorf("editor missing %q", want)
		}
	}
}

// TestPostsAdmin_EditDeTabLoadsTranslation asserts ?language=de selects the de
// tab (active + hidden locale field) and shows the shared-fields note.
func TestPostsAdmin_EditDeTabActive(t *testing.T) {
	id := uuid.New()
	user := accounts.User{ID: uuid.New(), Name: "Ed"}
	svc := stubPostAdmin{
		get:        posts.Post{ID: id, Title: "Hallo", Slug: "hello", Status: kernel.StatusPublished, AuthorID: user.ID},
		translated: []i18n.Locale{i18n.LocaleDE},
	}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{role: accounts.Role{Label: "Editor"}}, csrf: security.Token, siteURL: "/"}
	h := NewPostAdminHandler(svc, shell, nil, security.Token)

	req := httptest.NewRequest(http.MethodGet, "/admin/posts/"+id.String()+"/edit?language=de", nil)
	req = withPostID(req, id, user)
	rec := httptest.NewRecorder()
	h.Edit(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `name="locale" value="de"`) {
		t.Error("de tab did not set the hidden active-locale field to de")
	}
	if !strings.Contains(body, `data-testid="translation-note"`) {
		t.Error("de tab missing the shared-fields note")
	}
	if strings.Contains(body, `data-testid="field-status"`) {
		t.Error("de tab should hide the shared status field")
	}
}

// TestPostsAdmin_UpdateSavesDeTranslation asserts a POST carrying a non-default
// locale routes to SaveTranslation with that locale + content, then redirects
// back to the de tab (M7b-1 persistence).
func TestPostsAdmin_UpdateSavesDeTranslation(t *testing.T) {
	id := uuid.New()
	user := accounts.User{ID: uuid.New(), Name: "Ed"}
	saved := savedTranslation{}
	svc := stubPostAdmin{
		get:              posts.Post{ID: id, Title: "Hello", AuthorID: user.ID},
		savedTranslation: &saved,
	}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{role: accounts.Role{Label: "Editor"}}, csrf: security.Token, siteURL: "/"}
	h := NewPostAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{
		"title":  {"Hallo Welt"},
		"body":   {"<p>Deutscher Text</p>"},
		"locale": {"de"},
		"action": {"save"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/posts/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withPostID(req, id, user)
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update de = %d, want 303\n%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/posts/"+id.String()+"/edit?language=de" {
		t.Errorf("redirect = %q, want de tab", loc)
	}
	if saved.locale != i18n.LocaleDE {
		t.Errorf("SaveTranslation locale = %v, want de", saved.locale)
	}
	if saved.in.Title != "Hallo Welt" || saved.in.Body != "<p>Deutscher Text</p>" {
		t.Errorf("SaveTranslation content = %+v", saved.in)
	}
}

// TestPostsAdmin_UpdateEnEditsBaseRow asserts an en (no/default locale) POST
// takes the unchanged base Update path, NOT SaveTranslation (existing behavior).
func TestPostsAdmin_UpdateEnEditsBaseRow(t *testing.T) {
	id := uuid.New()
	user := accounts.User{ID: uuid.New(), Name: "Ed"}
	saved := savedTranslation{}
	svc := stubPostAdmin{
		get:              posts.Post{ID: id, Title: "Hello", AuthorID: user.ID},
		savedTranslation: &saved,
	}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{role: accounts.Role{Label: "Editor"}}, csrf: security.Token, siteURL: "/"}
	h := NewPostAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{
		"title":  {"Hello Base"},
		"body":   {"<p>en</p>"},
		"locale": {"en"}, // default -> base path
		"status": {"DRAFT"},
		"action": {"save"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/posts/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = withPostID(req, id, user)
	rec := httptest.NewRecorder()
	h.Update(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update en = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/admin/posts/"+id.String()+"/edit" {
		t.Errorf("en redirect = %q, want base edit (no ?language)", loc)
	}
	if saved.locale != "" {
		t.Errorf("en save must NOT call SaveTranslation, got locale %v", saved.locale)
	}
}
