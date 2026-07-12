package comments

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
)

// --- test doubles ------------------------------------------------------------

// fakeTx is a no-op pgx.Tx; the fake repo ignores it.
type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

// fakeBeginner runs RunInTx against a no-op tx.
type fakeBeginner struct{}

func (fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return fakeTx{}, nil }

// fakeRepo is an in-memory Repository.
type fakeRepo struct {
	store      map[uuid.UUID]Comment
	createErr  error
	created    *CreateCommentData
	updateBody *struct {
		id     uuid.UUID
		body   string
		status Status
	}
}

func newFakeRepo() *fakeRepo { return &fakeRepo{store: map[uuid.UUID]Comment{}} }

func (r *fakeRepo) put(c Comment) Comment {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	r.store[c.ID] = c
	return c
}

func (r *fakeRepo) CreateTx(_ context.Context, _ pgx.Tx, in CreateCommentData) (Comment, error) {
	if r.createErr != nil {
		return Comment{}, r.createErr
	}
	cp := in
	r.created = &cp
	c := Comment{
		ID:           uuid.New(),
		PostID:       in.PostID,
		ParentID:     in.ParentID,
		AuthorUserID: in.AuthorUserID,
		AuthorName:   in.AuthorName,
		AuthorEmail:  in.AuthorEmail,
		AuthorIP:     in.AuthorIP,
		Body:         in.Body,
		Status:       in.Status,
		CreatedAt:    time.Now(),
	}
	r.store[c.ID] = c
	return c, nil
}

