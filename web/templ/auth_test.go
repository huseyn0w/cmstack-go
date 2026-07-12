package templ

import (
	"context"
	"strings"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
)

func TestLoginPageRendersFieldsAndCSRF(t *testing.T) {
	f := AuthForm{CSRFToken: "tok-123", Values: map[string]string{}, FieldErrors: map[string]string{}}
	html, err := render.ToString(context.Background(), LoginPage(f))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		`data-testid="login-form"`,
		`name="csrf_token"`,
		`value="tok-123"`,
		`id="identifier"`,
		`id="password"`,
		`autocomplete="current-password"`,
		`data-testid="submit"`,
		"/forgot-password",
		"/signup",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("login page missing %q", want)
		}
	}
}

func TestLoginPageRendersErrorBanner(t *testing.T) {
	f := AuthForm{Error: "Invalid email/username or password.", Values: map[string]string{}, FieldErrors: map[string]string{}}
	html, err := render.ToString(context.Background(), LoginPage(f))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, `role="alert"`) {
		t.Error("expected role=alert error banner")
	}
	if !strings.Contains(html, "Invalid email/username or password.") {
		t.Error("expected error message text")
	}
}

func TestSignupPageErrorSummaryLinksFields(t *testing.T) {
	f := AuthForm{
		Values:      map[string]string{"email": "bad"},
		FieldErrors: map[string]string{"email": "Enter a valid email address."},
	}
	html, err := render.ToString(context.Background(), SignupPage(f))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		`data-testid="error-summary"`,
		`href="#email"`,                  // summary links to the field
		`aria-invalid="true"`,            // invalid input flagged
		`aria-describedby="email-error"`, // error linked to input
		`id="email-error"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("signup page missing %q", want)
		}
	}
}

func TestForgotAndResetPagesRender(t *testing.T) {
	f := AuthForm{Values: map[string]string{}, FieldErrors: map[string]string{}}
	forgot, err := render.ToString(context.Background(), ForgotPasswordPage(f))
	if err != nil {
		t.Fatalf("render forgot: %v", err)
	}
	if !strings.Contains(forgot, `data-testid="forgot-form"`) || !strings.Contains(forgot, `id="email"`) {
		t.Error("forgot page missing fields")
	}

	reset, err := render.ToString(context.Background(), ResetPasswordPage(f, "reset-tok"))
	if err != nil {
		t.Fatalf("render reset: %v", err)
	}
	if !strings.Contains(reset, `value="reset-tok"`) || !strings.Contains(reset, `id="password"`) {
		t.Error("reset page missing token or password field")
	}
}

func TestAdminStubShowsUserAndLogout(t *testing.T) {
	html, err := render.ToString(context.Background(), AdminStub("Alice", "csrf-x"))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{`data-testid="admin-stub"`, "Alice", `data-testid="logout"`, "/logout", "csrf-x"} {
		if !strings.Contains(html, want) {
			t.Errorf("admin stub missing %q", want)
		}
	}
}
