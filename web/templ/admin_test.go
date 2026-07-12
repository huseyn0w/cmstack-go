package templ

import (
	"context"
	"strings"
	"testing"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
)

// allowAll is a `can` predicate granting every permission (Administrator).
func allowAll(string, string) bool { return true }

// memberCan models a read-only Member: it can read post/page/service and create
// comments, nothing in Design or Settings.
func memberCan(action, subject string) bool {
	switch {
	case action == "read" && (subject == "post" || subject == "page" || subject == "service"):
		return true
	case action == "create" && subject == "comment":
		return true
	default:
		return false
	}
}

func adminShell(can func(string, string) bool) AdminShell {
	return AdminShell{
		UserName:   "Ada Lovelace",
		UserEmail:  "ada@example.com",
		RoleLabel:  "Administrator",
		CSRFToken:  "csrf-xyz",
		SiteURL:    "https://site.test",
		ActivePath: "/admin",
		Title:      "Dashboard",
		Nav:        BuildAdminNav(can),
	}
}

func TestAdminDashboardAdministratorSeesAllGroups(t *testing.T) {
	html, err := render.ToString(context.Background(), AdminDashboard(adminShell(allowAll), DashboardStats{}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		`data-testid="admin-sidebar"`,
		`data-testid="admin-topbar"`,
		`data-testid="admin-dashboard"`,
		"nav-group-Content",
		"nav-group-Design",
		"nav-group-Settings",
		"nav-item-Posts",
		"nav-item-Users",
		"nav-item-Plugins",
		"nav-item-Dashboard",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("administrator shell missing %q", want)
		}
	}
}

func TestAdminDashboardMemberSeesOnlyPermittedItemsHidden(t *testing.T) {
	html, err := render.ToString(context.Background(), AdminDashboard(adminShell(memberCan), DashboardStats{}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Member CAN read post/page/service -> Content group present with those.
	for _, want := range []string{"nav-group-Content", "nav-item-Posts", "nav-item-Pages", "nav-item-Services"} {
		if !strings.Contains(html, want) {
			t.Errorf("member should see %q", want)
		}
	}
	// Member CANNOT see Design/Settings groups or their items — HIDDEN, not
	// merely disabled (no markup, no `disabled` attribute).
	for _, gone := range []string{
		"nav-group-Design", "nav-group-Settings",
		"nav-item-Users", "nav-item-Plugins", "nav-item-Media", "nav-item-Comments",
	} {
		if strings.Contains(html, gone) {
			t.Errorf("member must NOT see %q (permission-gated items are hidden)", gone)
		}
	}
	if strings.Contains(html, "disabled") {
		t.Error("gated nav items must be hidden, never rendered-but-disabled")
	}
}

func TestAdminTopbarHasThemeToggleAndLogout(t *testing.T) {
	html, err := render.ToString(context.Background(), AdminDashboard(adminShell(allowAll), DashboardStats{}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		`data-testid="theme-toggle"`,      // dark/light toggle
		`data-testid="user-menu-trigger"`, // avatar dropdown trigger
		`aria-haspopup="menu"`,            // dropdown a11y
		`data-testid="role-badge"`,        // role badge in menu
		"Administrator",                   // the role label
		`data-testid="menu-logout"`,       // sign out
		`action="/logout"`,                // posts to logout
		"csrf-xyz",                        // csrf token embedded
		`data-testid="view-site"`,         // view site link
		"https://site.test",               // site url
		"Skip to content",                 // a11y skip link
	} {
		if !strings.Contains(html, want) {
			t.Errorf("topbar missing %q", want)
		}
	}
}

func TestAdminShellAvatarFallsBackToInitials(t *testing.T) {
	s := adminShell(allowAll) // no AvatarURL
	html, err := render.ToString(context.Background(), AdminDashboard(s, DashboardStats{}))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, ">AL<") {
		t.Errorf("expected initials AL fallback for %q", s.UserName)
	}
}

func TestBuildAdminNavDropsEmptyGroups(t *testing.T) {
	// A predicate that denies everything yields no groups at all.
	nav := BuildAdminNav(func(string, string) bool { return false })
	if len(nav) != 0 {
		t.Errorf("expected no groups when nothing is permitted, got %d", len(nav))
	}
}

func TestSocialButtonsRenderOnlyWhenProvidersEnabled(t *testing.T) {
	// With providers: buttons present.
	withProviders := AuthForm{
		Values: map[string]string{}, FieldErrors: map[string]string{},
		OAuthProviders: []OAuthProviderButton{
			{Name: "google", Label: "Google"},
			{Name: "github", Label: "GitHub"},
		},
	}
	html, err := render.ToString(context.Background(), LoginPage(withProviders))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		`data-testid="social-login"`,
		`data-testid="oauth-google"`,
		`data-testid="oauth-github"`,
		"Continue with Google",
		"Continue with GitHub",
		`href="/auth/google"`,
		`href="/auth/github"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("login page with providers missing %q", want)
		}
	}

	// Without providers: no social block at all.
	none := AuthForm{Values: map[string]string{}, FieldErrors: map[string]string{}}
	html2, err := render.ToString(context.Background(), LoginPage(none))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(html2, `data-testid="social-login"`) {
		t.Error("social buttons must not render when no providers are enabled")
	}
}
