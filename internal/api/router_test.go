package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/pages"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
	"github.com/huseyn0w/cmstack-go/internal/web"
)

// --- fakes for the web.AuthMiddleware dependencies --------------------------

type fakeSession struct{ vals map[string]string }

func (f fakeSession) GetString(_ context.Context, k string) string { return f.vals[k] }
func (f fakeSession) Put(_ context.Context, k string, v interface{}) {
	f.vals[k] = v.(string)
}
func (f fakeSession) Remove(_ context.Context, k string) { delete(f.vals, k) }
func (f fakeSession) RenewToken(_ context.Context) error { return nil }

type fakeUsers struct{ users map[uuid.UUID]accounts.User }

func (f fakeUsers) GetByID(_ context.Context, id uuid.UUID) (accounts.User, error) {
	if u, ok := f.users[id]; ok {
		return u, nil
	}
	return accounts.User{}, accounts.ErrNotFound
}

type fakeAuthz struct{ allow map[string]bool }

func (f fakeAuthz) Can(_ context.Context, _ uuid.UUID, action, subject string) bool {
	return f.allow[action+":"+subject]
}

type fakeVerifier struct {
	token  string
	userID uuid.UUID
}

func (f fakeVerifier) Verify(_ context.Context, plaintext string) (uuid.UUID, bool, error) {
	if plaintext == f.token {
		return f.userID, true, nil
	}
	return uuid.Nil, false, nil
}

// --- fake content readers ---------------------------------------------------

type fakePosts struct {
	list     []posts.Post
	total    int
	gotF     posts.ListFilter
	byID     map[uuid.UUID]posts.Post
	getErr   error
	getActor uuid.UUID

	// write-path recording (M17-2).
	createActor uuid.UUID
	createIn    posts.CreateInput
	createOut   posts.Post
	updateActor uuid.UUID
	updateID    uuid.UUID
	updateIn    posts.UpdateInput
	published   []uuid.UUID
	unpublished []uuid.UUID
	trashed     []uuid.UUID
	restored    []uuid.UUID
	revs        []kernel.Revision
	writeErr    error
}

func (f *fakePosts) AdminList(_ context.Context, ff posts.ListFilter) ([]posts.Post, int, error) {
	f.gotF = ff
	return f.list, f.total, nil
}

func (f *fakePosts) Get(_ context.Context, actorID, id uuid.UUID) (posts.Post, error) {
	f.getActor = actorID
	if f.getErr != nil {
		return posts.Post{}, f.getErr
	}
	if p, ok := f.byID[id]; ok {
		return p, nil
	}
	return posts.Post{}, posts.ErrNotFound
}

func (f *fakePosts) Revisions(_ context.Context, _, _ uuid.UUID) ([]kernel.Revision, error) {
	if f.writeErr != nil {
		return nil, f.writeErr
	}
	return f.revs, nil
}

func (f *fakePosts) Create(_ context.Context, actorID uuid.UUID, in posts.CreateInput) (posts.Post, error) {
	f.createActor = actorID
	f.createIn = in
	if f.writeErr != nil {
		return posts.Post{}, f.writeErr
	}
	return f.createOut, nil
}

func (f *fakePosts) Update(_ context.Context, actorID, id uuid.UUID, in posts.UpdateInput) (posts.Post, error) {
	f.updateActor = actorID
	f.updateID = id
	f.updateIn = in
	if f.writeErr != nil {
		return posts.Post{}, f.writeErr
	}
	if p, ok := f.byID[id]; ok {
		return p, nil
	}
	return f.createOut, nil
}

func (f *fakePosts) Publish(_ context.Context, _, id uuid.UUID) (posts.Post, error) {
	f.published = append(f.published, id)
	if f.writeErr != nil {
		return posts.Post{}, f.writeErr
	}
	return f.byID[id], nil
}

func (f *fakePosts) Unpublish(_ context.Context, _, id uuid.UUID) (posts.Post, error) {
	f.unpublished = append(f.unpublished, id)
	if f.writeErr != nil {
		return posts.Post{}, f.writeErr
	}
	return f.byID[id], nil
}

func (f *fakePosts) Trash(_ context.Context, _, id uuid.UUID) error {
	f.trashed = append(f.trashed, id)
	return f.writeErr
}

func (f *fakePosts) Restore(_ context.Context, _, id uuid.UUID) error {
	f.restored = append(f.restored, id)
	return f.writeErr
}

