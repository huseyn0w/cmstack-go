package web

import (
	"net/http"
	"strings"

	"github.com/huseyn0w/agentic-cms-go/internal/content/kernel"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/i18n"
)

// LLMs serves GET /llms.txt: a concise Markdown overview of the site per the
// emerging llms.txt convention — an H1 site name, a blockquote description, then
// Pages/Blog/Services sections listing published items as Markdown links.
// Content-Type text/plain; charset=utf-8.
func (h *CrawlerHandler) LLMs(w http.ResponseWriter, r *http.Request) {
	h.writeLLMs(w, r, false)
}

// LLMsFull serves GET /llms-full.txt: the same structure as /llms.txt but with
// each item's description (meta_description || excerpt || summary) appended to
// its line. It remains an index — no full bodies are emitted.
func (h *CrawlerHandler) LLMsFull(w http.ResponseWriter, r *http.Request) {
	h.writeLLMs(w, r, true)
}

// writeLLMs renders the llms.txt document. withDesc toggles the per-item
// description suffix used by /llms-full.txt.
func (h *CrawlerHandler) writeLLMs(w http.ResponseWriter, r *http.Request, withDesc bool) {
	ctx := r.Context()
	var b strings.Builder

	name := h.site.SiteName
	if name == "" {
		name = "Agentic CMS"
	}
	b.WriteString("# ")
	b.WriteString(name)
	b.WriteString("\n")
	if h.site.SiteDescription != "" {
		b.WriteString("\n> ")
		b.WriteString(collapse(h.site.SiteDescription))
		b.WriteString("\n")
	}

	section := func(title, prefix string, en SitemapEnumerator) bool {
		if en == nil {
			return true
		}
		items, err := en.SitemapItems(ctx)
		if err != nil {
			return false
		}
		if len(items) == 0 {
			return true
		}
		b.WriteString("\n## ")
		b.WriteString(title)
		b.WriteString("\n\n")
		for _, it := range items {
			h.writeItem(&b, prefix, it, withDesc)
		}
		return true
	}

	ok := section("Pages", "/p/", h.pages) &&
		section("Blog", "/blog/", h.posts) &&
		section("Services", "/services/", h.services)
	if !ok {
		http.Error(w, "llms error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

// writeItem appends one Markdown bullet: - [Title](absolute-url) optionally
// followed by ": description".
func (h *CrawlerHandler) writeItem(b *strings.Builder, prefix string, it kernel.SitemapItem, withDesc bool) {
	title := it.Title
	if title == "" {
		title = it.Slug
	}
	base := strings.TrimSuffix(h.site.BaseURL, "/")
	url := base + i18n.LocalizePath(i18n.Default(), prefix+it.Slug)

	b.WriteString("- [")
	b.WriteString(collapse(title))
	b.WriteString("](")
	b.WriteString(url)
	b.WriteString(")")
	if withDesc && it.Description != "" {
		b.WriteString(": ")
		b.WriteString(collapse(it.Description))
	}
	b.WriteString("\n")
}

// collapse flattens whitespace/newlines to single spaces so a description never
// breaks the single-line Markdown bullet structure.
func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
