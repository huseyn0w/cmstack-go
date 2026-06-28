package templ_test

import (
	"testing"
	"time"

	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

func TestPageList_TreeIndentAndBadges(t *testing.T) {
	v := webtempl.PageListView{
		Rows: []webtempl.PageRow{
			{ID: "p1", Title: "About", Slug: "about", Status: webtempl.PostStatusPublished, Template: "default", Depth: 0, Date: "Jan 1, 2026", EditURL: "/admin/pages/p1/edit"},
			{ID: "p2", Title: "Team", Slug: "team", Status: webtempl.PostStatusDraft, Template: "landing", Depth: 1, Date: "Jan 2, 2026", EditURL: "/admin/pages/p2/edit"},
		},
		Tabs:   []webtempl.StatusTab{{Label: "All", Active: true, Href: "/admin/pages"}},
		Pager:  webtempl.Pagination{Page: 1, PageSize: 20, Total: 2},
		NewURL: "/admin/pages/new",
	}
	html := renderStr(t, webtempl.PageList(v))
	mustContain(
		t, html,
		`data-testid="pages-table"`,
		`data-testid="pages-tree"`,
		`data-testid="page-status-tabs"`,
		`data-testid="page-row-p1"`,
		`data-testid="new-page"`,
		"About",
		"Team",
		"padding-left:20px", // depth-1 indent
		`data-testid="status-badge"`,
	)
}

func TestPageList_Empty(t *testing.T) {
	v := webtempl.PageListView{Tabs: []webtempl.StatusTab{{Label: "All", Active: true}}, NewURL: "/admin/pages/new"}
	html := renderStr(t, webtempl.PageList(v))
	mustContain(t, html, `data-testid="pages-empty"`, "No pages yet")
}

// TestPageList_BulkSelectionUI asserts the pages list has parity with the §5 bulk
// selection UI (select-all, per-row checkbox, action bar, destructive modal).
func TestPageList_BulkSelectionUI(t *testing.T) {
	v := webtempl.PageListView{
		Rows: []webtempl.PageRow{
			{ID: "p1", Title: "About", Slug: "about", Status: webtempl.PostStatusPublished, Template: "default", Date: "Jan 1, 2026", EditURL: "/admin/pages/p1/edit"},
		},
		Tabs:      []webtempl.StatusTab{{Label: "All", Active: true, Href: "/admin/pages"}},
		Pager:     webtempl.Pagination{Page: 1, PageSize: 20, Total: 1},
		NewURL:    "/admin/pages/new",
		BulkURL:   "/admin/pages/bulk",
		CSRFToken: "tok",
	}
	html := renderStr(t, webtempl.PageList(v))
	mustContain(
		t, html,
		`data-testid="bulk-select-all"`,
		`data-testid="bulk-select-p1"`,
		`data-testid="bulk-bar"`,
		`data-testid="bulk-action-trash"`,
		`data-testid="bulk-confirm-modal"`,
		`action="/admin/pages/bulk"`,
	)
}

func TestPageEditor_ParentPickerAndTemplateSelector(t *testing.T) {
	v := webtempl.PageFormView{
		IsNew:    true,
		Status:   webtempl.PostStatusDraft,
		Template: "default",
		Parents: []webtempl.PageParentOption{
			{ID: "root", Label: "Root", Indent: 0},
			{ID: "child", Label: "Child", Indent: 1},
		},
		TemplateOpts: []webtempl.PageTemplateOption{
			{Value: "default", Label: "Default"},
			{Value: "full-width", Label: "Full width"},
			{Value: "landing", Label: "Landing"},
		},
		ActionURL:   "/admin/pages",
		CSRFToken:   "tok",
		FieldErrors: map[string]string{},
		BackURL:     "/admin/pages",
	}
	html := renderStr(t, webtempl.PageEditor(v))
	mustContain(
		t, html,
		`data-testid="page-editor"`,
		`data-testid="page-field-parent"`,   // parent picker
		`data-testid="page-field-template"`, // template selector
		`data-testid="editor-toolbar"`,      // shared rich-text editor
		"Root",
		"Child",
		"Full width",
		"Landing",
		`data-testid="page-action-save"`,
		`data-testid="page-action-publish"`,
	)
}

func TestPublicPage_TemplateAndBreadcrumbs(t *testing.T) {
	v := webtempl.PublicPageView{
		SiteName:    "CMStack",
		HomeURL:     "/",
		Title:       "Team",
		Slug:        "team",
		BodyHTML:    "<p>Our team.</p>",
		Template:    "full-width",
		Breadcrumbs: []webtempl.PageBreadcrumb{{Title: "About", URL: "/p/about"}},
		PublishedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	html := renderStr(t, webtempl.PublicPage(v))
	mustContain(
		t, html,
		`data-testid="page-article"`,
		`data-template="full-width"`, // template drives the layout
		"<article",
		`class="prose`,
		`data-testid="page-breadcrumb"`,
		`aria-label="Breadcrumb"`,
		"About",               // ancestor crumb
		"/p/about",            // ancestor link reflects hierarchy
		"<p>Our team.</p>",    // sanitized body verbatim
		`aria-current="page"`, // the page itself
	)
}
