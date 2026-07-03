package templ

import "time"

// PageRow is one row in the admin pages tree/table. Depth drives the indentation
// of the hierarchy visualization (DESIGN_SYSTEM §5 Sortable list/tree).
type PageRow struct {
	ID       string
	Title    string
	Slug     string
	Status   PostStatus
	Template string
	Depth    int // 0 = top-level; each level indents the title
	Date     string
	EditURL  string
}

// PageParentOption is one entry in the editor's parent picker. Indent prefixes
// the label so the option list reads as a tree; the page being edited (and its
// descendants) are excluded upstream to prevent cycles.
type PageParentOption struct {
	ID     string
	Label  string
	Indent int
}

// PageTemplateOption is one entry in the template selector.
type PageTemplateOption struct {
	Value string
	Label string
}

// PageListView is the admin pages list page view-model.
type PageListView struct {
	Shell     AdminShell
	Rows      []PageRow
	Tabs      []StatusTab
	Pager     Pagination
	NewURL    string
	BulkURL   string
	Summary   BulkSummary
	CSRFToken string
}

// PageFormView is the admin page editor view-model.
type PageFormView struct {
	Shell        AdminShell
	IsNew        bool
	ID           string
	Title        string
	Slug         string
	Body         string
	Status       PostStatus
	ParentID     string // selected parent id, "" for top-level
	Template     string
	Parents      []PageParentOption
	TemplateOpts []PageTemplateOption
	ActionURL    string
	CSRFToken    string
	FieldErrors  map[string]string
	Error        string
	RevisionsURL string
	BackURL      string

	// Per-locale translation (M7b-2). LocaleTabs is the one-tab-per-language strip
	// on the editor (django-parler ?language=xx parity); ActiveLocale is the tag of
	// the tab being edited (en = base row, de/ru = translation overlay).
	// IsDefaultLocale is true on the en tab, where the structural fields (slug/
	// parent/template/status) render and are editable; on de/ru only the
	// translatable title/body show. Empty LocaleTabs means the strip is not shown
	// (e.g. the new-page form, which has no id yet to translate against).
	LocaleTabs      []LocaleTab
	ActiveLocale    string
	IsDefaultLocale bool
}

// editStructural reports whether the page editor should render the SHARED
// structural fields (slug/parent/template/status + publish action). They show
// only when editing the default-locale base row (or when the locale strip is not
// present at all, e.g. the new-page form), never on a de/ru translation tab.
func (v PageFormView) editStructural() bool {
	return len(v.LocaleTabs) == 0 || v.IsDefaultLocale
}

// PageRevisionsView is the page revision history page.
type PageRevisionsView struct {
	Shell     AdminShell
	PageTitle string
	PageID    string
	Current   RevisionRow
	Rows      []RevisionRow
	BackURL   string
	CSRFToken string
}

// PageTrashView is the admin pages trash page.
type PageTrashView struct {
	Shell     AdminShell
	Rows      []TrashRow
	Pager     Pagination
	BulkURL   string
	Summary   BulkSummary
	CSRFToken string
}

// --- public --------------------------------------------------------------

// PageBreadcrumb is one ancestor link in the public page breadcrumb trail.
type PageBreadcrumb struct {
	Title string
	URL   string
}

// PublicPageView is the public page detail page.
type PublicPageView struct {
	SiteName     string
	HomeURL      string
	Title        string
	Slug         string
	BodyHTML     string // already sanitized server-side; rendered verbatim
	Template     string // selected template name (drives the layout)
	Breadcrumbs  []PageBreadcrumb
	PublishedAt  time.Time
	ReadingTime  int
	CanonicalURL string
	// SEO carries the resolved document-head view-model (M8).
	SEO *SEOView
}
