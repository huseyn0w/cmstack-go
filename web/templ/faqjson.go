package templ

import "encoding/json"

// faqsJSON serializes the server-rendered FAQ rows into a JSON array literal for
// the Alpine faqEditor() x-data initializer. Each row carries a stable client
// `key` (its initial index) so x-for can track rows across reorders/removals.
// json.Marshal escapes quotes/backslashes/control chars so a question or answer
// can never break out of the attribute (defense in depth; answers are already
// sanitized server-side). templ then HTML-attribute-escapes the whole string.
func faqsJSON(faqs []ServiceFAQField) string {
	type item struct {
		Key      int    `json:"key"`
		Question string `json:"question"`
		Answer   string `json:"answer"`
	}
	items := make([]item, 0, len(faqs))
	for i, f := range faqs {
		items = append(items, item{Key: i, Question: f.Question, Answer: f.Answer})
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(b)
}
