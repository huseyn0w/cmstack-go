// Package kernel holds the shared content primitives reused across content
// types (Posts now; Pages/Services next). It is deliberately lean: only the
// pieces that are genuinely generic across content types live here — the
// publish-state machine, slug generation/dedup, the rich-text sanitizer policy,
// reading-time estimation, and a generic immutable revision (snapshot/restore)
// mechanism. Type-specific logic stays in the owning domain package.
package kernel

// Status is the publish state of a content item. Only two states exist: DRAFT
// (not publicly visible) and PUBLISHED. Scheduling is modeled as a DRAFT with a
// scheduled_at timestamp, not a third status, so the public read path only ever
// filters on PUBLISHED.
type Status string

const (
	// StatusDraft is the default, unpublished state. A scheduled item is a DRAFT
	// with a future scheduled_at; the scheduler flips it to PUBLISHED when due.
	StatusDraft Status = "DRAFT"
	// StatusPublished marks an item visible on the public site.
	StatusPublished Status = "PUBLISHED"
)

// Valid reports whether s is a recognized status.
func (s Status) Valid() bool {
	return s == StatusDraft || s == StatusPublished
}

// String returns the status as its canonical string.
func (s Status) String() string { return string(s) }

// ParseStatus parses a string into a Status, defaulting to DRAFT for anything
// unrecognized (including empty) so a malformed input can never accidentally
// publish content.
func ParseStatus(raw string) Status {
	if Status(raw) == StatusPublished {
		return StatusPublished
	}
	return StatusDraft
}
