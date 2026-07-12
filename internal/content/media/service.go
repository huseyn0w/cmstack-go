package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/db"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/storage"
)

// Domain errors. ErrForbidden is the permission gate; the validation sentinels
// are re-exported from storage so the handler maps a single error set.
var (
	// ErrForbidden is returned when the actor lacks the required media grant.
	ErrForbidden = errors.New("media: forbidden")
)

// Service holds ALL media logic: validate -> store original -> probe dims ->
// generate thumbnails -> persist row+variants -> emit media.uploaded. It accesses
// data only through the repository, blobs only through the BlobStore, and fires
// side effects only via events. It owns no globals.
type Service struct {
	pool        db.Beginner
	repo        Repository
	store       BlobStore
	validator   Validator
	thumbnailer Thumbnailer
	authz       Authorizer
	bus         Publisher
	now         func() time.Time
}

// NewService constructs the media Service with explicit dependencies. A nil
// thumbnailer disables variant generation (originals still upload); a nil clock
// defaults to time.Now.
func NewService(
	pool db.Beginner,
	repo Repository,
	store BlobStore,
	validator Validator,
	thumbnailer Thumbnailer,
	authz Authorizer,
	bus Publisher,
	now func() time.Time,
) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		pool:        pool,
		repo:        repo,
		store:       store,
		validator:   validator,
		thumbnailer: thumbnailer,
		authz:       authz,
		bus:         bus,
		now:         now,
	}
}

// UploadInput is the validated upload request from the handler: the raw stream
// plus the (untrusted) client filename, recorded for display only.
type UploadInput struct {
	Reader   io.Reader
	Filename string
}

// MaxUploadBytes exposes the configured size cap so the handler can bound the
// multipart parse and the UI can show the hint.
func (s *Service) MaxUploadBytes() int64 { return s.validator.MaxBytes() }

// Upload runs the full pipeline. It requires create:media. The original is
// written to storage BEFORE the DB row so a failed upload never leaves a
// dangling reference; thumbnails are generated and written next; finally the row
// + variant rows are persisted in one transaction and media.uploaded is emitted
// (async, in-tx). On a DB failure the orphaned storage objects are best-effort
// removed (an orphan blob is preferable to a dangling ref).
func (s *Service) Upload(ctx context.Context, actorID uuid.UUID, in UploadInput) (Media, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionCreate, accounts.SubjectMedia) {
		return Media{}, ErrForbidden
	}

	validated, err := s.validator.Validate(in.Reader)
	if err != nil {
		return Media{}, err
	}

	// Derive the stored key from the VALIDATED extension only (anti-polyglot):
	// the client filename never reaches the path. ObjectKey adds a random,
	// path-safe component under a date-bucketed prefix.
	prefix := "media/" + s.now().UTC().Format("2006/01")
	key, err := storage.ObjectKey(prefix, "asset", validated.Ext)
	if err != nil {
		return Media{}, err
	}
	if _, err := s.store.Save(ctx, key, bytes.NewReader(validated.Data), validated.ContentType); err != nil {
		return Media{}, fmt.Errorf("store original: %w", err)
	}

	// Generate + store thumbnails for raster images only (PDFs get a generic icon
	// in the UI, no rasterization). Track written keys so we can roll them back
	// alongside the original if the DB write fails.
	written := []string{key}
	var variants []CreateThumbnailData
	if validated.IsRaster() && s.thumbnailer != nil {
		thumbs, gerr := s.thumbnailer.Generate(validated.Data, validated.ContentType)
		if gerr != nil {
			s.cleanup(ctx, written)
			return Media{}, fmt.Errorf("generate thumbnails: %w", gerr)
		}
		for _, th := range thumbs {
			tkey, kerr := storage.ObjectKey(prefix, "thumb", th.Ext)
			if kerr != nil {
				s.cleanup(ctx, written)
				return Media{}, kerr
			}
			if _, serr := s.store.Save(ctx, tkey, bytes.NewReader(th.Data), th.ContentType); serr != nil {
				s.cleanup(ctx, written)
				return Media{}, fmt.Errorf("store thumbnail %s: %w", th.Variant, serr)
			}
			written = append(written, tkey)
			variants = append(variants, CreateThumbnailData{
				Variant:    th.Variant,
				StorageKey: tkey,
				Width:      th.Width,
				Height:     th.Height,
			})
		}
	}

	data := CreateMediaData{
		StorageKey:       key,
		OriginalFilename: cleanFilename(in.Filename),
		MIME:             validated.ContentType,
		SizeBytes:        int64(len(validated.Data)),
		UploadedBy:       actorID,
	}
	if validated.IsRaster() {
		w, h := validated.Width, validated.Height
		data.Width, data.Height = &w, &h
	}

	var created Media
	err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		m, cerr := s.repo.CreateTx(ctx, tx, data)
		if cerr != nil {
			return fmt.Errorf("create media: %w", cerr)
		}
		created = m
		for _, v := range variants {
			v.MediaID = m.ID
			th, terr := s.repo.CreateThumbnailTx(ctx, tx, v)
			if terr != nil {
				return fmt.Errorf("create thumbnail: %w", terr)
			}
			created.Thumbnails = append(created.Thumbnails, th)
		}
		return s.bus.Publish(ctx, tx, UploadedEvent{
			MediaID:    m.ID,
			StorageKey: m.StorageKey,
			MIME:       m.MIME,
			SizeBytes:  m.SizeBytes,
			UploadedBy: actorID,
			UploadedAt: s.now(),
		})
	})
	if err != nil {
		// Roll back every orphaned storage object (best effort).
		s.cleanup(ctx, written)
		return Media{}, err
	}
	return created, nil
}

