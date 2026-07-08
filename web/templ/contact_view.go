package templ

// ContactFormView is the view-model for the public contact page (M12). It holds
// the CSRF token, the reCAPTCHA v3 site-key hook, the success/error banners, the
// per-field validation messages, and the prefilled values re-populated after a
// validation error. It is rendered inside the public @Base layout with a
// resolved SEOView.
type ContactFormView struct {
	SiteName  string
	SEO       *SEOView
	SubmitURL string // POST target (locale-aware /contact)
	CSRFToken string

	// RecaptchaKey drives the client-side v3 token hook (same as comments). Empty
	// when no site key is configured (the token field is then omitted).
	RecaptchaKey string

	// Submitted/Error banners (set after a POST round-trip).
	Submitted   bool   // the message was accepted (async email enqueued)
	Error       string // top-level error (rate-limit / recaptcha)
	FieldErrors map[string]string

	// Prefill* re-populate the form after a validation error.
	PrefillName    string
	PrefillEmail   string
	PrefillSubject string
	PrefillMessage string
}

// HasFieldErrors reports whether any field-level error is present.
func (v ContactFormView) HasFieldErrors() bool { return len(v.FieldErrors) > 0 }

// FieldErr returns the error for a field (or "").
func (v ContactFormView) FieldErr(name string) string {
	if v.FieldErrors == nil {
		return ""
	}
	return v.FieldErrors[name]
}
