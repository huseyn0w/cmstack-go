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
}
