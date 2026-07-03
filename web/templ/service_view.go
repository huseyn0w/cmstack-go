package templ

import "time"

// ServiceRow is one row in the admin services table.
type ServiceRow struct {
	ID      string
	Title   string
	Slug    string
	Status  PostStatus
	Price   string
	Date    string
	EditURL string
}

// ServiceFAQField is one editable FAQ row in the editor's repeatable section.
type ServiceFAQField struct {
	Question string
	Answer   string
}

// ServiceListView is the admin services list page view-model.
type ServiceListView struct {
	Shell     AdminShell
	Rows      []ServiceRow
	Tabs      []StatusTab
	Pager     Pagination
	NewURL    string
	BulkURL   string
	Summary   BulkSummary
	CSRFToken string
}

// ServiceFormView is the admin service editor view-model.
type ServiceFormView struct {
	Shell        AdminShell
	IsNew        bool
	ID           string
	Title        string
	Slug         string
	Summary      string
	Body         string
	Price        string
	AreaServed   string
	Status       PostStatus
	FAQs         []ServiceFAQField
	ActionURL    string
	CSRFToken    string
	FieldErrors  map[string]string
	Error        string
	RevisionsURL string
	BackURL      string

	// Per-locale translation (M7b-2). LocaleTabs is the one-tab-per-language strip
	// on the editor (django-parler ?language=xx parity); ActiveLocale is the tag of
	// the tab being edited (en = base row, de/ru = translation overlay).
	// IsDefaultLocale is true on the en tab, where the structural/citable fields
	// (slug/status/price/area-served + FAQ block) render and are editable; on de/ru
	// only the translatable title/summary/body show. Empty LocaleTabs means the
	// strip is not shown (e.g. the new-service form).
	LocaleTabs      []LocaleTab
	ActiveLocale    string
	IsDefaultLocale bool
}

// editStructural reports whether the service editor should render the SHARED
// structural/citable fields (slug/status/price/area-served + FAQ + publish
// action). They show only when editing the default-locale base row (or when the
// locale strip is absent, e.g. the new-service form), never on a de/ru tab.
func (v ServiceFormView) editStructural() bool {
	return len(v.LocaleTabs) == 0 || v.IsDefaultLocale
}

// ServiceRevisionsView is the service revision history page.
type ServiceRevisionsView struct {
	Shell        AdminShell
	ServiceTitle string
	ServiceID    string
	Current      RevisionRow
	Rows         []RevisionRow
	BackURL      string
	CSRFToken    string
}

// ServiceTrashView is the admin services trash page.
type ServiceTrashView struct {
	Shell     AdminShell
	Rows      []TrashRow
	Pager     Pagination
	BulkURL   string
	Summary   BulkSummary
	CSRFToken string
}

// --- public --------------------------------------------------------------

// PublicServiceFAQ is one rendered Q&A entry on the public service detail.
type PublicServiceFAQ struct {
	Question   string
	AnswerHTML string // sanitized server-side
}

// PublicServiceCard is one card on the public services index.
type PublicServiceCard struct {
	Title   string
	URL     string
	Summary string
	Price   string
}

// PublicServiceIndexView is the public /services index.
type PublicServiceIndexView struct {
	SiteName string
	HomeURL  string
	Cards    []PublicServiceCard
	Pager    Pagination
	// SEO carries the resolved document-head view-model (M8).
	SEO *SEOView
}

// PublicServiceView is the public service detail page.
type PublicServiceView struct {
	SiteName     string
	HomeURL      string
	Title        string
	Slug         string
	Summary      string
	BodyHTML     string // sanitized server-side; rendered verbatim
	Price        string
	AreaServed   string
	FAQs         []PublicServiceFAQ
	PublishedAt  time.Time
	ReadingTime  int
	CanonicalURL string
	// SEO carries the resolved document-head view-model (M8).
	SEO *SEOView
}
