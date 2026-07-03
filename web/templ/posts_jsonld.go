package templ

// PostJSONLD builds the BlogPosting JSON-LD for a public post detail page and
// returns it SAFE to embed inside a <script type="application/ld+json"> (via
// marshalJSONLD's anti-XSS escaping). Beyond the core fields it emits, when
// available: image (post OGImage or the site default), dateModified, the
// publisher Organization node, and inLanguage (the active locale).
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
	if !v.UpdatedAt.IsZero() {
		article["dateModified"] = v.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")
	}
	if v.ImageURL != "" {
		article["image"] = v.ImageURL
	}
	if v.InLanguage != "" {
		article["inLanguage"] = v.InLanguage
	}
	if v.Publisher.Name != "" {
		article["publisher"] = v.Publisher.organizationNode()
	}
	return marshalJSONLD(article)
}
