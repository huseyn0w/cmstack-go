package comments

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// fakeResolver returns fixed recipients (or an error).
type fakeResolver struct {
	to  []string
	err error
}

func (f fakeResolver) NotificationRecipients(context.Context, uuid.UUID) ([]string, error) {
	return f.to, f.err
}

// fakeNotifier records the last send.
type fakeNotifier struct {
	to     []string
	msg    CommentNotification
	called bool
	err    error
}

func (n *fakeNotifier) SendCommentNotification(_ context.Context, to []string, msg CommentNotification) error {
	n.called = true
	n.to = to
	n.msg = msg
	return n.err
}

func createdPayload(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(CommentCreatedEvent{
		CommentID:  uuid.New(),
		PostID:     uuid.New(),
		PostSlug:   "hello",
		PostTitle:  "Hello World",
		AuthorName: "Guest",
		Excerpt:    "great post",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestNotificationListener_SendsToRecipients(t *testing.T) {
	notifier := &fakeNotifier{}
	l := NewNotificationListener(nil, fakeResolver{to: []string{"author@example.com"}}, notifier, "https://cms.test/")

	if err := l.onCommentCreated(context.Background(), createdPayload(t)); err != nil {
		t.Fatalf("onCommentCreated: %v", err)
	}
	if !notifier.called {
		t.Fatal("notifier not called")
	}
	if len(notifier.to) != 1 || notifier.to[0] != "author@example.com" {
		t.Fatalf("recipients = %v", notifier.to)
	}
	if notifier.msg.PostTitle != "Hello World" || notifier.msg.AuthorName != "Guest" {
		t.Fatalf("message not composed from event: %+v", notifier.msg)
	}
	if !strings.HasSuffix(notifier.msg.ModerateURL, "/admin/comments") {
		t.Fatalf("moderate URL = %q, want absolute /admin/comments", notifier.msg.ModerateURL)
	}
}

func TestNotificationListener_NoRecipientsNoOp(t *testing.T) {
	notifier := &fakeNotifier{}
	l := NewNotificationListener(nil, fakeResolver{to: nil}, notifier, "https://cms.test")
	if err := l.onCommentCreated(context.Background(), createdPayload(t)); err != nil {
		t.Fatalf("onCommentCreated: %v", err)
	}
	if notifier.called {
		t.Fatal("notifier should not be called when there are no recipients")
	}
}

func TestNotificationListener_ResolverErrorPropagates(t *testing.T) {
	notifier := &fakeNotifier{}
	l := NewNotificationListener(nil, fakeResolver{err: errors.New("db down")}, notifier, "https://cms.test")
	if err := l.onCommentCreated(context.Background(), createdPayload(t)); err == nil {
		t.Fatal("expected resolver error to propagate (leaves outbox row for retry)")
	}
	if notifier.called {
		t.Fatal("notifier must not be called on resolver failure")
	}
}
