package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/comments"
	"github.com/huseyn0w/cmstack-go/internal/platform/security"
)

// stubCommentPublic is a controllable CommentsPublicService.
type stubCommentPublic struct {
	thread    []comments.PublicComment
	total     int
	submitErr error
	submitted *comments.SubmitInput
	editErr   error
	deleteErr error
}

func (s *stubCommentPublic) PublicThread(context.Context, string, *comments.Viewer) ([]comments.PublicComment, int, error) {
	return s.thread, s.total, nil
}

func (s *stubCommentPublic) Submit(_ context.Context, in comments.SubmitInput) (comments.Comment, error) {
	cp := in
	s.submitted = &cp
	if s.submitErr != nil {
		return comments.Comment{}, s.submitErr
	}
	return comments.Comment{ID: uuid.New(), Status: comments.StatusPending}, nil
}

func (s *stubCommentPublic) SelfEdit(context.Context, comments.Viewer, uuid.UUID, string) (comments.Comment, error) {
	if s.editErr != nil {
		return comments.Comment{}, s.editErr
	}
	return comments.Comment{ID: uuid.New()}, nil
}

func (s *stubCommentPublic) SelfDelete(context.Context, comments.Viewer, uuid.UUID) error {
	return s.deleteErr
}

func commentPublicHandler(svc CommentsPublicService) *CommentsPublicHandler {
	return NewCommentsPublicHandler(svc, security.Token, "site-key")
}

// withChiParams injects several chi route params in one route context (the
// single-key withChiParam replaces the context on each call).
func withChiParams(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// submitReq builds a POST /blog/{slug}/comments with a chi slug param.
func submitReq(t *testing.T, slug string, form url.Values) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/blog/"+slug+"/comments", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.7:5555"
	return withChiParam(req, "slug", slug)
}

func TestCommentsPublic_SubmitHappyRerendersThread(t *testing.T) {
	svc := &stubCommentPublic{total: 1}
	h := commentPublicHandler(svc)
	form := url.Values{"name": {"Guest"}, "email": {"g@x.com"}, "body": {"nice post"}}
	req := submitReq(t, "hello", form)
	rec := httptest.NewRecorder()
	h.Submit(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("submit = %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	if svc.submitted == nil || svc.submitted.Body != "nice post" {
		t.Fatalf("service Submit not called with body: %+v", svc.submitted)
	}
	if svc.submitted.ClientIP != "203.0.113.7" {
		t.Errorf("client IP = %q, want honest RemoteAddr host", svc.submitted.ClientIP)
	}
}

func TestCommentsPublic_SubmitInvalidIs422(t *testing.T) {
	svc := &stubCommentPublic{submitErr: comments.ErrValidation}
	h := commentPublicHandler(svc)
	form := url.Values{"name": {""}, "email": {""}, "body": {""}}
	req := submitReq(t, "hello", form)
	rec := httptest.NewRecorder()
	h.Submit(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid submit = %d, want 422", rec.Code)
	}
}

func TestCommentsPublic_SubmitRateLimitedIs429(t *testing.T) {
	svc := &stubCommentPublic{submitErr: comments.ErrRateLimited}
	h := commentPublicHandler(svc)
	form := url.Values{"name": {"G"}, "email": {"g@x.com"}, "body": {"hi"}}
	req := submitReq(t, "hello", form)
	rec := httptest.NewRecorder()
	h.Submit(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited submit = %d, want 429", rec.Code)
	}
}

func TestCommentsPublic_SelfEditGuestForbidden(t *testing.T) {
	// A guest (no user in context) cannot self-edit.
	h := commentPublicHandler(&stubCommentPublic{})
	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/blog/hello/comments/"+id.String()+"/edit", nil)
	req = withChiParam(req, "slug", "hello")
	rec := httptest.NewRecorder()
	h.SelfEdit(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("guest self-edit = %d, want 403", rec.Code)
	}
}

func TestCommentsPublic_SelfEditWindowExpiredIs403(t *testing.T) {
	svc := &stubCommentPublic{editErr: comments.ErrEditWindowExpired}
	h := commentPublicHandler(svc)
	id := uuid.New()
	form := url.Values{"body": {"updated"}}
	req := httptest.NewRequest(http.MethodPost, "/blog/hello/comments/"+id.String()+"/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New(), Email: "u@x.com", Name: "U", PasswordChangedAt: time.Now()}))
	req = withChiParams(req, map[string]string{"slug": "hello", "id": id.String()})
	rec := httptest.NewRecorder()
	h.SelfEdit(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expired self-edit = %d, want 403", rec.Code)
	}
}
