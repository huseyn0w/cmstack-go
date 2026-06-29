package categories

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// --- fakes -------------------------------------------------------------------

type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

type fakeBeginner struct{}

func (fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return fakeTx{}, nil }

// memRepo is an in-memory category Repository.
type memRepo struct {
	mu    sync.Mutex
	cats  map[uuid.UUID]Category
	links map[uuid.UUID][]uuid.UUID // postID -> categoryIDs
}

func newMemRepo() *memRepo {
	return &memRepo{cats: map[uuid.UUID]Category{}, links: map[uuid.UUID][]uuid.UUID{}}
}

func (m *memRepo) CreateTx(_ context.Context, _ pgx.Tx, in CreateCategoryData) (Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := Category{ID: uuid.New(), Name: in.Name, Slug: in.Slug, Description: in.Description, ParentID: in.ParentID}
	m.cats[c.ID] = c
	return c, nil
}

func (m *memRepo) UpdateTx(_ context.Context, _ pgx.Tx, id uuid.UUID, in UpdateCategoryData) (Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cats[id]
	if !ok {
		return Category{}, ErrNotFound
	}
	c.Name, c.Slug, c.Description, c.ParentID = in.Name, in.Slug, in.Description, in.ParentID
	m.cats[id] = c
	return c, nil
}

func (m *memRepo) DeleteTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cats, id)
	return nil
}

func (m *memRepo) GetByID(_ context.Context, id uuid.UUID) (Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cats[id]
	if !ok {
		return Category{}, ErrNotFound
	}
	return c, nil
}

func (m *memRepo) GetBySlug(_ context.Context, slug string) (Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.cats {
		if c.Slug == slug {
			return c, nil
		}
	}
	return Category{}, ErrNotFound
}

func (m *memRepo) SlugTaken(_ context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.cats {
		if c.Slug == slug && c.ID != excludeID {
			return true, nil
		}
	}
	return false, nil
}

func (m *memRepo) ListAll(_ context.Context) ([]Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Category, 0, len(m.cats))
	for _, c := range m.cats {
		out = append(out, c)
	}
	return out, nil
}

func (m *memRepo) ListChildren(_ context.Context, parentID uuid.UUID) ([]Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Category
	for _, c := range m.cats {
		if c.ParentID != nil && *c.ParentID == parentID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memRepo) List(_ context.Context, _, _ int) ([]Category, error) {
	return m.ListAll(context.Background())
}
func (m *memRepo) Count(_ context.Context) (int, error) { return len(m.cats), nil }

func (m *memRepo) AttachTx(_ context.Context, _ pgx.Tx, postID, categoryID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links[postID] = append(m.links[postID], categoryID)
	return nil
}

func (m *memRepo) DetachAllTx(_ context.Context, _ pgx.Tx, postID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.links, postID)
	return nil
}

func (m *memRepo) ListForPost(_ context.Context, postID uuid.UUID) ([]Category, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Category
	for _, id := range m.links[postID] {
		if c, ok := m.cats[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memRepo) IDsForPost(_ context.Context, postID uuid.UUID) ([]uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]uuid.UUID(nil), m.links[postID]...), nil
}

func (m *memRepo) ListPublishedPostIDsInCategory(_ context.Context, _ uuid.UUID, _, _ int) ([]uuid.UUID, error) {
	return nil, nil
}

func (m *memRepo) CountPublishedPostsInCategory(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}

// allowAuthorizer always grants.
type allowAuthorizer struct{}

func (allowAuthorizer) Can(context.Context, uuid.UUID, string, string) bool { return true }

// denyAuthorizer always denies.
type denyAuthorizer struct{}

func (denyAuthorizer) Can(context.Context, uuid.UUID, string, string) bool { return false }

func newSvc(repo Repository, authz Authorizer) *Service {
	return NewService(fakeBeginner{}, repo, authz)
}

// --- tests -------------------------------------------------------------------

