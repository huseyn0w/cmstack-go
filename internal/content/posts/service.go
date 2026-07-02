package posts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/platform/db"
	"github.com/huseyn0w/cmstack-go/internal/platform/i18n"
)

// Domain errors. ErrForbidden is the ownership/permission gate; ErrValidation
// carries per-field messages for the handler's error summary.
var (
	// ErrForbidden is returned when the actor lacks permission (coarse grant) or
	// ownership (Author editing another's post) for the attempted action.
	ErrForbidden = errors.New("posts: forbidden")
	// ErrTitleRequired is returned when a create/update has no usable title.
	ErrTitleRequired = errors.New("posts: title is required")
	// ErrRevisionMismatch is returned when a revision does not belong to the post.
	ErrRevisionMismatch = errors.New("posts: revision does not belong to this post")
	// ErrSlugTaken is the friendly outcome when a concurrent create raced us to
	// the same slug (a unique-violation the dedupe loop could not foresee). The
	// handler surfaces it as a validation message rather than a 500.
	ErrSlugTaken = errors.New("posts: slug is already taken")
	// ErrNotLikeable is returned when a like is attempted on a post that is not
	// in a likeable state (trashed or unpublished). Enforced in the service so the
	// rule holds regardless of the calling handler.
	ErrNotLikeable = errors.New("posts: post is not available for liking")
	// ErrDefaultLocaleTranslation is returned when a translation write targets the
	// default locale (en). The default locale's content lives on the base post row
	// and is edited via Create/Update, not the translation overlay (M7b-1).
	ErrDefaultLocaleTranslation = errors.New("posts: cannot store a translation for the default locale")
	// ErrUnsupportedLocale is returned when a translation write/read targets a
	// locale outside the supported set.
	ErrUnsupportedLocale = errors.New("posts: unsupported locale")
)

// Service holds ALL post logic. It accesses data only through the repositories,
// fires side effects only via events, and owns no globals. Ownership is enforced
// here (the seed grants are coarse; the service is the gate).
type Service struct {
	pool      db.Beginner
	repo      Repository
	revisions kernel.RevisionRepository
	authz     Authorizer
	users     UserRoleResolver
	bus       Publisher
	now       Clock
	// taxonomy persists post<->category/tag M2M inside the post write tx. It is
	// optional (nil = taxonomy not wired) and set via WithTaxonomy so existing
	// constructor call sites are unchanged (M3 seam).
	taxonomy TaxonomyAssigner
}

// WithTaxonomy attaches the taxonomy assigner that persists a post's
// category/tag associations in the same transaction as the post write and
// powers the related-posts query. It returns the receiver for chaining at wire
// time. Passing nil is a no-op (leaves taxonomy unwired).
func (s *Service) WithTaxonomy(t TaxonomyAssigner) *Service {
	s.taxonomy = t
	return s
}

// UserRoleResolver resolves a user's role key so the service can apply the
// ownership rule (Author may only touch own posts; Editor/Administrator any).
// *accounts.RoleRepoPG + *accounts.UserRepoPG compose to satisfy it via the
// small adapter below.
type UserRoleResolver interface {
	RoleKey(ctx context.Context, userID uuid.UUID) (string, error)
}

// NewService constructs the post Service with explicit dependencies.
func NewService(
	pool db.Beginner,
	repo Repository,
	revisions kernel.RevisionRepository,
	authz Authorizer,
	users UserRoleResolver,
	bus Publisher,
	now Clock,
) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		pool:      pool,
		repo:      repo,
		revisions: revisions,
		authz:     authz,
		users:     users,
		bus:       bus,
		now:       now,
	}
}

// CreateInput is the validated create request from the handler.
type CreateInput struct {
	Title       string
	Slug        string // optional; derived from title when empty
	Excerpt     string
	Body        string
	Status      kernel.Status
	ScheduledAt *time.Time
	// Taxonomy (M3): the full category/tag sets to associate with the new post,
	// persisted in the SAME transaction as the post insert. nil = none. A nil
	// SetTaxonomy on update vs. an empty slice both clear; on create an empty
	// slice simply attaches nothing.
	CategoryIDs []uuid.UUID
	TagIDs      []uuid.UUID
}

