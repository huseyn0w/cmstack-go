package tags

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

type fakeBeginner struct{}

func (fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return fakeTx{}, nil }

type memRepo struct {
	mu    sync.Mutex
	tags  map[uuid.UUID]Tag
	links map[uuid.UUID][]uuid.UUID
}

func newMemRepo() *memRepo {
	return &memRepo{tags: map[uuid.UUID]Tag{}, links: map[uuid.UUID][]uuid.UUID{}}
}

func (m *memRepo) CreateTx(_ context.Context, _ pgx.Tx, in CreateTagData) (Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := Tag{ID: uuid.New(), Name: in.Name, Slug: in.Slug}
	m.tags[t.ID] = t
	return t, nil
}

func (m *memRepo) UpdateTx(_ context.Context, _ pgx.Tx, id uuid.UUID, in UpdateTagData) (Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tags[id]
	if !ok {
		return Tag{}, ErrNotFound
	}
	t.Name, t.Slug = in.Name, in.Slug
	m.tags[id] = t
	return t, nil
}

func (m *memRepo) DeleteTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tags, id)
	return nil
}

func (m *memRepo) GetByID(_ context.Context, id uuid.UUID) (Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tags[id]
	if !ok {
		return Tag{}, ErrNotFound
	}
	return t, nil
}

func (m *memRepo) GetBySlug(_ context.Context, slug string) (Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tags {
		if t.Slug == slug {
			return t, nil
		}
	}
	return Tag{}, ErrNotFound
}

func (m *memRepo) SlugTaken(_ context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.tags {
		if t.Slug == slug && t.ID != excludeID {
			return true, nil
		}
	}
	return false, nil
}

func (m *memRepo) ListAll(_ context.Context) ([]Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Tag, 0, len(m.tags))
	for _, t := range m.tags {
		out = append(out, t)
	}
	return out, nil
}

func (m *memRepo) List(_ context.Context, _, _ int) ([]Tag, error) {
	return m.ListAll(context.Background())
}
func (m *memRepo) Count(_ context.Context) (int, error) { return len(m.tags), nil }

func (m *memRepo) AttachTx(_ context.Context, _ pgx.Tx, postID, tagID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links[postID] = append(m.links[postID], tagID)
	return nil
}

func (m *memRepo) DetachAllTx(_ context.Context, _ pgx.Tx, postID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.links, postID)
	return nil
}

func (m *memRepo) ListForPost(_ context.Context, postID uuid.UUID) ([]Tag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Tag
	for _, id := range m.links[postID] {
		if t, ok := m.tags[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *memRepo) IDsForPost(_ context.Context, postID uuid.UUID) ([]uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]uuid.UUID(nil), m.links[postID]...), nil
}

func (m *memRepo) ListPublishedPostIDsInTag(_ context.Context, _ uuid.UUID, _, _ int) ([]uuid.UUID, error) {
	return nil, nil
}

func (m *memRepo) CountPublishedPostsInTag(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}

type allowAuthorizer struct{}

func (allowAuthorizer) Can(context.Context, uuid.UUID, string, string) bool { return true }

type denyAuthorizer struct{}

func (denyAuthorizer) Can(context.Context, uuid.UUID, string, string) bool { return false }

func newSvc(repo Repository, authz Authorizer) *Service {
	return NewService(fakeBeginner{}, repo, authz)
}

func TestCreate_DerivesAndDedupesSlug(t *testing.T) {
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	ctx := context.Background()
	actor := uuid.New()

	a, err := svc.Create(ctx, actor, CreateInput{Name: "Cloud Native"})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	if a.Slug != "cloud-native" {
		t.Fatalf("slug = %q", a.Slug)
	}
	b, _ := svc.Create(ctx, actor, CreateInput{Name: "Cloud Native"})
	if b.Slug != "cloud-native-2" {
		t.Fatalf("dedupe slug = %q, want cloud-native-2", b.Slug)
	}
}

func TestCreate_Forbidden(t *testing.T) {
	svc := newSvc(newMemRepo(), denyAuthorizer{})
	if _, err := svc.Create(context.Background(), uuid.New(), CreateInput{Name: "X"}); err != ErrForbidden {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestCreate_NameRequired(t *testing.T) {
	svc := newSvc(newMemRepo(), allowAuthorizer{})
	if _, err := svc.Create(context.Background(), uuid.New(), CreateInput{Name: ""}); err != ErrNameRequired {
		t.Fatalf("err = %v, want ErrNameRequired", err)
	}
}

func TestAssignTx_ReplacesSet(t *testing.T) {
	repo := newMemRepo()
	svc := newSvc(repo, allowAuthorizer{})
	ctx := context.Background()
	a, _ := svc.Create(ctx, uuid.New(), CreateInput{Name: "A"})
	b, _ := svc.Create(ctx, uuid.New(), CreateInput{Name: "B"})
	post := uuid.New()

	if err := svc.AssignTx(ctx, fakeTx{}, post, []uuid.UUID{a.ID, b.ID}); err != nil {
		t.Fatalf("assign: %v", err)
	}
	if ids, _ := repo.IDsForPost(ctx, post); len(ids) != 2 {
		t.Fatalf("len = %d, want 2", len(ids))
	}
	if err := svc.AssignTx(ctx, fakeTx{}, post, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if ids, _ := repo.IDsForPost(ctx, post); len(ids) != 0 {
		t.Fatalf("after clear len = %d, want 0", len(ids))
	}
}
