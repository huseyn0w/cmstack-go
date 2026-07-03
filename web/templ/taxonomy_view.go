package templ

// --- categories (admin) ------------------------------------------------------

// CategoryRow is one row in the admin categories indented tree/list. Depth drives
// the visual indentation (DESIGN_SYSTEM §5 Sortable tree).
type CategoryRow struct {
	ID       string
	Name     string
	Slug     string
	Depth    int
	EditURL  string
	PostsURL string // public archive link
}

// CategoryListView is the admin categories list page view-model.
type CategoryListView struct {
	Shell     AdminShell
	Rows      []CategoryRow
	NewURL    string
	BulkURL   string
	Summary   BulkSummary
	CSRFToken string
}

// ParentOption is one entry in the category parent picker (indented by depth).
// Disabled marks an option that cannot be chosen (the category itself or its
// descendants, which would create a cycle).
type ParentOption struct {
	ID       string
	Label    string // name, indented by depth
	Selected bool
	Disabled bool
}

// CategoryFormView is the admin category editor view-model.
type CategoryFormView struct {
	Shell         AdminShell
	IsNew         bool
	ID            string
	Name          string
	Slug          string
	Description   string
	ParentID      string
	ParentChoices []ParentOption
	ActionURL     string
	CSRFToken     string
	FieldErrors   map[string]string
	Error         string
	BackURL       string

	// Per-locale translation (M7b-3). LocaleTabs is the one-tab-per-language strip
	// on the editor (django-parler ?language=xx parity); ActiveLocale is the tag of
	// the tab being edited; IsDefaultLocale is true on the en tab, where the
	// structural fields (slug/parent) are editable — on a de/ru tab only the
	// translatable name/description show. Empty LocaleTabs means the strip is not
	// shown (the new-category form, before an id exists).
	LocaleTabs      []LocaleTab
	ActiveLocale    string
	IsDefaultLocale bool
}

// editStructural reports whether the structural (shared, non-translatable) fields
// should render: true on the default-locale tab or when no locale strip is shown.
func (v CategoryFormView) editStructural() bool {
	return len(v.LocaleTabs) == 0 || v.IsDefaultLocale
}

// --- tags (admin) ------------------------------------------------------------

// TagRow is one row in the admin tags table.
type TagRow struct {
	ID       string
	Name     string
	Slug     string
	EditURL  string
	PostsURL string
}

// TagListView is the admin tags list page view-model.
type TagListView struct {
	Shell     AdminShell
	Rows      []TagRow
	Pager     Pagination
	NewURL    string
	BulkURL   string
	Summary   BulkSummary
	CSRFToken string
}

// TagFormView is the admin tag editor view-model.
type TagFormView struct {
	Shell       AdminShell
	IsNew       bool
	ID          string
	Name        string
	Slug        string
	ActionURL   string
	CSRFToken   string
	FieldErrors map[string]string
	Error       string
	BackURL     string

	// Per-locale translation (M7b-3): see CategoryFormView. Tags translate only
	// name; the slug is the sole structural field gated to the default tab.
	LocaleTabs      []LocaleTab
	ActiveLocale    string
	IsDefaultLocale bool
}

// editStructural reports whether the structural (shared) fields should render:
// true on the default-locale tab or when no locale strip is shown.
func (v TagFormView) editStructural() bool {
	return len(v.LocaleTabs) == 0 || v.IsDefaultLocale
}

// --- public archives ---------------------------------------------------------

// TaxonomyPill is a single category/tag pill linking to its archive (§5 Badges).
type TaxonomyPill struct {
	Label string
	URL   string
}

// TaxonomyArchiveView is the public category/tag archive page (a filtered list
// of published posts with breadcrumbs + pagination + empty state).
type TaxonomyArchiveView struct {
	SiteName    string
	HomeURL     string
	Kind        string // "Category" or "Tag" (breadcrumb + eyebrow label)
	Name        string
	Description string // categories only; empty for tags
	Cards       []PublicPostCard
	Pager       Pagination
	// SEO carries the resolved document-head view-model (M8).
	SEO *SEOView
	// JSONLD carries ready-to-emit JSON-LD blocks (BreadcrumbList, ItemList),
	// each already script-safe; rendered verbatim in a ld+json script.
	JSONLD []string
}