// Create makes a new post owned by authorID. Body is sanitized, reading time is
// computed, the slug is derived+deduped, and publishedAt is stamped when the
// post is created already-published. Requires create:post.
func (s *Service) Create(ctx context.Context, authorID uuid.UUID, in CreateInput) (Post, error) {
	if !s.authz.Can(ctx, authorID, accounts.ActionCreate, accounts.SubjectPost) {
		return Post{}, ErrForbidden
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		return Post{}, ErrTitleRequired
	}

	body := kernel.SanitizeRichText(in.Body)
	status := in.Status
	if !status.Valid() {
		status = kernel.StatusDraft
	}

	desired := kernel.Slugify(firstNonEmpty(in.Slug, title))
	slug, err := s.uniqueSlug(ctx, desired, uuid.Nil)
	if err != nil {
		return Post{}, err
	}

	var publishedAt, scheduledAt *time.Time
	if status == kernel.StatusPublished {
		now := s.now()
		publishedAt = &now
	} else if in.ScheduledAt != nil {
		scheduledAt = in.ScheduledAt
	}

	data := CreatePostData{
		Title:       title,
		Slug:        slug,
		Excerpt:     sanitizeExcerpt(in.Excerpt),
		Body:        body,
		Status:      status,
		PublishedAt: publishedAt,
		ScheduledAt: scheduledAt,
		AuthorID:    authorID,
		ReadingTime: kernel.ReadingTimeMinutes(body),
	}

	// A concurrent create of the same title can win the slug between our dedupe
	// check and our INSERT, surfacing as a pg unique-violation (23505). Retry a
	// bounded number of times, re-deriving a fresh unique slug each pass, so two
	// simultaneous same-title creates both succeed (with distinct slugs) instead
	// of one returning a 500. After the retry budget we map to a friendly
	// ErrSlugTaken rather than leaking the raw constraint error.
	var created Post
	for attempt := 0; attempt < 5; attempt++ {
		err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
			p, err := s.repo.CreateTx(ctx, tx, data)
			if err != nil {
				return fmt.Errorf("create post: %w", err)
			}
			created = p
			if err := s.assignTaxonomy(ctx, tx, p.ID, in.CategoryIDs, in.TagIDs); err != nil {
				return err
			}
			if p.Published() {
				return s.emitPublished(ctx, tx, p)
			}
			return nil
		})
		if err == nil {
			return created, nil
		}
		if !isSlugUniqueViolation(err) {
			return Post{}, err
		}
		// Re-derive a unique slug and retry.
		newSlug, slugErr := s.uniqueSlug(ctx, desired, uuid.Nil)
		if slugErr != nil {
			return Post{}, slugErr
		}
		data.Slug = newSlug
	}
	return Post{}, ErrSlugTaken
}

// UpdateInput is the validated update request. Pointer fields are "set" when
// non-nil; a nil field leaves the existing value unchanged.
type UpdateInput struct {
	Title       *string
	Slug        *string
	Excerpt     *string
	Body        *string
	Status      *kernel.Status
	ScheduledAt *time.Time // nil = unchanged
	// Taxonomy (M3): when SetTaxonomy is true the post's category/tag sets are
	// REPLACED with CategoryIDs/TagIDs (an empty/nil slice clears that axis),
	// persisted in the SAME transaction as the post update. When false the
	// associations are left untouched (e.g. a revision restore, which does not
	// carry taxonomy).
	SetTaxonomy bool
	CategoryIDs []uuid.UUID
	TagIDs      []uuid.UUID
}

