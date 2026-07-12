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

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/content/comments"
	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/health"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/config"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/security"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/session"
)

// stubCommentAdmin is a controllable CommentsAdminService.
type stubCommentAdmin struct {
	list     []comments.Comment
	total    int
	counts   map[comments.Status]int
	approved *[]uuid.UUID
	spammed  *[]uuid.UUID
	trashed  *[]uuid.UUID
	deleted  *[]uuid.UUID
	bulk     *[]string
}

func (s stubCommentAdmin) AdminList(context.Context, uuid.UUID, comments.ModerationFilter) ([]comments.Comment, int, error) {
	return s.list, s.total, nil
}

func (s stubCommentAdmin) StatusCounts(context.Context, uuid.UUID) (map[comments.Status]int, error) {
	if s.counts == nil {
		return map[comments.Status]int{}, nil
	}
	return s.counts, nil
}

func (s stubCommentAdmin) Approve(_ context.Context, _, id uuid.UUID) (comments.Comment, error) {
	if s.approved != nil {
		*s.approved = append(*s.approved, id)
	}
	return comments.Comment{ID: id, Status: comments.StatusApproved}, nil
}

func (s stubCommentAdmin) Spam(_ context.Context, _, id uuid.UUID) (comments.Comment, error) {
	if s.spammed != nil {
		*s.spammed = append(*s.spammed, id)
	}
	return comments.Comment{ID: id, Status: comments.StatusSpam}, nil
}

func (s stubCommentAdmin) Trash(_ context.Context, _, id uuid.UUID) (comments.Comment, error) {
	if s.trashed != nil {
		*s.trashed = append(*s.trashed, id)
	}
	return comments.Comment{ID: id, Status: comments.StatusTrash}, nil
}

func (s stubCommentAdmin) Delete(_ context.Context, _, id uuid.UUID) error {
	if s.deleted != nil {
		*s.deleted = append(*s.deleted, id)
	}
	return nil
}

func (s stubCommentAdmin) record(verb string) {
	if s.bulk != nil {
		*s.bulk = append(*s.bulk, verb)
	}
}

func (s stubCommentAdmin) BulkApprove(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error) {
	s.record("approve")
	return bulkApplied(ids), nil
}

func (s stubCommentAdmin) BulkSpam(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error) {
	s.record("spam")
	return bulkApplied(ids), nil
}

func (s stubCommentAdmin) BulkTrash(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error) {
	s.record("trash")
	return bulkApplied(ids), nil
}

func (s stubCommentAdmin) BulkDelete(_ context.Context, _ uuid.UUID, ids []uuid.UUID) (kernel.BulkResult, error) {
	s.record("delete")
	return bulkApplied(ids), nil
}

// bulkApplied builds a BulkResult marking every id applied (via the exported
// RunBulk driver, so the test does not reach into kernel internals).
func bulkApplied(ids []uuid.UUID) kernel.BulkResult {
	res, _ := kernel.RunBulk(ids, func(uuid.UUID) error { return nil }, func(error) (bool, bool, bool) {
		return false, false, true
	})
	return res
}

func buildCommentsAdminEnv(t *testing.T, svc CommentsAdminService, authz PermissionChecker) (http.Handler, *scs.SessionManager, *AuthMiddleware, accounts.User) {
	t.Helper()
	user := accounts.User{ID: uuid.New(), Email: "mod@example.com", Name: "Mod", PasswordChangedAt: time.Now()}
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
		CommentAdminSvc: svc,
		Authors:         users,
	})
	return r, sess, mw, user
}

func modComment(status comments.Status, name, body string) comments.Comment {
	return comments.Comment{
		ID:         uuid.New(),
		PostID:     uuid.New(),
		AuthorName: name,
		Body:       body,
		Status:     status,
		CreatedAt:  time.Now(),
	}
}

func TestCommentsAdmin_DeniedPermissionIs403(t *testing.T) {
	r, sess, mw, user := buildCommentsAdminEnv(t, stubCommentAdmin{}, denyAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/comments", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied = %d, want 403", rec.Code)
	}
}

