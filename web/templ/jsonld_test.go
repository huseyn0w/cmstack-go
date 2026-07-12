package templ_test

import (
	"context"
	"encoding/json"
	"html"
	"strings"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

func sampleView() webtempl.ProfilePageView {
	return webtempl.ProfilePageView{
		Name:        "Grace Hopper",
		Bio:         "Computing pioneer",
		AvatarURL:   "/uploads/avatars/u1/x.png",
		Website:     "https://grace.dev",
		ProfileURL:  "https://example.com/authors/u1",
		SocialOrder: []string{"github", "twitter"},
		Socials:     map[string]string{"github": "https://github.com/grace", "twitter": "https://twitter.com/grace"},
		RoleLabel:   "Author",
		SiteName:    "Agentic CMS",
		HomeURL:     "https://example.com",
	}
}

func TestProfileJSONLD_WellFormedAndComplete(t *testing.T) {
	raw := webtempl.ProfileJSONLD(sampleView())
	// The escaped output must unescape back to valid JSON with the right shape.
	unescaped := html.UnescapeString(raw)
	var doc map[string]any
	if err := json.Unmarshal([]byte(unescaped), &doc); err != nil {
		t.Fatalf("JSON-LD not well-formed: %v\n%s", err, raw)
	}
	if doc["@type"] != "ProfilePage" {
		t.Errorf("@type = %v, want ProfilePage", doc["@type"])
	}
	person, ok := doc["mainEntity"].(map[string]any)
	if !ok {
		t.Fatal("mainEntity missing")
	}
	if person["@type"] != "Person" {
		t.Errorf("mainEntity @type = %v", person["@type"])
	}
	if person["name"] != "Grace Hopper" {
		t.Errorf("person name = %v", person["name"])
	}
	sameAs, ok := person["sameAs"].([]any)
	if !ok || len(sameAs) != 3 {
		t.Fatalf("sameAs should list website + 2 socials, got %v", person["sameAs"])
	}
}

func TestProfileJSONLD_EscapesAngleBracketsAndAmp_NoEmail(t *testing.T) {
	v := sampleView()
	v.Name = `Eve <script>alert("xss")</script> & Co`
	v.Bio = "contact me at hidden@secret.test"
	raw := webtempl.ProfileJSONLD(v)

	// No raw markup-significant chars survive into the script context.
	if strings.Contains(raw, "<script") || strings.Contains(raw, "</script>") {
		t.Errorf("unescaped <script> leaked into JSON-LD:\n%s", raw)
	}
	if strings.Contains(raw, "<") || strings.Contains(raw, ">") {
		t.Errorf("raw angle bracket leaked:\n%s", raw)
	}
	// It still unescapes to valid JSON carrying the literal name.
	var doc map[string]any
	if err := json.Unmarshal([]byte(html.UnescapeString(raw)), &doc); err != nil {
		t.Fatalf("escaped JSON-LD not recoverable: %v", err)
	}
}

func TestAuthorPage_RendersJSONLDWithoutEmail(t *testing.T) {
	view := webtempl.AuthorPageView{ProfilePageView: sampleView()}
	out, err := render.ToString(context.Background(), webtempl.AuthorPage(view))
	if err != nil {
		t.Fatalf("render AuthorPage: %v", err)
	}
	if !strings.Contains(out, `application/ld+json`) {
		t.Error("author page missing JSON-LD script")
	}
	if !strings.Contains(out, `aria-label="Breadcrumb"`) {
		t.Error("author page missing breadcrumb landmark")
	}
	if !strings.Contains(out, "<article") {
		t.Error("author page missing semantic <article>")
	}
	if !strings.Contains(out, "No published posts yet") {
		t.Error("author page should show empty posts placeholder (M2 seam)")
	}
	// The page must NEVER render an email — assert a representative one is absent.
	if strings.Contains(out, "@example.com") || strings.Contains(strings.ToLower(out), "secret@") {
		t.Error("email leaked into rendered author page")
	}
}
