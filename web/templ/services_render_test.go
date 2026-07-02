package templ_test

import (
	"strings"
	"testing"
	"time"

	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

func TestServiceList_TableAndEmpty(t *testing.T) {
	v := webtempl.ServiceListView{
		Rows: []webtempl.ServiceRow{
			{ID: "s1", Title: "SEO Audit", Slug: "seo-audit", Status: webtempl.PostStatusPublished, Price: "From $499", Date: "Jan 1, 2026", EditURL: "/admin/services/s1/edit"},
		},
		Tabs:   []webtempl.StatusTab{{Label: "All", Active: true, Href: "/admin/services"}},
		Pager:  webtempl.Pagination{Page: 1, PageSize: 20, Total: 1},
		NewURL: "/admin/services/new",
	}
	html := renderStr(t, webtempl.ServiceList(v))
	mustContain(
		t, html,
		`data-testid="services-table"`,
		`data-testid="service-status-tabs"`,
		`data-testid="service-row-s1"`,
		`data-testid="new-service"`,
		"SEO Audit",
		"From $499",
	)

	empty := webtempl.ServiceListView{Tabs: []webtempl.StatusTab{{Label: "All", Active: true}}, NewURL: "/admin/services/new"}
	emptyHTML := renderStr(t, webtempl.ServiceList(empty))
	mustContain(t, emptyHTML, `data-testid="services-empty"`, "No services yet")
}

// TestServiceList_BulkSelectionUI asserts the services list has parity with the
// §5 bulk selection UI.
func TestServiceList_BulkSelectionUI(t *testing.T) {
	v := webtempl.ServiceListView{
		Rows: []webtempl.ServiceRow{
			{ID: "s1", Title: "SEO Audit", Slug: "seo-audit", Status: webtempl.PostStatusPublished, Price: "From $499", Date: "Jan 1, 2026", EditURL: "/admin/services/s1/edit"},
		},
		Tabs:      []webtempl.StatusTab{{Label: "All", Active: true, Href: "/admin/services"}},
		Pager:     webtempl.Pagination{Page: 1, PageSize: 20, Total: 1},
		NewURL:    "/admin/services/new",
		BulkURL:   "/admin/services/bulk",
		CSRFToken: "tok",
	}
	html := renderStr(t, webtempl.ServiceList(v))
	mustContain(
		t, html,
		`data-testid="bulk-select-all"`,
		`data-testid="bulk-select-s1"`,
		`data-testid="bulk-bar"`,
		`data-testid="bulk-action-trash"`,
		`data-testid="bulk-confirm-modal"`,
		`action="/admin/services/bulk"`,
	)
}

func TestServiceEditor_RepeatableFAQSection(t *testing.T) {
	v := webtempl.ServiceFormView{
		IsNew:      true,
		Status:     webtempl.PostStatusDraft,
		Price:      "From $499",
		AreaServed: "Berlin",
		FAQs: []webtempl.ServiceFAQField{
			{Question: "How long does it take?", Answer: "About a week."},
		},
		ActionURL:   "/admin/services",
		CSRFToken:   "tok",
		FieldErrors: map[string]string{},
		BackURL:     "/admin/services",
	}
	html := renderStr(t, webtempl.ServiceEditor(v))
	mustContain(
		t, html,
		`data-testid="service-editor"`,
		`data-testid="service-field-price"`,
		`data-testid="service-field-area"`,
		`data-testid="faq-editor"`, // repeatable FAQ section
		`data-testid="faq-add"`,    // add row
		`data-testid="faq-list"`,
		`aria-label="Move question up"`, // keyboard-accessible reorder
		`aria-label="Move question down"`,
		`aria-label="Remove question"`,
		`name="faq_question[]"`, // posted array fields
		`name="faq_answer[]"`,
		`data-testid="service-action-save"`,
		`data-testid="service-action-publish"`,
		"How long does it take?", // seeded row reaches the Alpine initializer
	)
}

// TestServiceEditor_LocaleTabStrip asserts the service editor's per-locale tab
// strip renders with tablist/tab a11y, an active marker, a has-translation dot,
// a hidden active-locale field, and (on a de tab) the shared structural/citable
// fields + FAQ are hidden and the translation note shown (M7b-2).
func TestServiceEditor_LocaleTabStrip(t *testing.T) {
	v := webtempl.ServiceFormView{
		ID:           "s1",
		Title:        "SEO Pruefung",
		Body:         "<p>DE</p>",
		Status:       webtempl.PostStatusPublished,
		ActionURL:    "/admin/services/s1",
		CSRFToken:    "tok",
		FieldErrors:  map[string]string{},
		BackURL:      "/admin/services",
		ActiveLocale: "de",
		LocaleTabs: []webtempl.LocaleTab{
			{Label: "English", Code: "en", Href: "/admin/services/s1/edit", Active: false},
			{Label: "Deutsch", Code: "de", Href: "/admin/services/s1/edit?language=de", Active: true, HasTranslation: true},
			{Label: "Русский", Code: "ru", Href: "/admin/services/s1/edit?language=ru", Active: false},
		},
		IsDefaultLocale: false,
	}
	html := renderStr(t, webtempl.ServiceEditor(v))
	mustContain(
		t, html,
		`data-testid="locale-tabs"`,
		`role="tablist"`,
		`data-testid="locale-tab-en"`,
		`data-testid="locale-tab-de"`,
		`data-testid="locale-tab-ru"`,
		`aria-selected="true"`,
		`data-testid="locale-dot-de"`,
		`role="tabpanel"`,
		`name="locale"`,
		`data-testid="service-translation-note"`,
	)
	for _, absent := range []string{`data-testid="service-field-price"`, `data-testid="service-field-status"`, `data-testid="faq-editor"`, `data-testid="service-action-publish"`} {
		if strings.Contains(html, absent) {
			t.Errorf("de translation tab should hide %q", absent)
		}
	}
}

func TestPublicServiceDetail_FactsAndAccessibleFAQ(t *testing.T) {
	v := webtempl.PublicServiceView{
		SiteName:   "CMStack",
		HomeURL:    "/",
		Title:      "SEO Audit",
		Slug:       "seo-audit",
		Summary:    "We audit your site.",
		BodyHTML:   "<p>Details here.</p>",
		Price:      "From $499",
		AreaServed: "Berlin and surrounding areas",
		FAQs: []webtempl.PublicServiceFAQ{
			{Question: "How long?", AnswerHTML: "<p>About a week.</p>"},
		},
		PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	html := renderStr(t, webtempl.PublicServiceDetail(v))
	mustContain(
		t, html,
		`data-testid="service-article"`,
		"<article",
		`data-testid="service-summary"`,
		`data-testid="service-facts"`,
		`data-testid="service-price"`,
		"From $499",
		`data-testid="service-area"`,
		"Berlin and surrounding areas",
		`class="prose`,
		`data-testid="service-faq"`,
		"<details", // accessible native disclosure accordion
		"<summary",
		"How long?",
		"<p>About a week.</p>", // sanitized answer verbatim
		`aria-labelledby="faq-heading"`,
	)
}

func TestPublicServiceIndex_CardsAndEmpty(t *testing.T) {
	withCards := webtempl.PublicServiceIndexView{
		SiteName: "CMStack",
		Cards: []webtempl.PublicServiceCard{
			{Title: "SEO Audit", URL: "/services/seo-audit", Summary: "We audit.", Price: "From $499"},
		},
		Pager: webtempl.Pagination{Page: 1, PageSize: 12, Total: 1},
	}
	html := renderStr(t, webtempl.PublicServiceIndex(withCards))
	mustContain(t, html, `data-testid="services-grid"`, `data-testid="service-card"`, "SEO Audit", `data-testid="service-card-price"`)

	empty := webtempl.PublicServiceIndexView{SiteName: "CMStack"}
	emptyHTML := renderStr(t, webtempl.PublicServiceIndex(empty))
	mustContain(t, emptyHTML, `data-testid="services-index-empty"`, "No services yet")
}
