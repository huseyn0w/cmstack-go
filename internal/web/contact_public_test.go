package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/contact"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/security"
)

// stubContactPublic is a controllable ContactPublicService.
type stubContactPublic struct {
	submitErr error
	submitted *contact.Input
}

func (s *stubContactPublic) Submit(_ context.Context, in contact.Input) error {
	cp := in
	s.submitted = &cp
	return s.submitErr
}

func contactPublicHandler(svc ContactPublicService) *ContactPublicHandler {
	return NewContactPublicHandler(svc, "Agentic CMS", "https://cms.test", security.Token, "site-key")
}

func contactPostReq(form url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/contact", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.7:5555"
	return req
}

func TestContactPublic_ShowRendersForm(t *testing.T) {
	h := contactPublicHandler(&stubContactPublic{})
	req := httptest.NewRequest(http.MethodGet, "/contact", nil)
	rec := httptest.NewRecorder()
	h.Show(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("show = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-testid="contact-form"`,
		`data-testid="contact-input-name"`,
		`data-testid="contact-input-email"`,
		`data-testid="contact-input-subject"`,
		`data-testid="contact-input-message"`,
		`data-recaptcha-key="site-key"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("form missing %q\n%s", want, body)
		}
	}
}

func TestContactPublic_SubmitSuccess(t *testing.T) {
	svc := &stubContactPublic{}
	h := contactPublicHandler(svc)
	form := url.Values{
		"name":    {"Ada"},
		"email":   {"ada@example.com"},
		"subject": {"Hi"},
		"message": {"hello there"},
	}
	rec := httptest.NewRecorder()
	h.Submit(rec, contactPostReq(form))
	if rec.Code != http.StatusOK {
		t.Fatalf("submit = %d, want 200\n%s", rec.Code, rec.Body.String())
	}
	if svc.submitted == nil || svc.submitted.Message != "hello there" {
		t.Fatalf("service Submit not called with message: %+v", svc.submitted)
	}
	if svc.submitted.RemoteIP != "203.0.113.7" {
		t.Errorf("RemoteIP = %q, want honest RemoteAddr host", svc.submitted.RemoteIP)
	}
	if !strings.Contains(rec.Body.String(), `data-testid="contact-success-banner"`) {
		t.Errorf("success banner missing\n%s", rec.Body.String())
	}
}

func TestContactPublic_SubmitInvalidShowsFieldError(t *testing.T) {
	svc := &stubContactPublic{submitErr: contact.ValidationError{Field: "email", Message: "Please enter a valid email address."}}
	h := contactPublicHandler(svc)
	form := url.Values{"name": {"Ada"}, "email": {"bad"}, "message": {"hi"}}
	rec := httptest.NewRecorder()
	h.Submit(rec, contactPostReq(form))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid submit = %d, want 422", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="contact-error-summary"`) {
		t.Errorf("error summary missing\n%s", body)
	}
	if !strings.Contains(body, "valid email") {
		t.Errorf("field message missing\n%s", body)
	}
	// The entered values are re-populated.
	if !strings.Contains(body, `value="Ada"`) || !strings.Contains(body, `value="bad"`) {
		t.Errorf("entered values not re-populated\n%s", body)
	}
}

func TestContactPublic_SubmitRecaptchaShowsFriendlyError(t *testing.T) {
	svc := &stubContactPublic{submitErr: contact.ErrRecaptcha}
	h := contactPublicHandler(svc)
	form := url.Values{"name": {"Ada"}, "email": {"ada@example.com"}, "message": {"hi"}}
	rec := httptest.NewRecorder()
	h.Submit(rec, contactPostReq(form))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("recaptcha submit = %d, want 422", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-testid="contact-error"`) {
		t.Errorf("top-level error banner missing\n%s", body)
	}
	if !strings.Contains(body, "could not be verified") {
		t.Errorf("friendly recaptcha message missing\n%s", body)
	}
}

func TestContactPublic_SubmitRateLimitedIs429(t *testing.T) {
	svc := &stubContactPublic{submitErr: contact.ErrRateLimited}
	h := contactPublicHandler(svc)
	form := url.Values{"name": {"Ada"}, "email": {"ada@example.com"}, "message": {"hi"}}
	rec := httptest.NewRecorder()
	h.Submit(rec, contactPostReq(form))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited submit = %d, want 429", rec.Code)
	}
}
