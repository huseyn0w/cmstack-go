package templ

// DashboardStats carries the pre-formatted counts shown on the /admin landing
// page's stat cards. Values are strings so the handler can format numbers (incl.
// "0") while an empty string signals "not available" — rendered as a dash.
type DashboardStats struct {
	PublishedPosts  string
	PublishedPages  string
	PendingComments string
}

// orDash returns v, or an em dash when v is empty (stat unavailable / not wired).
func orDash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}
