// Package i18n is the internationalization foundation (M7a): the supported
// locale set, UI-string message catalogs with fallback + interpolation, the
// URL-prefix routing helpers ("as-needed": default en unprefixed, de/ru
// prefixed), and the per-locale alternate-link data used by the language
// switcher (and, later, hreflang emission in M8).
//
// This is the ADDITIVE i18n foundation only. It does NOT touch the content
// storage model; per-locale CONTENT translation is a separate later milestone
// (M7b).
package i18n

import "strings"

// Locale is a supported UI language, identified by its BCP-47 base tag.
type Locale string

// The supported locale set. en is the default and is served UNPREFIXED; de and
// ru are served under the /de and /ru URL prefixes ("as-needed" routing).
const (
	// LocaleEN is English — the default locale (unprefixed URLs).
	LocaleEN Locale = "en"
	// LocaleDE is German (prefixed /de).
	LocaleDE Locale = "de"
	// LocaleRU is Russian (prefixed /ru).
	LocaleRU Locale = "ru"
)

// supported lists every locale in display order (default first). It backs All,
// IsSupported and Parse so the set is defined in exactly one place.
var supported = []Locale{LocaleEN, LocaleDE, LocaleRU}

// Default returns the default locale (en), which is served on unprefixed URLs
// and is the fallback for missing translations.
func Default() Locale { return LocaleEN }

// All returns the supported locales in display order (default first). The
// returned slice is a copy; callers may mutate it freely.
func All() []Locale {
	out := make([]Locale, len(supported))
	copy(out, supported)
	return out
}

// IsSupported reports whether l is one of the supported locales.
func IsSupported(l Locale) bool {
	for _, s := range supported {
		if s == l {
			return true
		}
	}
	return false
}

// Parse resolves a raw tag (e.g. from a URL prefix or Accept-Language) to a
// supported Locale. Matching is case-insensitive and ignores any region/script
// suffix ("de-DE" -> de). It returns (Default(), false) when nothing matches so
// callers can both fall back and detect the miss.
func Parse(raw string) (Locale, bool) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return Default(), false
	}
	// Ignore region/script suffix: en-US, de_DE, ru-Cyrl all reduce to the base.
	if i := strings.IndexAny(raw, "-_"); i >= 0 {
		raw = raw[:i]
	}
	l := Locale(raw)
	if IsSupported(l) {
		return l, true
	}
	return Default(), false
}

// String returns the locale tag as a plain string (e.g. "en"), suitable for an
// <html lang> attribute or a URL prefix.
func (l Locale) String() string { return string(l) }

// IsDefault reports whether l is the default locale (served unprefixed).
func (l Locale) IsDefault() bool { return l == Default() }
