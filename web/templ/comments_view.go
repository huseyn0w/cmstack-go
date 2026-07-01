package templ

import "fmt"

// CommentNode is one public comment in the rendered thread (recursive). It
// carries ONLY public-safe fields — never the author email or IP. Mine/Pending
// drive the self-edit/delete controls and the "awaiting moderation" hint.
type CommentNode struct {
	ID         string
	AuthorName string
	Initials   string
	Body       string
	Date       string // formatted, mono
	Edited     bool
	Mine       bool
	Pending    bool
	EditURL    string
	DeleteURL  string
	ReplyToID  string // parent id for the reply form target
	Replies    []CommentNode
}

// CommentThreadView is the view-model for the public comments partial: the
// rendered thread, the (guest or member) submit form, the optional success/error
// banners, the CSRF token, and the reCAPTCHA site key hook.
type CommentThreadView struct {
	PostSlug    string
	Count       int
	Comments    []CommentNode
	SubmitURL   string // POST target for a new top-level comment
	CSRFToken   string
	IsGuest     bool // true -> render name+email fields; false -> body only
	RecaptchaKey string

	// Submitted/Error banners (set after a POST round-trip).
	Submitted   bool   // a comment was accepted (now PENDING moderation)
	Error       string // top-level error message (rate-limit / spam / validation)
	FieldErrors map[string]string

	// PrefillName/Email/Body re-populate the guest form after a validation error.
	PrefillName  string
	PrefillEmail string
	PrefillBody  string
}

// HasFieldErrors reports whether any field-level error is present.
func (v CommentThreadView) HasFieldErrors() bool { return len(v.FieldErrors) > 0 }

// FieldErr returns the error for a field (or "").
func (v CommentThreadView) FieldErr(name string) string {
	if v.FieldErrors == nil {
		return ""
	}
	return v.FieldErrors[name]
}

// CountLabel renders the comment count header ("3 comments" / "1 comment").
func (v CommentThreadView) CountLabel() string {
	if v.Count == 1 {
		return "1 comment"
	}
	return fmt.Sprintf("%d comments", v.Count)
}
