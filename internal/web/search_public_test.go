package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/huseyn0w/agentic-cms-go/internal/content/search"
)

// stubSearch is a scripted SearchService for the handler tests. It records the
// last args so pagination/query wiring can be asserted.
type stubSearch struct {
	res        search.Result
	err        error
	lastQuery  string
	lastPage   int
	lastPerPag int
}

func (s *stubSearch) Search(_ context.Context, q string, page, perPage int) (search.Result, error) {
	s.lastQuery, s.lastPage, s.lastPerPag = q, page, perPage
	// Echo the query into the result the way the real service does.
	res := s.res
	res.Query = strings.TrimSpace(q)
	return res, s.err
}

func TestSearchHandler_RendersResults(t *testing.T) {
	published := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	stub := &stubSearch{res: search.Result{
		Total: 1,
		Hits: []search.Hit{
			{Type: search.HitPost, Title: "PG Tuning", Slug: "pg-tuning", URL: "/blog/pg-tuning", Snippet: "fast <mark>pg</mark> queries", PublishedAt: &published},
		},
	}}
	h := NewSearchPublicHandler(stub, "Agentic CMS")

	req := httptest.NewRequest(http.MethodGet, "/search?q=postgresql", nil)
	rec := httptest.NewRecorder()
	h.Search(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="search-results"`,
		"PG Tuning",
		"/blog/pg-tuning",
		"Post", // type eyebrow
		"<mark>pg</mark>",
		"January 2, 2026",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("results body missing %q", want)
		}
	}
	if stub.lastQuery != "postgresql" {
		t.Errorf("handler passed query %q, want postgresql", stub.lastQuery)
	}
}

func TestSearchHandler_EmptyState(t *testing.T) {
	stub := &stubSearch{res: search.Result{Total: 0, Hits: nil}}
	h := NewSearchPublicHandler(stub, "Agentic CMS")

	req := httptest.NewRequest(http.MethodGet, "/search?q=zzzznope", nil)
	rec := httptest.NewRecorder()
	h.Search(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="search-empty"`) {
		t.Error("expected empty-state marker")
	}
	if !strings.Contains(body, "zzzznope") {
		t.Error("empty state should name the missing term")
	}
}

func TestSearchHandler_BlankQueryPrompt(t *testing.T) {
	stub := &stubSearch{res: search.Result{}}
	h := NewSearchPublicHandler(stub, "Agentic CMS")

	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	rec := httptest.NewRecorder()
	h.Search(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="search-prompt"`) {
		t.Error("blank query should render the prompt, not results/empty")
	}
	if strings.Contains(body, `data-testid="search-results"`) {
		t.Error("blank query should not render a results list")
	}
}

func TestSearchHandler_PaginationParam(t *testing.T) {
	stub := &stubSearch{res: search.Result{Total: 30, Hits: []search.Hit{{Type: search.HitPage, Slug: "p", URL: "/p/p"}}}}
	h := NewSearchPublicHandler(stub, "Agentic CMS")

	req := httptest.NewRequest(http.MethodGet, "/search?q=x&page=3", nil)
	rec := httptest.NewRecorder()
	h.Search(rec, req)

	if stub.lastPage != 3 {
		t.Errorf("handler passed page %d, want 3", stub.lastPage)
	}
	if stub.lastPerPag != searchPageSize {
		t.Errorf("handler passed perPage %d, want %d", stub.lastPerPag, searchPageSize)
	}
}

func TestSearchHandler_SanitizesSnippetToMarkOnly(t *testing.T) {
	// A snippet carrying a stray tag (e.g. from rich-text body) must be stripped
	// except for the <mark> highlight.
	stub := &stubSearch{res: search.Result{
		Total: 1,
		Hits: []search.Hit{
			{Type: search.HitPost, Title: "T", Slug: "t", URL: "/blog/t", Snippet: `<script>alert(1)</script> keep <mark>hit</mark>`},
		},
	}}
	h := NewSearchPublicHandler(stub, "Agentic CMS")

	req := httptest.NewRequest(http.MethodGet, "/search?q=hit", nil)
	rec := httptest.NewRecorder()
	h.Search(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "<script>") {
		t.Error("snippet must not carry a raw <script> tag")
	}
	if !strings.Contains(body, "<mark>hit</mark>") {
		t.Error("snippet should preserve the <mark> highlight")
	}
}
