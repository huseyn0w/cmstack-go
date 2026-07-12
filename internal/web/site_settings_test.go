package web

import (
	"context"
	"errors"
	"testing"

	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

// fakeOverrides is an in-memory SiteOverrideReader for the overlay tests. When
// err is set, every Get returns it (exercising the read-error fallback).
type fakeOverrides struct {
	values map[string]string
	err    error
}

func (f fakeOverrides) Get(_ context.Context, key string) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	v, ok := f.values[key]
	return v, ok, nil
}

func TestOverlay_StringResolution(t *testing.T) {
	ctx := context.Background()
	base := baseSite()

	tests := []struct {
		name     string
		reader   SiteOverrideReader
		resolver func(SiteConfig) string
		want     string
	}{
		{"nil overrides falls back to config", base.overrides, func(s SiteConfig) string { return s.resolveSiteName(ctx) }, "Agentic CMS"},
		{"override wins when set", fakeOverrides{values: map[string]string{keySiteName: "Overridden"}}, func(s SiteConfig) string { return s.resolveSiteName(ctx) }, "Overridden"},
		{"empty override falls back", fakeOverrides{values: map[string]string{keySiteName: "   "}}, func(s SiteConfig) string { return s.resolveSiteName(ctx) }, "Agentic CMS"},
		{"unset key falls back", fakeOverrides{values: map[string]string{}}, func(s SiteConfig) string { return s.resolveSiteName(ctx) }, "Agentic CMS"},
		{"read error falls back", fakeOverrides{err: errors.New("boom")}, func(s SiteConfig) string { return s.resolveSiteName(ctx) }, "Agentic CMS"},
		{"description override", fakeOverrides{values: map[string]string{keySiteDescription: "New desc"}}, func(s SiteConfig) string { return s.resolveSiteDescription(ctx) }, "New desc"},
		{"twitter override", fakeOverrides{values: map[string]string{keySiteTwitterHandle: "@new"}}, func(s SiteConfig) string { return s.resolveTwitterHandle(ctx) }, "@new"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			if tc.reader != nil {
				s = base.WithOverrides(tc.reader)
			}
			if got := tc.resolver(s); got != tc.want {
				t.Errorf("resolver = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOverlay_BoolResolution(t *testing.T) {
	ctx := context.Background()
	base := baseSite() // GlobalNoindex=false, AllowAICrawlers=false (zero value)

	tests := []struct {
		val  string
		def  bool
		want bool
	}{
		{"1", false, true},
		{"true", false, true},
		{"on", false, true},
		{"yes", false, true},
		{"0", true, false},
		{"false", true, false},
		{"off", true, false},
		{"no", true, false},
		{"garbage", true, true},   // unparseable -> def
		{"garbage", false, false}, // unparseable -> def
		{"", true, true},          // empty -> def
	}
	for _, tc := range tests {
		s := base.WithOverrides(fakeOverrides{values: map[string]string{keySEOGlobalNoindex: tc.val}})
		if got := s.boolOverride(ctx, keySEOGlobalNoindex, tc.def); got != tc.want {
			t.Errorf("boolOverride(%q, def=%v) = %v, want %v", tc.val, tc.def, got, tc.want)
		}
	}
}

func TestOverlay_BuildSEOUsesOverrides(t *testing.T) {
	s := baseSite().WithOverrides(fakeOverrides{values: map[string]string{
		keySiteName:              "Live Name",
		keySiteDescription:       "Live Desc",
		keySEOGlobalNoindex:      "1",
		keySEOGoogleVerification: "gtok",
		keySEOBingVerification:   "btok",
		keySiteTwitterHandle:     "@live",
	}})
	got := s.BuildSEO(seoReq(), SEOInput{Title: "Hello"})

	if got.DocTitle != "Hello · Live Name" {
		t.Errorf("DocTitle = %q, want %q", got.DocTitle, "Hello · Live Name")
	}
	if got.OGSiteName != "Live Name" {
		t.Errorf("OGSiteName = %q, want Live Name", got.OGSiteName)
	}
	if got.Robots != "noindex, follow" {
		t.Errorf("Robots = %q, want noindex, follow", got.Robots)
	}
	if got.TwitterSite != "@live" {
		t.Errorf("TwitterSite = %q, want @live", got.TwitterSite)
	}
	// Verification overlay: google overridden, bing added, others absent.
	names := map[string]string{}
	for _, m := range got.Verifications {
		names[m.Name] = m.Content
	}
	if names["google-site-verification"] != "gtok" {
		t.Errorf("google verification = %q, want gtok", names["google-site-verification"])
	}
	if names["msvalidate.01"] != "btok" {
		t.Errorf("bing verification = %q, want btok", names["msvalidate.01"])
	}
}

func TestOverlay_BuildSEONilOverridesIsConfigOnly(t *testing.T) {
	// Regression guard: with no overlay, BuildSEO must equal the pure-config path.
	base := baseSite()
	withNil := baseSite().WithOverrides(nil)

	a := base.BuildSEO(seoReq(), SEOInput{Title: "X"})
	b := withNil.BuildSEO(seoReq(), SEOInput{Title: "X"})

	if a.DocTitle != b.DocTitle || a.Description != b.Description ||
		a.Robots != b.Robots || a.OGSiteName != b.OGSiteName ||
		a.TwitterSite != b.TwitterSite || a.OGImage != b.OGImage {
		t.Errorf("nil overlay diverged from config-only path:\n a=%+v\n b=%+v", a, b)
	}
	if len(a.Verifications) != 1 || a.Verifications[0].Content != "tok" {
		t.Errorf("verifications = %+v, want boot tok", a.Verifications)
	}
}

func TestOverlay_ResolveOrg(t *testing.T) {
	ctx := context.Background()
	base := baseSite()
	base.BaseURL = "https://site.test"
	base.Org = webtempl.OrgIdentity{
		Name:      "Boot Org",
		LegalName: "Boot Legal",
		Email:     "boot@site.test",
		SameAs:    []string{"https://boot.example"},
	}

	t.Run("nil overrides returns boot org", func(t *testing.T) {
		org := base.resolveOrg(ctx)
		if org.Name != "Boot Org" || org.LegalName != "Boot Legal" || org.Email != "boot@site.test" {
			t.Errorf("org = %+v, want boot values", org)
		}
		if len(org.SameAs) != 1 || org.SameAs[0] != "https://boot.example" {
			t.Errorf("sameAs = %v, want boot", org.SameAs)
		}
	})

	t.Run("overrides overlay", func(t *testing.T) {
		s := base.WithOverrides(fakeOverrides{values: map[string]string{
			keyOrgName:         "New Org",
			keyOrgEmail:        "new@site.test",
			keyOrgLogo:         "/static/logo.png",
			keyOrgSameAs:       "https://a.example\n  \nhttps://b.example\n",
			keyOrgGeoStatement: "Serving Berlin.",
		}})
		org := s.resolveOrg(ctx)
		if org.Name != "New Org" {
			t.Errorf("Name = %q, want New Org", org.Name)
		}
		if org.LegalName != "Boot Legal" {
			t.Errorf("LegalName = %q, want fallback Boot Legal", org.LegalName)
		}
		if org.Email != "new@site.test" {
			t.Errorf("Email = %q, want new@site.test", org.Email)
		}
		if org.LogoURL != "https://site.test/static/logo.png" {
			t.Errorf("LogoURL = %q, want absolutized", org.LogoURL)
		}
		if len(org.SameAs) != 2 || org.SameAs[0] != "https://a.example" || org.SameAs[1] != "https://b.example" {
			t.Errorf("SameAs = %v, want split+trimmed", org.SameAs)
		}
		if org.GeoStatement != "Serving Berlin." {
			t.Errorf("GeoStatement = %q", org.GeoStatement)
		}
	})
}

func TestSplitSameAs(t *testing.T) {
	got := splitSameAs("  https://a \n\n https://b \n")
	if len(got) != 2 || got[0] != "https://a" || got[1] != "https://b" {
		t.Errorf("splitSameAs = %v", got)
	}
	if splitSameAs("  \n \n ") != nil {
		t.Error("all-blank sameAs should be nil")
	}
}
