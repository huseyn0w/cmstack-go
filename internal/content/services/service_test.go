package services

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
)

// --- fakes -------------------------------------------------------------------

type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

type fakeBeginner struct{}

func (fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return fakeTx{}, nil }

type memRepo struct {
	mu       sync.Mutex
	services map[uuid.UUID]Service
	faqs     map[uuid.UUID][]FAQ
}

func newMemRepo() *memRepo {
	return &memRepo{services: map[uuid.UUID]Service{}, faqs: map[uuid.UUID][]FAQ{}}
}

func (m *memRepo) CreateTx(_ context.Context, _ pgx.Tx, in CreateServiceData) (Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Service{
		ID: uuid.New(), Title: in.Title, Slug: in.Slug, Summary: in.Summary,
		Body: in.Body, Price: in.Price, AreaServed: in.AreaServed, Status: in.Status,
		PublishedAt: in.PublishedAt, ReadingTime: in.ReadingTime,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	m.services[s.ID] = s
	return s, nil
}

func (m *memRepo) UpdateTx(_ context.Context, _ pgx.Tx, id uuid.UUID, in UpdateServiceData) (Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.services[id]
	if !ok {
		return Service{}, ErrNotFound
	}
	s.Title, s.Slug, s.Summary, s.Body = in.Title, in.Slug, in.Summary, in.Body
	s.Price, s.AreaServed, s.Status = in.Price, in.AreaServed, in.Status
	s.PublishedAt, s.ReadingTime = in.PublishedAt, in.ReadingTime
	s.UpdatedAt = time.Now()
	m.services[id] = s
	return s, nil
}

func (m *memRepo) GetByID(_ context.Context, id uuid.UUID) (Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.services[id]
	if !ok {
		return Service{}, ErrNotFound
	}
	return s, nil
}

func (m *memRepo) GetActiveByID(ctx context.Context, id uuid.UUID) (Service, error) {
	s, err := m.GetByID(ctx, id)
	if err != nil {
		return Service{}, err
	}
	if s.DeletedAt != nil {
		return Service{}, ErrNotFound
	}
	return s, nil
}

func (m *memRepo) GetPublishedBySlug(_ context.Context, slug string) (Service, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.services {
		if s.Slug == slug && s.Published() {
			return s, nil
		}
	}
	return Service{}, ErrNotFound
}

func (m *memRepo) SlugTaken(_ context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.services {
		if s.Slug == slug && s.ID != excludeID {
			return true, nil
		}
	}
	return false, nil
}

func (m *memRepo) List(context.Context, ListFilter) ([]Service, error)        { return nil, nil }
func (m *memRepo) Count(context.Context, ListFilter) (int, error)             { return 0, nil }
func (m *memRepo) ListTrashed(context.Context, int, int) ([]Service, error)   { return nil, nil }
func (m *memRepo) CountTrashed(context.Context) (int, error)                  { return 0, nil }
func (m *memRepo) ListPublished(context.Context, int, int) ([]Service, error) { return nil, nil }
func (m *memRepo) CountPublished(context.Context) (int, error)                { return 0, nil }

func (m *memRepo) TrashTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.services[id]
	now := time.Now()
	s.DeletedAt = &now
	m.services[id] = s
	return nil
}

func (m *memRepo) RestoreTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.services[id]
	s.DeletedAt = nil
	m.services[id] = s
	return nil
}

func (m *memRepo) PermanentDeleteTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.services, id)
	delete(m.faqs, id)
	return nil
}

func (m *memRepo) ListFAQs(_ context.Context, serviceID uuid.UUID) ([]FAQ, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]FAQ, len(m.faqs[serviceID]))
	copy(out, m.faqs[serviceID])
	return out, nil
}

func (m *memRepo) ReplaceFAQsTx(_ context.Context, _ pgx.Tx, serviceID uuid.UUID, faqs []FAQData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := make([]FAQ, 0, len(faqs))
	for _, f := range faqs {
		rows = append(rows, FAQ{
			ID: uuid.New(), ServiceID: serviceID, Question: f.Question,
			Answer: f.Answer, Position: f.Position,
		})
	}
	m.faqs[serviceID] = rows
	return nil
}

type memRevisions struct {
	mu   sync.Mutex
	rows map[uuid.UUID]kernel.Revision
}