// Update mutates an existing post. It first snapshots the prior state into a
// revision (SYNC, in-tx), then applies the changes: body is re-sanitized,
// reading time recomputed, slug re-deduped. publishedAt is stamped on first
// publish and PRESERVED thereafter. Ownership is enforced: an Author may update
// only their own post.
func (s *Service) Update(ctx context.Context, actorID uuid.UUID, id uuid.UUID, in UpdateInput) (Post, error) {
	existing, err := s.repo.GetActiveByID(ctx, id)
	if err != nil {
		return Post{}, err
	}
	if err := s.requireOwnerOrPrivileged(ctx, actorID, existing, accounts.ActionUpdate); err != nil {
		return Post{}, err
	}

	next := existing
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if t == "" {
			return Post{}, ErrTitleRequired
		}
		next.Title = t
	}
	if in.Excerpt != nil {
		next.Excerpt = sanitizeExcerpt(*in.Excerpt)
	}
	if in.Body != nil {
		next.Body = kernel.SanitizeRichText(*in.Body)
		next.ReadingTime = kernel.ReadingTimeMinutes(next.Body)
	}
	if in.Slug != nil {
		desired := kernel.Slugify(*in.Slug)
		slug, err := s.uniqueSlug(ctx, desired, id)
		if err != nil {
			return Post{}, err
		}
		next.Slug = slug
	}

	becamePublished := false
	if in.Status != nil && in.Status.Valid() {
		next.Status = *in.Status
		if *in.Status == kernel.StatusPublished {
			// Stamp publishedAt on FIRST publish only; preserve thereafter.
			if existing.PublishedAt == nil {
				now := s.now()
				next.PublishedAt = &now
			}
			// A manual publish cancels any pending schedule.
			next.ScheduledAt = nil
			becamePublished = existing.Status != kernel.StatusPublished
		}
	}
	if in.ScheduledAt != nil && next.Status != kernel.StatusPublished {
		next.ScheduledAt = in.ScheduledAt
	}

	return s.persistUpdate(ctx, actorID, existing, next, becamePublished, in)
}

// persistUpdate snapshots the prior state and writes the next state in one
// transaction, emitting the sync revision event and (when newly published) the
// async publish event. When in.SetTaxonomy is set, the category/tag M2M is
// REPLACED inside the SAME tx so the post row and its associations commit
// atomically (M3 seam).
func (s *Service) persistUpdate(ctx context.Context, actorID uuid.UUID, prior, next Post, becamePublished bool, in UpdateInput) (Post, error) {
	snap, err := kernel.MarshalSnapshot(snapshot{
		Title:   prior.Title,
		Slug:    prior.Slug,
		Excerpt: prior.Excerpt,
		Body:    prior.Body,
		Status:  prior.Status.String(),
	})
	if err != nil {
		return Post{}, fmt.Errorf("marshal snapshot: %w", err)
	}

	var updated Post
	err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		author := actorID
		rev, err := s.revisions.CreateTx(ctx, tx, kernel.CreateRevisionInput{
			EntityType: kernel.EntityTypePost,
			EntityID:   prior.ID,
			Snapshot:   snap,
			AuthorID:   &author,
		})
		if err != nil {
			return fmt.Errorf("snapshot revision: %w", err)
		}
		// content.revision.created is SYNC (in-tx): it commits with the snapshot.
		if err := s.bus.Publish(ctx, tx, RevisionCreatedEvent{
			RevisionID: rev.ID,
			EntityType: rev.EntityType,
			EntityID:   rev.EntityID,
			AuthorID:   rev.AuthorID,
			CreatedAt:  rev.CreatedAt,
		}); err != nil {
			return err
		}

		p, err := s.repo.UpdateTx(ctx, tx, prior.ID, UpdatePostData{
			Title:       next.Title,
			Slug:        next.Slug,
			Excerpt:     next.Excerpt,
			Body:        next.Body,
			Status:      next.Status,
			PublishedAt: next.PublishedAt,
			ScheduledAt: next.ScheduledAt,
			ReadingTime: next.ReadingTime,
		})
		if err != nil {
			return fmt.Errorf("update post: %w", err)
		}
		updated = p

		if in.SetTaxonomy {
			if err := s.assignTaxonomy(ctx, tx, p.ID, in.CategoryIDs, in.TagIDs); err != nil {
				return err
			}
		}

		if becamePublished && p.Published() {
			return s.emitPublished(ctx, tx, p)
		}
		return nil
	})
	if err != nil {
		return Post{}, err
	}
	return updated, nil
}

// assignTaxonomy replaces the post's category + tag associations inside tx via
// the wired TaxonomyAssigner. It is a no-op when taxonomy is not wired (nil
// assigner), so reduced-deps callers and tests are unaffected.
func (s *Service) assignTaxonomy(ctx context.Context, tx pgx.Tx, postID uuid.UUID, categoryIDs, tagIDs []uuid.UUID) error {
	if s.taxonomy == nil {
		return nil
	}
	if err := s.taxonomy.AssignCategoriesTx(ctx, tx, postID, categoryIDs); err != nil {
		return fmt.Errorf("assign categories: %w", err)
	}
	if err := s.taxonomy.AssignTagsTx(ctx, tx, postID, tagIDs); err != nil {
		return fmt.Errorf("assign tags: %w", err)
	}
	return nil
}

