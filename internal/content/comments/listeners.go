package comments

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/events"
)

// CommentNotifier sends the comment-notification email. The platform LogMailer
// (dev) satisfies it now; SMTP is wired in M14. Declaring the narrow interface
// here keeps comments decoupled from the mailer package and trivially fakeable.
type CommentNotifier interface {
	SendCommentNotification(ctx context.Context, to []string, msg CommentNotification) error
}

// CommentNotification is the composed message handed to the notifier.
type CommentNotification struct {
	PostTitle   string
	AuthorName  string
	Excerpt     string
	ModerateURL string
}

// ModeratorResolver returns the email addresses that should be notified of a new
// comment: the post's author plus the users holding the comment-moderation
// permission. The web wiring composes it over the post + user repositories.
type ModeratorResolver interface {
	// NotificationRecipients returns the de-duplicated recipient emails for a new
	// comment on postID (post author + moderators). An empty slice means "no
	// recipients" (the listener then no-ops).
	NotificationRecipients(ctx context.Context, postID uuid.UUID) ([]string, error)
}

// NotificationListener consumes the async comment.created events drained from the
// outbox and sends the moderation-notification email. It is fault-tolerant by
// the relay's retry semantics; a recipient-resolution or send failure leaves the
// outbox row for retry.
type NotificationListener struct {
	log        *slog.Logger
	recipients ModeratorResolver
	notifier   CommentNotifier
	baseURL    string
}

// NewNotificationListener constructs the listener. baseURL builds the absolute
// /admin/comments link in the email.
func NewNotificationListener(log *slog.Logger, recipients ModeratorResolver, notifier CommentNotifier, baseURL string) *NotificationListener {
	if log == nil {
		log = slog.Default()
	}
	return &NotificationListener{log: log, recipients: recipients, notifier: notifier, baseURL: baseURL}
}

// Register subscribes the async comment.created handler. Call in BOTH the server
// (so the event is marked async + enqueued in-tx) and the worker (so the relay
// dispatches it after commit).
func (l *NotificationListener) Register(bus *events.Bus) {
	bus.SubscribeAsyncHandler(EventCommentCreated, l.onCommentCreated)
}

func (l *NotificationListener) onCommentCreated(ctx context.Context, payload []byte) error {
	var ev CommentCreatedEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return fmt.Errorf("unmarshal %s: %w", EventCommentCreated, err)
	}

	to, err := l.recipients.NotificationRecipients(ctx, ev.PostID)
	if err != nil {
		return fmt.Errorf("resolve comment notification recipients: %w", err)
	}
	if len(to) == 0 {
		l.log.Debug("comment notification: no recipients", "comment_id", ev.CommentID)
		return nil
	}

	msg := CommentNotification{
		PostTitle:   ev.PostTitle,
		AuthorName:  ev.AuthorName,
		Excerpt:     ev.Excerpt,
		ModerateURL: l.moderateURL(),
	}
	if err := l.notifier.SendCommentNotification(ctx, to, msg); err != nil {
		return fmt.Errorf("send comment notification: %w", err)
	}
	return nil
}

// moderateURL builds the absolute admin moderation link from baseURL.
func (l *NotificationListener) moderateURL() string {
	base := strings.TrimRight(l.baseURL, "/")
	if base == "" {
		return "/admin/comments"
	}
	return base + "/admin/comments"
}
