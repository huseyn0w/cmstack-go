package web

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/content/media"
	"github.com/huseyn0w/agentic-cms-go/internal/health"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/config"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/security"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/session"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/storage"
)

// stubMediaAdmin is a controllable MediaAdminService.
type stubMediaAdmin struct {
	list        []media.Media
	listTotal   int
	get         media.Media
	getErr      error
	uploadErr   error
	uploaded    media.Media
	uploadCalls *int
	updateErr   error
	updated     media.Media
	deleteErr   error
	deleteCalls *[]uuid.UUID
}

func (s stubMediaAdmin) List(context.Context, uuid.UUID, int, int) ([]media.Media, int, error) {
	return s.list, s.listTotal, nil
}

func (s stubMediaAdmin) Get(context.Context, uuid.UUID, uuid.UUID) (media.Media, error) {
	return s.get, s.getErr
}

func (s stubMediaAdmin) Upload(_ context.Context, _ uuid.UUID, _ media.UploadInput) (media.Media, error) {
	if s.uploadCalls != nil {
		*s.uploadCalls++
	}
	return s.uploaded, s.uploadErr
}

func (s stubMediaAdmin) UpdateMetadata(_ context.Context, _, _ uuid.UUID, _, _, _ string) (media.Media, error) {
	return s.updated, s.updateErr
}

func (s stubMediaAdmin) Delete(_ context.Context, _, id uuid.UUID) error {
	if s.deleteCalls != nil {
		*s.deleteCalls = append(*s.deleteCalls, id)
	}
	return s.deleteErr
}
func (s stubMediaAdmin) URL(key string) string { return "/uploads/" + key }
func (s stubMediaAdmin) MaxUploadBytes() int64 { return 10 << 20 }

func ptrInt(v int) *int { return &v }

func imageMedia(id uuid.UUID, title string) media.Media {
	return media.Media{
		ID: id, StorageKey: "media/" + id.String() + ".png", OriginalFilename: "p.png",
		MIME: "image/png", SizeBytes: 2048, Width: ptrInt(800), Height: ptrInt(600), Title: title,
		Thumbnails: []media.Thumbnail{{Variant: "thumb", StorageKey: "media/thumb-" + id.String() + ".png", Width: 320, Height: 240}},
		CreatedAt:  time.Now(),
	}
}

func buildMediaEnv(t *testing.T, svc MediaAdminService, authz PermissionChecker) (http.Handler, *scs.SessionManager, *AuthMiddleware, accounts.User) {
	t.Helper()
	user := accounts.User{ID: uuid.New(), Email: "ed@example.com", Name: "Ed", PasswordChangedAt: time.Now()}
	sess := session.NewManager(false)
	users := fakeUsers{users: map[uuid.UUID]accounts.User{user.ID: user}}
	mw := NewAuthMiddleware(sess, users, authz)
	authH := accounts.NewHandler(stubAuthService{}, mw, security.Token, accounts.NewValidator())
	r := Router(Deps{
		Config:        config.Config{AppEnv: "test", BaseURL: "https://site.test"},
		Health:        health.NewHandler(health.NewService(nil)),
		Session:       sess,
		Auth:          authH,
		AuthMW:        mw,
		CSRFFunc:      security.Token,
		Authz:         authz,
		Roles:         fakeRoles{role: accounts.Role{Key: "editor", Label: "Editor"}},
		MediaAdminSvc: svc,
		Authors:       users,
	})
	return r, sess, mw, user
}

func TestMediaAdmin_UnauthenticatedRedirects(t *testing.T) {
	r, _, _, _ := buildMediaEnv(t, stubMediaAdmin{}, allowAllAuthz{})
	req := httptest.NewRequest(http.MethodGet, "/admin/media", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("unauth = %d, want 303", rec.Code)
	}
}

func TestMediaAdmin_DeniedPermissionIs403(t *testing.T) {
	r, sess, mw, user := buildMediaEnv(t, stubMediaAdmin{}, denyAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/media", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied = %d, want 403", rec.Code)
	}
}

func TestMediaAdmin_ListRendersGridAndDropzone(t *testing.T) {
	svc := stubMediaAdmin{list: []media.Media{imageMedia(uuid.New(), "Sunset")}, listTotal: 1}
	r, sess, mw, user := buildMediaEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/media", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/admin/media = %d\n%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`data-testid="media-grid"`, `data-testid="media-dropzone"`, "Sunset", `data-testid="media-file-input"`} {
		if !strings.Contains(body, want) {
			t.Errorf("list missing %q", want)
		}
	}
}

func TestMediaAdmin_ListEmptyState(t *testing.T) {
	r, sess, mw, user := buildMediaEnv(t, stubMediaAdmin{}, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/media", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `data-testid="media-empty"`) {
		t.Error("expected empty state")
	}
}

// uploadRequest builds a multipart upload POST with one file part.
func uploadRequest(t *testing.T, field, filename string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	_, _ = part.Write(content)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/admin/media", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestMediaAdmin_UploadHappyXHRReturns201(t *testing.T) {
	calls := 0
	svc := stubMediaAdmin{uploadCalls: &calls, uploaded: imageMedia(uuid.New(), "")}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewMediaAdminHandler(svc, shell, security.Token)

	req := uploadRequest(t, "file", "photo.png", []byte("PNGBYTES"))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Upload(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("upload = %d, want 201\n%s", rec.Code, rec.Body.String())
	}
	if calls != 1 {
		t.Errorf("service Upload called %d times, want 1", calls)
	}
}

