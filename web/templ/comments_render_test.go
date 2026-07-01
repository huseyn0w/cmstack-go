package templ_test

import (
	"strings"
	"testing"

	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
)

// --- admin moderation render -------------------------------------------------

func adminModerationView(rows []webtempl.CommentAdminRow, pending int) webtempl.CommentModerationView {
	return webtempl.CommentModerationView{
		Shell: webtempl.AdminShell{
			UserName: "Mod", RoleLabel: "Editor", CSRFToken: "csrf", SiteURL: "/", Title: "Comments",
			Nav: webtempl.BuildAdminNav(func(string, string) bool { return true }),
		},
		Rows: rows,
		Tabs: []webtempl.CommentModerationTab{
			{Label: "Pending", Value: "PENDING", Href: "/admin/comments?status=PENDING", Active: true, Count: pending, ShowBadge: true},
			{Label: "Approved", Value: "APPROVED", Href: "/admin/comments?status=APPROVED", Count: 4},
			{Label: "Spam", Value: "SPAM", Href: "/admin/comments?status=SPAM"},
			{Label: "Trash", Value: "TRASH", Href: "/admin/comments?status=TRASH"},
		},
		BulkURL:      "/admin/comments/bulk",
		CSRFToken:    "csrf",
		PendingCount: pending,
	}
}

func TestCommentModeration_RendersTabsAndBadge(t *testing.T) {
	rows := []webtempl.CommentAdminRow{{
		ID: "row-1", AuthorName: "Alice", PostTitle: "First Post",
		Excerpt: "please approve", Status: webtempl.CommentStatusPending, Date: "Jan 2, 2026",
		ApproveURL: "/admin/comments/row-1/approve", SpamURL: "/admin/comments/row-1/spam",
		TrashURL: "/admin/comments/row-1/trash", DeleteURL: "/admin/comments/row-1/delete",
	}}
	html := renderStr(t, webtempl.CommentModeration(adminModerationView(rows, 7)))
	mustContain(t, html,
		`data-testid="comment-tabs"`,
		`data-testid="comment-tab-PENDING"`,
		`data-testid="comment-tab-count-PENDING"`,
		`data-testid="comments-pending-badge"`,
		`data-testid="comments-table"`,
		`data-testid="comment-approve-row-1"`,
		`data-testid="comment-spam-row-1"`,
		`data-testid="comment-trash-row-1"`,
		`data-testid="comment-delete-row-1"`,
		"Alice", "First Post", "please approve",
	)
	// The Pending count badge shows the count.
	if !strings.Contains(html, ">7<") {
		t.Error("pending badge count 7 not rendered")
	}
}

func TestCommentModeration_EmptyState(t *testing.T) {
	html := renderStr(t, webtempl.CommentModeration(adminModerationView(nil, 0)))
	mustContain(t, html, `data-testid="comments-empty"`)
	if strings.Contains(html, `data-testid="comments-table"`) {
		t.Error("empty view must not render the table")
	}
}

func TestAdminNav_CommentsBadge(t *testing.T) {
	nav := webtempl.BuildAdminNav(func(string, string) bool { return true })
	nav = webtempl.SetNavBadge(nav, "Comments", 5)
	shell := webtempl.AdminShell{UserName: "Admin", CSRFToken: "c", SiteURL: "/", Title: "Dashboard", Nav: nav}
	html := renderStr(t, webtempl.AdminDashboard(shell))
	mustContain(t, html, `data-testid="nav-badge-Comments"`)
}

// --- public thread render ----------------------------------------------------

func publicThreadView() webtempl.CommentThreadView {
	return webtempl.CommentThreadView{
		PostSlug:  "hello",
		Count:     1,
		SubmitURL: "/blog/hello/comments",
		CSRFToken: "csrf",
		IsGuest:   true,
		Comments: []webtempl.CommentNode{{
			ID: "c1", AuthorName: "Jane Reader", Initials: "JR",
			Body: "Loved this article", Date: "Jan 2, 2026 10:00",
		}},
		FieldErrors: map[string]string{},
	}
}

func TestCommentsPublic_ThreadRendersNoEmailOrIP(t *testing.T) {
	html := renderStr(t, webtempl.CommentsSection(publicThreadView()))
	mustContain(t, html,
		`data-testid="comments-section"`,
		`data-testid="comments-list"`,
		"Jane Reader", "Loved this article",
	)
	// PII boundary: the public thread must never leak an author email or IP.
	// The view-model carries none, so neither must appear in the markup.
	for _, leaked := range []string{"@example.com", "AuthorEmail", "AuthorIP", "203.0.113"} {
		if strings.Contains(html, leaked) {
			t.Errorf("public thread leaked PII fragment %q", leaked)
		}
	}
}

func TestCommentsPublic_GuestFormIsAccessible(t *testing.T) {
	html := renderStr(t, webtempl.CommentsSection(publicThreadView()))
	// Guest form exposes labeled name+email inputs and a labeled body textarea.
	mustContain(t, html,
		`data-testid="comment-input-name"`,
		`data-testid="comment-input-email"`,
		`data-testid="comment-input-body"`,
		`<label for="comment-name"`,
		`<label for="comment-email"`,
		`<label for="comment-body"`,
		`aria-labelledby="comment-form-title"`,
		"Your email is never published.",
	)
}
