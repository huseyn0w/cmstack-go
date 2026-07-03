package templ

import (
	"bytes"
	"encoding/json"
	"strings"
)

// marshalJSONLD serializes doc into a string SAFE to embed inside a
// <script type="application/ld+json"> element. It marshals with
// SetEscapeHTML(true) then additionally escapes any stray '<', '>' and '&' so
// the payload can never break out of the script element or inject markup
// (anti-XSS), mirroring DESIGN_SYSTEM §8. Every JSON-LD builder in this package
// funnels through here so the escaping is defined in exactly one place.
func marshalJSONLD(doc any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(doc); err != nil {
		return "{}"
	}
	out := strings.TrimRight(buf.String(), "\n")
	// Defense in depth: even with SetEscapeHTML, guarantee no raw </script> or
	// markup-significant characters survive into the inline script context.
	out = strings.ReplaceAll(out, "<", `<`)
	out = strings.ReplaceAll(out, ">", `>`)
	out = strings.ReplaceAll(out, "&", `&`)
	return out
}

// ProfilePageView is the view-model for the public author page. It is assembled
// by the web layer from accounts.PublicAuthor and carries NO email — the public
// page must never leak it, and the type makes that impossible by omission.
type ProfilePageView struct {
	Name        string
	Bio         string
	AvatarURL   string
	Website     string
	ProfileURL  string   // absolute canonical URL of this profile page
	SocialOrder []string // stable, known-key order for rendering
	Socials     map[string]string
	RoleLabel   string
	// SiteName is used for the breadcrumb root.
	SiteName string
	HomeURL  string
}

// SameAs returns the author's external profile URLs (website + socials) in a
// stable order for the Person.sameAs JSON-LD array.
func (v ProfilePageView) SameAs() []string {
	var out []string
	if v.Website != "" {
		out = append(out, v.Website)
	}
	for _, k := range v.SocialOrder {
		if u := v.Socials[k]; u != "" {
			out = append(out, u)
		}
	}
	return out
}

// ProfileJSONLD builds the ProfilePage + Person JSON-LD for the author and
// returns it as a string SAFE to embed inside a <script type="application/ld+json">
// element (via marshalJSONLD's anti-XSS escaping).
func ProfileJSONLD(v ProfilePageView) string {
	person := map[string]any{
		"@type": "Person",
		"name":  v.Name,
		"url":   v.ProfileURL,
	}
	if v.AvatarURL != "" {
		person["image"] = v.AvatarURL
	}
	if sameAs := v.SameAs(); len(sameAs) > 0 {
		person["sameAs"] = sameAs
	}
	if v.Bio != "" {
		person["description"] = v.Bio
	}
	if v.RoleLabel != "" {
		person["jobTitle"] = v.RoleLabel
	}

	doc := map[string]any{
		"@context":   "https://schema.org",
		"@type":      "ProfilePage",
		"mainEntity": person,
	}
	return marshalJSONLD(doc)
}

// OrgIdentity is the site publisher's business identity (schema.org
// Organization), populated by the web layer from the site config. Empty fields
// are omitted from the emitted JSON-LD; sub-objects (PostalAddress, contact) are
// included only when at least one of their fields is set.
type OrgIdentity struct {
	Name         string
	LegalName    string
	LogoURL      string
	Email        string
	Phone        string
	Street       string
	Locality     string
	Region       string
	PostalCode   string
	Country      string
	URL          string
	SameAs       []string
	GeoStatement string
}

// hasAddress reports whether any PostalAddress field is set.
func (o OrgIdentity) hasAddress() bool {
	return o.Street != "" || o.Locality != "" || o.Region != "" || o.PostalCode != "" || o.Country != ""
}

// organizationNode builds the Organization object WITHOUT the @context, so it
// can be embedded as a `publisher` node inside another document (e.g.
// BlogPosting) or promoted to a top-level document by OrganizationJSONLD.
func (o OrgIdentity) organizationNode() map[string]any {
	org := map[string]any{
		"@type": "Organization",
		"name":  o.Name,
	}
	if o.URL != "" {
		org["url"] = o.URL
	}
	if o.LegalName != "" {
		org["legalName"] = o.LegalName
	}
	if o.LogoURL != "" {
		org["logo"] = map[string]any{
			"@type": "ImageObject",
			"url":   o.LogoURL,
		}
	}
	if o.GeoStatement != "" {
		org["description"] = o.GeoStatement
	}
	if o.Email != "" || o.Phone != "" {
		contact := map[string]any{"@type": "ContactPoint"}
		if o.Email != "" {
			contact["email"] = o.Email
		}
		if o.Phone != "" {
			contact["telephone"] = o.Phone
		}
		org["contactPoint"] = contact
	}
	if o.hasAddress() {
		addr := map[string]any{"@type": "PostalAddress"}
		if o.Street != "" {
			addr["streetAddress"] = o.Street
		}
		if o.Locality != "" {
			addr["addressLocality"] = o.Locality
		}
		if o.Region != "" {
			addr["addressRegion"] = o.Region
		}
		if o.PostalCode != "" {
			addr["postalCode"] = o.PostalCode
		}
		if o.Country != "" {
			addr["addressCountry"] = o.Country
		}
		org["address"] = addr
	}
	if len(o.SameAs) > 0 {
		org["sameAs"] = o.SameAs
	}
	return org
}

