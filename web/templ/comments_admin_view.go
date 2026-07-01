package templ

// CommentStatus mirrors the comments domain status as a plain string for the
// view layer (the templ package must not import domain packages).
type CommentStatus string

// Comment moderation status values surfaced to the view layer.
const (
	CommentStatusPending  CommentStatus = "PENDING"
	CommentStatusApproved CommentStatus = "APPROVED"
	CommentStatusSpam     CommentStatus = "SPAM"
	CommentStatusTrash    CommentStatus = "TRASH"
)

// CommentAdminRow is one row in the admin moderation table.
type CommentAdminRow struct {
	ID         string
	AuthorName string
	PostTitle  string
	Excerpt    string // truncated body preview
	Status     CommentStatus
	Date       string // formatted display date
	// Per-row action POST targets.
	ApproveURL string
	SpamURL    string
	TrashURL   string
	DeleteURL  string
}

// CommentModerationTab is one status filter tab on the moderation list. Count is
// the per-status total; PendingBadge marks the tab that surfaces the pending
// count badge.
type CommentModerationTab struct {
	Label     string
	Value     string // "" = all
	Href      string
	Active    bool
	Count     int
	ShowBadge bool // render the count as a badge (used for Pending)
}

// CommentModerationView is the admin moderation list page view-model.
type CommentModerationView struct {
	Shell        AdminShell
	Rows         []CommentAdminRow
	Tabs         []CommentModerationTab
	Pager        Pagination
	BulkURL      string      // POST target for bulk moderation actions
	Summary      BulkSummary // post-redirect outcome banner (aria-live)
	CSRFToken    string
	PendingCount int // total pending, surfaced as the section badge
}

// Label maps a moderation status to a short display label.
func (s CommentStatus) Label() string {
	switch s {
	case CommentStatusApproved:
		return "Approved"
	case CommentStatusSpam:
		return "Spam"
	case CommentStatusTrash:
		return "Trash"
	default:
		return "Pending"
	}
}
