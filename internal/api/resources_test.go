package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/categories"
	"github.com/huseyn0w/cmstack-go/internal/content/comments"
	"github.com/huseyn0w/cmstack-go/internal/content/media"
	"github.com/huseyn0w/cmstack-go/internal/content/tags"
)

// --- category fake + tests --------------------------------------------------

type fakeCategories struct {
	all       []categories.Category
	createIn  categories.CreateInput
	createOut categories.Category
	updateID  uuid.UUID
	updateIn  categories.UpdateInput
	deleted   []uuid.UUID
	err       error
}

func (f *fakeCategories) AllFlat(context.Context) ([]categories.Category, error) {
	return f.all, f.err
}

func (f *fakeCategories) Get(_ context.Context, _, id uuid.UUID) (categories.Category, error) {
	for _, c := range f.all {
		if c.ID == id {
			return c, nil
		}
	}
	return categories.Category{}, categories.ErrNotFound
}

func (f *fakeCategories) Create(_ context.Context, _ uuid.UUID, in categories.CreateInput) (categories.Category, error) {
	f.createIn = in
	if f.err != nil {
		return categories.Category{}, f.err
	}
	return f.createOut, nil
}

func (f *fakeCategories) Update(_ context.Context, _, id uuid.UUID, in categories.UpdateInput) (categories.Category, error) {
	f.updateID = id
	f.updateIn = in
	if f.err != nil {
		return categories.Category{}, f.err
	}
	return f.createOut, nil
}

func (f *fakeCategories) Delete(_ context.Context, _, id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	return f.err
}

func TestListCategories(t *testing.T) {
	userID := uuid.New()
	parent := uuid.New()
	fc := &fakeCategories{all: []categories.Category{
		{ID: uuid.New(), Name: "Tech", Slug: "tech", ParentID: &parent},
	}}
	srv := newServerDeps(t, userID, map[string]bool{"read:category": true}, Deps{Categories: fc})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/categories"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	items := decode(t, rec)["data"].([]any)
	item := items[0].(map[string]any)
	if item["name"] != "Tech" || item["slug"] != "tech" || item["parentId"] != parent.String() {
		t.Errorf("category dto wrong: %v", item)
	}
}

func TestCreateCategory(t *testing.T) {
	userID := uuid.New()
	newID := uuid.New()
	fc := &fakeCategories{createOut: categories.Category{ID: newID, Name: "Go", Slug: "go"}}
	srv := newServerDeps(t, userID, map[string]bool{"create:category": true}, Deps{Categories: fc})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/categories", `{"name":"Go","description":"d"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if fc.createIn.Name != "Go" || fc.createIn.Description != "d" {
		t.Errorf("create input wrong: %+v", fc.createIn)
	}
}

