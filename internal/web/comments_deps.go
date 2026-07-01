package web

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/content/comments"
	"github.com/huseyn0w/cmstack-go/internal/content/posts"
)

// commentPostPublic is the narrow subset of *posts.Service the comment adapters
// need to resolve a published post by slug. *posts.Service satisfies it.
type commentPostPublic interface {
	PublicBySlug(ctx context.Context, slug string) (posts.Post, error)
}

// commentPostByID resolves a post (any status) by id so the notification
// recipient (the post author) can be looked up. *posts.RepoPG satisfies it.
type commentPostByID interface {
	GetByID(ctx context.Context, id uuid.UUID) (posts.Post, error)
}

// CommentAdapters bundles the small adapters that bridge the comments domain
// ports (PostLookup, ModeratorResolver, CommentNotifier) onto the existing post,
// user, and mailer infrastructure. It is constructed in the server wiring and
// owns no business logic.
type CommentAdapters struct {
	posts  commentPostPublic
	postID commentPostByID
	emails userEmailLookup
}

// userEmailLookup resolves an author email by user id. The accounts user repo
// satisfies it via a one-line wrapper in the wiring.
type userEmailLookup interface {
	AuthorEmail(ctx context.Context, userID uuid.UUID) (string, error)
}

// NewCommentAdapters constructs the adapters. posts resolves published posts by
// slug; postByID resolves a post (any status) by id for the author lookup; emails
// resolves an author email by user id.
func NewCommentAdapters(public commentPostPublic, postByID commentPostByID, emails userEmailLookup) *CommentAdapters {
	return &CommentAdapters{posts: public, postID: postByID, emails: emails}
}

// PublishedBySlug satisfies comments.PostLookup. It maps the posts not-found
// sentinel onto the comments one so the service/handlers stay decoupled from the
// posts package.
func (a *CommentAdapters) PublishedBySlug(ctx context.Context, slug string) (comments.PostRef, error) {
	p, err := a.posts.PublicBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, posts.ErrNotFound) {
			return comments.PostRef{}, comments.ErrNotFound
		}
		return comments.PostRef{}, err
	}
	return comments.PostRef{ID: p.ID, Slug: p.Slug, Title: p.Title}, nil
}

// AuthorEmail satisfies comments.PostLookup: it resolves the post's author email
// (the notification recipient). A missing post/author yields "" + nil so the
// listener simply no-ops rather than failing the submit.
func (a *CommentAdapters) AuthorEmail(ctx context.Context, postID uuid.UUID) (string, error) {
	email, err := a.authorEmail(ctx, postID)
	if err != nil {
		if errors.Is(err, posts.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return email, nil
}

// authorEmail looks up the post then its author's email.
func (a *CommentAdapters) authorEmail(ctx context.Context, postID uuid.UUID) (string, error) {
	p, err := a.postID.GetByID(ctx, postID)
	if err != nil {
		return "", err
	}
	if p.AuthorID == uuid.Nil {
		return "", nil
	}
	return a.emails.AuthorEmail(ctx, p.AuthorID)
}

// NotificationRecipients satisfies comments.ModeratorResolver. For M5 the post
// author's email is sufficient; moderators-by-permission is a later milestone.
//
// TODO(M14/M15): also include users holding (read|update, comment) so non-author
// moderators are notified. For now the author email (de-duplicated) is returned.
func (a *CommentAdapters) NotificationRecipients(ctx context.Context, postID uuid.UUID) ([]string, error) {
	email, err := a.authorEmail(ctx, postID)
	if err != nil {
		if errors.Is(err, posts.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if email == "" {
		return nil, nil
	}
	return dedupeEmails([]string{email}), nil
}

// dedupeEmails removes empty/duplicate addresses while preserving order.
func dedupeEmails(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, e := range in {
		if e == "" {
			continue
		}
		if _, ok := seen[e]; ok {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out
}

// Title satisfies CommentPostTitler: it resolves a target post's title for the
// moderation row, returning "" (the handler falls back to an id fragment) when
// the post is missing or the lookup fails.
func (a *CommentAdapters) Title(ctx context.Context, postID uuid.UUID) string {
	p, err := a.postID.GetByID(ctx, postID)
	if err != nil {
		return ""
	}
	return p.Title
}

// commentMailer is the subset of *mailer.LogMailer the notifier adapter calls.
type commentMailer interface {
	SendCommentNotification(ctx context.Context, to []string, postTitle, authorName, excerpt, moderateURL string) error
}

// CommentNotifierAdapter bridges comments.CommentNotifier onto the platform
// mailer's flat-argument SendCommentNotification, mapping the composed message
// struct to the mailer's signature. It avoids an import cycle (the mailer never
// imports comments).
type CommentNotifierAdapter struct {
	mailer commentMailer
}

// NewCommentNotifierAdapter wraps a mailer as a comments.CommentNotifier.
func NewCommentNotifierAdapter(m commentMailer) *CommentNotifierAdapter {
	return &CommentNotifierAdapter{mailer: m}
}

// SendCommentNotification satisfies comments.CommentNotifier.
func (a *CommentNotifierAdapter) SendCommentNotification(ctx context.Context, to []string, msg comments.CommentNotification) error {
	return a.mailer.SendCommentNotification(ctx, to, msg.PostTitle, msg.AuthorName, msg.Excerpt, msg.ModerateURL)
}

// userByIDEmailer is the subset of the accounts user repo the email lookup needs.
type userByIDEmailer[U any] interface {
	GetByID(ctx context.Context, id uuid.UUID) (U, error)
}

// UserEmailRepo adapts a user repo into a userEmailLookup: it resolves a user by
// id and projects their email via the supplied accessor. The wiring constructs it
// over *accounts.UserRepoPG with func(u accounts.User) string { return u.Email }.
type UserEmailRepo[U any] struct {
	repo  userByIDEmailer[U]
	email func(U) string
}

// NewUserEmailRepo constructs a UserEmailRepo.
func NewUserEmailRepo[U any](repo userByIDEmailer[U], email func(U) string) *UserEmailRepo[U] {
	return &UserEmailRepo[U]{repo: repo, email: email}
}

// AuthorEmail satisfies userEmailLookup.
func (r *UserEmailRepo[U]) AuthorEmail(ctx context.Context, userID uuid.UUID) (string, error) {
	u, err := r.repo.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	return r.email(u), nil
}
