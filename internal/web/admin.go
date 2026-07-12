package web

import (
	"context"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

// RoleResolver resolves a role by id so the admin shell can show the role label
// badge. *accounts.RoleRepoPG satisfies it.
type RoleResolver interface {
	GetByID(ctx context.Context, id uuid.UUID) (accounts.Role, error)
}

// PublishedCounter reports the number of published rows for a content type; it
// feeds the dashboard's posts/pages stat cards. *posts.Service and *pages.Service
// satisfy it.
type PublishedCounter interface {
	CountPublished(ctx context.Context) (int, error)
}

// PendingCommentCounter reports how many comments await moderation, for the
// dashboard's comments stat card. *comments.Service satisfies it.
type PendingCommentCounter interface {
	CountPending(ctx context.Context, actorID uuid.UUID) (int, error)
}

// adminShellDeps are the dependencies the admin shell handler needs to assemble
// a permission-filtered AdminShell view-model for the current user.
type adminShellDeps struct {
	authz   PermissionChecker
	roles   RoleResolver
	csrf    func(*http.Request) string
	siteURL string

	// Optional dashboard stat readers. Nil (or an error at read time) renders the
	// corresponding stat card as a dash, so the dashboard degrades gracefully.
	posts    PublishedCounter
	pages    PublishedCounter
	comments PendingCommentCounter
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

// dashboard renders the /admin landing page using the shell, populated with live
// published-content and pending-moderation counts. Any unavailable stat (reader
// not wired or a read error) is left blank and shown as a dash by the template.
func (a adminShellDeps) dashboard(w http.ResponseWriter, r *http.Request) {
	shell := a.buildShell(r, "Dashboard")
	stats := a.dashboardStats(r)
	if err := render.Component(r.Context(), w, http.StatusOK, webtempl.AdminDashboard(shell, stats)); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// dashboardStats reads the published post/page and pending-comment counts,
// formatting each as a string (incl. "0"). A nil reader or read error leaves the
// field empty, which the template renders as a dash.
func (a adminShellDeps) dashboardStats(r *http.Request) webtempl.DashboardStats {
	ctx := r.Context()
	var stats webtempl.DashboardStats

	if a.posts != nil {
		if n, err := a.posts.CountPublished(ctx); err == nil {
			stats.PublishedPosts = strconv.Itoa(n)
		}
	}
	if a.pages != nil {
		if n, err := a.pages.CountPublished(ctx); err == nil {
			stats.PublishedPages = strconv.Itoa(n)
		}
	}
	if a.comments != nil {
		if u, ok := UserFromContext(ctx); ok {
			if n, err := a.comments.CountPending(ctx, u.ID); err == nil {
				stats.PendingComments = strconv.Itoa(n)
			}
		}
	}
	return stats
}
