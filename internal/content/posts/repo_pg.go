package posts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/huseyn0w/cmstack-go/internal/content/kernel"
	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
)

// compile-time assertions that the pg repos satisfy the domain interfaces.
var (
	_ Repository                = (*RepoPG)(nil)
	_ kernel.RevisionRepository = (*RevisionRepoPG)(nil)
)

// RepoPG is the sqlc/pgx-backed post Repository — the ONLY layer touching
// generated SQL for posts.
type RepoPG struct{ q *sqlcgen.Queries }

// NewRepoPG constructs a RepoPG over the base querier.
func NewRepoPG(q *sqlcgen.Queries) *RepoPG { return &RepoPG{q: q} }

// CreateTx inserts a post within tx.
func (r *RepoPG) CreateTx(ctx context.Context, tx pgx.Tx, in CreatePostData) (Post, error) {
	row, err := r.q.WithTx(tx).CreatePost(ctx, sqlcgen.CreatePostParams{
		Title:       in.Title,
		Slug:        in.Slug,
		Excerpt:     in.Excerpt,
		Body:        in.Body,
		Status:      in.Status.String(),
		PublishedAt: optTime(in.PublishedAt),
		ScheduledAt: optTime(in.ScheduledAt),
		AuthorID:    toPgUUID(in.AuthorID),
		ReadingTime: int32(in.ReadingTime),
	})
	return postFromRow(row), mapErr(err)
}

// UpdateTx updates an active post within tx.
func (r *RepoPG) UpdateTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, in UpdatePostData) (Post, error) {
	row, err := r.q.WithTx(tx).UpdatePost(ctx, sqlcgen.UpdatePostParams{
		ID:          toPgUUID(id),
		Title:       in.Title,
		Slug:        in.Slug,
		Excerpt:     in.Excerpt,
		Body:        in.Body,
		Status:      in.Status.String(),
		PublishedAt: optTime(in.PublishedAt),
		ScheduledAt: optTime(in.ScheduledAt),
		ReadingTime: int32(in.ReadingTime),
	})
	return postFromRow(row), mapErr(err)
}

// GetByID loads any post (including trashed) by id.
func (r *RepoPG) GetByID(ctx context.Context, id uuid.UUID) (Post, error) {
	row, err := r.q.GetPostByID(ctx, toPgUUID(id))
	return postFromRow(row), mapErr(err)
}

// GetActiveByID loads a non-trashed post by id.
func (r *RepoPG) GetActiveByID(ctx context.Context, id uuid.UUID) (Post, error) {
	row, err := r.q.GetActivePostByID(ctx, toPgUUID(id))
	return postFromRow(row), mapErr(err)
}

// GetPublishedBySlug loads a published, non-trashed post by slug.
func (r *RepoPG) GetPublishedBySlug(ctx context.Context, slug string) (Post, error) {
	row, err := r.q.GetPublishedPostBySlug(ctx, slug)
	return postFromRow(row), mapErr(err)
}

// SlugTaken reports whether slug is used by a post other than excludeID.
func (r *RepoPG) SlugTaken(ctx context.Context, slug string, excludeID uuid.UUID) (bool, error) {
	n, err := r.q.CountPostsBySlug(ctx, sqlcgen.CountPostsBySlugParams{
		Slug: slug,
		ID:   toPgUUID(excludeID),
	})
	return n > 0, mapErr(err)
}

