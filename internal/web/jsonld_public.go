package web

import (
	"github.com/huseyn0w/cmstack-go/internal/content/services"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// serviceFAQItems converts the service JSON-LD seam's FAQ entries into the
// web/templ FAQPage builder input.
func serviceFAQItems(faqs []services.JSONLDFAQ) []webtempl.FAQItem {
	items := make([]webtempl.FAQItem, 0, len(faqs))
	for _, f := range faqs {
		items = append(items, webtempl.FAQItem{Question: f.Question, Answer: f.Answer})
	}
	return items
}

// compact drops empty strings from a JSON-LD block list. Builders return "" when
// they have nothing worth emitting (e.g. a <2-item breadcrumb or an empty
// ItemList); compact keeps only the blocks that carry a payload.
func compact(blocks ...string) []string {
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b != "" {
			out = append(out, b)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// absoluteURL resolves a possibly-rooted URL against BaseURL. An already-absolute
// URL (has a scheme) or empty value is returned unchanged.
func (h *PostPublicHandler) absoluteURL(u string) string { return h.site.absolutizeIfRooted(u) }

// cardItems converts rendered blog cards into ItemList entries with absolute
// URLs, preserving order (position is the index).
func (h *PostPublicHandler) cardItems(cards []webtempl.PublicPostCard) []webtempl.Breadcrumb {
	items := make([]webtempl.Breadcrumb, 0, len(cards))
	for _, c := range cards {
		items = append(items, webtempl.Breadcrumb{Name: c.Title, URL: h.absoluteURL(c.URL)})
	}
	return items
}

// postCardItems converts blog cards for a taxonomy archive into ItemList entries.
func (h *TaxonomyPublicHandler) postCardItems(cards []webtempl.PublicPostCard) []webtempl.Breadcrumb {
	items := make([]webtempl.Breadcrumb, 0, len(cards))
	for _, c := range cards {
		items = append(items, webtempl.Breadcrumb{Name: c.Title, URL: h.site.absolutizeIfRooted(c.URL)})
	}
	return items
}

// serviceCardItems converts service cards into ItemList entries with absolute URLs.
func (h *ServicePublicHandler) serviceCardItems(cards []webtempl.PublicServiceCard) []webtempl.Breadcrumb {
	items := make([]webtempl.Breadcrumb, 0, len(cards))
	for _, c := range cards {
		items = append(items, webtempl.Breadcrumb{Name: c.Title, URL: h.site.absolutizeIfRooted(c.URL)})
	}
	return items
}
