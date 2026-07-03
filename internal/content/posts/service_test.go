package posts

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

// --- fakes -------------------------------------------------------------------

// fakeTx is a no-op pgx.Tx good enough for the in-memory repo (which ignores it).
type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

// fakeBeginner returns a fakeTx so RunInTx commits in-memory work.
type fakeBeginner struct{}

func (fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return fakeTx{}, nil }

// memRepo is an in-memory post Repository.
type memRepo struct {
	mu           sync.Mutex
	posts        map[uuid.UUID]Post
	likes        map[uuid.UUID]map[uuid.UUID]bool     // postID -> set(userID)
	translations map[uuid.UUID]map[string]Translation // postID -> locale -> row
}

func newMemRepo() *memRepo {
	return &memRepo{
		posts:        map[uuid.UUID]Post{},
		likes:        map[uuid.UUID]map[uuid.UUID]bool{},
		translations: map[uuid.UUID]map[string]Translation{},
	}
}

// --- per-locale overlay (M7b-1), functional so the service overlay/fallback is
// exercised end-to-end in a fast unit test.

func (m *memRepo) UpsertTranslationTx(_ context.Context, _ pgx.Tx, postID uuid.UUID, t Translation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.translations[postID] == nil {
		m.translations[postID] = map[string]Translation{}
	}
	m.translations[postID][t.Locale] = t
	return nil
}

func (m *memRepo) GetTranslation(_ context.Context, postID uuid.UUID, locale string) (Translation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.translations[postID][locale]; ok {
		return t, nil
	}
	return Translation{}, ErrNotFound
}

func (m *memRepo) ListTranslations(_ context.Context, postID uuid.UUID) ([]Translation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Translation
	for _, t := range m.translations[postID] {
		out = append(out, t)
	}
	return out, nil
}

func (m *memRepo) TranslatedLocales(_ context.Context, postID uuid.UUID) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for loc := range m.translations[postID] {
		out = append(out, loc)
	}
	return out, nil
}

func (m *memRepo) DeleteTranslationTx(_ context.Context, _ pgx.Tx, postID uuid.UUID, locale string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.translations[postID], locale)
	return nil
}

// overlay applies locale's translation to a base post with per-field base
// fallback (empty translation field -> base), mirroring the COALESCE/NULLIF SQL.
func (m *memRepo) overlay(p Post, locale string) Post {
	t, ok := m.translations[p.ID][locale]
	if !ok {
		return p
	}
	if t.Title != "" {
		p.Title = t.Title
	}
	if t.Excerpt != "" {
		p.Excerpt = t.Excerpt
	}
	if t.Body != "" {
		p.Body = t.Body
	}
	// meta_title/meta_description overlay with base fallback; canonical_url/noindex
	// are structural (base row only) and are NOT overridden per-locale.
	if t.MetaTitle != "" {
		p.MetaTitle = t.MetaTitle
	}
	if t.MetaDescription != "" {
		p.MetaDescription = t.MetaDescription
	}
	return p
}