func newMemRevisions() *memRevisions {
	return &memRevisions{rows: map[uuid.UUID]kernel.Revision{}}
}

func (m *memRevisions) CreateTx(_ context.Context, _ pgx.Tx, in kernel.CreateRevisionInput) (kernel.Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := kernel.Revision{
		ID: uuid.New(), EntityType: in.EntityType, EntityID: in.EntityID,
		Snapshot: in.Snapshot, AuthorID: in.AuthorID, CreatedAt: time.Now(),
	}
	m.rows[r.ID] = r
	return r, nil
}

func (m *memRevisions) List(_ context.Context, entityType string, entityID uuid.UUID) ([]kernel.Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []kernel.Revision
	for _, r := range m.rows {
		if r.EntityType == entityType && r.EntityID == entityID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memRevisions) Get(_ context.Context, id uuid.UUID) (kernel.Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.rows[id]
	if !ok {
		return kernel.Revision{}, ErrNotFound
	}
	return r, nil
}

type fakeAuthz struct{ allowed map[uuid.UUID]bool }

func (a fakeAuthz) Can(_ context.Context, userID uuid.UUID, _, _ string) bool {
	return a.allowed[userID]
}

type nullBus struct{}

func (nullBus) Publish(context.Context, pgx.Tx, events.Event) error { return nil }

func fixedClock(t time.Time) Clock { return func() time.Time { return t } }

func newTestManager(repo Repository, revs kernel.RevisionRepository, authz Authorizer, bus Publisher, now time.Time) *Manager {
	return NewManager(fakeBeginner{}, repo, revs, authz, bus, fixedClock(now))
}

func allow(ids ...uuid.UUID) fakeAuthz {
	m := map[uuid.UUID]bool{}
	for _, id := range ids {
		m[id] = true
	}
	return fakeAuthz{allowed: m}
}

// --- tests -------------------------------------------------------------------

func TestCreate_SanitizesSummaryBodyAndFAQAnswers(t *testing.T) {
	actor := uuid.New()
	repo := newMemRepo()
	mgr := newTestManager(repo, newMemRevisions(), allow(actor), nullBus{}, time.Now())

	svc, err := mgr.Create(context.Background(), actor, CreateInput{
		Title:   "SEO Audit",
		Summary: `We audit <script>alert(1)</script> your site.`,
		Body:    `<p>ok</p><script>bad()</script>`,
		FAQs: []FAQInput{
			{Question: "How long?", Answer: `<p>About a week</p><img src=x onerror=alert(1)>`},
			{Question: "", Answer: "dropped blank row"},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if svc.Slug != "seo-audit" {
		t.Errorf("slug = %q", svc.Slug)
	}
	// Summary is plain text with all tags stripped.
	if want := "We audit  your site."; svc.Summary != want {
		t.Errorf("summary = %q, want %q", svc.Summary, want)
	}
	if svc.Body != "<p>ok</p>" {
		t.Errorf("body not sanitized: %q", svc.Body)
	}
	if len(svc.FAQs) != 1 {
		t.Fatalf("blank FAQ row not dropped: got %d rows", len(svc.FAQs))
	}
	ans := svc.FAQs[0].Answer
	if ans != `<p>About a week</p><img src="x">` && ans != `<p>About a week</p><img src="x"/>` {
		t.Errorf("FAQ answer not sanitized: %q", ans)
	}
}

func TestCreate_DeniedWithoutPermission(t *testing.T) {
	actor := uuid.New()
	mgr := newTestManager(newMemRepo(), newMemRevisions(), fakeAuthz{allowed: map[uuid.UUID]bool{}}, nullBus{}, time.Now())
	if _, err := mgr.Create(context.Background(), actor, CreateInput{Title: "X"}); err != ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestFAQReorder_PositionsFollowSubmittedOrder(t *testing.T) {
	actor := uuid.New()
	repo := newMemRepo()
	mgr := newTestManager(repo, newMemRevisions(), allow(actor), nullBus{}, time.Now())

	svc, _ := mgr.Create(context.Background(), actor, CreateInput{
		Title: "S",
		FAQs: []FAQInput{
			{Question: "A", Answer: "a"},
			{Question: "B", Answer: "b"},
			{Question: "C", Answer: "c"},
		},
	})
	// Reorder to C, A, B via update.
	updated, err := mgr.Update(context.Background(), actor, svc.ID, UpdateInput{
		SetFAQs: true,
		FAQs: []FAQInput{
			{Question: "C", Answer: "c"},
			{Question: "A", Answer: "a"},
			{Question: "B", Answer: "b"},
		},
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(updated.FAQs) != 3 {
		t.Fatalf("want 3 FAQs, got %d", len(updated.FAQs))
	}
	wantOrder := []string{"C", "A", "B"}
	for i, f := range updated.FAQs {
		if f.Question != wantOrder[i] {
			t.Errorf("FAQ[%d] = %q, want %q", i, f.Question, wantOrder[i])
		}
		if f.Position != i {
			t.Errorf("FAQ[%d] position = %d, want %d", i, f.Position, i)
		}
	}
}

func TestUpdate_LeavesFAQsWhenNotSet(t *testing.T) {
	actor := uuid.New()
	repo := newMemRepo()
	mgr := newTestManager(repo, newMemRevisions(), allow(actor), nullBus{}, time.Now())

	svc, _ := mgr.Create(context.Background(), actor, CreateInput{
		Title: "S", FAQs: []FAQInput{{Question: "Q", Answer: "A"}},
	})
	nt := "Renamed"
	updated, err := mgr.Update(context.Background(), actor, svc.ID, UpdateInput{Title: &nt}) // SetFAQs=false
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(updated.FAQs) != 1 {
		t.Errorf("FAQs should be preserved when SetFAQs=false, got %d", len(updated.FAQs))
	}
}

func TestPublish_StampsOnceAndPreserves(t *testing.T) {
	actor := uuid.New()
	t0 := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	mgr := newTestManager(newMemRepo(), newMemRevisions(), allow(actor), nullBus{}, t0)

	svc, _ := mgr.Create(context.Background(), actor, CreateInput{Title: "P"})
	pub, _ := mgr.Publish(context.Background(), actor, svc.ID)
	if pub.PublishedAt == nil || !pub.PublishedAt.Equal(t0) {
		t.Fatalf("first publish = %v, want %v", pub.PublishedAt, t0)
	}
	mgr.now = fixedClock(t0.Add(72 * time.Hour))
	_, _ = mgr.Unpublish(context.Background(), actor, svc.ID)
	re, _ := mgr.Publish(context.Background(), actor, svc.ID)
	if re.PublishedAt == nil || !re.PublishedAt.Equal(t0) {
		t.Errorf("re-publish must preserve %v, got %v", t0, re.PublishedAt)
	}
}

func TestRestoreRevision_ReappliesScalarFields(t *testing.T) {
	actor := uuid.New()
	repo := newMemRepo()
	revs := newMemRevisions()
	mgr := newTestManager(repo, revs, allow(actor), nullBus{}, time.Now())

	svc, _ := mgr.Create(context.Background(), actor, CreateInput{Title: "Original", Summary: "orig", Body: "<p>v1</p>", Price: "$100"})
	nt, np := "Changed", "$200"
	if _, err := mgr.Update(context.Background(), actor, svc.ID, UpdateInput{Title: &nt, Price: &np}); err != nil {
		t.Fatalf("update: %v", err)
	}
	list, _ := revs.List(context.Background(), kernel.EntityTypeService, svc.ID)
	restored, err := mgr.RestoreRevision(context.Background(), actor, svc.ID, list[0].ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Title != "Original" || restored.Price != "$100" || restored.Summary != "orig" {
		t.Errorf("restore did not reapply: %+v", restored)
	}
}

func TestJSONLDSeam_CollectsData(t *testing.T) {
	svc := Service{
		Title: "T", Summary: "s", Price: "$1", AreaServed: "Berlin",
		FAQs: []FAQ{{Question: "Q", Answer: "<p>A</p>"}},
	}
	data := svc.JSONLD("https://x/services/t")
	if data.Title != "T" || data.CanonicalURL != "https://x/services/t" {
		t.Errorf("seam basic fields wrong: %+v", data)
	}
	if len(data.FAQs) != 1 || data.FAQs[0].Question != "Q" {
		t.Errorf("seam FAQ not collected: %+v", data.FAQs)
	}
}
