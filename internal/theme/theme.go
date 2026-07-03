// Package theme is the in-code catalogue of public site themes (M9-1). A theme
// is a named palette: the "default" base tokens, or an alternate that re-scopes
// the color tokens under a `.theme-<id>` CSS block (see web/static/tokens.css).
// The registry is pure Go with no dependencies; the settings store persists
// which theme is active and the web layer resolves + applies it per request.
package theme

// Theme is a registered palette a public site can run under. ID is the stable
// identifier persisted in settings and used to build the `.theme-<id>` CSS
// class; Label + Description are human-facing (the later admin switcher slice).
type Theme struct {
	// ID is the stable identifier ("default", "sepia", "noir"). The default
	// theme is the base `:root` palette and needs no CSS class.
	ID string
	// Label is the human-facing name shown in the (later) admin switcher.
	Label string
	// Description is a one-line summary of the palette's character.
	Description string
}

// defaultID is the id of the base palette. It is the fallback and always sorts
// first in All().
const defaultID = "default"

// catalogue is the registered set of themes in display order (default first).
// Alternate themes each have a matching `.theme-<id>` (+ `.theme-<id>.dark`)
// block in web/static/tokens.css overriding the color tokens.
var catalogue = []Theme{
	{
		ID:          defaultID,
		Label:       "Default",
		Description: "The quiet-luxury base palette — warm off-white paper with terracotta accents.",
	},
	{
		ID:          "sepia",
		Label:       "Sepia",
		Description: "Warm paper-and-ink editorial: aged cream surfaces with deep espresso text.",
	},
	{
		ID:          "noir",
		Label:       "Noir",
		Description: "Dark-first high-contrast ink: near-black surfaces with a cool bone foreground.",
	},
}

// byID indexes the catalogue for O(1) lookups.
var byID = func() map[string]Theme {
	m := make(map[string]Theme, len(catalogue))
	for _, t := range catalogue {
		m[t.ID] = t
	}
	return m
}()

// All returns every registered theme in display order (default first). The
// returned slice is a copy, so callers may not mutate the registry.
func All() []Theme {
	out := make([]Theme, len(catalogue))
	copy(out, catalogue)
	return out
}

// Get returns the theme with the given id and whether it is registered.
func Get(id string) (Theme, bool) {
	t, ok := byID[id]
	return t, ok
}

// Default returns the base theme (id "default").
func Default() Theme {
	return byID[defaultID]
}

// Resolve returns the theme for id, falling back to Default() when id is empty
// or unknown. This is the single validation seam the web layer uses so a
// stale/hostile stored id can never apply an unregistered palette.
func Resolve(id string) Theme {
	if t, ok := byID[id]; ok {
		return t
	}
	return Default()
}

// IsValid reports whether id names a registered theme.
func IsValid(id string) bool {
	_, ok := byID[id]
	return ok
}
