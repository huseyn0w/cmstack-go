package templ

import "time"

// PostStatus mirrors the kernel status as a plain string for the view layer
// (the templ package must not import domain packages).
type PostStatus string

// Content status values surfaced to the view layer.
const (
	PostStatusDraft     PostStatus = "DRAFT"
	PostStatusPublished PostStatus = "PUBLISHED"
)

// PostRow is one row in the admin posts table.
type PostRow struct {
	ID         string
	Title      string
	Slug       string
	AuthorName string
	Status     PostStatus
	Scheduled  bool
	Date       string // formatted display date (published/updated)
	EditURL    string
}

// StatusTab is one status filter tab on the admin list.
type StatusTab struct {
	Label  string
	Value  string // "" = all
	Href   string
	Active bool
	Count  int
}

// Pagination is the shared pager view-model (DESIGN_SYSTEM §5).
type Pagination struct {
	Page     int
	PageSize int
	Total    int
	PrevURL  string // empty when no previous page
	NextURL  string // empty when no next page
}

// TotalPages returns the number of pages (>=1).
func (p Pagination) TotalPages() int {
	if p.PageSize <= 0 {
		return 1
	}
	n := (p.Total + p.PageSize - 1) / p.PageSize
	if n < 1 {
		return 1
	}
	return n
}

// PostListView is the admin posts list page view-model.
type PostListView struct {
	Shell     AdminShell
	Rows      []PostRow
	Tabs      []StatusTab
	Pager     Pagination
	NewURL    string
	CSRFToken string
}

// PostFormView is the admin post editor view-model.
type PostFormView struct {
	Shell        AdminShell
	IsNew        bool
	ID           string
	Title        string
	Slug         string
	Excerpt      string
	Body         string // sanitized HTML loaded into the editor
	Status       PostStatus
	ScheduledAt  string // RFC3339 / datetime-local value, empty when unset
	ActionURL    string
	CSRFToken    string
	FieldErrors  map[string]string
	Error        string
	RevisionsURL string
	BackURL      string
}

// RevisionRow is one entry in the revision history list.
type RevisionRow struct {
	ID         string
	AuthorName string
	CreatedAt  string
	// Snapshot fields surfaced for the simple diff view.
	Title      string
	Body       string
	RestoreURL string
}

// RevisionsView is the revision history page.
type RevisionsView struct {
	Shell     AdminShell
	PostTitle string
	PostID    string
	Current   RevisionRow // the live post, for the diff baseline
	Rows      []RevisionRow
	BackURL   string
	CSRFToken string
}

// TrashRow is one row in the trash list.
type TrashRow struct {
	ID         string
	Title      string
	DeletedAt  string
	RestoreURL string
	DeleteURL  string
}

// TrashView is the admin trash page.
type TrashView struct {
	Shell     AdminShell
	Rows      []TrashRow
	Pager     Pagination
	CSRFToken string
}

// --- public --------------------------------------------------------------

// PublicPostCard is one card on the public blog index.
type PublicPostCard struct {
	Title       string
	URL         string
	Excerpt     string
	AuthorName  string
	Date        string
	ReadingTime int
}

// PublicPostIndexView is the public /blog index.
type PublicPostIndexView struct {
	SiteName string
	HomeURL  string
	Cards    []PublicPostCard
	Pager    Pagination
}

// PublicPostView is the public post detail page.
type PublicPostView struct {
	SiteName     string
	HomeURL      string
	Title        string
	Slug         string
	BodyHTML     string // already sanitized server-side; rendered verbatim
	Excerpt      string
	AuthorID     string
	AuthorName   string
	AuthorURL    string
	PublishedAt  time.Time
	ReadingTime  int
	LikeCount    int
	Liked        bool
	CanLike      bool // true for signed-in users
	LikeURL      string
	CSRFToken    string
	CanonicalURL string
}
