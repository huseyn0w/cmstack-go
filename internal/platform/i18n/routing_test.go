package i18n

import "testing"

func TestSplitLocalePrefix(t *testing.T) {
	cases := []struct {
		path     string
		wantLoc  Locale
		wantRest string
	}{
		{"/", LocaleEN, "/"},
		{"/blog", LocaleEN, "/blog"},    // en stays unprefixed
		{"/de", LocaleDE, "/"},          // bare prefix -> root
		{"/de/blog", LocaleDE, "/blog"}, // strips /de
		{"/ru/services", LocaleRU, "/services"},
		{"/de/", LocaleDE, "/"},
		{"/fr/blog", LocaleEN, "/fr/blog"}, // unknown prefix -> default, path intact
		{"/den", LocaleEN, "/den"},         // not exactly /de
		{"", LocaleEN, "/"},
	}
	for _, c := range cases {
		loc, rest := SplitLocalePrefix(c.path)
		if loc != c.wantLoc || rest != c.wantRest {
			t.Errorf("SplitLocalePrefix(%q) = (%q, %q), want (%q, %q)",
				c.path, loc, rest, c.wantLoc, c.wantRest)
		}
	}
}

func TestLocalizePath(t *testing.T) {
	cases := []struct {
		loc  Locale
		rest string
		want string
	}{
		{LocaleEN, "/", "/"}, // default unprefixed
		{LocaleEN, "/blog", "/blog"},
		{LocaleDE, "/", "/de"}, // bare root -> /de (no trailing slash)
		{LocaleDE, "/blog", "/de/blog"},
		{LocaleRU, "/services", "/ru/services"},
	}
	for _, c := range cases {
		if got := LocalizePath(c.loc, c.rest); got != c.want {
			t.Errorf("LocalizePath(%q, %q) = %q, want %q", c.loc, c.rest, got, c.want)
		}
	}
}

func TestLocalizeURLPreservesQuery(t *testing.T) {
	if got := LocalizeURL(LocaleDE, "/blog", "tag=x"); got != "/de/blog?tag=x" {
		t.Errorf("LocalizeURL de = %q", got)
	}
	if got := LocalizeURL(LocaleEN, "/blog", "tag=x"); got != "/blog?tag=x" {
		t.Errorf("LocalizeURL en = %q", got)
	}
	if got := LocalizeURL(LocaleRU, "/", ""); got != "/ru" {
		t.Errorf("LocalizeURL ru root = %q", got)
	}
}

// Round-trip: splitting a localized path must recover the original rest.
func TestRoundTrip(t *testing.T) {
	for _, loc := range All() {
		for _, rest := range []string{"/", "/blog", "/services/x"} {
			full := LocalizePath(loc, rest)
			gotLoc, gotRest := SplitLocalePrefix(full)
			if gotLoc != loc || gotRest != rest {
				t.Errorf("round-trip %q %q: full=%q -> (%q,%q)", loc, rest, full, gotLoc, gotRest)
			}
		}
	}
}

func TestAlternates(t *testing.T) {
	alts := Alternates("/blog", "tag=x")
	if len(alts) != 3 {
		t.Fatalf("Alternates len = %d, want 3", len(alts))
	}
	// Default first, marked x-default.
	if alts[0].Locale != LocaleEN || !alts[0].IsDefault || alts[0].Hreflang != "x-default" {
		t.Errorf("first alternate = %+v, want en/x-default/default", alts[0])
	}
	if alts[0].URL != "/blog?tag=x" {
		t.Errorf("en URL = %q, want /blog?tag=x", alts[0].URL)
	}
	if alts[1].Locale != LocaleDE || alts[1].URL != "/de/blog?tag=x" || alts[1].Hreflang != "de" {
		t.Errorf("de alternate = %+v", alts[1])
	}
	if alts[2].Locale != LocaleRU || alts[2].URL != "/ru/blog?tag=x" {
		t.Errorf("ru alternate = %+v", alts[2])
	}
}
