package contact

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// fakeRecipient returns a fixed recipient.
type fakeRecipient struct{ to string }

func (f fakeRecipient) Recipient(context.Context) string { return f.to }

// fakeContactNotifier records the last send.
type fakeContactNotifier struct {
	called                                    bool
	to, fromEmail, fromName, subject, message string
	err                                       error
}

func (n *fakeContactNotifier) SendContactNotification(_ context.Context, to, fromEmail, fromName, subject, message string) error {
	n.called = true
	n.to, n.fromEmail, n.fromName, n.subject, n.message = to, fromEmail, fromName, subject, message
	return n.err
}

func submittedPayload(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(SubmittedEvent{
		FromName:    "Ada Lovelace",
		FromEmail:   "ada@example.com",
		Subject:     "Hello",
		Message:     "Please get in touch.",
		SubmittedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestNotifyListener_ComposesAndSends(t *testing.T) {
	notifier := &fakeContactNotifier{}
	l := NewNotifyListener(nil, fakeRecipient{to: "owner@example.com"}, notifier)

	if err := l.onContactSubmitted(context.Background(), submittedPayload(t)); err != nil {
		t.Fatalf("onContactSubmitted: %v", err)
	}
	if !notifier.called {
		t.Fatal("notifier not called")
	}
	if notifier.to != "owner@example.com" {
		t.Fatalf("to = %q, want owner@example.com", notifier.to)
	}
	if notifier.fromEmail != "ada@example.com" || notifier.fromName != "Ada Lovelace" {
		t.Fatalf("from not composed from event: %+v", notifier)
	}
	if notifier.subject != "Hello" || notifier.message != "Please get in touch." {
		t.Fatalf("subject/message not composed: %+v", notifier)
	}
}

func TestNotifyListener_EmptyRecipientNoOps(t *testing.T) {
	notifier := &fakeContactNotifier{}
	l := NewNotifyListener(nil, fakeRecipient{to: ""}, notifier)
	if err := l.onContactSubmitted(context.Background(), submittedPayload(t)); err != nil {
		t.Fatalf("empty recipient must not error the drain: %v", err)
	}
	if notifier.called {
		t.Fatal("notifier must not be called when the recipient is empty")
	}
}
