package contact

import "time"

// EventContactSubmitted fires ASYNCHRONOUSLY after a contact-form submission is
// accepted. Its async listener resolves the settings-driven recipient and sends
// the contact-notification email (drained from the outbox by the worker).
const EventContactSubmitted = "contact.submitted"

// SubmittedEvent is the async payload for an accepted contact submission.
// It carries everything the notification listener needs to compose the email
// WITHOUT re-reading any row (there is no contact row — the outbox is the only
// durable record). The outbox persists this JSON.
type SubmittedEvent struct {
	FromName    string    `json:"from_name"`
	FromEmail   string    `json:"from_email"`
	Subject     string    `json:"subject"`
	Message     string    `json:"message"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// Name implements events.Event.
func (SubmittedEvent) Name() string { return EventContactSubmitted }
