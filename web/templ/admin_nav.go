package templ

import "strconv"

// navBadgeLabel caps a badge count for display ("99+" beyond 99).
func navBadgeLabel(n int) string {
	if n > 99 {
		return "99+"
	}
	return strconv.Itoa(n)
}

// AdminShell carries everything the admin layout needs: the current user's
// display identity, the permission-filtered navigation, the active section, and
// the CSRF token for the logout form. It is assembled by the web layer (which
// owns the Authorizer) and handed to the templ layout as a pure view-model — the
// template performs NO authorization itself.
type AdminShell struct {
	UserName   string // display name (falls back to email upstream)
	UserEmail  string
	AvatarURL  string // provider avatar; empty -> initials fallback
	RoleLabel  string // e.g. "Administrator" — shown as a badge in the user menu
	CSRFToken  string
	SiteURL    string     // "View site" target
	ActivePath string     // current path, used to mark the active nav item
	Title      string     // current section title (topbar h4)
	Nav        []NavGroup // already permission-filtered
}

// NavGroup is a labeled cluster of nav items (DESIGN_SYSTEM §5: mono-uppercase
// eyebrow group label).
type NavGroup struct {
	Label string
	Items []NavItem
}

// NavItem is one sidebar entry. Action/Subject declare the permission required
// to SEE it; the builder hides items the user cannot access (hidden, not
// disabled). Href may be "#" for milestones not built yet.
type NavItem struct {
	Label   string
	Href    string
	Icon    string // key into the icon set (see admin.templ navIcon)
	Action  string
	Subject string
	// Badge, when > 0, renders a small count pill next to the item (e.g. the
	// number of comments awaiting moderation).
	Badge int
}

// navBlueprint is the full, unfiltered admin navigation. Each item declares the
// (action, subject) needed to view it. Hrefs point at real routes where they
// exist and "#" placeholders for later milestones (gated correctly now).
//
// Subjects/actions are kept as plain strings here to avoid importing the
// accounts package into the view layer; the web layer maps the real constants
// when it builds the `can` predicate.
func navBlueprint() []NavGroup {
	return []NavGroup{
		{
			Label: "Content",
			Items: []NavItem{
				{Label: "Posts", Href: "/admin/posts", Icon: "post", Action: "read", Subject: "post"},
				{Label: "Pages", Href: "/admin/pages", Icon: "page", Action: "read", Subject: "page"},
				{Label: "Services", Href: "/admin/services", Icon: "service", Action: "read", Subject: "service"},
				{Label: "Categories", Href: "/admin/categories", Icon: "tag", Action: "read", Subject: "category"},
				{Label: "Tags", Href: "/admin/tags", Icon: "tag", Action: "read", Subject: "tag"},
				{Label: "Media", Href: "/admin/media", Icon: "media", Action: "read", Subject: "media"},
				{Label: "Comments", Href: "/admin/comments", Icon: "comment", Action: "read", Subject: "comment"},
			},
		},
		{
			Label: "Design",
			Items: []NavItem{
				{Label: "Appearance", Href: "#", Icon: "theme", Action: "read", Subject: "theme"},
				{Label: "Menus", Href: "#", Icon: "menu", Action: "read", Subject: "menu"},
				{Label: "Plugins", Href: "#", Icon: "plugin", Action: "read", Subject: "plugin"},
			},
		},
		{
			Label: "Settings",
			Items: []NavItem{
				{Label: "General", Href: "#", Icon: "setting", Action: "read", Subject: "setting"},
				{Label: "SEO & GEO", Href: "#", Icon: "seo", Action: "read", Subject: "setting"},
				{Label: "Users", Href: "#", Icon: "user", Action: "read", Subject: "user"},
			},
		},
	}
}

// BuildAdminNav returns the navigation filtered to the items the user may see.
// can reports whether the current user holds (action, subject). Items that fail
// the check are HIDDEN (omitted), and groups that end up empty are dropped — so
// a Member never sees a Settings group it cannot use, and an Administrator
// (can:everything) sees every group. This is the single permission gate for the
// shell; the template just renders what it is given.
func BuildAdminNav(can func(action, subject string) bool) []NavGroup {
	var out []NavGroup
	for _, g := range navBlueprint() {
		var items []NavItem
		for _, it := range g.Items {
			if can(it.Action, it.Subject) {
				items = append(items, it)
			}
		}
		if len(items) > 0 {
			out = append(out, NavGroup{Label: g.Label, Items: items})
		}
	}
	return out
}

// SetNavBadge stamps a count badge onto the nav item with the given label
// (matching NavItem.Label) across all groups. A count <= 0 clears it. It is used
// to surface the pending-comments count in the sidebar. Returns the same slice.
func SetNavBadge(nav []NavGroup, label string, count int) []NavGroup {
	for gi := range nav {
		for ii := range nav[gi].Items {
			if nav[gi].Items[ii].Label == label {
				nav[gi].Items[ii].Badge = count
			}
		}
	}
	return nav
}

// initials returns up to two uppercase initials for the avatar fallback.
func initials(name, email string) string {
	src := name
	if src == "" {
		src = email
	}
	if src == "" {
		return "?"
	}
	var out []rune
	prevSpace := true
	for _, r := range src {
		if r == ' ' {
			prevSpace = true
			continue
		}
		if prevSpace {
			out = append(out, upperRune(r))
			if len(out) == 2 {
				break
			}
		}
		prevSpace = false
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

func upperRune(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 32
	}
	return r
}
