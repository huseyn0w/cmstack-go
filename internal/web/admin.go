package web

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/huseyn0w/cmstack-go/internal/accounts"
	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// RoleResolver resolves a role by id so the admin shell can show the role label
// badge. *accounts.RoleRepoPG satisfies it.
type RoleResolver interface {
	GetByID(ctx context.Context, id uuid.UUID) (accounts.Role, error)
}

// adminShellDeps are the dependencies the admin shell handler needs to assemble
// a permission-filtered AdminShell view-model for the current user.
type adminShellDeps struct {
	authz   PermissionChecker
	roles   RoleResolver
	csrf    func(*http.Request) string
	siteURL string
}

// buildShell assembles the AdminShell for the current request: it filters the
// navigation through the Authorizer (hidden, not disabled), resolves the role
// label, and sets the active path + section title. The Authorizer is the single
// source of truth for what the user may see.
func (a adminShellDeps) buildShell(r *http.Request, title string) webtempl.AdminShell {
	u, _ := UserFromContext(r.Context())

	name := u.Name
	if name == "" {
		name = u.Email
	}

	roleLabel := ""
	if role, err := a.roles.GetByID(r.Context(), u.RoleID); err == nil {
		roleLabel = role.Label
	}

	// The `can` predicate closes over the current user; BuildAdminNav hides any
	// item the user cannot access and drops emptied groups.
	can := func(action, subject string) bool {
		return a.authz.Can(r.Context(), u.ID, action, subject)
	}

	csrf := ""
	if a.csrf != nil {
		csrf = a.csrf(r)
	}

	siteURL := a.siteURL
	if siteURL == "" {
		siteURL = "/"
	}

	return webtempl.AdminShell{
		UserName:   name,
		UserEmail:  u.Email,
		AvatarURL:  u.AvatarURL,
		RoleLabel:  roleLabel,
		CSRFToken:  csrf,
		SiteURL:    siteURL,
		ActivePath: r.URL.Path,
		Title:      title,
		Nav:        webtempl.BuildAdminNav(can),
	}
}

// dashboard renders the /admin landing page using the shell.
func (a adminShellDeps) dashboard(w http.ResponseWriter, r *http.Request) {
	shell := a.buildShell(r, "Dashboard")
	if err := render.Component(r.Context(), w, http.StatusOK, webtempl.AdminDashboard(shell)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
