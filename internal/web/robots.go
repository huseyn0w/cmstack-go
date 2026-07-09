package web

import (
	"net/http"
	"strings"
)

// aiCrawlerAgents are the well-known AI crawler user-agents blocked in robots.txt
// when AllowAICrawlers is false (M8).
var aiCrawlerAgents = []string{
	"GPTBot",
	"ChatGPT-User",
	"CCBot",
	"Google-Extended",
	"anthropic-ai",
	"ClaudeBot",
	"PerplexityBot",
	"Bytespider",
	"Amazonbot",
}

// Robots serves GET /robots.txt: a dynamic policy that always disallows the
// admin/account/auth areas, advertises the sitemap, honors GlobalNoindex (a
// staging gate that disallows everything), and — when AllowAICrawlers is false —
// blocks the well-known AI crawlers. Content-Type text/plain; charset=utf-8.
func (h *CrawlerHandler) Robots(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var b strings.Builder

	b.WriteString("User-agent: *\n")
	if h.site.resolveGlobalNoindex(ctx) {
		// Staging gate: block everything for all crawlers.
		b.WriteString("Disallow: /\n")
	} else {
		b.WriteString("Allow: /\n")
		// Keep private/functional areas out of the index.
		b.WriteString("Disallow: /admin/\n")
		b.WriteString("Disallow: /account\n")
		b.WriteString("Disallow: /auth/\n")
	}

	// AI crawlers: default-allow unless explicitly disabled.
	if !h.site.resolveAllowAICrawlers(ctx) {
		for _, ua := range aiCrawlerAgents {
			b.WriteString("\nUser-agent: ")
			b.WriteString(ua)
			b.WriteString("\nDisallow: /\n")
		}
	}

	base := strings.TrimSuffix(h.site.BaseURL, "/")
	b.WriteString("\nSitemap: ")
	b.WriteString(base)
	b.WriteString("/sitemap.xml\n")

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}