func (m *memRepo) GetActiveInLocaleByID(ctx context.Context, id uuid.UUID, locale string) (Post, error) {
	p, err := m.GetActiveByID(ctx, id)
	if err != nil {
		return Post{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.overlay(p, locale), nil
}

func (m *memRepo) GetPublishedInLocaleBySlug(ctx context.Context, slug, locale string) (Post, error) {
	p, err := m.GetPublishedBySlug(ctx, slug)
	if err != nil {
		return Post{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.overlay(p, locale), nil
}

func (m *memRepo) ListPublishedInLocale(ctx context.Context, locale string, limit, offset int) ([]Post, error) {
	items, err := m.ListPublished(ctx, limit, offset)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Post, 0, len(items))
	for _, p := range items {
		out = append(out, m.overlay(p, locale))
	}
	return out, nil
}

func (m *memRepo) put(p Post) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.posts[p.ID] = p
}

func (m *memRepo) CreateTx(_ context.Context, _ pgx.Tx, in CreatePostData) (Post, error) {
	p := Post{
		ID: uuid.New(), Title: in.Title, Slug: in.Slug, Excerpt: in.Excerpt,
		Body: in.Body, Status: in.Status, PublishedAt: in.PublishedAt,
		ScheduledAt: in.ScheduledAt, AuthorID: in.AuthorID, ReadingTime: in.ReadingTime,
		MetaTitle: in.MetaTitle, MetaDescription: in.MetaDescription,
		CanonicalURL: in.CanonicalURL, NoIndex: in.NoIndex,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	m.put(p)
	return p, nil
}

func (m *memRepo) UpdateTx(_ context.Context, _ pgx.Tx, id uuid.UUID, in UpdatePostData) (Post, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.posts[id]
	if !ok {
		return Post{}, ErrNotFound
	}
	p.Title, p.Slug, p.Excerpt, p.Body = in.Title, in.Slug, in.Excerpt, in.Body
	p.Status, p.PublishedAt, p.ScheduledAt, p.ReadingTime = in.Status, in.PublishedAt, in.ScheduledAt, in.ReadingTime
	p.MetaTitle, p.MetaDescription = in.MetaTitle, in.MetaDescription
	p.CanonicalURL, p.NoIndex = in.CanonicalURL, in.NoIndex
	p.UpdatedAt = time.Now()
	m.posts[id] = p
	return p, nil
}

func (m *memRepo) GetByID(_ context.Context, id uuid.UUID) (Post, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.posts[id]
	if !ok {
		return Post{}, ErrNotFound
	}
	return p, nil
}

func (m *memRepo) GetActiveByID(ctx context.Context, id uuid.UUID) (Post, error) {
	p, err := m.GetByID(ctx, id)
	if err != nil {
		return Post{}, err
	}
	if p.DeletedAt != nil {
		return Post{}, ErrNotFound
	}
	return p, nil
}

func (m *memRepo) GetPublishedBySlug(_ context.Context, slug string) (Post, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.posts {
		if p.Slug == slug && p.Published() {
			return p, nil
		}
	}
	return Post{}, ErrNotFound
}

func (m *memRepo) SlugTaken(_ context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.posts {
		if p.Slug == slug && p.ID != excludeID {
			return true, nil
		}
	}
	return false, nil
}

func (m *memRepo) List(context.Context, ListFilter) ([]Post, error)        { return nil, nil }
func (m *memRepo) Count(context.Context, ListFilter) (int, error)          { return 0, nil }
func (m *memRepo) ListTrashed(context.Context, int, int) ([]Post, error)   { return nil, nil }
func (m *memRepo) CountTrashed(context.Context) (int, error)               { return 0, nil }
func (m *memRepo) ListPublished(context.Context, int, int) ([]Post, error) { return nil, nil }
func (m *memRepo) CountPublished(context.Context) (int, error)             { return 0, nil }

func (m *memRepo) SitemapItems(context.Context) ([]kernel.SitemapItem, error) { return nil, nil }

func (m *memRepo) ListPublishedFiltered(context.Context, string, string, int, int) ([]Post, error) {
	return nil, nil
}

func (m *memRepo) CountPublishedFiltered(context.Context, string, string) (int, error) { return 0, nil }

func (m *memRepo) ListRelatedPublished(context.Context, uuid.UUID, int) ([]Post, error) {
	return nil, nil
}

func (m *memRepo) GetPublishedByIDs(_ context.Context, ids []uuid.UUID) ([]Post, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Post
	for _, id := range ids {
		if p, ok := m.posts[id]; ok && p.Published() {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *memRepo) ListPublishedByAuthor(_ context.Context, authorID uuid.UUID) ([]Post, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Post
	for _, p := range m.posts {
		if p.AuthorID == authorID && p.Published() {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *memRepo) TrashTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.posts[id]
	now := time.Now()
	p.DeletedAt = &now
	m.posts[id] = p
	return nil
}

func (m *memRepo) RestoreTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.posts[id]
	p.DeletedAt = nil
	m.posts[id] = p
	return nil
}

func (m *memRepo) PermanentDeleteTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.posts, id)
	return nil
}

func (m *memRepo) ListDueScheduledIDs(_ context.Context, now time.Time) ([]uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []uuid.UUID
	for _, p := range m.posts {
		if p.Status == kernel.StatusDraft && p.ScheduledAt != nil && !p.ScheduledAt.After(now) && p.DeletedAt == nil {
			out = append(out, p.ID)
		}
	}
	return out, nil
}

func (m *memRepo) LikeTx(_ context.Context, _ pgx.Tx, postID, userID uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.likes[postID] == nil {
		m.likes[postID] = map[uuid.UUID]bool{}
	}
	if m.likes[postID][userID] {
		return false, nil
	}
	m.likes[postID][userID] = true
	return true, nil
}

func (m *memRepo) UnlikeTx(_ context.Context, _ pgx.Tx, postID, userID uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.likes[postID] == nil || !m.likes[postID][userID] {
		return false, nil
	}
	delete(m.likes[postID], userID)
	return true, nil
}

func (m *memRepo) SyncLikeCountTx(_ context.Context, _ pgx.Tx, postID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.posts[postID]
	p.LikeCount = len(m.likes[postID])
	m.posts[postID] = p
	return nil
}

func (m *memRepo) HasLiked(_ context.Context, postID, userID uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.likes[postID][userID], nil
}

// memRevisions is an in-memory kernel.RevisionRepository.
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

// fakeAuthz grants any action to users in the allow set; ownership is layered on
// top by the service.
type fakeAuthz struct{ allowed map[uuid.UUID]bool }

func (a fakeAuthz) Can(_ context.Context, userID uuid.UUID, _ string, _ string) bool {
	return a.allowed[userID]
}

// fakeRoles maps user ids to role keys.
type fakeRoles struct{ byUser map[uuid.UUID]string }

func (r fakeRoles) RoleKey(_ context.Context, userID uuid.UUID) (string, error) {
	return r.byUser[userID], nil
}

// nullBus discards events (it satisfies Publisher). For revision-emit assertions
// a real *events.Bus is used instead.
type nullBus struct{}

func (nullBus) Publish(context.Context, pgx.Tx, events.Event) error { return nil }

// --- test fixtures -----------------------------------------------------------

func fixedClock(t time.Time) Clock { return func() time.Time { return t } }

func newTestService(repo Repository, revs kernel.RevisionRepository, authz Authorizer, roles UserRoleResolver, bus Publisher, now time.Time) *Service {
	return NewService(fakeBeginner{}, repo, revs, authz, roles, bus, fixedClock(now))
}

// --- tests -------------------------------------------------------------------

func TestCreate_SanitizesBodyOnSave(t *testing.T) {
	author := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p, err := svc.Create(context.Background(), author, CreateInput{
		Title: "Hello",
		Body:  `<p>ok</p><script>alert(1)</script>`,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := p.Body; got != "<p>ok</p>" {
		t.Errorf("body not sanitized on save: got %q", got)
	}
	if p.ReadingTime < 1 {
		t.Errorf("reading time not computed: %d", p.ReadingTime)
	}
	if p.Slug != "hello" {
		t.Errorf("slug = %q, want hello", p.Slug)
	}
}

func TestUpdate_SanitizesBodyOnSave(t *testing.T) {
	author := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), author, CreateInput{Title: "T", Body: "<p>x</p>"})
	evil := `<p>safe</p><img src=x onerror=alert(1)>`
	updated, err := svc.Update(context.Background(), author, p.ID, UpdateInput{Body: &evil})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := updated.Body; got != `<p>safe</p><img src="x">` && got != `<p>safe</p><img src="x"/>` {
		t.Errorf("update did not sanitize: got %q", got)
	}
}

func TestPublish_StampsOnceAndPreserves(t *testing.T) {
	author := uuid.New()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, t0)

	p, _ := svc.Create(context.Background(), author, CreateInput{Title: "P", Body: "<p>b</p>"})

	// First publish stamps published_at = t0.
	pub, err := svc.Publish(context.Background(), author, p.ID)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if pub.PublishedAt == nil || !pub.PublishedAt.Equal(t0) {
		t.Fatalf("first publish did not stamp t0: %v", pub.PublishedAt)
	}

	// Unpublish then re-publish at a LATER clock must preserve the original t0.
	svc.now = fixedClock(t0.Add(48 * time.Hour))
	if _, err := svc.Unpublish(context.Background(), author, p.ID); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	republished, err := svc.Publish(context.Background(), author, p.ID)
	if err != nil {
		t.Fatalf("republish: %v", err)
	}
	if republished.PublishedAt == nil || !republished.PublishedAt.Equal(t0) {
		t.Errorf("re-publish must preserve original published_at %v, got %v", t0, republished.PublishedAt)
	}
}

func TestUpdate_OwnershipDeniedForAuthorEditingOthers(t *testing.T) {
	owner := uuid.New()
	intruder := uuid.New() // also an Author, but not the owner
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{owner: true, intruder: true}},
		fakeRoles{byUser: map[uuid.UUID]string{owner: accounts.RoleAuthor, intruder: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), owner, CreateInput{Title: "Owned", Body: "<p>x</p>"})

	newTitle := "hijacked"
	_, err := svc.Update(context.Background(), intruder, p.ID, UpdateInput{Title: &newTitle})
	if err != ErrForbidden {
		t.Fatalf("expected ErrForbidden for author editing another's post, got %v", err)
	}
}

func TestUpdate_EditorMayEditOthers(t *testing.T) {
	owner := uuid.New()
	editor := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{owner: true, editor: true}},
		fakeRoles{byUser: map[uuid.UUID]string{owner: accounts.RoleAuthor, editor: accounts.RoleEditor}},
		nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), owner, CreateInput{Title: "Owned", Body: "<p>x</p>"})
	newTitle := "edited by editor"
	if _, err := svc.Update(context.Background(), editor, p.ID, UpdateInput{Title: &newTitle}); err != nil {
		t.Fatalf("editor should edit any post, got %v", err)
	}
}

