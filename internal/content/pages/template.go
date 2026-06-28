package pages

// Template selector allow-list. A page's template names which public layout
// renders it; it is validated SERVER-SIDE against this closed set so a tampered
// form value can never select an arbitrary template. The default is applied when
// the submitted value is empty or unrecognized.
const (
	// TemplateDefault is the standard single-column page layout.
	TemplateDefault = "default"
	// TemplateFullWidth is an edge-to-edge layout with no prose max-width.
	TemplateFullWidth = "full-width"
	// TemplateLanding is a marketing/landing layout.
	TemplateLanding = "landing"
)

// PageTemplate is one selectable template with a human label for the editor.
type PageTemplate struct {
	Value string
	Label string
}

// Templates is the ordered allow-list surfaced in the editor's template picker.
func Templates() []PageTemplate {
	return []PageTemplate{
		{Value: TemplateDefault, Label: "Default"},
		{Value: TemplateFullWidth, Label: "Full width"},
		{Value: TemplateLanding, Label: "Landing"},
	}
}

// validTemplates is the set form of the allow-list for O(1) validation.
var validTemplates = map[string]bool{
	TemplateDefault:   true,
	TemplateFullWidth: true,
	TemplateLanding:   true,
}

// normalizeTemplate returns t when it is in the allow-list, else TemplateDefault.
// This is the single gate: a malformed/tampered template selector silently
// degrades to the safe default rather than erroring.
func normalizeTemplate(t string) string {
	if validTemplates[t] {
		return t
	}
	return TemplateDefault
}