// UpdateMetadata edits the alt/title/caption of an asset. Requires update:media.
func (s *Service) UpdateMetadata(ctx context.Context, actorID, id uuid.UUID, alt, title, caption string) (Media, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectMedia) {
		return Media{}, ErrForbidden
	}
	return s.repo.UpdateMetadata(ctx, id, strings.TrimSpace(alt), strings.TrimSpace(title), strings.TrimSpace(caption))
}

// Delete removes an asset: it deletes the STORAGE objects (original + every
// variant) FIRST, then the DB row (whose thumbnail rows cascade). The order is
// deliberate — if the row delete fails after the blobs are gone we have a
// dangling ref (visible, fixable), which is still preferable to silently
// orphaned blobs nobody can find; but the common path removes both. Requires
// delete:media.
func (s *Service) Delete(ctx context.Context, actorID, id uuid.UUID) error {
	if !s.authz.Can(ctx, actorID, accounts.ActionDelete, accounts.SubjectMedia) {
		return ErrForbidden
	}
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(m.Thumbnails)+1)
	keys = append(keys, m.StorageKey)
	for _, t := range m.Thumbnails {
		keys = append(keys, t.StorageKey)
	}
	s.cleanup(ctx, keys)

	return s.repo.Delete(ctx, id)
}

// List returns a page of media (newest first) plus the total count.
func (s *Service) List(ctx context.Context, actorID uuid.UUID, limit, offset int) ([]Media, int, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectMedia) {
		return nil, 0, ErrForbidden
	}
	items, err := s.repo.List(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Get returns a single asset (with thumbnails). Requires read:media.
func (s *Service) Get(ctx context.Context, actorID, id uuid.UUID) (Media, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectMedia) {
		return Media{}, ErrForbidden
	}
	return s.repo.GetByID(ctx, id)
}

// URL resolves a storage key to its public URL via the backend.
func (s *Service) URL(key string) string { return s.store.URL(key) }

// cleanup removes every key from storage, ignoring errors (best effort). Used to
// roll back orphaned blobs on a failed upload and to delete blobs on delete.
func (s *Service) cleanup(ctx context.Context, keys []string) {
	for _, k := range keys {
		_ = s.store.Delete(ctx, k)
	}
}

// cleanFilename strips any directory component and control characters from the
// client filename so the stored display name is a bare, safe basename. It is
// display-only metadata — it NEVER influences the storage key (which is derived
// from the validated MIME).
func cleanFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if len(name) > 255 {
		name = name[:255]
	}
	if name == "." || name == ".." {
		return ""
	}
	return name
}