func TestMediaAdmin_UploadInvalidTypeIs422(t *testing.T) {
	svc := stubMediaAdmin{uploadErr: storage.ErrMediaType}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewMediaAdminHandler(svc, shell, security.Token)

	req := uploadRequest(t, "file", "evil.svg", []byte("<svg/>"))
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Upload(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid upload = %d, want 422", rec.Code)
	}
}

func TestMediaAdmin_UploadNoFileIs422(t *testing.T) {
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewMediaAdminHandler(stubMediaAdmin{}, shell, security.Token)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("csrf_token", "x")
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/admin/media", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Upload(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("no-file upload = %d, want 422", rec.Code)
	}
}

func TestMediaAdmin_DetailRendersMetadataForm(t *testing.T) {
	id := uuid.New()
	svc := stubMediaAdmin{get: imageMedia(id, "Sunset")}
	r, sess, mw, user := buildMediaEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/media/"+id.String()+"/detail", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`data-testid="media-field-alt"`, `data-testid="media-field-title"`, `data-testid="media-field-caption"`} {
		if !strings.Contains(body, want) {
			t.Errorf("detail missing %q", want)
		}
	}
}

// withChiParam injects a chi route param so a handler can be driven directly
// (bypassing the router + CSRF) while still resolving chi.URLParam.
func withChiParam(req *http.Request, key, val string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, val)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestMediaAdmin_UpdateMetadataRedirects(t *testing.T) {
	id := uuid.New()
	svc := stubMediaAdmin{updated: imageMedia(id, "New")}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewMediaAdminHandler(svc, shell, security.Token)

	form := url.Values{"alt": {"alt"}, "title": {"New"}, "caption": {"cap"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/media/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()
	h.UpdateMetadata(rec, req)
	// Non-XHR metadata save redirects back to the library.
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("update metadata = %d, want 303\n%s", rec.Code, rec.Body.String())
	}
}

func TestMediaAdmin_UpdateMetadataXHRRendersPanel(t *testing.T) {
	id := uuid.New()
	svc := stubMediaAdmin{updated: imageMedia(id, "New")}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewMediaAdminHandler(svc, shell, security.Token)

	form := url.Values{"alt": {"alt"}, "title": {"New"}, "caption": {"cap"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/media/"+id.String(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()
	h.UpdateMetadata(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("xhr update = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="media-saved"`) {
		t.Error("xhr update should re-render the panel with a saved status")
	}
}

func TestMediaAdmin_DeleteCallsServiceAndRedirects(t *testing.T) {
	id := uuid.New()
	deletes := []uuid.UUID{}
	svc := stubMediaAdmin{deleteCalls: &deletes}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewMediaAdminHandler(svc, shell, security.Token)

	req := httptest.NewRequest(http.MethodPost, "/admin/media/"+id.String()+"/delete", nil)
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	req = withChiParam(req, "id", id.String())
	rec := httptest.NewRecorder()
	h.Delete(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("delete = %d, want 303\n%s", rec.Code, rec.Body.String())
	}
	if len(deletes) != 1 || deletes[0] != id {
		t.Errorf("service Delete not called with id: %v", deletes)
	}
}

func TestMediaAdmin_BulkDeleteCounts(t *testing.T) {
	deletes := []uuid.UUID{}
	svc := stubMediaAdmin{deleteCalls: &deletes}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewMediaAdminHandler(svc, shell, security.Token)

	id1, id2 := uuid.New(), uuid.New()
	form := url.Values{"action": {"delete"}, "ids": {id1.String(), id2.String()}}
	req := httptest.NewRequest(http.MethodPost, "/admin/media/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Bulk(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("bulk = %d, want 303", rec.Code)
	}
	if len(deletes) != 2 {
		t.Errorf("expected 2 deletes, got %d", len(deletes))
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "applied=2") {
		t.Errorf("redirect summary = %q, want applied=2", loc)
	}
}

func TestMediaAdmin_BulkUnknownActionRejected(t *testing.T) {
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewMediaAdminHandler(stubMediaAdmin{}, shell, security.Token)
	form := url.Values{"action": {"nuke"}, "ids": {uuid.New().String()}}
	req := httptest.NewRequest(http.MethodPost, "/admin/media/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Bulk(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown bulk = %d, want 400", rec.Code)
	}
}

func TestMediaAdmin_PickerRendersOnlyImages(t *testing.T) {
	img := imageMedia(uuid.New(), "Pic")
	doc := media.Media{ID: uuid.New(), MIME: "application/pdf", StorageKey: "media/doc.pdf", CreatedAt: time.Now()}
	svc := stubMediaAdmin{list: []media.Media{img, doc}, listTotal: 2}
	r, sess, mw, user := buildMediaEnv(t, svc, allowAllAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/media/picker", nil)
	req.AddCookie(cookie)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("picker = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="media-picker-grid"`) {
		t.Error("picker missing grid")
	}
	// The image is offered; the PDF is excluded (no media-pick button for it).
	if !strings.Contains(body, "media-pick-"+img.ID.String()) {
		t.Error("picker should offer the image")
	}
	if strings.Contains(body, "media-pick-"+doc.ID.String()) {
		t.Error("picker must NOT offer the PDF as an insertable image")
	}
}

func TestMediaAdmin_PickerGatedByRead(t *testing.T) {
	r, sess, mw, user := buildMediaEnv(t, stubMediaAdmin{}, denyAuthz{})
	cookie := mintSession(t, sess, mw, user)
	req := httptest.NewRequest(http.MethodGet, "/admin/media/picker", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("picker denied = %d, want 403", rec.Code)
	}
}
