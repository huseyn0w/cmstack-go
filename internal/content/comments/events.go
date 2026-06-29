package comments

import (
	"time"

	"github.com/google/uuid"
)

// EventCommentCreated fires ASYNCHRONOUSLY after a new comment is stored. Its
// async listener composes + sends the comment-notification email to the post
// author + comment moderators (drained from the outbox by the worker).
const EventCommentCreated = "comment.created"

// CommentCreatedEvent is the async payload for a newly stored comment. It
// carries enough for the notification listener to compose the email and build a
// link WITHOUT re-reading the row. The author EMAIL and IP are DELIBERATELY
// omitted from the payload (PII minimization) — the outbox persists this JSON.
type CommentCreatedEvent struct {
	CommentID  uuid.UUID `json:"comment_id"`
	PostID     uuid.UUID `json:"post_id"`
	PostSlug   string    `json:"post_slug"`
	PostTitle  string    `json:"post_title"`
	AuthorName string    `json:"author_name"`
	Excerpt    string    `json:"excerpt"`
	CreatedAt  time.Time `json:"created_at"`
}

// Name implements events.Event.
func (CommentCreatedEvent) Name() string { return EventCommentCreated }