func TestCreate_DerivesAndDedupesSlug(t *testing.T) {
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	ctx := context.Background()
	actor := uuid.New()

	a, err := svc.Create(ctx, actor, CreateInput{Name: "Go Tips"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	if a.Slug != "go-tips" {
		t.Fatalf("slug = %q, want go-tips", a.Slug)
	}
	b, err := svc.Create(ctx, actor, CreateInput{Name: "Go Tips"})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	if b.Slug != "go-tips-2" {
		t.Fatalf("dedupe slug = %q, want go-tips-2", b.Slug)
	}
}

func TestCreate_SanitizesDescription(t *testing.T) {
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	c, err := svc.Create(context.Background(), uuid.New(), CreateInput{
		Name:        "Sec",
		Description: `<p>ok</p><script>alert(1)</script>`,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if want := "<p>ok</p>"; c.Description != want {
		t.Fatalf("description = %q, want %q (script stripped)", c.Description, want)
	}
}

func TestCreate_NameRequired(t *testing.T) {
	svc := newSvc(newMemRepo(), allowAuthorizer{})
	if _, err := svc.Create(context.Background(), uuid.New(), CreateInput{Name: "   "}); err != ErrNameRequired {
		t.Fatalf("err = %v, want ErrNameRequired", err)
	}
}

func TestCreate_Forbidden(t *testing.T) {
	svc := newSvc(newMemRepo(), denyAuthorizer{})
	if _, err := svc.Create(context.Background(), uuid.New(), CreateInput{Name: "X"}); err != ErrForbidden {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestUpdate_ParentCycleRejected(t *testing.T) {
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	ctx := context.Background()
	actor := uuid.New()

	root, _ := svc.Create(ctx, actor, CreateInput{Name: "Root"})
	child, _ := svc.Create(ctx, actor, CreateInput{Name: "Child", ParentID: &root.ID})

	// Making root a child of its own descendant (child) must be rejected.
	_, err := svc.Update(ctx, actor, root.ID, UpdateInput{SetParent: true, ParentID: &child.ID})
	if err != ErrParentCycle {
		t.Fatalf("err = %v, want ErrParentCycle", err)
	}

	// A category may not be its own parent.
	_, err = svc.Update(ctx, actor, root.ID, UpdateInput{SetParent: true, ParentID: &root.ID})
	if err != ErrParentCycle {
		t.Fatalf("self-parent err = %v, want ErrParentCycle", err)
	}
}

func TestCreate_ParentNotFound(t *testing.T) {
	svc := newSvc(newMemRepo(), allowAuthorizer{})
	ghost := uuid.New()
	if _, err := svc.Create(context.Background(), uuid.New(), CreateInput{Name: "X", ParentID: &ghost}); err != ErrParentNotFound {
		t.Fatalf("err = %v, want ErrParentNotFound", err)
	}
}

func TestBuildTree_DepthOrdering(t *testing.T) {
	root := Category{ID: uuid.New(), Name: "Root"}
	childID := uuid.New()
	child := Category{ID: childID, Name: "Child", ParentID: &root.ID}
	grand := Category{ID: uuid.New(), Name: "Grand", ParentID: &childID}
	// Pass in a deliberately scrambled order.
	nodes := BuildTree([]Category{grand, root, child})

	if len(nodes) != 3 {
		t.Fatalf("len = %d, want 3", len(nodes))
	}
	if nodes[0].Category.ID != root.ID || nodes[0].Depth != 0 {
		t.Fatalf("node0 = %+v, want root depth 0", nodes[0])
	}
	if nodes[1].Category.ID != child.ID || nodes[1].Depth != 1 {
		t.Fatalf("node1 = %+v, want child depth 1", nodes[1])
	}
	if nodes[2].Category.ID != grand.ID || nodes[2].Depth != 2 {
		t.Fatalf("node2 = %+v, want grand depth 2", nodes[2])
	}
}

func TestAssignTx_ReplacesSet(t *testing.T) {
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	ctx := context.Background()
	actor := uuid.New()
	a, _ := svc.Create(ctx, actor, CreateInput{Name: "A"})
	b, _ := svc.Create(ctx, actor, CreateInput{Name: "B"})
	post := uuid.New()

	if err := svc.AssignTx(ctx, fakeTx{}, post, []uuid.UUID{a.ID, b.ID, a.ID}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	ids, _ := repo.IDsForPost(ctx, post)
	if len(ids) != 2 {
		t.Fatalf("after assign len = %d, want 2 (deduped)", len(ids))
	}

	// Re-assign a smaller set: the previous set must be fully replaced.
	if err := svc.AssignTx(ctx, fakeTx{}, post, []uuid.UUID{b.ID}); err != nil {
		t.Fatalf("reassign: %v", err)
	}
	ids, _ = repo.IDsForPost(ctx, post)
	if len(ids) != 1 || ids[0] != b.ID {
		t.Fatalf("after reassign ids = %v, want [b]", ids)
	}
}

func TestAssignTx_SkipsUnknownIDs(t *testing.T) {
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	ctx := context.Background()
	a, _ := svc.Create(ctx, uuid.New(), CreateInput{Name: "A"})
	post := uuid.New()
	ghost := uuid.New()

	if err := svc.AssignTx(ctx, fakeTx{}, post, []uuid.UUID{a.ID, ghost}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	ids, _ := repo.IDsForPost(ctx, post)
	if len(ids) != 1 || ids[0] != a.ID {
		t.Fatalf("ids = %v, want [a] (ghost skipped)", ids)
	}
}