// Related returns up to limit published posts sharing >=1 category or tag with
// the given post (excluding self), most-related first (laravel parity).
func (s *Service) Related(ctx context.Context, postID uuid.UUID, limit int) ([]Post, error) {
	return s.repo.ListRelatedPublished(ctx, postID, limit)
}

// PublicListFiltered returns a page of published posts narrowed by optional,
// combinable category/tag slug filters, plus the total (M3). Drafts/trashed are
// always excluded.
func (s *Service) PublicListFiltered(ctx context.Context, categorySlug, tagSlug string, limit, offset int) ([]Post, int, error) {
	items, err := s.repo.ListPublishedFiltered(ctx, categorySlug, tagSlug, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountPublishedFiltered(ctx, categorySlug, tagSlug)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// PublishedByIDs hydrates an ordered set of post ids to their published rows —
// the data behind the category/tag archives, which first resolve ranked ids.
func (s *Service) PublishedByIDs(ctx context.Context, ids []uuid.UUID) ([]Post, error) {
	return s.repo.GetPublishedByIDs(ctx, ids)
}

// Publish transitions a post to PUBLISHED, stamping publishedAt once and
// preserving it on re-publish. Ownership enforced.
func (s *Service) Publish(ctx context.Context, actorID, id uuid.UUID) (Post, error) {
	published := kernel.StatusPublished
	return s.Update(ctx, actorID, id, UpdateInput{Status: &published})
}

// Unpublish returns a post to DRAFT. publishedAt is PRESERVED (the original
// publication date) so a later re-publish keeps it. Ownership enforced.
func (s *Service) Unpublish(ctx context.Context, actorID, id uuid.UUID) (Post, error) {
	draft := kernel.StatusDraft
	return s.Update(ctx, actorID, id, UpdateInput{Status: &draft})
}

// Schedule sets a future scheduledAt on a DRAFT so the periodic scheduler
// publishes it when due. Ownership enforced.
func (s *Service) Schedule(ctx context.Context, actorID, id uuid.UUID, at time.Time) (Post, error) {
	draft := kernel.StatusDraft
	when := at
	return s.Update(ctx, actorID, id, UpdateInput{Status: &draft, ScheduledAt: &when})
}

// Trash soft-deletes a post (sets deleted_at). Ownership enforced.
func (s *Service) Trash(ctx context.Context, actorID, id uuid.UUID) error {
	existing, err := s.repo.GetActiveByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.requireOwnerOrPrivileged(ctx, actorID, existing, accounts.ActionDelete); err != nil {
		return err
	}
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.repo.TrashTx(ctx, tx, id)
	})
}

// Restore un-trashes a post. Ownership enforced (against the trashed row).
func (s *Service) Restore(ctx context.Context, actorID, id uuid.UUID) error {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.requireOwnerOrPrivileged(ctx, actorID, existing, accounts.ActionUpdate); err != nil {
		return err
	}
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.repo.RestoreTx(ctx, tx, id)
	})
}

// PermanentDelete hard-deletes a trashed post. Requires delete:post AND
// ownership (or privilege). This is irreversible.
func (s *Service) PermanentDelete(ctx context.Context, actorID, id uuid.UUID) error {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.requireOwnerOrPrivileged(ctx, actorID, existing, accounts.ActionDelete); err != nil {
		return err
	}
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.repo.PermanentDeleteTx(ctx, tx, id)
	})
}

// Revisions lists a post's revision snapshots (newest first).
func (s *Service) Revisions(ctx context.Context, actorID, id uuid.UUID) ([]kernel.Revision, error) {
	existing, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.requireOwnerOrPrivileged(ctx, actorID, existing, accounts.ActionUpdate); err != nil {
		return nil, err
	}
	return s.revisions.List(ctx, kernel.EntityTypePost, id)
}

