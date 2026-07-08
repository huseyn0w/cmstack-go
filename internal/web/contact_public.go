package web

import (
	"context"
	"errors"
	"net/http"

	"github.com/huseyn0w/cmstack-go/internal/contact"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// ContactPublicService is the subset of *contact.Service the public handler
// calls. Declaring it here keeps the handler testable with a stub.
type ContactPublicService interface {
	Submit(ctx context.Context, in contact.Input) error
}

// ContactPublicHandler is the thin HTTP boundary for the public /contact page:
// it renders the form (GET), decodes + submits (POST), and re-renders with the
// mapped banners. It holds NO business logic and NEVER exposes the recipient.
type ContactPublicHandler struct {
	svc          ContactPublicService
	siteName     string
	baseURL      string
	site         SiteConfig
	csrf         func(*http.Request) string
	recaptchaKey string
}

// NewContactPublicHandler constructs the public contact handler.
func NewContactPublicHandler(svc ContactPublicService, siteName, baseURL string, csrf func(*http.Request) string, recaptchaKey string) *ContactPublicHandler {
	if siteName == "" {
		siteName = "CMStack"
	}
	return &ContactPublicHandler{svc: svc, siteName: siteName, baseURL: baseURL, csrf: csrf, recaptchaKey: recaptchaKey}
}

// WithSite attaches the resolved site-identity + SEO config. Returns the
// receiver.
func (h *ContactPublicHandler) WithSite(s SiteConfig) *ContactPublicHandler {
	h.site = s
	return h
}

// Show renders the contact form for GET /contact. `?sent=1` (the post-redirect
// success form) shows the success banner.
func (h *ContactPublicHandler) Show(w http.ResponseWriter, r *http.Request) {
	view := h.view(r)
	if r.URL.Query().Get("sent") == "1" {
		view.Submitted = true
	}
	h.render(w, r, http.StatusOK, view)
}

// Submit handles POST /contact. On success it re-renders with a success banner;
// validation/recaptcha/rate-limit failures re-render the form with the mapped
// status, field errors, and the entered values.
func (h *ContactPublicHandler) Submit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	in := contact.Input{
		Name:           r.PostFormValue("name"),
		Email:          r.PostFormValue("email"),
		Subject:        r.PostFormValue("subject"),
		Message:        r.PostFormValue("message"),
		RecaptchaToken: r.PostFormValue("recaptcha_token"),
		RemoteIP:       clientIP(r),
	}

	err := h.svc.Submit(r.Context(), in)
	if err != nil {
		h.renderSubmitError(w, r, in, err)
		return
	}

	view := h.view(r)
	view.Submitted = true
	h.render(w, r, http.StatusOK, view)
}

// renderSubmitError re-renders the form with the entered values and the mapped
// error/field messages for a failed submission.
func (h *ContactPublicHandler) renderSubmitError(w http.ResponseWriter, r *http.Request, in contact.Input, err error) {
	view := h.view(r)
	view.PrefillName = in.Name
	view.PrefillEmail = in.Email
	view.PrefillSubject = in.Subject
	view.PrefillMessage = in.Message

	status := http.StatusUnprocessableEntity
	switch {
	case errors.Is(err, contact.ErrRateLimited):
		status = http.StatusTooManyRequests
		view.Error = "You are sending messages too quickly. Please wait a moment and try again."
	case errors.Is(err, contact.ErrRecaptcha):
		view.Error = "Your message could not be verified. Please try again."
	case errors.Is(err, contact.ErrInvalid):
		var ve contact.ValidationError
		fields := map[string]string{}
		if errors.As(err, &ve) {
			fields[ve.Field] = ve.Message
		} else {
			fields["message"] = "Your message could not be sent."
		}
		view.FieldErrors = fields
		view.Error = "Please check the form and try again."
	default:
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}

	h.render(w, r, status, view)
}

// view assembles the base ContactFormView (SEO, CSRF, recaptcha hook) for the
// current request.
func (h *ContactPublicHandler) view(r *http.Request) webtempl.ContactFormView {
	seo := h.site.BuildSEO(r, SEOInput{
		Title:         "Contact",
		CanonicalPath: "/contact",
		OGType:        "website",
	})
	return webtempl.ContactFormView{
		SiteName:     h.siteName,
		SEO:          seo,
		SubmitURL:    "/contact",
		CSRFToken:    h.csrfToken(r),
		RecaptchaKey: h.recaptchaKey,
		FieldErrors:  map[string]string{},
	}
}

func (h *ContactPublicHandler) csrfToken(r *http.Request) string {
	if h.csrf == nil {
		return ""
	}
	return h.csrf(r)
}

func (h *ContactPublicHandler) render(w http.ResponseWriter, r *http.Request, status int, view webtempl.ContactFormView) {
	if err := render.Component(r.Context(), w, status, webtempl.ContactPage(view)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
