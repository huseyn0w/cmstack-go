package templ_test

import (
	"strings"
	"testing"

	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

func TestPublicSearch_ResultsRenderCardsBadgesPagination(t *testing.T) {
	v := webtempl.SearchView{
		SiteName:  "Agentic CMS",
		HomeURL:   "/",
		ActionURL: "/search",
		Query:     "postgresql",
		Submitted: true,
		Total:     25,
		Cards: []webtempl.SearchResultCard{
			{Eyebrow: "Post", Title: "PG Tuning", URL: "/blog/pg-tuning", SnippetHTML: "fast <mark>postgresql</mark> queries", Date: "January 1, 2026"},
			{Eyebrow: "Service", Title: "PG Consulting", URL: "/services/pg-consult", SnippetHTML: "experts"},
		},
		Pager: webtempl.Pagination{Page: 1, PageSize: 10, Total: 25, NextURL: "/search?page=2&q=postgresql"},
	}
	html := renderStr(t, webtempl.PublicSearch(v))
	mustContain(
		t, html,
		`data-testid="search-form"`,
		`data-testid="search-input"`,
		`data-testid="search-results"`,
		`data-testid="search-card"`,
		`data-testid="search-card-type"`,
		"Post",
		"Service",
		"/blog/pg-tuning",
		"<mark>postgresql</mark>", // highlight survives (mark allowed)
		`data-testid="pagination"`,
		"January 1, 2026",
	)
	// the search input echoes the current query
	if !strings.Contains(html, `value="postgresql"`) {
		t.Error("search input should echo the query value")
	}
}

func TestPublicSearch_EmptyState(t *testing.T) {
	v := webtempl.SearchView{
		SiteName:  "Agentic CMS",
		HomeURL:   "/",
		ActionURL: "/search",
		Query:     "zzzznope",
		Submitted: true,
		Total:     0,
		Cards:     nil,
	}
	html := renderStr(t, webtempl.PublicSearch(v))
	mustContain(t, html, `data-testid="search-empty"`, "No results for", "zzzznope")
	if strings.Contains(html, `data-testid="search-results"`) {
		t.Error("empty state must not render a results list")
	}
}

func TestPublicSearch_BlankPrompt(t *testing.T) {
	v := webtempl.SearchView{
		SiteName:  "Agentic CMS",
		HomeURL:   "/",
		ActionURL: "/search",
		Query:     "",
		Submitted: false,
	}
	html := renderStr(t, webtempl.PublicSearch(v))
	mustContain(t, html, `data-testid="search-prompt"`, "What are you looking for?")
	if strings.Contains(html, `data-testid="search-empty"`) {
		t.Error("blank prompt must not show the no-results empty state")
	}
}

func TestPublicSearch_BoxAccessibility(t *testing.T) {
	v := webtempl.SearchView{SiteName: "Agentic CMS", HomeURL: "/", ActionURL: "/search"}
	html := renderStr(t, webtempl.PublicSearch(v))
	mustContain(
		t, html,
		`role="search"`,
		`<label for="q"`, // input is labeled
		`aria-label="Search terms"`,
		`aria-label="Breadcrumb"`, // semantic breadcrumb landmark
	)
}
