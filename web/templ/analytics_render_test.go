package templ

import (
	"context"
	"strings"
	"testing"

	"github.com/huseyn0w/cmstack-go/internal/platform/render"
)

// fakeAnalyticsSource is a stub analyticsSource returning fixed snippets.
type fakeAnalyticsSource struct {
	snips AnalyticsSnippets
}

func (f fakeAnalyticsSource) Snippets(context.Context) AnalyticsSnippets { return f.snips }

func TestPublicLayout_RendersAnalyticsSnippets(t *testing.T) {
	SetAnalyticsSource(fakeAnalyticsSource{snips: AnalyticsSnippets{
		GA4ID: "G-ABC1234",
		GTMID: "GTM-ABCD12",
	}})
	defer SetAnalyticsSource(nil)

	body, err := render.ToString(context.Background(), Base(LayoutData{Title: "T"}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, want := range []string{
		"https://www.googletagmanager.com/gtag/js?id=G-ABC1234",  // GA4 loader
		"gtag('config','G-ABC1234')",                             // GA4 init
		"'script','dataLayer','GTM-ABCD12'",                      // GTM head bootstrap
		"https://www.googletagmanager.com/ns.html?id=GTM-ABCD12", // GTM noscript iframe
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered layout missing %q", want)
		}
	}
}

func TestPublicLayout_NoAnalyticsWhenDisabled(t *testing.T) {
	// Zero-value source (both ids empty) => nothing emitted.
	SetAnalyticsSource(fakeAnalyticsSource{})
	defer SetAnalyticsSource(nil)

	body, err := render.ToString(context.Background(), Base(LayoutData{Title: "T"}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, absent := range []string{
		"googletagmanager.com/gtag/js",
		"ns.html?id=",
	} {
		if strings.Contains(body, absent) {
			t.Errorf("rendered layout should not contain %q when analytics disabled", absent)
		}
	}
}

func TestPublicLayout_NoAnalyticsSourceOmitsSnippets(t *testing.T) {
	SetAnalyticsSource(nil)

	body, err := render.ToString(context.Background(), Base(LayoutData{Title: "T"}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(body, "googletagmanager.com/gtag/js") || strings.Contains(body, "ns.html?id=") {
		t.Error("analytics should be absent with no source")
	}
}