func TestCreate_DeniedWithoutPermission(t *testing.T) {
	member := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{}}, // member has no create grant
		fakeRoles{byUser: map[uuid.UUID]string{member: accounts.RoleMember}},
		nullBus{}, time.Now())
	if _, err := svc.Create(context.Background(), member, CreateInput{Title: "x", Body: "y"}); err != ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}

func TestSchedule_SetsDraftWithScheduledAt(t *testing.T) {
	author := uuid.New()
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, now)

	p, _ := svc.Create(context.Background(), author, CreateInput{Title: "S", Body: "<p>b</p>"})
	at := now.Add(24 * time.Hour)
	sch, err := svc.Schedule(context.Background(), author, p.ID, at)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if !sch.Scheduled() || sch.ScheduledAt == nil || !sch.ScheduledAt.Equal(at) {
		t.Errorf("post not scheduled correctly: status=%s scheduledAt=%v", sch.Status, sch.ScheduledAt)
	}
}

func TestPublishDue_AutoPublishesDuePost(t *testing.T) {
	author := uuid.New()
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, now)

	p, _ := svc.Create(context.Background(), author, CreateInput{Title: "Due", Body: "<p>b</p>"})
	_, _ = svc.Schedule(context.Background(), author, p.ID, now.Add(-time.Minute)) // already due

	n, err := svc.PublishDue(context.Background())
	if err != nil {
		t.Fatalf("publishDue: %v", err)
	}
	if n != 1 {
		t.Fatalf("published count = %d, want 1", n)
	}
	got, _ := repo.GetByID(context.Background(), p.ID)
	if !got.Published() {
		t.Errorf("due post was not published: status=%s", got.Status)
	}
	if got.ScheduledAt != nil {
		t.Errorf("scheduled_at should be cleared after auto-publish")
	}
	if got.PublishedAt == nil {
		t.Errorf("published_at should be stamped on auto-publish")
	}
}

