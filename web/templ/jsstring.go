package templ

import "encoding/json"

// jsString returns s as a safe single-quoted-or-JSON JavaScript string literal
// for embedding inside an Alpine x-data expression attribute. It JSON-encodes
// (escaping quotes, backslashes, and control chars) so HTML body markup cannot
// break out of the attribute or the JS string, then the result is additionally
// HTML-attribute-escaped by templ when the attribute is rendered. This keeps the
// editor's initial content injection XSS-safe even though the body itself is
// already bluemonday-sanitized server-side (defense in depth).
func jsString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// jsStringArray returns ss as a safe JavaScript array literal of string literals
// for embedding inside an Alpine x-data expression attribute. Each element is
// JSON-encoded; templ additionally HTML-attribute-escapes the whole value.
func jsStringArray(ss []string) string {
	b, err := json.Marshal(ss)
	if err != nil {
		return "[]"
	}
	return string(b)
}
