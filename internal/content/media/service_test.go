package media

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/platform/events"
	"github.com/huseyn0w/cmstack-go/internal/platform/storage"
)

// --- fakes -------------------------------------------------------------------

// fakeTx is a no-op pgx.Tx; the fake repo ignores it. Embedding the interface
// means only the methods we touch need defining (none do here).
type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

// fakeBeginner runs RunInTx against a no-op tx.
type fakeBeginner struct{}

func (fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return fakeTx{}, nil }

type fakeStore struct {
	mu      sync.Mutex
	saved   map[string][]byte
	deleted []string
	saveErr error
}

func newFakeStore() *fakeStore { return &fakeStore{saved: map[string][]byte{}} }

func (f *fakeStore) Save(_ context.Context, key string, r io.Reader, _ string) (string, error) {
	if f.saveErr != nil {
		return "", f.saveErr
	}
	b, _ := io.ReadAll(r)
	f.mu.Lock()
	f.saved[key] = b
	f.mu.Unlock()
	return key, nil
}

func (f *fakeStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	f.deleted = append(f.deleted, key)
	delete(f.saved, key)
	f.mu.Unlock()
	return nil
}

func (f *fakeStore) URL(key string) string { return "/uploads/" + key }

type fakeValidator struct {
	result storage.ValidatedMedia
	err    error
}

func (f fakeValidator) Validate(io.Reader) (storage.ValidatedMedia, error) {
	return f.result, f.err
}
func (f fakeValidator) MaxBytes() int64 { return 10 << 20 }

type fakeThumbnailer struct {
	called int
	out    []storage.GeneratedThumbnail
	err    error
}

func (f *fakeThumbnailer) Generate(_ []byte, _ string) ([]storage.GeneratedThumbnail, error) {
	f.called++
	return f.out, f.err
}

// fakeRepo is an in-memory Repository.
type fakeRepo struct {
	mu        sync.Mutex
	media     map[uuid.UUID]Media
	thumbs    map[uuid.UUID][]Thumbnail
	createErr error
	deleted   []uuid.UUID
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{media: map[uuid.UUID]Media{}, thumbs: map[uuid.UUID][]Thumbnail{}}
}

func (r *fakeRepo) CreateTx(_ context.Context, _ pgx.Tx, in CreateMediaData) (Media, error) {
	if r.createErr != nil {
		return Media{}, r.createErr
	}
	m := Media{
		ID:               uuid.New(),
		StorageKey:       in.StorageKey,
		OriginalFilename: in.OriginalFilename,
		MIME:             in.MIME,
		SizeBytes:        in.SizeBytes,
		Width:            in.Width,
		Height:           in.Height,
		UploadedBy:       in.UploadedBy,
	}
	r.mu.Lock()
	r.media[m.ID] = m
	r.mu.Unlock()
	return m, nil
}

func (r *fakeRepo) CreateThumbnailTx(_ context.Context, _ pgx.Tx, in CreateThumbnailData) (Thumbnail, error) {
	t := Thumbnail{ID: uuid.New(), MediaID: in.MediaID, Variant: in.Variant, StorageKey: in.StorageKey, Width: in.Width, Height: in.Height}
	r.mu.Lock()
	r.thumbs[in.MediaID] = append(r.thumbs[in.MediaID], t)
	r.mu.Unlock()
	return t, nil
}

func (r *fakeRepo) GetByID(_ context.Context, id uuid.UUID) (Media, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.media[id]
	if !ok {
		return Media{}, ErrNotFound
	}
	m.Thumbnails = r.thumbs[id]
	return m, nil
}

func (r *fakeRepo) List(_ context.Context, _, _ int) ([]Media, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Media, 0, len(r.media))
	for _, m := range r.media {
		out = append(out, m)
	}
	return out, nil
}

func (r *fakeRepo) Count(context.Context) (int, error) { return len(r.media), nil }

func (r *fakeRepo) UpdateMetadata(_ context.Context, id uuid.UUID, alt, title, caption string) (Media, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.media[id]
	if !ok {
		return Media{}, ErrNotFound
	}
	m.Alt, m.Title, m.Caption = alt, title, caption
	r.media[id] = m
	return m, nil
}

func (r *fakeRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, id)
	delete(r.media, id)
	delete(r.thumbs, id)
	return nil
}

func (r *fakeRepo) ThumbnailsForMedia(_ context.Context, id uuid.UUID) ([]Thumbnail, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.thumbs[id], nil
}

// allowAuthz allows a fixed action set; denyAuthz denies everything.
type stubAuthz struct{ allow map[string]bool }