// OrganizationJSONLD builds the top-level schema.org Organization JSON-LD for
// the site publisher. Empty fields and empty sub-objects (address/contact) are
// omitted. Returns the script-safe string.
func OrganizationJSONLD(o OrgIdentity) string {
	doc := o.organizationNode()
	doc["@context"] = "https://schema.org"
	return marshalJSONLD(doc)
}

// WebSiteJSONLD builds the schema.org WebSite JSON-LD for the home page. When
// searchURLTemplate is non-empty it attaches a SearchAction potentialAction
// whose target is homeURL + "/search?q={search_term_string}" and whose
// query-input is "required name=search_term_string" (Sitelinks Searchbox).
func WebSiteJSONLD(name, homeURL, searchURLTemplate string) string {
	doc := map[string]any{
		"@context": "https://schema.org",
		"@type":    "WebSite",
		"name":     name,
		"url":      homeURL,
	}
	if searchURLTemplate != "" {
		doc["potentialAction"] = map[string]any{
			"@type":       "SearchAction",
			"target":      strings.TrimSuffix(homeURL, "/") + "/search?q={search_term_string}",
			"query-input": "required name=search_term_string",
		}
	}
	return marshalJSONLD(doc)
}

// Breadcrumb is one crumb in a BreadcrumbList (absolute URL).
type Breadcrumb struct {
	Name string
	URL  string
}

// BreadcrumbListJSONLD builds the schema.org BreadcrumbList JSON-LD from ordered
// crumbs (1-based position). It returns "" when there are fewer than 2 items (a
// single-crumb trail is not worth emitting).
func BreadcrumbListJSONLD(items []Breadcrumb) string {
	if len(items) < 2 {
		return ""
	}
	elements := make([]map[string]any, 0, len(items))
	for i, it := range items {
		elements = append(elements, map[string]any{
			"@type":    "ListItem",
			"position": i + 1,
			"name":     it.Name,
			"item":     it.URL,
		})
	}
	doc := map[string]any{
		"@context":        "https://schema.org",
		"@type":           "BreadcrumbList",
		"itemListElement": elements,
	}
	return marshalJSONLD(doc)
}

// ItemListJSONLD builds the schema.org ItemList JSON-LD for a listing page
// (blog/services index, archives) from the rendered cards (absolute URLs,
// 1-based position). It returns "" when there are no items.
func ItemListJSONLD(pageURL string, items []Breadcrumb) string {
	if len(items) == 0 {
		return ""
	}
	elements := make([]map[string]any, 0, len(items))
	for i, it := range items {
		elements = append(elements, map[string]any{
			"@type":    "ListItem",
			"position": i + 1,
			"url":      it.URL,
			"name":     it.Name,
		})
	}
	doc := map[string]any{
		"@context":        "https://schema.org",
		"@type":           "ItemList",
		"itemListElement": elements,
	}
	if pageURL != "" {
		doc["url"] = pageURL
	}
	return marshalJSONLD(doc)
}

// ServiceJSONLD builds the schema.org Service JSON-LD for a service detail page.
// provider is embedded as the Organization node when org.Name is set. Price is
// deliberately NOT emitted as an Offer (a freeform price is invalid without a
// numeric price + currency); it stays a visible on-page fact (parity with the
// services.JSONLDData seam note).
func ServiceJSONLD(title, summary, areaServed, canonicalURL string, org OrgIdentity) string {
	doc := map[string]any{
		"@context": "https://schema.org",
		"@type":    "Service",
		"name":     title,
	}
	if summary != "" {
		doc["description"] = summary
	}
	if areaServed != "" {
		doc["areaServed"] = areaServed
	}
	if canonicalURL != "" {
		doc["url"] = canonicalURL
	}
	if org.Name != "" {
		doc["provider"] = org.organizationNode()
	}
	return marshalJSONLD(doc)
}

// FAQItem is one question/answer pair for a FAQPage.
type FAQItem struct {
	Question string
	Answer   string
}

// FAQPageJSONLD builds the schema.org FAQPage JSON-LD from the service's FAQs.
// It returns "" when there are no FAQs (an empty FAQPage is invalid).
func FAQPageJSONLD(faqs []FAQItem) string {
	if len(faqs) == 0 {
		return ""
	}
	entities := make([]map[string]any, 0, len(faqs))
	for _, f := range faqs {
		entities = append(entities, map[string]any{
			"@type": "Question",
			"name":  f.Question,
			"acceptedAnswer": map[string]any{
				"@type": "Answer",
				"text":  f.Answer,
			},
		})
	}
	doc := map[string]any{
		"@context":   "https://schema.org",
		"@type":      "FAQPage",
		"mainEntity": entities,
	}
	return marshalJSONLD(doc)
}
