package templ

import (
	"bytes"
	"encoding/json"
	"strings"
)

// PostJSONLD builds the BlogPosting JSON-LD for a public post detail page and
// returns it SAFE to embed inside a <script type="application/ld+json">: it
// marshals with SetEscapeHTML(true) then additionally escapes stray '<','>','&'
// so the payload can never break out of the script element (anti-XSS),
// mirroring jsonld.go's ProfileJSONLD. This is the SEO seam for posts (richer
// fields land with M8 SEO metadata).
func PostJSONLD(v PublicPostView) string {
	article := map[string]any{
		"@context":      "https://schema.org",
		"@type":         "BlogPosting",
		"headline":      v.Title,
		"datePublished": v.PublishedAt.Format("2006-01-02T15:04:05Z07:00"),
		"author": map[string]any{
			"@type": "Person",
			"name":  v.AuthorName,
			"url":   v.AuthorURL,
		},
	}
	if v.Excerpt != "" {
		article["description"] = v.Excerpt
	}
	if v.CanonicalURL != "" {
		article["mainEntityOfPage"] = v.CanonicalURL
		article["url"] = v.CanonicalURL
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(article); err != nil {
		return "{}"
	}
	out := strings.TrimRight(buf.String(), "\n")
	out = strings.ReplaceAll(out, "<", `<`)
	out = strings.ReplaceAll(out, ">", `>`)
	out = strings.ReplaceAll(out, "&", `&`)
	return out
}
