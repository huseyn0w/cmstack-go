package comments

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/platform/db"
)

// SelfEditWindow bounds how long after posting a signed-in author may edit or
// delete their own comment (mirrors the ts COMMENT_EDIT_WINDOW_MINUTES = 15).
const SelfEditWindow = 15 * time.Minute

// rateLimitKeyPrefix namespaces the per-IP comment-submit bucket so it does not
// collide with other limiter keys when a shared limiter is ever used.
const rateLimitKeyPrefix = "comment-submit:"

// Domain errors. They are mapped by handlers to HTTP outcomes and per-field
// validation messages.
var (
	// ErrForbidden is returned when the actor lacks the moderation permission or
	// (for self-edit) does not own the comment / is outside the edit window.
	ErrForbidden = errors.New("comments: forbidden")
	// ErrValidation carries a user-facing validation message (missing name/email,
	// empty body, bad parent).
	ErrValidation = errors.New("comments: validation failed")
	// ErrSpam is returned when the anti-spam (reCAPTCHA) check rejects a guest
	// submission.
	ErrSpam = errors.New("comments: spam check failed")
	// ErrRateLimited is returned when the per-IP submit rate limit is exceeded.
	ErrRateLimited = errors.New("comments: rate limit exceeded")
	// ErrEditWindowExpired is returned when a self-edit/delete is attempted after
	// the window has elapsed.
	ErrEditWindowExpired = errors.New("comments: edit window expired")
)

// Service holds ALL comment logic. It reaches data only through the Repository,
// resolves the target post via PostLookup, fires side effects only via events,
// and owns no globals.
type Service struct {
	pool    db.Beginner
	repo    Repository
	posts   PostLookup
	authz   Authorizer
	spam    SpamChecker
	limiter RateLimiter
	bus     Publisher
	now     Clock
}

// Clock returns the current time; injected so the self-edit window is testable.
type Clock func() time.Time

// NewService constructs the comment Service with explicit dependencies. A nil
// clock defaults to time.Now. spam/limiter may be nil (verification/rate-limit
// disabled).
func NewService(
	pool db.Beginner,
	repo Repository,
	posts PostLookup,
	authz Authorizer,
	spam SpamChecker,
	limiter RateLimiter,
	bus Publisher,
	now Clock,
) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		pool:    pool,
		repo:    repo,
		posts:   posts,
		authz:   authz,
		spam:    spam,
		limiter: limiter,
		bus:     bus,
		now:     now,
	}
}

// Viewer is the signed-in author of a comment action (subset of the auth user).
// A zero Viewer (or SubmitInput.Viewer == nil) means a guest.
type Viewer struct {
	ID    uuid.UUID
	Name  string
	Email string
}

// SubmitInput is the validated public submission. Slug targets the post; Viewer
// (when non-nil) attributes a signed-in author (Name/Email taken from the
// account, body name/email ignored). ClientIP is the real client IP for the
// rate limiter + moderation record. RecaptchaToken is the optional v3 token.
type SubmitInput struct {
	Slug           string
	ParentID       *uuid.UUID
	AuthorName     string // guest only
	AuthorEmail    string // guest only
	Body           string
	ClientIP       string
	RecaptchaToken string
	Viewer         *Viewer
}