type fakePages struct {
	list  []pages.Page
	total int
	byID  map[uuid.UUID]pages.Page

	createActor uuid.UUID
	createIn    pages.CreateInput
	createOut   pages.Page
	updateID    uuid.UUID
	updateIn    pages.UpdateInput
	published   []uuid.UUID
	unpublished []uuid.UUID
	trashed     []uuid.UUID
	restored    []uuid.UUID
	writeErr    error
}

func (f *fakePages) AdminList(_ context.Context, _ pages.ListFilter) ([]pages.Page, int, error) {
	return f.list, f.total, nil
}

func (f *fakePages) Get(_ context.Context, _, id uuid.UUID) (pages.Page, error) {
	if p, ok := f.byID[id]; ok {
		return p, nil
	}
	return pages.Page{}, pages.ErrNotFound
}

func (f *fakePages) Create(_ context.Context, actorID uuid.UUID, in pages.CreateInput) (pages.Page, error) {
	f.createActor = actorID
	f.createIn = in
	if f.writeErr != nil {
		return pages.Page{}, f.writeErr
	}
	return f.createOut, nil
}

func (f *fakePages) Update(_ context.Context, _, id uuid.UUID, in pages.UpdateInput) (pages.Page, error) {
	f.updateID = id
	f.updateIn = in
	if f.writeErr != nil {
		return pages.Page{}, f.writeErr
	}
	if p, ok := f.byID[id]; ok {
		return p, nil
	}
	return f.createOut, nil
}

func (f *fakePages) Publish(_ context.Context, _, id uuid.UUID) (pages.Page, error) {
	f.published = append(f.published, id)
	if f.writeErr != nil {
		return pages.Page{}, f.writeErr
	}
	return f.byID[id], nil
}

func (f *fakePages) Unpublish(_ context.Context, _, id uuid.UUID) (pages.Page, error) {
	f.unpublished = append(f.unpublished, id)
	if f.writeErr != nil {
		return pages.Page{}, f.writeErr
	}
	return f.byID[id], nil
}

func (f *fakePages) Trash(_ context.Context, _, id uuid.UUID) error {
	f.trashed = append(f.trashed, id)
	return f.writeErr
}

func (f *fakePages) Restore(_ context.Context, _, id uuid.UUID) error {
	f.restored = append(f.restored, id)
	return f.writeErr
}

// --- harness ----------------------------------------------------------------

const testToken = "cmg_test"

// newServer mounts the /api/v1 group behind a real web.AuthMiddleware. The
// bearer token maps to userID; grants controls which (action:subject) the user
// holds ("read:post" etc.).
func newServer(t *testing.T, userID uuid.UUID, grants map[string]bool, p PostService, pg PageService) http.Handler {
	t.Helper()
	return newServerDeps(t, userID, grants, Deps{Posts: p, Pages: pg})
}

// newServerDeps mounts the /api/v1 group with the supplied service deps behind a
// real web.AuthMiddleware, so each resource test injects only the fakes it needs.
func newServerDeps(t *testing.T, userID uuid.UUID, grants map[string]bool, d Deps) http.Handler {
	t.Helper()
	users := fakeUsers{users: map[uuid.UUID]accounts.User{userID: {ID: userID, Email: "a@b.com"}}}
	mw := web.NewAuthMiddleware(fakeSession{vals: map[string]string{}}, users, fakeAuthz{allow: grants})
	d.Auth = mw
	d.TokenAuth = mw.APITokenAuth(fakeVerifier{token: testToken, userID: userID})

	r := chi.NewRouter()
	Mount(r, d)
	return r
}

func authGet(path string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	return req
}

// authJSON builds an authenticated request carrying a raw JSON body for a write
// verb (POST/PATCH/DELETE). body may be "" for a bodiless action.
func authJSON(method, path, body string) *http.Request {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return out
}

// --- tests ------------------------------------------------------------------

