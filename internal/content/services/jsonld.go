package services

// JSONLDData is the typed seam M8 will serialize into Service + FAQPage JSON-LD.
// It is intentionally a plain data struct with no schema.org marshaling here: the
// SEO/GEO milestone (M8) owns the schema vocabulary and the script-safe encoding
// (mirroring posts' PostJSONLD). Collecting the data behind one typed method now
// keeps the serialization a pure, well-defined add-on later.
//
// TODO(M8): serialize this into <script type="application/ld+json"> with a
// schema.org "Service" object and a sibling "FAQPage" built from FAQs. A freeform
// Price is deliberately NOT emitted as an Offer (invalid without numeric
// price/currency) — it stays a visible on-page fact (mirrors django).
type JSONLDData struct {
	Title        string
	Summary      string
	AreaServed   string
	Price        string
	CanonicalURL string
	FAQs         []JSONLDFAQ
}

// JSONLDFAQ is one Q&A entry for the FAQPage seam.
type JSONLDFAQ struct {
	Question string
	Answer   string
}

// JSONLD returns the typed data M8 will serialize. canonicalURL is supplied by
// the caller (the web layer owns the base URL). This is the single seam method;
// no JSON is produced here.
func (s Service) JSONLD(canonicalURL string) JSONLDData {
	faqs := make([]JSONLDFAQ, 0, len(s.FAQs))
	for _, f := range s.FAQs {
		faqs = append(faqs, JSONLDFAQ{Question: f.Question, Answer: f.Answer})
	}
	return JSONLDData{
		Title:        s.Title,
		Summary:      s.Summary,
		AreaServed:   s.AreaServed,
		Price:        s.Price,
		CanonicalURL: canonicalURL,
		FAQs:         faqs,
	}
}