// Submit stores a new PENDING comment after validating the post, the optional
// threading parent (must be an APPROVED comment on the SAME post), the guest
// identity, the rate limit, and the optional reCAPTCHA. It emits comment.created
// (async) so the notification email is sent after commit. Returns the stored
// comment (PENDING).
func (s *Service) Submit(ctx context.Context, in SubmitInput) (Comment, error) {
	// Rate-limit per real client IP BEFORE any work (cheap guard, ts parity ~8/min).
	if s.limiter != nil && in.ClientIP != "" {
		if !s.limiter.Allow(rateLimitKeyPrefix + in.ClientIP) {
			return Comment{}, ErrRateLimited
		}
	}

	guest := in.Viewer == nil
	if guest {
		// Guests are spam-checked and must identify themselves.
		if s.spam != nil {
			ok, err := s.spam.Verify(ctx, in.RecaptchaToken)
			if err != nil || !ok {
				return Comment{}, ErrSpam
			}
		}
		if strings.TrimSpace(in.AuthorName) == "" || strings.TrimSpace(in.AuthorEmail) == "" {
			return Comment{}, fmt.Errorf("%w: name and email are required", ErrValidation)
		}
	}

	body := kernel.SanitizePlainText(in.Body)
	if strings.TrimSpace(body) == "" {
		return Comment{}, fmt.Errorf("%w: comment cannot be empty", ErrValidation)
	}

	post, err := s.posts.PublishedBySlug(ctx, in.Slug)
	if err != nil {
		return Comment{}, err
	}

	// Threading: a reply's parent must be an APPROVED comment on the SAME post.
	if in.ParentID != nil {
		if _, err := s.repo.GetApprovedByID(ctx, *in.ParentID, post.ID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return Comment{}, fmt.Errorf("%w: invalid parent comment", ErrValidation)
			}
			return Comment{}, err
		}
	}

	var (
		authorName, authorEmail string
		authorUserID            *uuid.UUID
	)
	if guest {
		authorName = strings.TrimSpace(in.AuthorName)
		authorEmail = strings.TrimSpace(in.AuthorEmail)
	} else {
		authorName = strings.TrimSpace(in.Viewer.Name)
		if authorName == "" {
			authorName = "Member"
		}
		authorEmail = in.Viewer.Email
		id := in.Viewer.ID
		authorUserID = &id
	}

	data := CreateCommentData{
		PostID:       post.ID,
		ParentID:     in.ParentID,
		AuthorUserID: authorUserID,
		AuthorName:   authorName,
		AuthorEmail:  authorEmail,
		AuthorIP:     in.ClientIP,
		Body:         body,
		Status:       StatusPending,
	}

	var created Comment
	err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		c, err := s.repo.CreateTx(ctx, tx, data)
		if err != nil {
			return fmt.Errorf("create comment: %w", err)
		}
		created = c
		// comment.created is ASYNC: the notification email is sent after commit by
		// the worker draining the outbox. The event payload omits email/IP (PII).
		return s.bus.Publish(ctx, tx, CommentCreatedEvent{
			CommentID:  c.ID,
			PostID:     post.ID,
			PostSlug:   post.Slug,
			PostTitle:  post.Title,
			AuthorName: c.AuthorName,
			Excerpt:    excerpt(c.Body, 160),
			CreatedAt:  c.CreatedAt,
		})
	})
	if err != nil {
		return Comment{}, err
	}
	return created, nil
}

// PublicThread returns the APPROVED, threaded comments for a published post. When
// viewer is non-nil, that viewer's OWN comments (including PENDING) are merged in
// and flagged Mine/Pending so they can self-edit them — other users never see
// another viewer's pending comments. total counts ONLY the public (approved)
// comments. The returned PublicComment tree NEVER contains author email/IP.
func (s *Service) PublicThread(ctx context.Context, slug string, viewer *Viewer) ([]PublicComment, int, error) {
	post, err := s.posts.PublishedBySlug(ctx, slug)
	if err != nil {
		return nil, 0, err
	}

	approved, err := s.repo.ListApprovedForPost(ctx, post.ID)
	if err != nil {
		return nil, 0, err
	}
	total := len(approved)

	flat := make([]PublicComment, 0, len(approved))
	seen := make(map[uuid.UUID]int, len(approved))
	for _, c := range approved {
		pc := toPublic(c)
		if viewer != nil && c.AuthorUserID != nil && *c.AuthorUserID == viewer.ID {
			pc.Mine = true
		}
		seen[c.ID] = len(flat)
		flat = append(flat, pc)
	}

	// Merge the viewer's own (incl. PENDING) comments not already in the approved
	// set so they can edit their pending comment.
	if viewer != nil {
		own, err := s.repo.ListForModeration(ctx, ModerationFilter{Limit: 1000})
		if err != nil {
			return nil, 0, err
		}
		for _, c := range own {
			if c.PostID != post.ID || c.AuthorUserID == nil || *c.AuthorUserID != viewer.ID {
				continue
			}
			if _, ok := seen[c.ID]; ok {
				continue // already in approved set (flagged Mine above)
			}
			if c.Status != StatusApproved && c.Status != StatusPending {
				continue // never surface the viewer's own SPAM/TRASH
			}
			pc := toPublic(c)
			pc.Mine = true
			pc.Pending = c.Status == StatusPending
			flat = append(flat, pc)
		}
		sortByCreatedAt(flat)
	}

	return buildThread(flat), total, nil
}