// List returns a filtered, paginated active listing.
func (r *RepoPG) List(ctx context.Context, f ListFilter) ([]Post, error) {
	rows, err := r.q.ListPosts(ctx, sqlcgen.ListPostsParams{
		Limit:    int32(limitOrDefault(f.Limit)),
		Offset:   int32(f.Offset),
		Status:   statusFilter(f.Status),
		AuthorID: authorFilter(f.AuthorID),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return postsFromRows(rows), nil
}

// Count returns the total matching the filter (ignoring pagination).
func (r *RepoPG) Count(ctx context.Context, f ListFilter) (int, error) {
	n, err := r.q.CountPosts(ctx, sqlcgen.CountPostsParams{
		Status:   statusFilter(f.Status),
		AuthorID: authorFilter(f.AuthorID),
	})
	return int(n), mapErr(err)
}

// ListTrashed returns a page of trashed posts.
func (r *RepoPG) ListTrashed(ctx context.Context, limit, offset int) ([]Post, error) {
	rows, err := r.q.ListTrashedPosts(ctx, sqlcgen.ListTrashedPostsParams{
		Limit:  int32(limitOrDefault(limit)),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return postsFromRows(rows), nil
}

// CountTrashed returns the trashed total.
func (r *RepoPG) CountTrashed(ctx context.Context) (int, error) {
	n, err := r.q.CountTrashedPosts(ctx)
	return int(n), mapErr(err)
}

// ListPublished returns a page of published posts (newest first).
func (r *RepoPG) ListPublished(ctx context.Context, limit, offset int) ([]Post, error) {
	rows, err := r.q.ListPublishedPosts(ctx, sqlcgen.ListPublishedPostsParams{
		Limit:  int32(limitOrDefault(limit)),
		Offset: int32(offset),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return postsFromRows(rows), nil
}

// CountPublished returns the published total.
func (r *RepoPG) CountPublished(ctx context.Context) (int, error) {
	n, err := r.q.CountPublishedPosts(ctx)
	return int(n), mapErr(err)
}

// ListPublishedByAuthor returns an author's published posts (newest first).
func (r *RepoPG) ListPublishedByAuthor(ctx context.Context, authorID uuid.UUID) ([]Post, error) {
	rows, err := r.q.ListPublishedPostsByAuthor(ctx, toPgUUID(authorID))
	if err != nil {
		return nil, mapErr(err)
	}
	return postsFromRows(rows), nil
}

// TrashTx soft-deletes within tx.
func (r *RepoPG) TrashTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).TrashPost(ctx, toPgUUID(id)))
}

// RestoreTx un-trashes within tx.
func (r *RepoPG) RestoreTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).RestorePost(ctx, toPgUUID(id)))
}

// PermanentDeleteTx hard-deletes within tx.
func (r *RepoPG) PermanentDeleteTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).PermanentDeletePost(ctx, toPgUUID(id)))
}

// ListDueScheduledIDs returns the ids of drafts whose scheduled_at <= now.
func (r *RepoPG) ListDueScheduledIDs(ctx context.Context, now time.Time) ([]uuid.UUID, error) {
	rows, err := r.q.ListDueScheduledPostIDs(ctx, pgtype.Timestamptz{Time: now, Valid: true})
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, id := range rows {
		out = append(out, fromPgUUID(id))
	}
	return out, nil
}

// LikeTx inserts a like (idempotent); added reports whether a row was created.
func (r *RepoPG) LikeTx(ctx context.Context, tx pgx.Tx, postID, userID uuid.UUID) (bool, error) {
	n, err := r.q.WithTx(tx).LikePost(ctx, sqlcgen.LikePostParams{
		PostID: toPgUUID(postID),
		UserID: toPgUUID(userID),
	})
	return n > 0, mapErr(err)
}

// UnlikeTx removes a like; removed reports whether a row was deleted.
func (r *RepoPG) UnlikeTx(ctx context.Context, tx pgx.Tx, postID, userID uuid.UUID) (bool, error) {
	n, err := r.q.WithTx(tx).UnlikePost(ctx, sqlcgen.UnlikePostParams{
		PostID: toPgUUID(postID),
		UserID: toPgUUID(userID),
	})
	return n > 0, mapErr(err)
}

// SyncLikeCountTx recomputes posts.like_count from post_likes within tx.
func (r *RepoPG) SyncLikeCountTx(ctx context.Context, tx pgx.Tx, postID uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).SetPostLikeCount(ctx, toPgUUID(postID)))
}

// HasLiked reports whether userID liked the post.
func (r *RepoPG) HasLiked(ctx context.Context, postID, userID uuid.UUID) (bool, error) {
	ok, err := r.q.HasLiked(ctx, sqlcgen.HasLikedParams{
		PostID: toPgUUID(postID),
		UserID: toPgUUID(userID),
	})
	return ok, mapErr(err)
}

