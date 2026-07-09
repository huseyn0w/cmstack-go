package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/content/services"
)

// fakeServices is an in-memory ServiceManager. It models the real FAQ-replace
// semantics: an Update with SetFAQs rebuilds the FAQ list from the supplied
// inputs, assigning fresh ids and dense positions in order (mirroring
// prepareFAQs + ReplaceFAQsTx), so the order-preservation behavior is testable.
type fakeServices struct {
	list  []services.Service
	total int
	byID  map[uuid.UUID]services.Service

	createIn services.CreateInput
	updateIn services.UpdateInput
	trashed  []uuid.UUID
	getErr   error
}

func (f *fakeServices) AdminList(context.Context, services.ListFilter) ([]services.Service, int, error) {
	return f.list, f.total, nil
}

func (f *fakeServices) Get(_ context.Context, _, id uuid.UUID) (services.Service, error) {
	if f.getErr != nil {
		return services.Service{}, f.getErr
	}
	if s, ok := f.byID[id]; ok {
		return s, nil
	}
	return services.Service{}, services.ErrNotFound
}

func (f *fakeServices) Create(_ context.Context, _ uuid.UUID, in services.CreateInput) (services.Service, error) {
	f.createIn = in
	return services.Service{ID: uuid.New(), Title: in.Title, Slug: "svc", Status: kernel.StatusDraft}, nil
}

func (f *fakeServices) Update(_ context.Context, _, id uuid.UUID, in services.UpdateInput) (services.Service, error) {
	f.updateIn = in
	svc, ok := f.byID[id]
	if !ok {
		return services.Service{}, services.ErrNotFound
	}
	if in.SetFAQs {
		rebuilt := make([]services.FAQ, 0, len(in.FAQs))
		for i, fi := range in.FAQs {
			rebuilt = append(rebuilt, services.FAQ{
				ID:        uuid.New(),
				ServiceID: id,
				Question:  fi.Question,
				Answer:    fi.Answer,
				Position:  i,
			})
		}
		svc.FAQs = rebuilt
		f.byID[id] = svc
	}
	return svc, nil
}

func (f *fakeServices) Trash(_ context.Context, _, id uuid.UUID) error {
	f.trashed = append(f.trashed, id)
	return nil
}

func TestListServicesEnvelopeNoBodyLeak(t *testing.T) {
	userID := uuid.New()
	fs := &fakeServices{total: 2, list: []services.Service{
		{ID: uuid.New(), Title: "Web", Slug: "web", Summary: "we build", Body: "SECRET", Status: kernel.StatusPublished},
	}}
	srv := newServerDeps(t, userID, map[string]bool{"read:service": true}, Deps{Services: fs})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/services"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data := decode(t, rec)["data"].(map[string]any)
	item := data["items"].([]any)[0].(map[string]any)
	if item["summary"] != "we build" {
		t.Errorf("summary wrong: %v", item)
	}
	if _, has := item["body"]; has {
		t.Error("list DTO leaked body")
	}
}

func TestGetServiceDetailIncludesBodyAndFAQs(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fs := &fakeServices{byID: map[uuid.UUID]services.Service{
		id: {ID: id, Title: "T", Body: "FULL", Status: kernel.StatusDraft, FAQs: []services.FAQ{
			{ID: uuid.New(), Question: "Q1", Answer: "A1", Position: 0},
		}},
	}}
	srv := newServerDeps(t, userID, map[string]bool{"read:service": true}, Deps{Services: fs})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/services/"+id.String()))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	data := decode(t, rec)["data"].(map[string]any)
	if data["body"] != "FULL" {
		t.Errorf("body missing: %v", data)
	}
	faqs := data["faqs"].([]any)
	if len(faqs) != 1 {
		t.Fatalf("faqs len = %d, want 1", len(faqs))
	}
}

func TestCreateService201(t *testing.T) {
	userID := uuid.New()
	fs := &fakeServices{byID: map[uuid.UUID]services.Service{}}
	srv := newServerDeps(t, userID, map[string]bool{"create:service": true}, Deps{Services: fs})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/services", `{"title":"New","summary":"s"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if fs.createIn.Title != "New" {
		t.Errorf("create input title = %q", fs.createIn.Title)
	}
}