// RestoreRevision applies a prior revision's scalar fields as a NEW update
// (which itself snapshots the current state first, so the restore is reversible).
// Ownership enforced.
func (s *Service) RestoreRevision(ctx context.Context, actorID, id, revisionID uuid.UUID) (Post, error) {
	rev, err := s.revisions.Get(ctx, revisionID)
	if err != nil {
		return Post{}, err
	}
	if rev.EntityType != kernel.EntityTypePost || rev.EntityID != id {
		return Post{}, ErrRevisionMismatch
	}
	var snap snapshot
	if err := json.Unmarshal(rev.Snapshot, &snap); err != nil {
		return Post{}, fmt.Errorf("decode revision snapshot: %w", err)
	}
	status := kernel.ParseStatus(snap.Status)
	in := UpdateInput{
		Title:   &snap.Title,
		Slug:    &snap.Slug,
		Excerpt: &snap.Excerpt,
		Body:    &snap.Body,
		Status:  &status,
	}
	return s.Update(ctx, actorID, id, in)
}

// Like records that userID likes the post and recomputes the cached count. It is
// idempotent: liking twice changes nothing. Any signed-in user may like (no
// ownership/permission gate beyond authentication, enforced upstream).
func (s *Service) Like(ctx context.Context, postID, userID uuid.UUID) (Post, error) {
	return s.toggleLike(ctx, postID, userID, true)
}

// Unlike removes userID's like and recomputes the count. Idempotent.
func (s *Service) Unlike(ctx context.Context, postID, userID uuid.UUID) (Post, error) {
	return s.toggleLike(ctx, postID, userID, false)
}

func (s *Service) toggleLike(ctx context.Context, postID, userID uuid.UUID, like bool) (Post, error) {
	existing, err := s.repo.GetByID(ctx, postID)
	if err != nil {
		return Post{}, err
	}
	// Reject NEW likes on a post that is not publicly available (trashed or not
	// published). This rule lives in the service so it holds for every caller, not
	// just the public handler. An UNLIKE is always permitted so a user can retract
	// a like even after the post leaves the published set.
	if like && !existing.Published() {
		return Post{}, ErrNotLikeable
	}
	err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if like {
			if _, err := s.repo.LikeTx(ctx, tx, postID, userID); err != nil {
				return fmt.Errorf("like: %w", err)
			}
		} else {
			if _, err := s.repo.UnlikeTx(ctx, tx, postID, userID); err != nil {
				return fmt.Errorf("unlike: %w", err)
			}
		}
		return s.repo.SyncLikeCountTx(ctx, tx, postID)
	})
	if err != nil {
		return Post{}, err
	}
	return s.repo.GetByID(ctx, postID)
}

// HasLiked reports whether userID has liked the post.
func (s *Service) HasLiked(ctx context.Context, postID, userID uuid.UUID) (bool, error) {
	return s.repo.HasLiked(ctx, postID, userID)
}

// PublishDue publishes every DRAFT whose scheduledAt is due at now. It is the
// entry point the periodic worker scan calls. Each due post is published with a
// preserved-or-stamped publishedAt; the publish event fires per post. Returns
// the count published.
func (s *Service) PublishDue(ctx context.Context) (int, error) {
	now := s.now()
	ids, err := s.repo.ListDueScheduledIDs(ctx, now)
	if err != nil {
		return 0, fmt.Errorf("list due scheduled: %w", err)
	}
	var published int
	for _, id := range ids {
		if err := s.publishScheduled(ctx, id, now); err != nil {
			return published, err
		}
		published++
	}
	return published, nil
}

// publishScheduled flips a single due draft to PUBLISHED (race-safe: re-checks
// the row inside the tx) and emits the publish event. No revision snapshot is
// taken for an automated status flip (mirrors ts).
func (s *Service) publishScheduled(ctx context.Context, id uuid.UUID, now time.Time) error {
	post, err := s.repo.GetActiveByID(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil // raced away (trashed/deleted) — nothing to do
	}
	if err != nil {
		return err
	}
	if post.Status != kernel.StatusDraft || post.ScheduledAt == nil {
		return nil // already handled / no longer due
	}

	next := post
	next.Status = kernel.StatusPublished
	next.ScheduledAt = nil
	if post.PublishedAt == nil {
		next.PublishedAt = &now
	}

	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		p, err := s.repo.UpdateTx(ctx, tx, id, UpdatePostData{
			Title:       next.Title,
			Slug:        next.Slug,
			Excerpt:     next.Excerpt,
			Body:        next.Body,
			Status:      next.Status,
			PublishedAt: next.PublishedAt,
			ScheduledAt: next.ScheduledAt,
			ReadingTime: next.ReadingTime,
		})
		if err != nil {
			return fmt.Errorf("publish scheduled post: %w", err)
		}
		if p.Published() {
			return s.emitPublished(ctx, tx, p)
		}
		return nil
	})
}