// RevisionRepoPG is the sqlc-backed kernel.RevisionRepository — the ONLY layer
// touching generated SQL for the shared revisions table.
type RevisionRepoPG struct{ q *sqlcgen.Queries }

// NewRevisionRepoPG constructs a RevisionRepoPG.
func NewRevisionRepoPG(q *sqlcgen.Queries) *RevisionRepoPG { return &RevisionRepoPG{q: q} }

// CreateTx persists a snapshot within tx.
func (r *RevisionRepoPG) CreateTx(ctx context.Context, tx pgx.Tx, in kernel.CreateRevisionInput) (kernel.Revision, error) {
	row, err := r.q.WithTx(tx).CreateRevision(ctx, sqlcgen.CreateRevisionParams{
		EntityType: in.EntityType,
		EntityID:   toPgUUID(in.EntityID),
		Snapshot:   in.Snapshot,
		AuthorID:   optUUID(in.AuthorID),
	})
	if err != nil {
		return kernel.Revision{}, mapErr(err)
	}
	return revisionFromRow(row), nil
}

// List returns an entity's revisions, newest first.
func (r *RevisionRepoPG) List(ctx context.Context, entityType string, entityID uuid.UUID) ([]kernel.Revision, error) {
	rows, err := r.q.ListRevisions(ctx, sqlcgen.ListRevisionsParams{
		EntityType: entityType,
		EntityID:   toPgUUID(entityID),
	})
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]kernel.Revision, 0, len(rows))
	for _, row := range rows {
		out = append(out, revisionFromRow(row))
	}
	return out, nil
}

// Get loads a single revision by id.
func (r *RevisionRepoPG) Get(ctx context.Context, id uuid.UUID) (kernel.Revision, error) {
	row, err := r.q.GetRevision(ctx, toPgUUID(id))
	if err != nil {
		return kernel.Revision{}, mapErr(err)
	}
	return revisionFromRow(row), nil
}

// --- conversions -------------------------------------------------------------

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func limitOrDefault(limit int) int {
	if limit <= 0 {
		return 20
	}
	return limit
}

func statusFilter(s *kernel.Status) *string {
	if s == nil {
		return nil
	}
	v := s.String()
	return &v
}

func authorFilter(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return toPgUUID(*id)
}

func toPgUUID(id uuid.UUID) pgtype.UUID {
	if id == uuid.Nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func optUUID(id *uuid.UUID) pgtype.UUID {
	if id == nil {
		return pgtype.UUID{}
	}
	return toPgUUID(*id)
}

func fromPgUUID(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return id.Bytes
}

func optTime(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func fromTimestamptz(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

func postFromRow(p sqlcgen.Post) Post {
	return Post{
		ID:          fromPgUUID(p.ID),
		Title:       p.Title,
		Slug:        p.Slug,
		Excerpt:     p.Excerpt,
		Body:        p.Body,
		Status:      kernel.Status(p.Status),
		PublishedAt: fromTimestamptz(p.PublishedAt),
		ScheduledAt: fromTimestamptz(p.ScheduledAt),
		AuthorID:    fromPgUUID(p.AuthorID),
		ReadingTime: int(p.ReadingTime),
		LikeCount:   int(p.LikeCount),
		DeletedAt:   fromTimestamptz(p.DeletedAt),
		CreatedAt:   p.CreatedAt.Time,
		UpdatedAt:   p.UpdatedAt.Time,
	}
}

func postsFromRows(rows []sqlcgen.Post) []Post {
	out := make([]Post, 0, len(rows))
	for _, row := range rows {
		out = append(out, postFromRow(row))
	}
	return out
}

func revisionFromRow(r sqlcgen.Revision) kernel.Revision {
	var author *uuid.UUID
	if r.AuthorID.Valid {
		id := fromPgUUID(r.AuthorID)
		author = &id
	}
	return kernel.Revision{
		ID:         fromPgUUID(r.ID),
		EntityType: r.EntityType,
		EntityID:   fromPgUUID(r.EntityID),
		Snapshot:   r.Snapshot,
		AuthorID:   author,
		CreatedAt:  r.CreatedAt.Time,
	}
}