func TestUpdateServiceLeavesFAQsAloneWhenAbsent(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fs := &fakeServices{byID: map[uuid.UUID]services.Service{id: {ID: id, Title: "T"}}}
	srv := newServerDeps(t, userID, map[string]bool{"update:service": true}, Deps{Services: fs})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPatch, "/api/v1/services/"+id.String(), `{"title":"T2"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if fs.updateIn.SetFAQs {
		t.Error("SetFAQs must be false when faqs absent from body")
	}
}

func TestTrashService(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fs := &fakeServices{byID: map[uuid.UUID]services.Service{id: {ID: id}}}
	srv := newServerDeps(t, userID, map[string]bool{"delete:service": true}, Deps{Services: fs})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodDelete, "/api/v1/services/"+id.String(), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(fs.trashed) != 1 || fs.trashed[0] != id {
		t.Errorf("trash not recorded: %v", fs.trashed)
	}
}

func TestServiceForbidden(t *testing.T) {
	userID := uuid.New()
	srv := newServerDeps(t, userID, map[string]bool{}, Deps{Services: &fakeServices{}})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authGet("/api/v1/services"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// --- FAQ order-preservation --------------------------------------------------

func seededServiceWithFAQs(id uuid.UUID) *fakeServices {
	return &fakeServices{byID: map[uuid.UUID]services.Service{
		id: {ID: id, Title: "T", FAQs: []services.FAQ{
			{ID: uuid.New(), Question: "Q0", Answer: "A0", Position: 0},
			{ID: uuid.New(), Question: "Q1", Answer: "A1", Position: 1},
			{ID: uuid.New(), Question: "Q2", Answer: "A2", Position: 2},
		}},
	}}
}

func faqQuestions(t *testing.T, fs *fakeServices, id uuid.UUID) []string {
	t.Helper()
	svc := fs.byID[id]
	out := make([]string, len(svc.FAQs))
	for i, f := range svc.FAQs {
		out[i] = f.Question
	}
	return out
}

func TestCreateFAQPreservesOrder(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fs := seededServiceWithFAQs(id)
	srv := newServerDeps(t, userID, map[string]bool{"update:service": true}, Deps{Services: fs})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPost, "/api/v1/services/"+id.String()+"/faqs", `{"question":"Q3","answer":"A3"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	got := faqQuestions(t, fs, id)
	want := []string{"Q0", "Q1", "Q2", "Q3"}
	if len(got) != len(want) {
		t.Fatalf("faq count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("faq[%d] = %q, want %q (full %v)", i, got[i], want[i], got)
		}
	}
	created := decode(t, rec)["data"].(map[string]any)
	if created["question"] != "Q3" {
		t.Errorf("created faq = %v", created)
	}
}

func TestUpdateFAQPreservesOthersAndOrder(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fs := seededServiceWithFAQs(id)
	targetID := fs.byID[id].FAQs[1].ID
	srv := newServerDeps(t, userID, map[string]bool{"update:service": true}, Deps{Services: fs})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPatch, "/api/v1/services/"+id.String()+"/faqs/"+targetID.String(), `{"question":"Q1-edited","answer":"A1-edited"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := faqQuestions(t, fs, id)
	want := []string{"Q0", "Q1-edited", "Q2"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("faq[%d] = %q, want %q (full %v)", i, got[i], want[i], got)
		}
	}
}

func TestUpdateFAQNotFound(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fs := seededServiceWithFAQs(id)
	srv := newServerDeps(t, userID, map[string]bool{"update:service": true}, Deps{Services: fs})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodPatch, "/api/v1/services/"+id.String()+"/faqs/"+uuid.New().String(), `{"question":"X","answer":"Y"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteFAQPreservesOthersAndOrder(t *testing.T) {
	userID := uuid.New()
	id := uuid.New()
	fs := seededServiceWithFAQs(id)
	targetID := fs.byID[id].FAQs[1].ID // delete the middle one
	srv := newServerDeps(t, userID, map[string]bool{"update:service": true}, Deps{Services: fs})

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authJSON(http.MethodDelete, "/api/v1/services/"+id.String()+"/faqs/"+targetID.String(), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	got := faqQuestions(t, fs, id)
	want := []string{"Q0", "Q2"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("after delete = %v, want %v", got, want)
	}
}
