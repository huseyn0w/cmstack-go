package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

// messagesFS embeds the per-locale UI-string catalogs. Each file is a flat
// map of dotted keys (e.g. "nav.blog") to the translated string. Flat keys keep
// lookup a single map access and keep the catalog trivially diffable.
//
//go:embed messages/*.json
var messagesFS embed.FS

// Catalog holds the loaded message tables for every supported locale plus the
// default table used for fallback. It is immutable after Load and therefore
// safe for concurrent use.
type Catalog struct {
	tables map[Locale]map[string]string
}

// LoadCatalog parses the embedded per-locale JSON tables into a Catalog. It
// fails fast if the default locale's table is missing or malformed, since every
// lookup depends on it for fallback.
func LoadCatalog() (*Catalog, error) {
	c := &Catalog{tables: make(map[Locale]map[string]string, len(supported))}
	for _, loc := range supported {
		raw, err := messagesFS.ReadFile("messages/" + loc.String() + ".json")
		if err != nil {
			if loc == Default() {
				return nil, fmt.Errorf("i18n: default catalog %q missing: %w", loc, err)
			}
			// A non-default catalog may be absent; it falls back to the default.
			c.tables[loc] = map[string]string{}
			continue
		}
		var table map[string]string
		if err := json.Unmarshal(raw, &table); err != nil {
			return nil, fmt.Errorf("i18n: parse catalog %q: %w", loc, err)
		}
		c.tables[loc] = table
	}
	if _, ok := c.tables[Default()]; !ok {
		return nil, fmt.Errorf("i18n: default catalog %q not loaded", Default())
	}
	return c, nil
}

// MustLoadCatalog is LoadCatalog that panics on error. It is intended for
// process startup / package init where a broken embedded catalog is a
// programming error, not a runtime condition.
func MustLoadCatalog() *Catalog {
	c, err := LoadCatalog()
	if err != nil {
		panic(err)
	}
	return c
}

// lookup resolves key for locale, applying the fallback chain: requested locale
// -> default locale -> the key itself. The second return reports whether a real
// translation (in any locale) was found.
func (c *Catalog) lookup(loc Locale, key string) (string, bool) {
	if table, ok := c.tables[loc]; ok {
		if v, ok := table[key]; ok {
			return v, true
		}
	}
	if loc != Default() {
		if v, ok := c.tables[Default()][key]; ok {
			return v, true
		}
	}
	return key, false
}

// Translate resolves key for loc and interpolates {name}-style placeholders
// from args (a flat sequence of name, value pairs). Missing keys fall back to
// the default locale and finally to the key string itself, so a template never
// renders empty. Placeholder values are stringified with fmt's default verb.
func (c *Catalog) Translate(loc Locale, key string, args ...any) string {
	msg, _ := c.lookup(loc, key)
	if len(args) == 0 || !strings.Contains(msg, "{") {
		return msg
	}
	return interpolate(msg, args)
}

// Translator binds a Catalog to a single active locale so call sites (handlers,
// templ components) can translate without repeating the locale. It is a small
// value type, cheap to pass by value and safe for concurrent use.
type Translator struct {
	catalog *Catalog
	locale  Locale
}

// NewTranslator binds cat to loc. A nil catalog yields a translator that echoes
// keys back (with interpolation), so a zero-Deps render never panics.
func NewTranslator(cat *Catalog, loc Locale) Translator {
	if !IsSupported(loc) {
		loc = Default()
	}
	return Translator{catalog: cat, locale: loc}
}

// Locale returns the translator's active locale.
func (t Translator) Locale() Locale { return t.locale }

// T resolves key for the bound locale with {name}-style interpolation. It is
// the primary entry point used by templates.
func (t Translator) T(key string, args ...any) string {
	if t.catalog == nil {
		if len(args) == 0 || !strings.Contains(key, "{") {
			return key
		}
		return interpolate(key, args)
	}
	return t.catalog.Translate(t.locale, key, args...)
}

// interpolate replaces {name} placeholders in msg with values from args, a flat
// sequence of alternating name/value pairs. Unmatched placeholders are left
// intact; an odd trailing arg is ignored.
func interpolate(msg string, args []any) string {
	if len(args) < 2 {
		return msg
	}
	pairs := make([]string, 0, len(args))
	for i := 0; i+1 < len(args); i += 2 {
		name, ok := args[i].(string)
		if !ok {
			continue
		}
		pairs = append(pairs, "{"+name+"}", fmt.Sprintf("%v", args[i+1]))
	}
	return strings.NewReplacer(pairs...).Replace(msg)
}
