package templ

import "strconv"

// SearchResultCard is one rendered search hit on the public results page. Eyebrow
// is the type label (Post|Page|Service); SnippetHTML is the highlighted excerpt,
// already sanitized to allow ONLY <mark> (rendered verbatim); Date is the mono
// published date (may be empty).
type SearchResultCard struct {
	Eyebrow     string
	Title       string
	URL         string
	SnippetHTML string
	Date        string
}

// SearchView is the public /search results page view-model. Query echoes the
// term into the input + messages; Submitted reports whether a (non-blank) query
// was run (drives the blank-prompt vs. empty-results states); Fallback notes the
// ILIKE path served the hits.
type SearchView struct {
	SiteName  string
	HomeURL   string
	ActionURL string // the search form GET target ("/search")
	Query     string
	Submitted bool
	Fallback  bool
	Cards     []SearchResultCard
	Total     int
	Pager     Pagination
	// SEO carries the resolved document-head view-model (M8). Search result pages
	// are noindex (query pages should not be indexed).
	SEO *SEOView
}

// searchTitle builds the document <title>, echoing the query when one was run.
func searchTitle(v SearchView) string {
	if v.Submitted && v.Query != "" {
		return "Search: " + v.Query + " · " + v.SiteName
	}
	return "Search · " + v.SiteName
}

// searchSummary is the "N results for X" line above the result list.
func searchSummary(v SearchView) string {
	return strconv.Itoa(v.Total) + " " + resultsLabel(v.Total) + " for “" + v.Query + "”"
}

// searchEmptyHeading is the empty-state heading naming the missing term.
func searchEmptyHeading(v SearchView) string {
	return "No results for “" + v.Query + "”"
}

// resultsLabel pluralizes the result-count noun.
func resultsLabel(n int) string {
	if n == 1 {
		return "result"
	}
	return "results"
}