// SelfEdit lets a signed-in author edit their OWN comment within the window. The
// edit re-opens moderation (status back to PENDING) and stamps edited_at.
func (s *Service) SelfEdit(ctx context.Context, viewer Viewer, id uuid.UUID, body string) (Comment, error) {
	existing, err := s.requireOwnedWithinWindow(ctx, viewer, id)
	if err != nil {
		return Comment{}, err
	}
	clean := kernel.SanitizePlainText(body)
	if strings.TrimSpace(clean) == "" {
		return Comment{}, fmt.Errorf("%w: comment cannot be empty", ErrValidation)
	}
	_ = existing
	var updated Comment
	err = db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		c, err := s.repo.UpdateBodyTx(ctx, tx, id, clean, StatusPending)
		if err != nil {
			return fmt.Errorf("self-edit comment: %w", err)
		}
		updated = c
		return nil
	})
	if err != nil {
		return Comment{}, err
	}
	return updated, nil
}

// SelfDelete lets a signed-in author hard-delete their OWN comment within the
// window (replies cascade via the FK).
func (s *Service) SelfDelete(ctx context.Context, viewer Viewer, id uuid.UUID) error {
	if _, err := s.requireOwnedWithinWindow(ctx, viewer, id); err != nil {
		return err
	}
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.repo.DeleteTx(ctx, tx, id)
	})
}

// requireOwnedWithinWindow loads a comment owned by viewer, asserting it exists
// and is still within the self-edit window.
func (s *Service) requireOwnedWithinWindow(ctx context.Context, viewer Viewer, id uuid.UUID) (Comment, error) {
	c, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return Comment{}, err
	}
	if c.AuthorUserID == nil || *c.AuthorUserID != viewer.ID {
		return Comment{}, ErrForbidden
	}
	if s.now().Sub(c.CreatedAt) > SelfEditWindow {
		return Comment{}, ErrEditWindowExpired
	}
	return c, nil
}

// --- Admin moderation -------------------------------------------------------

// AdminList returns a filtered, paginated moderation listing plus the total.
// Requires read:comment.
func (s *Service) AdminList(ctx context.Context, actorID uuid.UUID, f ModerationFilter) ([]Comment, int, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectComment) {
		return nil, 0, ErrForbidden
	}
	items, err := s.repo.ListForModeration(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.repo.CountForModeration(ctx, f.Status)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// StatusCounts returns the per-status totals for the moderation tab badges.
// Requires read:comment.
func (s *Service) StatusCounts(ctx context.Context, actorID uuid.UUID) (map[Status]int, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionRead, accounts.SubjectComment) {
		return nil, ErrForbidden
	}
	counts, err := s.repo.CountsByStatus(ctx)
	if err != nil {
		return nil, err
	}
	out := map[Status]int{
		StatusPending:  0,
		StatusApproved: 0,
		StatusSpam:     0,
		StatusTrash:    0,
	}
	for _, c := range counts {
		out[c.Status] = c.Count
	}
	return out, nil
}

// Approve / Spam / Trash set a comment's moderation status. They require
// update:comment.
func (s *Service) Approve(ctx context.Context, actorID, id uuid.UUID) (Comment, error) {
	return s.setStatus(ctx, actorID, id, StatusApproved)
}

// Spam marks a comment as spam.
func (s *Service) Spam(ctx context.Context, actorID, id uuid.UUID) (Comment, error) {
	return s.setStatus(ctx, actorID, id, StatusSpam)
}

// Trash soft-trashes a comment (status=TRASH, retained).
func (s *Service) Trash(ctx context.Context, actorID, id uuid.UUID) (Comment, error) {
	return s.setStatus(ctx, actorID, id, StatusTrash)
}

func (s *Service) setStatus(ctx context.Context, actorID, id uuid.UUID, status Status) (Comment, error) {
	if !s.authz.Can(ctx, actorID, accounts.ActionUpdate, accounts.SubjectComment) {
		return Comment{}, ErrForbidden
	}
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return Comment{}, err
	}
	var updated Comment
	err := db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		c, err := s.repo.UpdateStatusTx(ctx, tx, id, status)
		if err != nil {
			return fmt.Errorf("update comment status: %w", err)
		}
		updated = c
		return nil
	})
	if err != nil {
		return Comment{}, err
	}
	return updated, nil
}

// Delete hard-deletes a comment (irreversible; replies cascade). Requires
// delete:comment.
func (s *Service) Delete(ctx context.Context, actorID, id uuid.UUID) error {
	if !s.authz.Can(ctx, actorID, accounts.ActionDelete, accounts.SubjectComment) {
		return ErrForbidden
	}
	if _, err := s.repo.GetByID(ctx, id); err != nil {
		return err
	}
	return db.RunInTx(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return s.repo.DeleteTx(ctx, tx, id)
	})
}

// excerpt returns up to max runes of plain text for the notification preview.
func excerpt(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return strings.TrimSpace(string(r[:max])) + "…"
}
