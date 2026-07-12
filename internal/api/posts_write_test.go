package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/content/posts"
)

func TestCreatePost(t *testing.T) {
	userID := uuid.New()
	newID := uuid.New()
	fp := &fakePosts{createOut: posts.Post{
		ID: newID, Title: "New", Slug: "new", Body: "BODY", Status: kernel.StatusDraft, AuthorID: userID,
	}}
	srv := newServer(t, userID, map[string]bool{"create:post": true}, fp, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/posts",
		`{"title":"New","excerpt":"e","body":"BODY","status":"DRAFT","categoryIds":["`+uuid.New().String()+`"]}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if fp.createActor != userID {
		t.Errorf("create actor = %v, want %v", fp.createActor, userID)
	}
	if fp.createIn.Title != "New" || fp.createIn.Body != "BODY" || fp.createIn.Status != kernel.StatusDraft {
		t.Errorf("create input wrong: %+v", fp.createIn)
	}
	if len(fp.createIn.CategoryIDs) != 1 {
		t.Errorf("categoryIds not parsed: %+v", fp.createIn.CategoryIDs)
	}
	data := decode(t, rec)["data"].(map[string]any)
	if data["id"] != newID.String() || data["body"] != "BODY" {
		t.Errorf("detail dto wrong: %v", data)
	}
}

func TestCreatePostMalformedJSON(t *testing.T) {
	userID := uuid.New()
	srv := newServer(t, userID, map[string]bool{"create:post": true}, &fakePosts{}, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/posts", `{"title":`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreatePostUnknownField(t *testing.T) {
	userID := uuid.New()
	srv := newServer(t, userID, map[string]bool{"create:post": true}, &fakePosts{}, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/posts", `{"title":"X","bogus":1}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unknown field", rec.Code)
	}
}

func TestCreatePostMissingTitle422(t *testing.T) {
	userID := uuid.New()
	fp := &fakePosts{writeErr: posts.ErrTitleRequired}
	srv := newServer(t, userID, map[string]bool{"create:post": true}, fp, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/posts", `{"title":"  "}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	errObj := decode(t, rec)["error"].(map[string]any)
	if errObj["code"] != "validation" {
		t.Errorf("error code = %v, want validation", errObj["code"])
	}
}

func TestCreatePostForbidden(t *testing.T) {
	userID := uuid.New()
	srv := newServer(t, userID, map[string]bool{}, &fakePosts{}, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/posts", `{"title":"X"}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestUpdatePostPartial(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fp := &fakePosts{byID: map[uuid.UUID]posts.Post{
		id: {ID: id, Title: "Updated", Slug: "u", Body: "B", Status: kernel.StatusDraft, AuthorID: userID},
	}}
	srv := newServer(t, userID, map[string]bool{"update:post": true}, fp, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPatch, "/api/v1/posts/"+id.String(), `{"title":"Updated"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if fp.updateID != id || fp.updateActor != userID {
		t.Errorf("update id/actor wrong: %v/%v", fp.updateID, fp.updateActor)
	}
	if fp.updateIn.Title == nil || *fp.updateIn.Title != "Updated" {
		t.Errorf("title not forwarded: %+v", fp.updateIn.Title)
	}
	// Body omitted => nil (partial update).
	if fp.updateIn.Body != nil {
		t.Errorf("omitted body should be nil, got %v", *fp.updateIn.Body)
	}
	// Taxonomy not present => SetTaxonomy false.
	if fp.updateIn.SetTaxonomy {
		t.Error("SetTaxonomy should be false when taxonomy absent")
	}
}

func TestUpdatePostBadUUID(t *testing.T) {
	userID := uuid.New()
	srv := newServer(t, userID, map[string]bool{"update:post": true}, &fakePosts{}, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPatch, "/api/v1/posts/not-a-uuid", `{"title":"X"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPublishUnpublishPost(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fp := &fakePosts{byID: map[uuid.UUID]posts.Post{id: {ID: id, Title: "T", Status: kernel.StatusPublished}}}
	srv := newServer(t, userID, map[string]bool{"publish:post": true}, fp, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/posts/"+id.String()+"/publish", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("publish status = %d, want 200", rec.Code)
	}
	if len(fp.published) != 1 || fp.published[0] != id {
		t.Errorf("publish not called with id: %v", fp.published)
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/posts/"+id.String()+"/unpublish", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("unpublish status = %d, want 200", rec.Code)
	}
	if len(fp.unpublished) != 1 {
		t.Errorf("unpublish not called: %v", fp.unpublished)
	}
}

func TestPublishRequiresPublishGrant(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	// Holds update but NOT publish.
	srv := newServer(t, userID, map[string]bool{"update:post": true}, &fakePosts{}, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/posts/"+id.String()+"/publish", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestTrashPostSoftDelete(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fp := &fakePosts{}
	srv := newServer(t, userID, map[string]bool{"delete:post": true}, fp, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodDelete, "/api/v1/posts/"+id.String(), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(fp.trashed) != 1 || fp.trashed[0] != id {
		t.Errorf("trash not called with id: %v", fp.trashed)
	}
	data := decode(t, rec)["data"].(map[string]any)
	if data["trashed"] != true || data["id"] != id.String() {
		t.Errorf("trash response wrong: %v", data)
	}
}

func TestRestorePost(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fp := &fakePosts{byID: map[uuid.UUID]posts.Post{id: {ID: id, Title: "R", Body: "B", AuthorID: userID}}}
	srv := newServer(t, userID, map[string]bool{"update:post": true}, fp, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/posts/"+id.String()+"/restore", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if len(fp.restored) != 1 || fp.restored[0] != id {
		t.Errorf("restore not called: %v", fp.restored)
	}
}

func TestListPostRevisions(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	author := uuid.New()
	fp := &fakePosts{revs: []kernel.Revision{
		{
			ID: uuid.New(), EntityType: kernel.EntityTypePost, EntityID: id, AuthorID: &author,
			Snapshot: []byte(`{"title":"Old title","excerpt":"ex","body":"HUGE BODY"}`),
		},
	}}
	srv := newServer(t, userID, map[string]bool{"read:post": true}, fp, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/posts/"+id.String()+"/revisions"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	items := decode(t, rec)["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("revisions len = %d, want 1", len(items))
	}
	rev := items[0].(map[string]any)
	if rev["title"] != "Old title" || rev["excerpt"] != "ex" {
		t.Errorf("revision dto wrong: %v", rev)
	}
	// The full body must NOT leak into the revision summary.
	if _, hasBody := rev["body"]; hasBody {
		t.Error("revision DTO leaked body")
	}
}

func TestUpdatePostNotFound(t *testing.T) {
	userID := uuid.New()
	fp := &fakePosts{writeErr: posts.ErrNotFound}
	srv := newServer(t, userID, map[string]bool{"update:post": true}, fp, nil)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPatch, "/api/v1/posts/"+uuid.New().String(), `{"title":"X"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