func (a stubAuthz) Can(_ context.Context, _ uuid.UUID, action, subject string) bool {
	return a.allow[action+":"+subject]
}

func allowAll() stubAuthz {
	return stubAuthz{allow: map[string]bool{
		accounts.ActionCreate + ":" + accounts.SubjectMedia: true,
		accounts.ActionRead + ":" + accounts.SubjectMedia:   true,
		accounts.ActionUpdate + ":" + accounts.SubjectMedia: true,
		accounts.ActionDelete + ":" + accounts.SubjectMedia: true,
	}}
}

// recordingBus records published events.
type recordingBus struct{ events []events.Event }

func (b *recordingBus) Publish(_ context.Context, _ pgx.Tx, e events.Event) error {
	b.events = append(b.events, e)
	return nil
}

// --- helpers -----------------------------------------------------------------

func rasterValidated() storage.ValidatedMedia {
	return storage.ValidatedMedia{
		Data: []byte("RASTERBYTES"), ContentType: "image/png", Ext: ".png",
		Kind: storage.KindRaster, Width: 800, Height: 600,
	}
}

func pdfValidated() storage.ValidatedMedia {
	return storage.ValidatedMedia{
		Data: []byte("%PDF-..."), ContentType: "application/pdf", Ext: ".pdf",
		Kind: storage.KindDocument,
	}
}

func newSvc(repo Repository, store BlobStore, val Validator, th Thumbnailer, authz Authorizer, bus Publisher) *Service {
	return NewService(fakeBeginner{}, repo, store, val, th, authz, bus, nil)
}

// --- tests -------------------------------------------------------------------

func TestUpload_RasterRunsFullPipeline(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	th := &fakeThumbnailer{out: []storage.GeneratedThumbnail{
		{Variant: "thumb", Data: []byte("T"), ContentType: "image/png", Ext: ".png", Width: 320, Height: 240},
		{Variant: "medium", Data: []byte("M"), ContentType: "image/png", Ext: ".png", Width: 1024, Height: 768},
	}}
	bus := &recordingBus{}
	svc := newSvc(repo, store, fakeValidator{result: rasterValidated()}, th, allowAll(), bus)

	m, err := svc.Upload(context.Background(), uuid.New(), UploadInput{Reader: strings.NewReader("ignored"), Filename: "../etc/passwd.png"})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Dimensions persisted.
	if m.Width == nil || *m.Width != 800 || m.Height == nil || *m.Height != 600 {
		t.Errorf("dims = %v x %v", m.Width, m.Height)
	}
	// Thumbnail generation invoked + 2 variants persisted.
	if th.called != 1 {
		t.Errorf("thumbnailer called %d times, want 1", th.called)
	}
	if len(m.Thumbnails) != 2 {
		t.Errorf("variants persisted = %d, want 2", len(m.Thumbnails))
	}
	// Original + 2 variants written to storage (3 keys).
	if len(store.saved) != 3 {
		t.Errorf("storage objects = %d, want 3", len(store.saved))
	}
	// Stored key derived from validated ext (.png), NOT the client filename path.
	if !strings.HasSuffix(m.StorageKey, ".png") || strings.Contains(m.StorageKey, "passwd") || strings.Contains(m.StorageKey, "..") {
		t.Errorf("storage key leaked client filename: %q", m.StorageKey)
	}
	// Filename sanitized to a bare basename for display.
	if m.OriginalFilename != "passwd.png" {
		t.Errorf("display filename = %q, want passwd.png", m.OriginalFilename)
	}
	// media.uploaded emitted.
	if len(bus.events) != 1 || bus.events[0].Name() != EventMediaUploaded {
		t.Errorf("events = %+v, want one media.uploaded", bus.events)
	}
}

func TestUpload_PDFSkipsThumbnails(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	th := &fakeThumbnailer{}
	svc := newSvc(repo, store, fakeValidator{result: pdfValidated()}, th, allowAll(), &recordingBus{})

	m, err := svc.Upload(context.Background(), uuid.New(), UploadInput{Reader: strings.NewReader("x"), Filename: "doc.pdf"})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if th.called != 0 {
		t.Errorf("PDF must not invoke thumbnailer, called %d", th.called)
	}
	if m.IsImage() {
		t.Error("PDF must not be an image")
	}
	if len(store.saved) != 1 {
		t.Errorf("only the original should be stored, got %d", len(store.saved))
	}
	if m.Width != nil || m.Height != nil {
		t.Error("PDF must carry no dimensions")
	}
}