func (r *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (Comment, error) {
	c, ok := r.store[id]
	if !ok {
		return Comment{}, ErrNotFound
	}
	return c, nil
}

func (r *fakeRepo) GetApprovedByID(_ context.Context, id, postID uuid.UUID) (Comment, error) {
	c, ok := r.store[id]
	if !ok || c.Status != StatusApproved || c.PostID != postID {
		return Comment{}, ErrNotFound
	}
	return c, nil
}

func (r *fakeRepo) ListApprovedForPost(_ context.Context, postID uuid.UUID) ([]Comment, error) {
	var out []Comment
	for _, c := range r.store {
		if c.PostID == postID && c.Status == StatusApproved {
			out = append(out, c)
		}
	}
	sortCommentsByCreated(out)
	return out, nil
}

func (r *fakeRepo) ListForModeration(_ context.Context, f ModerationFilter) ([]Comment, error) {
	var out []Comment
	for _, c := range r.store {
		if f.Status != nil && c.Status != *f.Status {
			continue
		}
		out = append(out, c)
	}
	sortCommentsByCreated(out)
	return out, nil
}

func (r *fakeRepo) CountForModeration(_ context.Context, status *Status) (int, error) {
	n := 0
	for _, c := range r.store {
		if status == nil || c.Status == *status {
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) CountsByStatus(_ context.Context) ([]StatusCount, error) {
	m := map[Status]int{}
	for _, c := range r.store {
		m[c.Status]++
	}
	out := make([]StatusCount, 0, len(m))
	for s, n := range m {
		out = append(out, StatusCount{Status: s, Count: n})
	}
	return out, nil
}

func (r *fakeRepo) UpdateStatusTx(_ context.Context, _ pgx.Tx, id uuid.UUID, status Status) (Comment, error) {
	c, ok := r.store[id]
	if !ok {
		return Comment{}, ErrNotFound
	}
	c.Status = status
	r.store[id] = c
	return c, nil
}

func (r *fakeRepo) UpdateBodyTx(_ context.Context, _ pgx.Tx, id uuid.UUID, body string, status Status) (Comment, error) {
	c, ok := r.store[id]
	if !ok {
		return Comment{}, ErrNotFound
	}
	c.Body = body
	c.Status = status
	now := time.Now()
	c.EditedAt = &now
	r.store[id] = c
	r.updateBody = &struct {
		id     uuid.UUID
		body   string
		status Status
	}{id, body, status}
	return c, nil
}

func (r *fakeRepo) DeleteTx(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	if _, ok := r.store[id]; !ok {
		return ErrNotFound
	}
	delete(r.store, id)
	return nil
}

func sortCommentsByCreated(cs []Comment) {
	for i := 1; i < len(cs); i++ {
		for j := i; j > 0 && cs[j].CreatedAt.Before(cs[j-1].CreatedAt); j-- {
			cs[j], cs[j-1] = cs[j-1], cs[j]
		}
	}
}

// fakePosts is a PostLookup.
type fakePosts struct {
	bySlug map[string]PostRef
	emails map[uuid.UUID]string
}

func (p fakePosts) PublishedBySlug(_ context.Context, slug string) (PostRef, error) {
	ref, ok := p.bySlug[slug]
	if !ok {
		return PostRef{}, ErrNotFound
	}
	return ref, nil
}

func (p fakePosts) AuthorEmail(_ context.Context, postID uuid.UUID) (string, error) {
	return p.emails[postID], nil
}

// fakeAuthz answers Can with a fixed allow/deny per (action,subject).
type fakeAuthz struct{ allow map[string]bool }

func (a fakeAuthz) Can(_ context.Context, _ uuid.UUID, action, subject string) bool {
	return a.allow[action+":"+subject]
}

func allowAll() fakeAuthz {
	return fakeAuthz{allow: map[string]bool{
		accounts.ActionRead + ":" + accounts.SubjectComment:   true,
		accounts.ActionUpdate + ":" + accounts.SubjectComment: true,
		accounts.ActionDelete + ":" + accounts.SubjectComment: true,
	}}
}

// fakeSpam is a SpamChecker that returns a fixed verdict.
type fakeSpam struct {
	ok     bool
	err    error
	called bool
}

func (s *fakeSpam) Verify(context.Context, string) (bool, error) {
	s.called = true
	return s.ok, s.err
}

// fakeLimiter is a RateLimiter that allows the first n calls.
type fakeLimiter struct {
	remaining int
}

func (l *fakeLimiter) Allow(string) bool {
	if l.remaining <= 0 {
		return false
	}
	l.remaining--
	return true
}

// recordingBus captures published events.
type recordingBus struct{ events []events.Event }

func (b *recordingBus) Publish(_ context.Context, _ pgx.Tx, e events.Event) error {
	b.events = append(b.events, e)
	return nil
}

// --- helpers -----------------------------------------------------------------

func newPost(t *testing.T, repo *fakeRepo, posts *fakePosts, slug string) PostRef {
	t.Helper()
	ref := PostRef{ID: uuid.New(), Slug: slug, Title: "Post " + slug}
	if posts.bySlug == nil {
		posts.bySlug = map[string]PostRef{}
	}
	posts.bySlug[slug] = ref
	return ref
}

func newService(repo *fakeRepo, posts *fakePosts, authz fakeAuthz, spam SpamChecker, limiter RateLimiter, bus Publisher, now Clock) *Service {
	return NewService(fakeBeginner{}, repo, posts, authz, spam, limiter, bus, now)
}

// --- tests -------------------------------------------------------------------

func TestSubmit_GuestPending(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	newPost(t, repo, posts, "hello")
	bus := &recordingBus{}
	svc := newService(repo, posts, allowAll(), nil, nil, bus, nil)

	c, err := svc.Submit(context.Background(), SubmitInput{
		Slug:        "hello",
		AuthorName:  "Guest",
		AuthorEmail: "guest@example.com",
		Body:        "Nice post!",
		ClientIP:    "1.2.3.4",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if c.Status != StatusPending {
		t.Fatalf("status = %s, want PENDING", c.Status)
	}
	if c.AuthorUserID != nil {
		t.Fatalf("guest comment should have nil AuthorUserID")
	}
	if c.AuthorEmail != "guest@example.com" {
		t.Fatalf("email not stored: %q", c.AuthorEmail)
	}
}

func TestSubmit_GuestRequiresNameEmail(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	newPost(t, repo, posts, "hello")
	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, nil)

	_, err := svc.Submit(context.Background(), SubmitInput{Slug: "hello", Body: "hi"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestSubmit_AuthedUsesAccountIdentity(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	newPost(t, repo, posts, "hello")
	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, nil)

	viewer := &Viewer{ID: uuid.New(), Name: "Alice", Email: "alice@acct.com"}
	c, err := svc.Submit(context.Background(), SubmitInput{
		Slug:        "hello",
		Body:        "from a member",
		AuthorName:  "ignored",
		AuthorEmail: "ignored@x.com",
		Viewer:      viewer,
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if c.AuthorUserID == nil || *c.AuthorUserID != viewer.ID {
		t.Fatalf("author user id not set")
	}
	if c.AuthorName != "Alice" || c.AuthorEmail != "alice@acct.com" {
		t.Fatalf("account identity not used: %q %q", c.AuthorName, c.AuthorEmail)
	}
}

func TestSubmit_SanitizesBody(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	newPost(t, repo, posts, "hello")
	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, nil)

	c, err := svc.Submit(context.Background(), SubmitInput{
		Slug:        "hello",
		AuthorName:  "G",
		AuthorEmail: "g@x.com",
		Body:        "<script>alert(1)</script>hi",
		ClientIP:    "9.9.9.9",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if want := "hi"; c.Body != want {
		t.Fatalf("body = %q, want sanitized %q", c.Body, want)
	}
}

func TestSubmit_ThreadingRejectsCrossPostParent(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	a := newPost(t, repo, posts, "a")
	newPost(t, repo, posts, "b")
	// approved parent belongs to post A.
	parent := repo.put(Comment{PostID: a.ID, Status: StatusApproved, AuthorName: "X", Body: "p", CreatedAt: time.Now()})

	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, nil)
	pid := parent.ID
	_, err := svc.Submit(context.Background(), SubmitInput{
		Slug: "b", ParentID: &pid, AuthorName: "G", AuthorEmail: "g@x.com", Body: "reply",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation (cross-post parent)", err)
	}
}

func TestSubmit_ThreadingRejectsNonApprovedParent(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	a := newPost(t, repo, posts, "a")
	parent := repo.put(Comment{PostID: a.ID, Status: StatusPending, AuthorName: "X", Body: "p", CreatedAt: time.Now()})

	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, nil)
	pid := parent.ID
	_, err := svc.Submit(context.Background(), SubmitInput{
		Slug: "a", ParentID: &pid, AuthorName: "G", AuthorEmail: "g@x.com", Body: "reply",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation (non-approved parent)", err)
	}
}

func TestSubmit_RateLimited(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	newPost(t, repo, posts, "hello")
	limiter := &fakeLimiter{remaining: 8} // 8/min parity
	svc := newService(repo, posts, allowAll(), nil, limiter, &recordingBus{}, nil)

	in := SubmitInput{Slug: "hello", AuthorName: "G", AuthorEmail: "g@x.com", Body: "hi", ClientIP: "5.5.5.5"}
	for i := 0; i < 8; i++ {
		if _, err := svc.Submit(context.Background(), in); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}
	if _, err := svc.Submit(context.Background(), in); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("9th submit err = %v, want ErrRateLimited", err)
	}
}

func TestSubmit_RecaptchaNoopWithoutKeys(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	newPost(t, repo, posts, "hello")
	// spam == nil simulates "no verifier wired"; submit must still succeed.
	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, nil)
	if _, err := svc.Submit(context.Background(), SubmitInput{
		Slug: "hello", AuthorName: "G", AuthorEmail: "g@x.com", Body: "hi",
	}); err != nil {
		t.Fatalf("submit without spam checker should pass: %v", err)
	}
}

func TestSubmit_RecaptchaRejects(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	newPost(t, repo, posts, "hello")
	spam := &fakeSpam{ok: false}
	svc := newService(repo, posts, allowAll(), spam, nil, &recordingBus{}, nil)
	_, err := svc.Submit(context.Background(), SubmitInput{
		Slug: "hello", AuthorName: "G", AuthorEmail: "g@x.com", Body: "hi", RecaptchaToken: "tok",
	})
	if !errors.Is(err, ErrSpam) {
		t.Fatalf("err = %v, want ErrSpam", err)
	}
	if !spam.called {
		t.Fatal("spam checker not consulted")
	}
}

func TestSubmit_EmitsCommentCreatedEvent(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	newPost(t, repo, posts, "hello")
	bus := &recordingBus{}
	svc := newService(repo, posts, allowAll(), nil, nil, bus, nil)

	c, err := svc.Submit(context.Background(), SubmitInput{
		Slug: "hello", AuthorName: "G", AuthorEmail: "g@x.com", Body: "hi",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if len(bus.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(bus.events))
	}
	ev, ok := bus.events[0].(CommentCreatedEvent)
	if !ok {
		t.Fatalf("event type = %T, want CommentCreatedEvent", bus.events[0])
	}
	if ev.Name() != EventCommentCreated {
		t.Fatalf("event name = %s", ev.Name())
	}
	if ev.CommentID != c.ID {
		t.Fatalf("event comment id mismatch")
	}
	// PII boundary: the event payload must not carry email/IP — it has no such fields.
}

func TestPublicThread_StripsEmailAndIP(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	p := newPost(t, repo, posts, "hello")
	repo.put(Comment{
		PostID: p.ID, Status: StatusApproved, AuthorName: "Pub",
		AuthorEmail: "secret@example.com", AuthorIP: "10.0.0.1",
		Body: "visible body", CreatedAt: time.Now(),
	})
	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, nil)

	tree, total, err := svc.PublicThread(context.Background(), "hello", nil)
	if err != nil {
		t.Fatalf("public thread: %v", err)
	}
	if total != 1 || len(tree) != 1 {
		t.Fatalf("expected 1 comment, got total=%d len=%d", total, len(tree))
	}
	// PublicComment has no email/IP fields by construction; assert the body is
	// present and the projection carries only public fields.
	if tree[0].Body != "visible body" {
		t.Fatalf("body = %q", tree[0].Body)
	}
	if tree[0].AuthorName != "Pub" {
		t.Fatalf("name = %q", tree[0].AuthorName)
	}
}

func TestPublicThread_NestsReplies(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	p := newPost(t, repo, posts, "hello")
	root := repo.put(Comment{PostID: p.ID, Status: StatusApproved, AuthorName: "R", Body: "root", CreatedAt: time.Now().Add(-time.Hour)})
	rid := root.ID
	repo.put(Comment{PostID: p.ID, ParentID: &rid, Status: StatusApproved, AuthorName: "C", Body: "child", CreatedAt: time.Now()})

	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, nil)
	tree, total, err := svc.PublicThread(context.Background(), "hello", nil)
	if err != nil {
		t.Fatalf("thread: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(tree) != 1 || len(tree[0].Replies) != 1 {
		t.Fatalf("expected 1 root with 1 reply, got %d roots", len(tree))
	}
}

func TestSelfEdit_WithinWindow(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	p := newPost(t, repo, posts, "hello")
	uid := uuid.New()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c := repo.put(Comment{PostID: p.ID, AuthorUserID: &uid, Status: StatusApproved, Body: "old", CreatedAt: now.Add(-5 * time.Minute)})

	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, func() time.Time { return now })
	updated, err := svc.SelfEdit(context.Background(), Viewer{ID: uid}, c.ID, "new body")
	if err != nil {
		t.Fatalf("self-edit: %v", err)
	}
	if updated.Body != "new body" {
		t.Fatalf("body = %q", updated.Body)
	}
	if updated.EditedAt == nil {
		t.Fatal("edited_at not stamped")
	}
	if updated.Status != StatusPending {
		t.Fatalf("status = %s, want PENDING (re-moderation)", updated.Status)
	}
}

func TestSelfEdit_WindowExpired(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	p := newPost(t, repo, posts, "hello")
	uid := uuid.New()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c := repo.put(Comment{PostID: p.ID, AuthorUserID: &uid, Status: StatusApproved, Body: "old", CreatedAt: now.Add(-30 * time.Minute)})

	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, func() time.Time { return now })
	_, err := svc.SelfEdit(context.Background(), Viewer{ID: uid}, c.ID, "new")
	if !errors.Is(err, ErrEditWindowExpired) {
		t.Fatalf("err = %v, want ErrEditWindowExpired", err)
	}
}

func TestSelfEdit_NotOwnerForbidden(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	p := newPost(t, repo, posts, "hello")
	owner := uuid.New()
	c := repo.put(Comment{PostID: p.ID, AuthorUserID: &owner, Status: StatusApproved, Body: "x", CreatedAt: time.Now()})

	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, nil)
	_, err := svc.SelfEdit(context.Background(), Viewer{ID: uuid.New()}, c.ID, "new")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

func TestModeration_StatusUpdates(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	p := newPost(t, repo, posts, "hello")
	c := repo.put(Comment{PostID: p.ID, Status: StatusPending, Body: "x", CreatedAt: time.Now()})
	actor := uuid.New()
	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, nil)

	if got, err := svc.Approve(context.Background(), actor, c.ID); err != nil || got.Status != StatusApproved {
		t.Fatalf("approve: status=%v err=%v", got.Status, err)
	}
	if got, err := svc.Spam(context.Background(), actor, c.ID); err != nil || got.Status != StatusSpam {
		t.Fatalf("spam: status=%v err=%v", got.Status, err)
	}
	if got, err := svc.Trash(context.Background(), actor, c.ID); err != nil || got.Status != StatusTrash {
		t.Fatalf("trash: status=%v err=%v", got.Status, err)
	}
}

func TestModeration_RequiresPermission(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	p := newPost(t, repo, posts, "hello")
	c := repo.put(Comment{PostID: p.ID, Status: StatusPending, Body: "x", CreatedAt: time.Now()})
	denied := fakeAuthz{allow: map[string]bool{}}
	svc := newService(repo, posts, denied, nil, nil, &recordingBus{}, nil)

	if _, err := svc.Approve(context.Background(), uuid.New(), c.ID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("approve err = %v, want ErrForbidden", err)
	}
	if _, _, err := svc.AdminList(context.Background(), uuid.New(), ModerationFilter{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("adminlist err = %v, want ErrForbidden", err)
	}
}

func TestStatusCounts(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	p := newPost(t, repo, posts, "hello")
	repo.put(Comment{PostID: p.ID, Status: StatusPending, Body: "a", CreatedAt: time.Now()})
	repo.put(Comment{PostID: p.ID, Status: StatusPending, Body: "b", CreatedAt: time.Now()})
	repo.put(Comment{PostID: p.ID, Status: StatusApproved, Body: "c", CreatedAt: time.Now()})
	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, nil)

	counts, err := svc.StatusCounts(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts[StatusPending] != 2 || counts[StatusApproved] != 1 || counts[StatusSpam] != 0 {
		t.Fatalf("counts = %v", counts)
	}
}

func TestBulkApprove_SkipsMissing(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	p := newPost(t, repo, posts, "hello")
	c1 := repo.put(Comment{PostID: p.ID, Status: StatusPending, Body: "a", CreatedAt: time.Now()})
	c2 := repo.put(Comment{PostID: p.ID, Status: StatusPending, Body: "b", CreatedAt: time.Now()})
	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, nil)

	res, err := svc.BulkApprove(context.Background(), uuid.New(), []uuid.UUID{c1.ID, c2.ID, uuid.New()})
	if err != nil {
		t.Fatalf("bulk approve: %v", err)
	}
	if res.AppliedCount() != 2 {
		t.Fatalf("applied = %d, want 2", res.AppliedCount())
	}
	if res.NotFoundCount() != 1 {
		t.Fatalf("missing = %d, want 1", res.NotFoundCount())
	}
}

func TestSubmit_PostNotFound(t *testing.T) {
	repo := newFakeRepo()
	posts := &fakePosts{}
	svc := newService(repo, posts, allowAll(), nil, nil, &recordingBus{}, nil)
	_, err := svc.Submit(context.Background(), SubmitInput{Slug: "missing", AuthorName: "G", AuthorEmail: "g@x.com", Body: "hi"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
