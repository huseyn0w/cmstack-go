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
	// Taxonomy (M3): category + tag labels shown as small pills in the admin row.
	Taxonomy []string
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
	BulkURL   string      // POST target for bulk actions
	Summary   BulkSummary // post-redirect outcome banner (aria-live)
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

	// Taxonomy (M3): the category tree (indented options, pre-selected) and the
	// flat tag set, plus the comma list the tag input mirrors back. Empty when
	// taxonomy is not wired.
	CategoryChoices []TaxonomyChoice
	TagChoices      []TaxonomyChoice

	// SEO metadata (M8). MetaTitle/MetaDescription are TRANSLATABLE and render on
	// every locale tab; CanonicalURL/NoIndex are STRUCTURAL and render only on the
	// default-locale base row (gated by editStructural, like slug/status).
	MetaTitle       string
	MetaDescription string
	CanonicalURL    string
	NoIndex         bool

	// Per-locale translation (M7b-1). LocaleTabs is the one-tab-per-language strip
	// on the editor (django-parler ?language=xx parity); ActiveLocale is the tag of
	// the tab currently being edited (en = base row, de/ru = translation overlay).
	// IsDefaultLocale is true on the en tab, where the structural fields (slug/
	// status/schedule/taxonomy) render and are editable; on de/ru only the
	// translatable title/excerpt/body show (structural fields are shared, edited on
	// en). Empty LocaleTabs means the strip is not shown (e.g. the new-post form,
	// which has no id yet to translate against).
	LocaleTabs      []LocaleTab
	ActiveLocale    string
	IsDefaultLocale bool
}

// editStructural reports whether the editor should render the SHARED structural
// fields (slug/status/schedule/taxonomy + publish/schedule actions). They show
// only when editing the default-locale base row (or when the locale strip is not
// present at all, e.g. the new-post form), never on a de/ru translation tab.
func (v PostFormView) editStructural() bool {
	return len(v.LocaleTabs) == 0 || v.IsDefaultLocale
}

// LocaleTab is one language tab on the post editor's per-locale tab strip
// (DESIGN_SYSTEM §5 Tabs). HasTranslation marks a de/ru tab whose translation
// row already exists (shown as a dot/badge). The default (en) tab edits the base
// row and is never marked.
type LocaleTab struct {
	Label          string // display name (e.g. "English", "Deutsch")
	Code           string // BCP-47 tag (e.g. "en", "de")
	Href           string // editor URL for this locale (?language=xx)
	Active         bool
	HasTranslation bool
}

// TaxonomyChoice is one selectable category/tag in the post editor. Depth
// indents categories in the tree-aware multi-select; Selected pre-checks the
// post's current associations.
type TaxonomyChoice struct {
	ID       string
	Label    string
	Depth    int
	Selected bool
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
	BulkURL   string      // POST target for bulk restore/permanent-delete
	Summary   BulkSummary // post-redirect outcome banner (aria-live)
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
	// SEO carries the resolved document-head view-model (M8); nil in reduced
	// contexts (the head then falls back to the minimal title/description).
	SEO *SEOView
	// JSONLD carries ready-to-emit JSON-LD blocks (ItemList), each script-safe.
	JSONLD []string
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
	// SEO carries the resolved document-head view-model (M8).
	SEO *SEOView

	// JSON-LD enrichment (M8). UpdatedAt feeds dateModified; ImageURL is the
	// post's absolute image (its OGImage or the site default); InLanguage is the
	// active locale's BCP-47 tag; Publisher is the site Organization node reused
	// as the BlogPosting publisher. All optional — empty fields are omitted.
	UpdatedAt  time.Time
	ImageURL   string
	InLanguage string
	Publisher  OrgIdentity

	// JSONLD carries extra ready-to-emit JSON-LD blocks (e.g. BreadcrumbList),
	// each already script-safe; rendered verbatim in a ld+json script.
	JSONLD []string

	// Taxonomy (M3): the post's categories + tags as archive-linking pills, and
	// the related-posts block (posts sharing >=1 category/tag).
	Categories []TaxonomyPill
	Tags       []TaxonomyPill
	Related    []PublicPostCard
}