func TestUpload_RequiresCreatePermission(t *testing.T) {
	svc := newSvc(newFakeRepo(), newFakeStore(), fakeValidator{result: rasterValidated()}, &fakeThumbnailer{}, stubAuthz{allow: map[string]bool{}}, &recordingBus{})
	_, err := svc.Upload(context.Background(), uuid.New(), UploadInput{Reader: strings.NewReader("x")})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}

func TestUpload_ValidationErrorSurfaces(t *testing.T) {
	svc := newSvc(newFakeRepo(), newFakeStore(), fakeValidator{err: storage.ErrMediaType}, &fakeThumbnailer{}, allowAll(), &recordingBus{})
	_, err := svc.Upload(context.Background(), uuid.New(), UploadInput{Reader: strings.NewReader("x")})
	if !errors.Is(err, storage.ErrMediaType) {
		t.Fatalf("want ErrMediaType, got %v", err)
	}
}

func TestUpload_DBFailureRollsBackStorage(t *testing.T) {
	repo := newFakeRepo()
	repo.createErr = errors.New("db down")
	store := newFakeStore()
	th := &fakeThumbnailer{out: []storage.GeneratedThumbnail{{Variant: "thumb", Data: []byte("T"), Ext: ".png", ContentType: "image/png", Width: 1, Height: 1}}}
	svc := newSvc(repo, store, fakeValidator{result: rasterValidated()}, th, allowAll(), &recordingBus{})

	_, err := svc.Upload(context.Background(), uuid.New(), UploadInput{Reader: strings.NewReader("x")})
	if err == nil {
		t.Fatal("expected upload to fail")
	}
	// Every written object (original + thumb) must have been cleaned up.
	if len(store.saved) != 0 {
		t.Errorf("orphaned storage objects remain: %v", store.saved)
	}
	if len(store.deleted) != 2 {
		t.Errorf("expected 2 rollback deletes, got %d", len(store.deleted))
	}
}

func TestDelete_RemovesStorageThenRow(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	th := &fakeThumbnailer{out: []storage.GeneratedThumbnail{{Variant: "thumb", Data: []byte("T"), Ext: ".png", ContentType: "image/png", Width: 1, Height: 1}}}
	svc := newSvc(repo, store, fakeValidator{result: rasterValidated()}, th, allowAll(), &recordingBus{})

	m, err := svc.Upload(context.Background(), uuid.New(), UploadInput{Reader: strings.NewReader("x")})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	store.deleted = nil // reset to observe delete

	if err := svc.Delete(context.Background(), uuid.New(), m.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Original + variant storage objects removed.
	if len(store.deleted) != 2 {
		t.Errorf("expected 2 storage deletes (original+variant), got %d: %v", len(store.deleted), store.deleted)
	}
	// Row deleted.
	if len(repo.deleted) != 1 || repo.deleted[0] != m.ID {
		t.Errorf("row not deleted: %v", repo.deleted)
	}
	// Storage delete happened before the row delete (orphan-preferable ordering).
	if _, ok := repo.media[m.ID]; ok {
		t.Error("row should be gone")
	}
}

func TestDelete_RequiresDeletePermission(t *testing.T) {
	repo := newFakeRepo()
	svc := newSvc(repo, newFakeStore(), fakeValidator{result: rasterValidated()}, &fakeThumbnailer{}, stubAuthz{allow: map[string]bool{}}, &recordingBus{})
	if err := svc.Delete(context.Background(), uuid.New(), uuid.New()); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}

func TestUpdateMetadata_TrimsAndPersists(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStore()
	svc := newSvc(repo, store, fakeValidator{result: pdfValidated()}, nil, allowAll(), &recordingBus{})
	m, _ := svc.Upload(context.Background(), uuid.New(), UploadInput{Reader: strings.NewReader("x"), Filename: "d.pdf"})

	got, err := svc.UpdateMetadata(context.Background(), uuid.New(), m.ID, "  alt text ", " Title ", " cap ")
	if err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
	if got.Alt != "alt text" || got.Title != "Title" || got.Caption != "cap" {
		t.Errorf("metadata not trimmed/persisted: %+v", got)
	}
}

func TestUpdateMetadata_RequiresUpdatePermission(t *testing.T) {
	svc := newSvc(newFakeRepo(), newFakeStore(), fakeValidator{}, nil, stubAuthz{allow: map[string]bool{}}, &recordingBus{})
	if _, err := svc.UpdateMetadata(context.Background(), uuid.New(), uuid.New(), "a", "t", "c"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}