func TestLike_Idempotent(t *testing.T) {
	author := uuid.New()
	liker := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), author, CreateInput{Title: "L", Body: "<p>b</p>", Status: kernel.StatusPublished})

	for i := 0; i < 3; i++ {
		liked, err := svc.Like(context.Background(), p.ID, liker)
		if err != nil {
			t.Fatalf("like %d: %v", i, err)
		}
		if liked.LikeCount != 1 {
			t.Fatalf("after %d likes count = %d, want 1 (idempotent)", i+1, liked.LikeCount)
		}
	}
	unliked, err := svc.Unlike(context.Background(), p.ID, liker)
	if err != nil {
		t.Fatalf("unlike: %v", err)
	}
	if unliked.LikeCount != 0 {
		t.Errorf("after unlike count = %d, want 0", unliked.LikeCount)
	}
}

func TestRestoreRevision_ReappliesSnapshot(t *testing.T) {
	author := uuid.New()
	repo := newMemRepo()
	revs := newMemRevisions()
	svc := newTestService(repo, revs,
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p, _ := svc.Create(context.Background(), author, CreateInput{Title: "Original", Body: "<p>v1</p>"})

	// Update creates a revision of the ORIGINAL.
	newTitle, newBody := "Changed", "<p>v2</p>"
	if _, err := svc.Update(context.Background(), author, p.ID, UpdateInput{Title: &newTitle, Body: &newBody}); err != nil {
		t.Fatalf("update: %v", err)
	}

	list, _ := revs.List(context.Background(), kernel.EntityTypePost, p.ID)
	if len(list) != 1 {
		t.Fatalf("want 1 revision after update, got %d", len(list))
	}
	// The snapshot must carry the original title.
	var snap struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal(list[0].Snapshot, &snap)
	if snap.Title != "Original" {
		t.Fatalf("snapshot title = %q, want Original", snap.Title)
	}

	restored, err := svc.RestoreRevision(context.Background(), author, p.ID, list[0].ID)
	if err != nil {
		t.Fatalf("restore revision: %v", err)
	}
	if restored.Title != "Original" || restored.Body != "<p>v1</p>" {
		t.Errorf("restore did not reapply snapshot: title=%q body=%q", restored.Title, restored.Body)
	}
	// Restore itself snapshots the pre-restore state -> now 2 revisions.
	list2, _ := revs.List(context.Background(), kernel.EntityTypePost, p.ID)
	if len(list2) != 2 {
		t.Errorf("restore must itself snapshot; want 2 revisions, got %d", len(list2))
	}
}

func TestUpdate_EmitsSyncRevisionEvent(t *testing.T) {
	author := uuid.New()
	repo := newMemRepo()
	bus := events.NewBus(nil)
	var got int
	bus.SubscribeSync(EventRevisionCreated, func(context.Context, pgx.Tx, events.Event) error {
		got++
		return nil
	})
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		bus, time.Now())

	p, _ := svc.Create(context.Background(), author, CreateInput{Title: "E", Body: "<p>b</p>"})
	nt := "E2"
	if _, err := svc.Update(context.Background(), author, p.ID, UpdateInput{Title: &nt}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got != 1 {
		t.Errorf("expected 1 sync revision event, got %d", got)
	}
}

func TestSlugDedupe(t *testing.T) {
	author := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p1, _ := svc.Create(context.Background(), author, CreateInput{Title: "Same Title", Body: "<p>a</p>"})
	p2, _ := svc.Create(context.Background(), author, CreateInput{Title: "Same Title", Body: "<p>b</p>"})
	if p1.Slug != "same-title" {
		t.Errorf("p1 slug = %q", p1.Slug)
	}
	if p2.Slug != "same-title-2" {
		t.Errorf("p2 slug = %q, want same-title-2", p2.Slug)
	}
}

func TestSEOMeta_RoundTripsAndTranslationOverlay(t *testing.T) {
	author := uuid.New()
	repo := newMemRepo()
	svc := newTestService(repo, newMemRevisions(),
		fakeAuthz{allowed: map[uuid.UUID]bool{author: true}},
		fakeRoles{byUser: map[uuid.UUID]string{author: accounts.RoleAuthor}},
		nullBus{}, time.Now())

	p, err := svc.Create(context.Background(), author, CreateInput{
		Title: "SEO", Body: "<p>b</p>", Status: kernel.StatusPublished,
		MetaTitle: "  Base Meta Title  ", MetaDescription: "Base desc",
		CanonicalURL: " https://x/seo ", NoIndex: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Create trims and persists all four base fields.
	if p.MetaTitle != "Base Meta Title" || p.MetaDescription != "Base desc" {
		t.Errorf("base meta not round-tripped: %q / %q", p.MetaTitle, p.MetaDescription)
	}
	if p.CanonicalURL != "https://x/seo" || !p.NoIndex {
		t.Errorf("structural SEO not persisted: canonical=%q noindex=%v", p.CanonicalURL, p.NoIndex)
	}

	// A de translation overrides meta_title but leaves meta_description empty ->
	// base fallback; canonical_url/noindex are structural and never per-locale.
	de := mustLocale(t, "de")
	if err := svc.SaveTranslation(context.Background(), author, p.ID, de, TranslationInput{
		Title: "DE", MetaTitle: "DE Meta Title",
	}); err != nil {
		t.Fatalf("save translation: %v", err)
	}
	got, err := svc.GetInLocale(context.Background(), author, p.ID, de)
	if err != nil {
		t.Fatalf("get in locale: %v", err)
	}
	if got.MetaTitle != "DE Meta Title" {
		t.Errorf("meta_title overlay = %q, want DE Meta Title", got.MetaTitle)
	}
	if got.MetaDescription != "Base desc" {
		t.Errorf("meta_description should fall back to base, got %q", got.MetaDescription)
	}
	if got.CanonicalURL != "https://x/seo" || !got.NoIndex {
		t.Errorf("structural SEO must not be per-locale: canonical=%q noindex=%v", got.CanonicalURL, got.NoIndex)
	}
}

func mustLocale(t *testing.T, s string) i18n.Locale {
	t.Helper()
	l, ok := i18n.Parse(s)
	if !ok {
		t.Fatalf("locale %q not supported", s)
	}
	return l
}