func TestListPostsEnvelopeAndPagination(t *testing.T) {
	userID := uuid.New()
	authorID := uuid.New()
	pub := time.Now().UTC()
	fp := &fakePosts{
		total: 42,
		list: []posts.Post{
			{
				ID: uuid.New(), Title: "Hello", Slug: "hello", Excerpt: "hi", Body: "SECRET-BODY",
				Status: kernel.StatusPublished, AuthorID: authorID, PublishedAt: &pub, UpdatedAt: pub,
			},
		},
	}
	srv := newServer(t, userID, map[string]bool{"read:post": true}, fp, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/posts?page=2&perPage=5&status=PUBLISHED"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	// Pagination math: page 2, perPage 5 => limit 5, offset 5.
	if fp.gotF.Limit != 5 || fp.gotF.Offset != 5 {
		t.Errorf("filter limit/offset = %d/%d, want 5/5", fp.gotF.Limit, fp.gotF.Offset)
	}
	if fp.gotF.Status == nil || *fp.gotF.Status != kernel.StatusPublished {
		t.Errorf("status filter not mapped: %v", fp.gotF.Status)
	}

	body := decode(t, rec)
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing data envelope: %v", body)
	}
	if data["total"].(float64) != 42 || data["page"].(float64) != 2 || data["perPage"].(float64) != 5 {
		t.Errorf("pagination fields wrong: %v", data)
	}
	items := data["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	item := items[0].(map[string]any)
	if item["title"] != "Hello" || item["slug"] != "hello" || item["status"] != "PUBLISHED" {
		t.Errorf("dto fields wrong: %v", item)
	}
	// List DTO must NOT include the body (omitempty) or any sensitive field.
	if _, hasBody := item["body"]; hasBody {
		t.Error("list DTO leaked body")
	}
	for _, forbidden := range []string{"likeCount", "readingTime", "deletedAt", "noIndex", "canonicalURL"} {
		if _, bad := item[forbidden]; bad {
			t.Errorf("DTO leaked internal field %q", forbidden)
		}
	}
}

func TestPerPageCappedAt100(t *testing.T) {
	userID := uuid.New()
	fp := &fakePosts{}
	srv := newServer(t, userID, map[string]bool{"read:post": true}, fp, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/posts?perPage=1000"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if fp.gotF.Limit != 100 {
		t.Errorf("perPage cap: limit = %d, want 100", fp.gotF.Limit)
	}
}

func TestGetPostDetailIncludesBody(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fp := &fakePosts{byID: map[uuid.UUID]posts.Post{
		id: {ID: id, Title: "T", Slug: "t", Body: "FULL BODY", Status: kernel.StatusDraft, AuthorID: userID},
	}}
	srv := newServer(t, userID, map[string]bool{"read:post": true}, fp, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/posts/"+id.String()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// The actor id passed to Get must be the token user.
	if fp.getActor != userID {
		t.Errorf("actor = %v, want token user %v", fp.getActor, userID)
	}
	data := decode(t, rec)["data"].(map[string]any)
	if data["body"] != "FULL BODY" {
		t.Errorf("detail DTO missing body: %v", data)
	}
}

func TestGetPostNotFound(t *testing.T) {
	userID := uuid.New()
	fp := &fakePosts{getErr: posts.ErrNotFound}
	srv := newServer(t, userID, map[string]bool{"read:post": true}, fp, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/posts/"+uuid.New().String()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	errObj := decode(t, rec)["error"].(map[string]any)
	if errObj["code"] != "not_found" {
		t.Errorf("error code = %v, want not_found", errObj["code"])
	}
}

func TestGetPostInvalidID(t *testing.T) {
	userID := uuid.New()
	srv := newServer(t, userID, map[string]bool{"read:post": true}, &fakePosts{}, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/posts/not-a-uuid"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestForbiddenWithoutPermission(t *testing.T) {
	userID := uuid.New()
	// User authenticates (valid token) but holds NO read:post grant.
	srv := newServer(t, userID, map[string]bool{}, &fakePosts{}, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/posts"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestUnauthenticatedNoToken401(t *testing.T) {
	srv := newServer(t, uuid.New(), map[string]bool{"read:post": true}, &fakePosts{}, nil)

	rec := httptest.NewRecorder()
	// No Authorization header at all => RequirePermission returns 401.
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/posts", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestListPagesEnvelope(t *testing.T) {
	userID := uuid.New()
	parent := uuid.New()
	fpg := &fakePages{
		total: 3,
		list: []pages.Page{
			{
				ID: uuid.New(), Title: "About", Slug: "about", Body: "B", Status: kernel.StatusPublished,
				Template: "default", ParentID: &parent,
			},
		},
	}
	srv := newServer(t, userID, map[string]bool{"read:page": true}, nil, fpg)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/pages"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data := decode(t, rec)["data"].(map[string]any)
	if data["total"].(float64) != 3 {
		t.Errorf("total = %v, want 3", data["total"])
	}
	item := data["items"].([]any)[0].(map[string]any)
	if item["template"] != "default" || item["parentId"] != parent.String() {
		t.Errorf("page dto fields wrong: %v", item)
	}
	if _, hasBody := item["body"]; hasBody {
		t.Error("list page DTO leaked body")
	}
}

func TestGetPageNotFound(t *testing.T) {
	userID := uuid.New()
	srv := newServer(t, userID, map[string]bool{"read:page": true}, nil, &fakePages{})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/pages/"+uuid.New().String()))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