func TestCommentsAdmin_UnauthenticatedRedirects(t *testing.T) {
	r, _, _, _ := buildCommentsAdminEnv(t, stubCommentAdmin{}, allowAllAuthz{})
	req := httptest.NewRequest(http.MethodGet, "/admin/comments", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unauth = %d, want 303", rec.Code)
	}
}

func TestCommentsAdmin_ListRendersTabsAndRows(t *testing.T) {
	svc := stubCommentAdmin{
		list:   []comments.Comment{modComment(comments.StatusPending, "Alice", "please approve me")},
		total:  1,
		counts: map[comments.Status]int{comments.StatusPending: 3, comments.StatusApproved: 5},
	}
	r, sess, mw, user := buildCommentsAdminEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/comments", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="comments-table"`,
		`data-testid="comment-tabs"`,
		`data-testid="comment-tab-PENDING"`,
		`data-testid="comments-pending-badge"`,
		"Alice",
		"please approve me",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing %q", want)
		}
	}
}

func TestCommentsAdmin_EmptyState(t *testing.T) {
	r, sess, mw, user := buildCommentsAdminEnv(t, stubCommentAdmin{}, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/comments?status=SPAM", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `data-testid="comments-empty"`) {
		t.Error("expected empty state")
	}
}

func TestCommentsAdmin_ApproveRedirects(t *testing.T) {
	id := uuid.New()
	approved := []uuid.UUID{}
	svc := stubCommentAdmin{approved: &approved}
	h := NewCommentsAdminHandler(svc, adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}, nil, security.Token)

	req := httptest.NewRequest(http.MethodPost, "/admin/comments/"+id.String()+"/approve", nil)
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()
	h.Approve(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("approve = %d, want 303", rec.Code)
	}
	if len(approved) != 1 || approved[0] != id {
		t.Errorf("service Approve not called with id: %v", approved)
	}
}

func TestCommentsAdmin_SpamAndTrash(t *testing.T) {
	id := uuid.New()
	spammed, trashed := []uuid.UUID{}, []uuid.UUID{}
	svc := stubCommentAdmin{spammed: &spammed, trashed: &trashed}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewCommentsAdminHandler(svc, shell, nil, security.Token)

	for _, tc := range []struct {
		name string
		fn   func(http.ResponseWriter, *http.Request)
		got  *[]uuid.UUID
	}{
		{"spam", h.Spam, &spammed},
		{"trash", h.Trash, &trashed},
	} {
		req := httptest.NewRequest(http.MethodPost, "/admin/comments/"+id.String()+"/"+tc.name, nil)
		req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
		req = withChiParam(req, "id", id.String())
		rec := httptest.NewRecorder()
		tc.fn(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("%s = %d, want 303\n%s", tc.name, rec.Code, rec.Body.String())
		}
		if len(*tc.got) != 1 || (*tc.got)[0] != id {
			t.Errorf("%s did not reach service with id: %v", tc.name, *tc.got)
		}
	}
}

func TestCommentsAdmin_BulkApprove(t *testing.T) {
	bulk := []string{}
	svc := stubCommentAdmin{bulk: &bulk}
	h := NewCommentsAdminHandler(svc, adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}, nil, security.Token)

	id1, id2 := uuid.New(), uuid.New()
	form := url.Values{"action": {"approve"}, "ids": {id1.String(), id2.String()}}
	req := httptest.NewRequest(http.MethodPost, "/admin/comments/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Bulk(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("bulk = %d, want 303\n%s", rec.Code, rec.Body.String())
	}
	if len(bulk) != 1 || bulk[0] != "approve" {
		t.Errorf("bulk verbs = %v, want [approve]", bulk)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "applied=2") {
		t.Errorf("redirect = %q, want applied=2", loc)
	}
}

func TestCommentsAdmin_BulkUnknownActionRejected(t *testing.T) {
	h := NewCommentsAdminHandler(stubCommentAdmin{}, adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}, nil, security.Token)
	form := url.Values{"action": {"nuke"}, "ids": {uuid.New().String()}}
	req := httptest.NewRequest(http.MethodPost, "/admin/comments/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Bulk(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown bulk = %d, want 400", rec.Code)
	}
}