// --- public reads (no auth; published only) ---------------------------------

// PublicBySlug returns a published, non-trashed post for the public detail page
// in the DEFAULT locale (en). It resolves to the base row and is unchanged from
// M2 — the locale-aware path is PublicBySlugLocale.
func (s *Service) PublicBySlug(ctx context.Context, slug string) (Post, error) {
	return s.repo.GetPublishedBySlug(ctx, slug)
}

// PublicBySlugLocale returns a published post by slug with its content overlaid
// by the active locale, falling back to the base (en) row for any missing
// translation field. When locale is the default (en) or unsupported it resolves
// to the base row (identical to PublicBySlug), so nothing breaks (M7b-1).
func (s *Service) PublicBySlugLocale(ctx context.Context, slug string, locale i18n.Locale) (Post, error) {
	if locale.IsDefault() || !i18n.IsSupported(locale) {
		return s.repo.GetPublishedBySlug(ctx, slug)
	}
	return s.repo.GetPublishedInLocaleBySlug(ctx, slug, locale.String())
}

// PublicListLocale returns a page of published posts overlaid by the active
// locale (base fallback per field), plus the total. The default/unsupported
// locale resolves to the base listing (identical to PublicList).
func (s *Service) PublicListLocale(ctx context.Context, locale i18n.Locale, limit, offset int) ([]Post, int, error) {
	total, err := s.repo.CountPublished(ctx)
	if err != nil {
		return nil, 0, err
	}
	var items []Post
	if locale.IsDefault() || !i18n.IsSupported(locale) {
		items, err = s.repo.ListPublished(ctx, limit, offset)
	} else {
		items, err = s.repo.ListPublishedInLocale(ctx, locale.String(), limit, offset)
	}
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// PublicList returns a page of published posts for the public index.
func (s *Service) PublicList(ctx context.Context, limit, offset int) ([]Post, int, error) {
	items, err := s.repo.ListPublished(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountPublished(ctx)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// PublishedByAuthor returns an author's published posts (newest first) — the
// data behind the public author page's Posts section seam.
func (s *Service) PublishedByAuthor(ctx context.Context, authorID uuid.UUID) ([]Post, error) {
	return s.repo.ListPublishedByAuthor(ctx, authorID)
}

// --- admin reads ------------------------------------------------------------

// AdminList returns a filtered, paginated admin listing plus the total count.
func (s *Service) AdminList(ctx context.Context, f ListFilter) ([]Post, int, error) {
	items, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.Count(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// AdminTrashed returns the trashed listing plus total.
func (s *Service) AdminTrashed(ctx context.Context, limit, offset int) ([]Post, int, error) {
	items, err := s.repo.ListTrashed(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountTrashed(ctx)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Get returns a post by id for the editor, enforcing read ownership/privilege.
func (s *Service) Get(ctx context.Context, actorID, id uuid.UUID) (Post, error) {
	p, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return Post{}, err
	}
	if err := s.requireOwnerOrPrivileged(ctx, actorID, p, accounts.ActionRead); err != nil {
		return Post{}, err
	}
	return p, nil
}

// --- per-locale content overlay (M7b-1) --------------------------------------

// TranslationInput is the editor's per-locale content save for a NON-default
// locale. The body is sanitized (same kernel sanitizer as the base body) and the
// excerpt is stripped write-time; structural fields are NOT part of it (they are
// shared on the base row and edited via Update).
type TranslationInput struct {
	Title   string
	Excerpt string
	Body    string
}

// SaveTranslation upserts a NON-default locale's content overlay for a post.
// Ownership is enforced exactly like Update (Author only their own; Editor/Admin
// any). The default locale is rejected (its content lives on the base row —
// callers edit it via Update). No revision snapshot is taken and no event is
// emitted: the translation overlay is content, not a publish-state change.
func (s *Service) SaveTranslation(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale, in TranslationInput) error {
	if !i18n.IsSupported(locale) {
		return ErrUnsupportedLocale
	}
	if locale.IsDefault() {
		return ErrDefaultLocaleTranslation
	}
	existing, err := s.repo.GetActiveByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.requireOwnerOrPrivileged(ctx, actorID, existing, accounts.ActionUpdate); err != nil {
		return err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return ErrTitleRequired
	}
	t := Translation{
		Locale:  locale.String(),
		Title:   title,
		Excerpt: sanitizeExcerpt(in.Excerpt),
		Body:    kernel.SanitizeRichText(in.Body),
	}
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.repo.UpsertTranslationTx(ctx, tx, id, t)
	})
}

// GetInLocale loads a post for the editor with its content overlaid by locale
// (base fallback per field). The default locale resolves to the base row. Read
// ownership/privilege is enforced. Used to populate the editor's per-locale tab.
func (s *Service) GetInLocale(ctx context.Context, actorID, id uuid.UUID, locale i18n.Locale) (Post, error) {
	// Ownership is checked against the base (structural) row.
	base, err := s.repo.GetActiveByID(ctx, id)
	if err != nil {
		return Post{}, err
	}
	if err := s.requireOwnerOrPrivileged(ctx, actorID, base, accounts.ActionRead); err != nil {
		return Post{}, err
	}
	if locale.IsDefault() || !i18n.IsSupported(locale) {
		return base, nil
	}
	return s.repo.GetActiveInLocaleByID(ctx, id, locale.String())
}

// TranslatedLocales returns the NON-default locales that already have a
// translation row for the post (drives the editor's per-tab "has translation"
// markers). Read ownership/privilege is enforced.
func (s *Service) TranslatedLocales(ctx context.Context, actorID, id uuid.UUID) ([]i18n.Locale, error) {
	base, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.requireOwnerOrPrivileged(ctx, actorID, base, accounts.ActionRead); err != nil {
		return nil, err
	}
	raw, err := s.repo.TranslatedLocales(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]i18n.Locale, 0, len(raw))
	for _, r := range raw {
		if l, ok := i18n.Parse(r); ok && !l.IsDefault() {
			out = append(out, l)
		}
	}
	return out, nil
}

// --- ownership + helpers -----------------------------------------------------

// requireOwnerOrPrivileged is the OWNERSHIP GATE. It first requires the coarse
// (action, post) grant, then narrows: an Author may act only on their OWN post;
// Editor and Administrator may act on ANY post. This closes the M1 carry-over —
// the seed grants are coarse and the service is the real gate.
func (s *Service) requireOwnerOrPrivileged(ctx context.Context, actorID uuid.UUID, post Post, action string) error {
	if !s.authz.Can(ctx, actorID, action, accounts.SubjectPost) {
		return ErrForbidden
	}
	if post.AuthorID == actorID {
		return nil
	}
	role, err := s.users.RoleKey(ctx, actorID)
	if err != nil {
		return ErrForbidden
	}
	if role == accounts.RoleEditor || role == accounts.RoleAdministrator {
		return nil
	}
	return ErrForbidden
}

// uniqueSlug derives a slug unique across posts, excluding excludeID (the post
// being updated) so re-saving under its own slug does not append a suffix.
func (s *Service) uniqueSlug(ctx context.Context, desired string, excludeID uuid.UUID) (string, error) {
	return kernel.UniqueSlug(ctx, desired, func(ctx context.Context, slug string) (bool, error) {
		return s.repo.SlugTaken(ctx, slug, excludeID)
	})
}

// emitPublished publishes the async content.published event inside tx.
func (s *Service) emitPublished(ctx context.Context, tx pgx.Tx, p Post) error {
	publishedAt := p.UpdatedAt
	if p.PublishedAt != nil {
		publishedAt = *p.PublishedAt
	}
	return s.bus.Publish(ctx, tx, ContentPublishedEvent{
		EntityType:  kernel.EntityTypePost,
		PostID:      p.ID,
		Slug:        p.Slug,
		Title:       p.Title,
		PublishedAt: publishedAt,
	})
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// sanitizeExcerpt strips ALL markup from the excerpt (defense-in-depth): the
// excerpt is rendered as text, so any tags are removed write-time on every save.
func sanitizeExcerpt(s string) string {
	return strings.TrimSpace(kernel.SanitizePlainText(s))
}

// isSlugUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505) — the race a concurrent same-slug create produces.
func isSlugUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