func TestUpdateCategory(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fc := &fakeCategories{createOut: categories.Category{ID: id, Name: "New"}}
	srv := newServerDeps(t, userID, map[string]bool{"update:category": true}, Deps{Categories: fc})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPatch, "/api/v1/categories/"+id.String(), `{"name":"New"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fc.updateID != id || fc.updateIn.Name == nil || *fc.updateIn.Name != "New" {
		t.Errorf("update wrong: id=%v in=%+v", fc.updateID, fc.updateIn)
	}
}

func TestDeleteCategory(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fc := &fakeCategories{}
	srv := newServerDeps(t, userID, map[string]bool{"delete:category": true}, Deps{Categories: fc})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodDelete, "/api/v1/categories/"+id.String(), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(fc.deleted) != 1 || fc.deleted[0] != id {
		t.Errorf("delete not called: %v", fc.deleted)
	}
}

func TestCategoryForbidden(t *testing.T) {
	userID := uuid.New()
	srv := newServerDeps(t, userID, map[string]bool{}, Deps{Categories: &fakeCategories{}})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/categories"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// --- tag fake + tests -------------------------------------------------------

type fakeTags struct {
	list      []tags.Tag
	total     int
	gotLimit  int
	gotOffset int
	createIn  tags.CreateInput
	createOut tags.Tag
	updateID  uuid.UUID
	deleted   []uuid.UUID
	err       error
}

func (f *fakeTags) AdminList(_ context.Context, limit, offset int) ([]tags.Tag, int, error) {
	f.gotLimit, f.gotOffset = limit, offset
	return f.list, f.total, f.err
}

func (f *fakeTags) Get(_ context.Context, _, id uuid.UUID) (tags.Tag, error) {
	for _, t := range f.list {
		if t.ID == id {
			return t, nil
		}
	}
	return tags.Tag{}, tags.ErrNotFound
}

func (f *fakeTags) Create(_ context.Context, _ uuid.UUID, in tags.CreateInput) (tags.Tag, error) {
	f.createIn = in
	if f.err != nil {
		return tags.Tag{}, f.err
	}
	return f.createOut, nil
}

func (f *fakeTags) Update(_ context.Context, _, id uuid.UUID, _ tags.UpdateInput) (tags.Tag, error) {
	f.updateID = id
	if f.err != nil {
		return tags.Tag{}, f.err
	}
	return f.createOut, nil
}

func (f *fakeTags) Delete(_ context.Context, _, id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	return f.err
}

func TestListTagsPaginated(t *testing.T) {
	userID := uuid.New()
	ft := &fakeTags{total: 5, list: []tags.Tag{{ID: uuid.New(), Name: "go", Slug: "go"}}}
	srv := newServerDeps(t, userID, map[string]bool{"read:tag": true}, Deps{Tags: ft})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/tags?page=2&perPage=10"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ft.gotLimit != 10 || ft.gotOffset != 10 {
		t.Errorf("pagination wrong: limit=%d offset=%d", ft.gotLimit, ft.gotOffset)
	}
	data := decode(t, rec)["data"].(map[string]any)
	if data["total"].(float64) != 5 {
		t.Errorf("total = %v, want 5", data["total"])
	}
}

func TestCreateTag(t *testing.T) {
	userID := uuid.New()
	ft := &fakeTags{createOut: tags.Tag{ID: uuid.New(), Name: "News", Slug: "news"}}
	srv := newServerDeps(t, userID, map[string]bool{"create:tag": true}, Deps{Tags: ft})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/tags", `{"name":"News"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if ft.createIn.Name != "News" {
		t.Errorf("create input wrong: %+v", ft.createIn)
	}
}

func TestDeleteTagNotFound(t *testing.T) {
	userID := uuid.New()
	ft := &fakeTags{err: tags.ErrNotFound}
	srv := newServerDeps(t, userID, map[string]bool{"delete:tag": true}, Deps{Tags: ft})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodDelete, "/api/v1/tags/"+uuid.New().String(), ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// --- media fake + tests -----------------------------------------------------

type fakeMedia struct {
	list     []media.Media
	total    int
	byID     map[uuid.UUID]media.Media
	updAlt   string
	updTitle string
	updCap   string
	updOut   media.Media
	deleted  []uuid.UUID
	err      error
}

func (f *fakeMedia) List(_ context.Context, _ uuid.UUID, _, _ int) ([]media.Media, int, error) {
	return f.list, f.total, f.err
}

func (f *fakeMedia) Get(_ context.Context, _, id uuid.UUID) (media.Media, error) {
	if m, ok := f.byID[id]; ok {
		return m, nil
	}
	return media.Media{}, media.ErrNotFound
}

func (f *fakeMedia) UpdateMetadata(_ context.Context, _, _ uuid.UUID, alt, title, caption string) (media.Media, error) {
	f.updAlt, f.updTitle, f.updCap = alt, title, caption
	if f.err != nil {
		return media.Media{}, f.err
	}
	return f.updOut, nil
}

func (f *fakeMedia) Delete(_ context.Context, _, id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	return f.err
}

func (f *fakeMedia) URL(key string) string { return "https://cdn.example/" + key }

func TestListMedia(t *testing.T) {
	userID := uuid.New()
	w, hgt := 800, 600
	fm := &fakeMedia{total: 1, list: []media.Media{
		{
			ID: uuid.New(), OriginalFilename: "a.png", MIME: "image/png", SizeBytes: 1234,
			Width: &w, Height: &hgt, Alt: "alt", Title: "t", Caption: "c", StorageKey: "media/a.png",
		},
	}}
	srv := newServerDeps(t, userID, map[string]bool{"read:media": true}, Deps{Media: fm})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/media"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	item := decode(t, rec)["data"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["filename"] != "a.png" || item["mime"] != "image/png" || item["url"] != "https://cdn.example/media/a.png" {
		t.Errorf("media dto wrong: %v", item)
	}
	if item["width"].(float64) != 800 {
		t.Errorf("width = %v, want 800", item["width"])
	}
	// The internal storage key must NOT leak as a field.
	if _, bad := item["storageKey"]; bad {
		t.Error("media DTO leaked storageKey")
	}
}

func TestUpdateMediaMetadataOnly(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fm := &fakeMedia{
		byID:   map[uuid.UUID]media.Media{id: {ID: id, Alt: "old", Title: "oldT", Caption: "oldC"}},
		updOut: media.Media{ID: id, Alt: "newAlt", Title: "oldT", Caption: "oldC"},
	}
	srv := newServerDeps(t, userID, map[string]bool{"read:media": true, "update:media": true}, Deps{Media: fm})

	rec := httptest.NewRecorder()
	// Only alt supplied; title/caption should carry forward from the current asset.
	srv.ServeHTTP(rec, authJSON(http.MethodPatch, "/api/v1/media/"+id.String(), `{"alt":"newAlt"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fm.updAlt != "newAlt" || fm.updTitle != "oldT" || fm.updCap != "oldC" {
		t.Errorf("metadata update wrong: alt=%q title=%q cap=%q", fm.updAlt, fm.updTitle, fm.updCap)
	}
}

func TestUpdateMediaRejectsNonMetadataField(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fm := &fakeMedia{byID: map[uuid.UUID]media.Media{id: {ID: id}}}
	srv := newServerDeps(t, userID, map[string]bool{"read:media": true, "update:media": true}, Deps{Media: fm})

	rec := httptest.NewRecorder()
	// Attempt to update the filename (not a metadata field) => strict decoder 400.
	srv.ServeHTTP(rec, authJSON(http.MethodPatch, "/api/v1/media/"+id.String(), `{"filename":"hacked.png"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for non-metadata field", rec.Code)
	}
}

func TestDeleteMedia(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fm := &fakeMedia{}
	srv := newServerDeps(t, userID, map[string]bool{"delete:media": true}, Deps{Media: fm})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodDelete, "/api/v1/media/"+id.String(), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(fm.deleted) != 1 || fm.deleted[0] != id {
		t.Errorf("delete not called: %v", fm.deleted)
	}
}

// --- comment fake + tests ---------------------------------------------------

type fakeComments struct {
	list      []comments.Comment
	total     int
	gotFilter comments.ModerationFilter
	approved  []uuid.UUID
	spammed   []uuid.UUID
	trashed   []uuid.UUID
	deleted   []uuid.UUID
	byID      map[uuid.UUID]comments.Comment
	err       error
}

func (f *fakeComments) AdminList(_ context.Context, _ uuid.UUID, ff comments.ModerationFilter) ([]comments.Comment, int, error) {
	f.gotFilter = ff
	return f.list, f.total, f.err
}

func (f *fakeComments) Approve(_ context.Context, _, id uuid.UUID) (comments.Comment, error) {
	f.approved = append(f.approved, id)
	return f.mutResult(id)
}

func (f *fakeComments) Spam(_ context.Context, _, id uuid.UUID) (comments.Comment, error) {
	f.spammed = append(f.spammed, id)
	return f.mutResult(id)
}

func (f *fakeComments) Trash(_ context.Context, _, id uuid.UUID) (comments.Comment, error) {
	f.trashed = append(f.trashed, id)
	return f.mutResult(id)
}

func (f *fakeComments) Delete(_ context.Context, _, id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	return f.err
}

func (f *fakeComments) mutResult(id uuid.UUID) (comments.Comment, error) {
	if f.err != nil {
		return comments.Comment{}, f.err
	}
	return f.byID[id], nil
}

func TestListCommentsStatusFilterForwarded(t *testing.T) {
	userID := uuid.New()
	fc := &fakeComments{total: 2, list: []comments.Comment{
		{
			ID: uuid.New(), PostID: uuid.New(), AuthorName: "Bob", AuthorEmail: "b@x.com",
			AuthorIP: "1.2.3.4", Body: "hi", Status: comments.StatusPending,
		},
	}}
	srv := newServerDeps(t, userID, map[string]bool{"read:comment": true}, Deps{Comments: fc})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/comments?status=spam"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if fc.gotFilter.Status == nil || *fc.gotFilter.Status != comments.StatusSpam {
		t.Errorf("status filter not forwarded: %v", fc.gotFilter.Status)
	}
	item := decode(t, rec)["data"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["authorName"] != "Bob" || item["status"] != "PENDING" {
		t.Errorf("comment dto wrong: %v", item)
	}
	// AuthorIP is PII and must never leak.
	if _, bad := item["authorIp"]; bad {
		t.Error("comment DTO leaked authorIp")
	}
}

func TestApproveSpamTrashComment(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fc := &fakeComments{byID: map[uuid.UUID]comments.Comment{id: {ID: id, PostID: uuid.New(), Status: comments.StatusApproved}}}
	srv := newServerDeps(t, userID, map[string]bool{"update:comment": true}, Deps{Comments: fc})

	for _, verb := range []string{"approve", "spam", "trash"} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/comments/"+id.String()+"/"+verb, ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200: %s", verb, rec.Code, rec.Body.String())
		}
	}
	if len(fc.approved) != 1 || len(fc.spammed) != 1 || len(fc.trashed) != 1 {
		t.Errorf("moderation ops not all called: a=%v s=%v t=%v", fc.approved, fc.spammed, fc.trashed)
	}
}

func TestDeleteCommentPermanent(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fc := &fakeComments{}
	srv := newServerDeps(t, userID, map[string]bool{"delete:comment": true}, Deps{Comments: fc})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodDelete, "/api/v1/comments/"+id.String(), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(fc.deleted) != 1 || fc.deleted[0] != id {
		t.Errorf("delete not called: %v", fc.deleted)
	}
}

func TestApproveCommentRequiresUpdateGrant(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	// read only, no update.
	srv := newServerDeps(t, userID, map[string]bool{"read:comment": true}, Deps{Comments: &fakeComments{}})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/comments/"+id.String()+"/approve", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestCommentNoToken401(t *testing.T) {
	srv := newServerDeps(t, uuid.New(), map[string]bool{"read:comment": true}, Deps{Comments: &fakeComments{}})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/comments", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
