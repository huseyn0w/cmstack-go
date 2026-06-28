package templ

import (
	"bytes"
	"encoding/json"
	"strings"
)

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
// element. It marshals with SetEscapeHTML(true) and then additionally escapes
// any stray '<', '>' and '&' so the payload can never break out of the script
// element or inject markup (anti-XSS), mirroring DESIGN_SYSTEM §8.
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
