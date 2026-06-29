// Package comments implements the Comments content vertical (M5): threaded,
// moderated comments on posts. All business logic lives in the service; data
// access is only through the Repository interface; side effects are emitted as
// events (async comment-notification email via the outbox). Handlers are thin.
//
// PII boundary: a Comment carries the author's email and IP for moderation, but
// the PublicComment serializer NEVER includes them — the public thread shows
// only the display name, body, and date.
package comments

import (
	"time"

	"github.com/google/uuid"
)

// Status is the moderation state of a comment. A comment is public only once
// APPROVED; everything else (PENDING/SPAM/TRASH) is hidden from the public read.
type Status string

const (
	// StatusPending is the default state of a freshly submitted comment, awaiting
	// moderation. It is NOT public.
	StatusPending Status = "PENDING"
	// StatusApproved is the only public state.
	StatusApproved Status = "APPROVED"
	// StatusSpam marks a comment as spam (hidden, retained for review).
	StatusSpam Status = "SPAM"
	// StatusTrash soft-trashes a comment (hidden, retained until hard-deleted).
	StatusTrash Status = "TRASH"
)

// Valid reports whether s is one of the known statuses.
func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusApproved, StatusSpam, StatusTrash:
		return true
	default:
		return false
	}
}

// String returns the status as its stored text value.
func (s Status) String() string { return string(s) }

// ParseStatus maps a stored/string value to a Status, defaulting to PENDING for
// an unknown value so a tampered filter can never widen visibility.
func ParseStatus(s string) Status {
	st := Status(s)
	if st.Valid() {
		return st
	}
	return StatusPending
}

// Comment is the full domain representation, including PII (AuthorEmail,
// AuthorIP) used only for moderation. AuthorUserID is set when a signed-in user
// authored the comment (which enables the self-edit window).
type Comment struct {
	ID           uuid.UUID
	PostID       uuid.UUID
	ParentID     *uuid.UUID
	AuthorUserID *uuid.UUID
	AuthorName   string
	AuthorEmail  string // PII — never serialized to the public payload
	AuthorIP     string // PII — never serialized to the public payload
	Body         string // sanitized plain text (strict policy on every write)
	Status       Status
	CreatedAt    time.Time
	UpdatedAt    time.Time
	EditedAt     *time.Time
}

// PublicComment is the public-safe projection of a Comment. It DELIBERATELY
// omits AuthorEmail and AuthorIP — the public thread must never leak PII. This
// is asserted in the service tests.
type PublicComment struct {
	ID         uuid.UUID
	ParentID   *uuid.UUID
	AuthorName string
	Body       string
	CreatedAt  time.Time
	Edited     bool
	// Mine flags the viewer's own comment so the UI can offer self-edit/delete.
	Mine bool
	// Pending flags the viewer's own not-yet-approved comment.
	Pending bool
	Replies []PublicComment
}

// toPublic projects a Comment to its public-safe form. mine/pending are layered
// on by the service when a signed-in viewer is present. Email/IP are never
// copied — this is the single conversion point that enforces the PII boundary.
func toPublic(c Comment) PublicComment {
	return PublicComment{
		ID:         c.ID,
		ParentID:   c.ParentID,
		AuthorName: c.AuthorName,
		Body:       c.Body,
		CreatedAt:  c.CreatedAt,
		Edited:     c.EditedAt != nil,
		Replies:    []PublicComment{},
	}
}
