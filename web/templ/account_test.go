package templ_test

import (
	"context"
	"strings"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

func sampleAccountForm() webtempl.AccountForm {
	return webtempl.AccountForm{
		Shell: webtempl.AdminShell{
			UserName:  "Grace Hopper",
			UserEmail: "grace@example.com",
			Title:     "Account",
			SiteURL:   "/",
		},
		CSRFToken:   "tok",
		Name:        "Grace Hopper",
		Bio:         "Pioneer",
		Website:     "https://grace.dev",
		Socials:     map[string]string{"github": "https://github.com/grace"},
		SocialOrder: []string{"twitter", "github", "linkedin", "mastodon"},
		FieldErrors: map[string]string{},
	}
}

func TestAccountPage_RendersFieldsInsideAdminShell(t *testing.T) {
	out, err := render.ToString(context.Background(), webtempl.AccountPage(sampleAccountForm()))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Lives inside the admin shell.
	if !strings.Contains(out, `data-testid="admin-main"`) {
		t.Error("account page should render inside the admin shell")
	}
	// Profile + avatar + password sections present with testids.
	for _, id := range []string{"profile-form", "avatar-form", "password-form", "avatar-input", "field-name", "field-bio", "field-website", "field-social_github", "account-breadcrumb"} {
		if !strings.Contains(out, `data-testid="`+id+`"`) {
			t.Errorf("missing data-testid %q", id)
		}
	}
	// Avatar uploader is a native file input with a dropzone progressive
	// enhancement and an accept allow-list (no SVG).
	if !strings.Contains(out, `type="file"`) || !strings.Contains(out, `data-testid="avatar-dropzone"`) {
		t.Error("expected native file input + dropzone")
	}
	if strings.Contains(out, "image/svg") {
		t.Error("avatar accept list must not include SVG")
	}
	// Breadcrumb landmark per §5.
	if !strings.Contains(out, `aria-label="Breadcrumb"`) {
		t.Error("missing breadcrumb landmark")
	}
}

func TestAccountPage_ShowsFieldErrorsWithA11yWiring(t *testing.T) {
	f := sampleAccountForm()
	f.FieldErrors = map[string]string{"website": "Enter a valid http(s) URL."}
	out, err := render.ToString(context.Background(), webtempl.AccountPage(f))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, `data-testid="account-error-summary"`) {
		t.Error("missing error summary")
	}
	if !strings.Contains(out, `aria-invalid="true"`) || !strings.Contains(out, `id="website-error"`) {
		t.Error("website field missing aria-invalid / error wiring")
	}
}

func TestAccountPage_AvatarInitialsFallback(t *testing.T) {
	f := sampleAccountForm()
	f.AvatarURL = "" // no avatar -> initials
	out, err := render.ToString(context.Background(), webtempl.AccountPage(f))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, `data-testid="avatar-initials"`) {
		t.Error("expected initials fallback when no avatar URL")
	}
}
