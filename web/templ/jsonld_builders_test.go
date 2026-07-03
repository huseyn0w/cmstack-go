package templ_test

import (
	"context"
	"encoding/json"
	"html"
	"strings"
	"testing"
	"time"

	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// decodeLD unescapes and unmarshals a script-safe JSON-LD string into a map.
func decodeLD(t *testing.T, raw string) map[string]any {
	t.Helper()
	if strings.Contains(raw, "<") || strings.Contains(raw, ">") {
		t.Fatalf("raw angle bracket leaked into JSON-LD:\n%s", raw)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(html.UnescapeString(raw)), &doc); err != nil {
		t.Fatalf("JSON-LD not well-formed: %v\n%s", err, raw)
	}
	return doc
}

func fullOrg() webtempl.OrgIdentity {
	return webtempl.OrgIdentity{
		Name:         "Acme GmbH",
		LegalName:    "Acme Gesellschaft mbH",
		LogoURL:      "https://site.test/logo.png",
		Email:        "hi@acme.test",
		Phone:        "+49 30 123456",
		Street:       "Torstrasse 1",
		Locality:     "Berlin",
		Region:       "Berlin",
		PostalCode:   "10119",
		Country:      "DE",
		URL:          "https://site.test",
		SameAs:       []string{"https://github.com/acme"},
		GeoStatement: "Serving Berlin and beyond.",
	}
}

func TestOrganizationJSONLD_IncludesAddressAndContactWhenPresent(t *testing.T) {
	doc := decodeLD(t, webtempl.OrganizationJSONLD(fullOrg()))
	if doc["@type"] != "Organization" {
		t.Errorf("@type = %v", doc["@type"])
	}
	if doc["description"] != "Serving Berlin and beyond." {
		t.Errorf("description (GeoStatement) = %v", doc["description"])
	}
	addr, ok := doc["address"].(map[string]any)
	if !ok {
		t.Fatal("address missing")
	}
	if addr["@type"] != "PostalAddress" || addr["addressLocality"] != "Berlin" || addr["postalCode"] != "10119" {
		t.Errorf("PostalAddress wrong: %v", addr)
	}
	contact, ok := doc["contactPoint"].(map[string]any)
	if !ok || contact["email"] != "hi@acme.test" || contact["telephone"] != "+49 30 123456" {
		t.Errorf("contactPoint wrong: %v", doc["contactPoint"])
	}
	logo, ok := doc["logo"].(map[string]any)
	if !ok || logo["@type"] != "ImageObject" || logo["url"] != "https://site.test/logo.png" {
		t.Errorf("logo wrong: %v", doc["logo"])
	}
}

func TestOrganizationJSONLD_OmitsEmptySubObjects(t *testing.T) {
	doc := decodeLD(t, webtempl.OrganizationJSONLD(webtempl.OrgIdentity{
		Name: "Acme",
		URL:  "https://site.test",
	}))
	if _, ok := doc["address"]; ok {
		t.Error("address should be omitted when no address fields set")
	}
	if _, ok := doc["contactPoint"]; ok {
		t.Error("contactPoint should be omitted when no email/phone set")
	}
	if _, ok := doc["logo"]; ok {
		t.Error("logo should be omitted when unset")
	}
	if _, ok := doc["description"]; ok {
		t.Error("description should be omitted without GeoStatement")
	}
	if _, ok := doc["sameAs"]; ok {
		t.Error("sameAs should be omitted when empty")
	}
}

func TestWebSiteJSONLD_SearchAction(t *testing.T) {
	doc := decodeLD(t, webtempl.WebSiteJSONLD("CMStack", "https://site.test", "https://site.test/search?q={search_term_string}"))
	if doc["@type"] != "WebSite" {
		t.Errorf("@type = %v", doc["@type"])
	}
	action, ok := doc["potentialAction"].(map[string]any)
	if !ok {
		t.Fatal("potentialAction missing")
	}
	if action["@type"] != "SearchAction" {
		t.Errorf("action @type = %v", action["@type"])
	}
	if action["target"] != "https://site.test/search?q={search_term_string}" {
		t.Errorf("target = %v", action["target"])
	}
	if action["query-input"] != "required name=search_term_string" {
		t.Errorf("query-input = %v", action["query-input"])
	}
}

func TestWebSiteJSONLD_SkipsSearchActionWhenEmpty(t *testing.T) {
	doc := decodeLD(t, webtempl.WebSiteJSONLD("CMStack", "https://site.test", ""))
	if _, ok := doc["potentialAction"]; ok {
		t.Error("potentialAction should be omitted when template empty")
	}
}

func TestBreadcrumbListJSONLD_PositionsAndSkip(t *testing.T) {
	if got := webtempl.BreadcrumbListJSONLD([]webtempl.Breadcrumb{{Name: "Home", URL: "https://site.test/"}}); got != "" {
		t.Errorf("single crumb should skip, got %q", got)
	}
	doc := decodeLD(t, webtempl.BreadcrumbListJSONLD([]webtempl.Breadcrumb{
		{Name: "Home", URL: "https://site.test/"},
		{Name: "Blog", URL: "https://site.test/blog"},
		{Name: "Post", URL: "https://site.test/blog/x"},
	}))
	items, ok := doc["itemListElement"].([]any)
	if !ok || len(items) != 3 {
		t.Fatalf("want 3 list items, got %v", doc["itemListElement"])
	}
	for i, raw := range items {
		it := raw.(map[string]any)
		if int(it["position"].(float64)) != i+1 {
			t.Errorf("item %d position = %v", i, it["position"])
		}
		if it["item"] == "" || it["name"] == "" {
			t.Errorf("item %d missing name/item: %v", i, it)
		}
	}
}

func TestItemListJSONLD_PositionsAndSkipEmpty(t *testing.T) {
	if got := webtempl.ItemListJSONLD("https://site.test/blog", nil); got != "" {
		t.Errorf("empty ItemList should skip, got %q", got)
	}
	doc := decodeLD(t, webtempl.ItemListJSONLD("https://site.test/blog", []webtempl.Breadcrumb{
		{Name: "A", URL: "https://site.test/blog/a"},
		{Name: "B", URL: "https://site.test/blog/b"},
	}))
	if doc["@type"] != "ItemList" {
		t.Errorf("@type = %v", doc["@type"])
	}
	items := doc["itemListElement"].([]any)
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	second := items[1].(map[string]any)
	if int(second["position"].(float64)) != 2 || second["url"] != "https://site.test/blog/b" {
		t.Errorf("second item wrong: %v", second)
	}
}

func TestServiceJSONLD_ProviderAndNoOffer(t *testing.T) {
	doc := decodeLD(t, webtempl.ServiceJSONLD("SEO Audit", "We audit.", "Berlin", "https://site.test/services/seo", fullOrg()))
	if doc["@type"] != "Service" || doc["name"] != "SEO Audit" {
		t.Errorf("service core wrong: %v", doc)
	}
	if doc["areaServed"] != "Berlin" || doc["url"] != "https://site.test/services/seo" {
		t.Errorf("service fields wrong: %v", doc)
	}
	if _, ok := doc["offers"]; ok {
		t.Error("Service must NOT emit an Offer (freeform price)")
	}
	provider, ok := doc["provider"].(map[string]any)
	if !ok || provider["@type"] != "Organization" || provider["name"] != "Acme GmbH" {
		t.Errorf("provider wrong: %v", doc["provider"])
	}
}

func TestFAQPageJSONLD_EmittedOnlyWithFAQs(t *testing.T) {
	if got := webtempl.FAQPageJSONLD(nil); got != "" {
		t.Errorf("no FAQs should skip, got %q", got)
	}
	doc := decodeLD(t, webtempl.FAQPageJSONLD([]webtempl.FAQItem{
		{Question: "How long?", Answer: "About a week."},
	}))
	if doc["@type"] != "FAQPage" {
		t.Errorf("@type = %v", doc["@type"])
	}
	entities := doc["mainEntity"].([]any)
	q := entities[0].(map[string]any)
	if q["@type"] != "Question" || q["name"] != "How long?" {
		t.Errorf("question wrong: %v", q)
	}
	ans := q["acceptedAnswer"].(map[string]any)
	if ans["@type"] != "Answer" || ans["text"] != "About a week." {
		t.Errorf("answer wrong: %v", ans)
	}
}

func TestPostJSONLD_CarriesPublisherDateModifiedInLanguage(t *testing.T) {
	v := webtempl.PublicPostView{
		Title:        "Hello",
		Excerpt:      "An intro",
		AuthorName:   "Grace",
		AuthorURL:    "https://site.test/authors/1",
		PublishedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:    time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
		CanonicalURL: "https://site.test/blog/hello",
		ImageURL:     "https://site.test/og.png",
		InLanguage:   "de",
		Publisher:    fullOrg(),
	}
	doc := decodeLD(t, webtempl.PostJSONLD(v))
	if doc["@type"] != "BlogPosting" {
		t.Errorf("@type = %v", doc["@type"])
	}
	if doc["dateModified"] != "2026-02-02T00:00:00Z" {
		t.Errorf("dateModified = %v", doc["dateModified"])
	}
	if doc["inLanguage"] != "de" {
		t.Errorf("inLanguage = %v", doc["inLanguage"])
	}
	if doc["image"] != "https://site.test/og.png" {
		t.Errorf("image = %v", doc["image"])
	}
	pub, ok := doc["publisher"].(map[string]any)
	if !ok || pub["@type"] != "Organization" || pub["name"] != "Acme GmbH" {
		t.Errorf("publisher wrong: %v", doc["publisher"])
	}
}

func TestJSONLDBuilders_XSSEscaped(t *testing.T) {
	evil := `</script><b>pwn</b> & "q"`
	blocks := []string{
		webtempl.OrganizationJSONLD(webtempl.OrgIdentity{Name: evil, URL: "https://site.test"}),
		webtempl.WebSiteJSONLD(evil, "https://site.test", "https://site.test/search?q={search_term_string}"),
		webtempl.BreadcrumbListJSONLD([]webtempl.Breadcrumb{{Name: evil, URL: "https://site.test/"}, {Name: "x", URL: "https://site.test/x"}}),
		webtempl.ItemListJSONLD("https://site.test", []webtempl.Breadcrumb{{Name: evil, URL: "https://site.test/x"}}),
		webtempl.ServiceJSONLD(evil, evil, evil, "https://site.test/s", webtempl.OrgIdentity{}),
		webtempl.FAQPageJSONLD([]webtempl.FAQItem{{Question: evil, Answer: evil}}),
	}
	for i, b := range blocks {
		if strings.Contains(b, "<") || strings.Contains(b, ">") {
			t.Errorf("block %d leaked a raw angle bracket:\n%s", i, b)
		}
		if strings.Contains(b, "</script>") {
			t.Errorf("block %d leaked </script>:\n%s", i, b)
		}
		// Still recovers to valid JSON carrying the literal payload.
		if b != "" {
			if _, err := json.Marshal(decodeLD(t, b)); err != nil {
				t.Errorf("block %d not recoverable: %v", i, err)
			}
		}
	}
}

func TestHomeStructured_EmitsOrganizationAndWebSite(t *testing.T) {
	org := webtempl.OrganizationJSONLD(fullOrg())
	site := webtempl.WebSiteJSONLD("CMStack", "https://site.test", "https://site.test/search?q={search_term_string}")
	out, err := render.ToString(context.Background(), webtempl.HomeStructured(nil, []string{org, site}))
	if err != nil {
		t.Fatalf("render HomeStructured: %v", err)
	}
	if strings.Count(out, `application/ld+json`) != 2 {
		t.Errorf("home should emit 2 JSON-LD scripts, got %d\n%s", strings.Count(out, `application/ld+json`), out)
	}
	if !strings.Contains(out, `"WebSite"`) || !strings.Contains(out, `"Organization"`) {
		t.Error("home missing Organization or WebSite JSON-LD")
	}
}

func TestPublicServiceDetail_EmitsServiceAndFAQPage(t *testing.T) {
	v := webtempl.PublicServiceView{
		SiteName: "CMStack",
		HomeURL:  "/",
		Title:    "SEO Audit",
		Slug:     "seo-audit",
		JSONLD: []string{
			webtempl.BreadcrumbListJSONLD([]webtempl.Breadcrumb{
				{Name: "CMStack", URL: "https://site.test/"},
				{Name: "Services", URL: "https://site.test/services"},
				{Name: "SEO Audit", URL: "https://site.test/services/seo-audit"},
			}),
			webtempl.ServiceJSONLD("SEO Audit", "We audit.", "Berlin", "https://site.test/services/seo-audit", fullOrg()),
			webtempl.FAQPageJSONLD([]webtempl.FAQItem{{Question: "How long?", Answer: "A week."}}),
		},
	}
	out, err := render.ToString(context.Background(), webtempl.PublicServiceDetail(v))
	if err != nil {
		t.Fatalf("render PublicServiceDetail: %v", err)
	}
	if !strings.Contains(out, `"Service"`) {
		t.Error("service detail missing Service JSON-LD")
	}
	if !strings.Contains(out, `"FAQPage"`) {
		t.Error("service detail missing FAQPage JSON-LD")
	}
	if !strings.Contains(out, `"BreadcrumbList"`) {
		t.Error("service detail missing BreadcrumbList JSON-LD")
	}
}
