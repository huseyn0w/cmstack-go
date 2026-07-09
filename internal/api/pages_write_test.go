package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/pages"
)

func TestCreatePage(t *testing.T) {
	userID := uuid.New()
	newID := uuid.New()
	fpg := &fakePages{createOut: pages.Page{ID: newID, Title: "About", Slug: "about", Body: "B", Status: kernel.StatusDraft, Template: "default"}}
	srv := newServer(t, userID, map[string]bool{"create:page": true}, nil, fpg)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/pages", `{"title":"About","body":"B","template":"default"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if fpg.createActor != userID || fpg.createIn.Title != "About" || fpg.createIn.Template != "default" {
		t.Errorf("create input wrong: actor=%v in=%+v", fpg.createActor, fpg.createIn)
	}
	data := decode(t, rec)["data"].(map[string]any)
	if data["body"] != "B" {
		t.Errorf("detail dto missing body: %v", data)
	}
}

func TestUpdatePageWithParent(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	parent := uuid.New()
	fpg := &fakePages{byID: map[uuid.UUID]pages.Page{id: {ID: id, Title: "P", Body: "B"}}}
	srv := newServer(t, userID, map[string]bool{"update:page": true}, nil, fpg)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPatch, "/api/v1/pages/"+id.String(), `{"title":"P","parentId":"`+parent.String()+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !fpg.updateIn.SetParent || fpg.updateIn.ParentID == nil || *fpg.updateIn.ParentID != parent {
		t.Errorf("parent not forwarded: setParent=%v id=%v", fpg.updateIn.SetParent, fpg.updateIn.ParentID)
	}
}

func TestUpdatePageBadParentUUID(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	srv := newServer(t, userID, map[string]bool{"update:page": true}, nil, &fakePages{})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPatch, "/api/v1/pages/"+id.String(), `{"parentId":"nope"}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestTrashAndRestorePage(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fpg := &fakePages{byID: map[uuid.UUID]pages.Page{id: {ID: id, Title: "P", Body: "B"}}}
	srv := newServer(t, userID, map[string]bool{"delete:page": true, "update:page": true}, nil, fpg)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodDelete, "/api/v1/pages/"+id.String(), ""))
	if rec.Code != http.StatusOK || len(fpg.trashed) != 1 {
		t.Fatalf("trash wrong: code=%d trashed=%v", rec.Code, fpg.trashed)
	}

	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/pages/"+id.String()+"/restore", ""))
	if rec.Code != http.StatusOK || len(fpg.restored) != 1 {
		t.Fatalf("restore wrong: code=%d restored=%v", rec.Code, fpg.restored)
	}
}

func TestPublishPageRequiresPublishGrant(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	srv := newServer(t, userID, map[string]bool{"update:page": true}, nil, &fakePages{})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/pages/"+id.String()+"/publish", ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
