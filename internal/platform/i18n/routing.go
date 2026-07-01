package i18n

import "strings"

// SplitLocalePrefix resolves the active locale from a URL path using "as-needed"
// routing: a leading /de or /ru segment selects that locale and is stripped from
// the returned path; anything else is the default locale (en) on the unchanged,
// unprefixed path.
//
// The returned rest is always a rooted path ("/..."); stripping /de from "/de"
// yields "/", and stripping from "/de/blog" yields "/blog". An unknown prefix
// (e.g. /fr) is NOT treated as a locale — it is left in the path and resolved as
// the default locale, so it flows to the normal router and 404s there like any
// other unknown path (see the middleware's decision note).
func SplitLocalePrefix(path string) (Locale, string) {
	if path == "" {
		return Default(), "/"
	}
	// Isolate the first path segment.
	trimmed := strings.TrimPrefix(path, "/")
	seg := trimmed
	if i := strings.IndexByte(trimmed, '/'); i >= 0 {
		seg = trimmed[:i]
	}
	loc, ok := Parse(seg)
	// Only strip when the segment is EXACTLY a supported, non-default locale.
	// Parse tolerates region suffixes / default matches, so guard on both the
	// exact string and non-default status here.
	if !ok || loc.IsDefault() || seg != loc.String() {
		return Default(), ensureRooted(path)
	}
	rest := strings.TrimPrefix(trimmed, seg)
	return loc, ensureRooted(rest)
}

// LocalizePath builds the URL for the unprefixed path rest as it should appear
// in loc: the default locale returns rest unchanged (unprefixed), other locales
// prepend the /{locale} segment. rest is expected to be the already-stripped,
// rooted path (the value returned by SplitLocalePrefix). A query string, if any,
// is preserved by the caller via LocalizeURL.
func LocalizePath(loc Locale, rest string) string {
	rest = ensureRooted(rest)
	if loc.IsDefault() {
		return rest
	}
	if rest == "/" {
		return "/" + loc.String()
	}
	return "/" + loc.String() + rest
}

// LocalizeURL is LocalizePath with the raw query string reattached (e.g.
// "tag=x"). An empty rawQuery yields just the path.
func LocalizeURL(loc Locale, rest, rawQuery string) string {
	p := LocalizePath(loc, rest)
	if rawQuery == "" {
		return p
	}
	return p + "?" + rawQuery
}

// Alternate is one entry in the per-locale alternate-link set: the locale and
// the fully-formed localized URL (path + query) for the current page in that
// locale. It is the data the language switcher renders now and that M8 will
// emit as <link rel="alternate" hreflang> tags.
type Alternate struct {
	Locale Locale
	// Hreflang is the value for a future hreflang attribute; it equals
	// Locale.String() for real locales and "x-default" for the default entry.
	Hreflang string
	URL      string
	// IsDefault marks the default-locale entry (the x-default target for M8).
	IsDefault bool
}

// Alternates returns the per-locale alternate URLs for a page, given the
// already-stripped rooted path (rest) and its raw query. The slice is in
// display order (default first) and drives both the header language switcher and
// the M8 hreflang <head> emission.
//
// TODO(M8): emit these as <link rel="alternate" hreflang="..."> + an
// x-default entry in the document <head>. The data structure is built now so the
// switcher and the future head share one source of truth.
func Alternates(rest, rawQuery string) []Alternate {
	locs := All()
	out := make([]Alternate, 0, len(locs))
	for _, loc := range locs {
		hreflang := loc.String()
		if loc.IsDefault() {
			hreflang = "x-default"
		}
		out = append(out, Alternate{
			Locale:    loc,
			Hreflang:  hreflang,
			URL:       LocalizeURL(loc, rest, rawQuery),
			IsDefault: loc.IsDefault(),
		})
	}
	return out
}

// ensureRooted guarantees a leading slash and collapses the empty path to "/".
func ensureRooted(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		return "/" + p
	}
	return p
}
