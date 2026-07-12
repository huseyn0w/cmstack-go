package templ_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/huseyn0w/agentic-cms-go/internal/platform/render"
	webtempl "github.com/huseyn0w/agentic-cms-go/web/templ"
)

func renderStr(t *testing.T, c webtempl.Component) string {
	t.Helper()
	s, err := render.ToString(context.Background(), c)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return s
}

func mustContain(t *testing.T, html string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(html, s) {
			t.Errorf("rendered HTML missing %q", s)
		}
	}
}

func TestPostList_TableBadgesTabs(t *testing.T) {
	v := webtempl.PostListView{
		Rows: []webtempl.PostRow{
			{ID: "p1", Title: "First", Slug: "first", AuthorName: "Ann", Status: webtempl.PostStatusPublished, Date: "Jan 1, 2026", EditURL: "/admin/posts/p1/edit"},
			{ID: "p2", Title: "Second", Slug: "second", AuthorName: "Bob", Status: webtempl.PostStatusDraft, Scheduled: true, Date: "Jan 2, 2026", EditURL: "/admin/posts/p2/edit"},
		},
		Tabs: []webtempl.StatusTab{
			{Label: "All", Value: "", Href: "/admin/posts", Active: true},
			{Label: "Published", Value: "PUBLISHED", Href: "/admin/posts?status=PUBLISHED"},
			{Label: "Draft", Value: "DRAFT", Href: "/admin/posts?status=DRAFT"},
		},
		Pager:  webtempl.Pagination{Page: 1, PageSize: 20, Total: 2},
		NewURL: "/admin/posts/new",
	}
	html := renderStr(t, webtempl.PostList(v))
	mustContain(
		t, html,
		`data-testid="posts-table"`,
		`data-testid="status-tabs"`,
		`role="tablist"`,
		`aria-selected="true"`, // active All tab
		`data-testid="status-badge"`,
		"Published",
		"Scheduled", // p2 scheduled badge
		`data-testid="post-row-p1"`,
		`data-testid="new-post"`,
	)
}

func TestPostList_EmptyState(t *testing.T) {
	v := webtempl.PostListView{Tabs: []webtempl.StatusTab{{Label: "All", Active: true}}, NewURL: "/admin/posts/new"}
	html := renderStr(t, webtempl.PostList(v))
	mustContain(t, html, `data-testid="posts-empty"`, "No posts yet")
}

// TestPostList_BulkSelectionUI asserts the §5 bulk-selection affordances render:
// the leading select-all + per-row checkboxes, the action bar with an aria-live
// count, a destructive confirm modal, and a clear-selection control.
func TestPostList_BulkSelectionUI(t *testing.T) {
	v := webtempl.PostListView{
		Rows: []webtempl.PostRow{
			{ID: "p1", Title: "First", Slug: "first", Status: webtempl.PostStatusPublished, Date: "Jan 1, 2026", EditURL: "/admin/posts/p1/edit"},
		},
		Tabs:      []webtempl.StatusTab{{Label: "All", Active: true}},
		Pager:     webtempl.Pagination{Page: 1, PageSize: 20, Total: 1},
		NewURL:    "/admin/posts/new",
		BulkURL:   "/admin/posts/bulk",
		CSRFToken: "tok",
	}
	html := renderStr(t, webtempl.PostList(v))
	mustContain(
		t, html,
		`data-testid="bulk-select-all"`,
		`data-testid="bulk-select-p1"`,
		`data-testid="bulk-bar"`,
		`data-testid="bulk-count"`,
		`aria-live="polite"`,
		`data-testid="bulk-action-trash"`,   // destructive
		`data-testid="bulk-action-publish"`, // non-destructive
		`data-testid="bulk-confirm-modal"`,  // §5 destructive confirm
		`aria-modal="true"`,
		`data-testid="bulk-clear"`,
		`action="/admin/posts/bulk"`,
	)
}

// TestPostList_BulkSummaryBanner asserts the post-redirect outcome is announced
// via an aria-live status region.
func TestPostList_BulkSummaryBanner(t *testing.T) {
	v := webtempl.PostListView{
		Tabs:    []webtempl.StatusTab{{Label: "All", Active: true}},
		NewURL:  "/admin/posts/new",
		Summary: webtempl.BulkSummary{Present: true, Action: "trash", Applied: 2, Skipped: 1},
	}
	html := renderStr(t, webtempl.PostList(v))
	mustContain(
		t, html,
		`data-testid="bulk-summary"`,
		`role="status"`,
		"2 moved to trash",
		"1 skipped (not permitted)",
	)
}

func TestPostEditor_ToolbarA11y(t *testing.T) {
	v := webtempl.PostFormView{
		IsNew:       true,
		Status:      webtempl.PostStatusDraft,
		ActionURL:   "/admin/posts",
		CSRFToken:   "tok",
		FieldErrors: map[string]string{},
		BackURL:     "/admin/posts",
	}
	html := renderStr(t, webtempl.PostEditor(v))
	mustContain(
		t, html,
		`data-testid="editor-toolbar"`,
		`role="toolbar"`,
		`aria-label="Bold"`,
		`aria-label="Italic"`,
		`aria-label="Insert link"`,
		`aria-label="Insert media"`,         // media-insert toolbar button (M4)
		`data-testid="media-picker-modal"`,  // M4: editor media-library picker modal
		`data-testid="media-picker-target"`, // htmx target the grid loads into
		`:aria-pressed=`,                    // toggle buttons bind aria-pressed
		`contenteditable="true"`,
		`role="textbox"`,
		`aria-multiline="true"`,
		`name="body"`, // hidden textarea carries the body
		`data-testid="action-save"`,
		`data-testid="action-publish"`,
		`data-testid="action-schedule"`,
	)
}

// TestPostEditor_LocaleTabStrip asserts the per-locale editor tab strip renders
// with tablist/tab a11y, an active marker, a has-translation dot, a hidden
// active-locale field, and (on a de tab) the shared structural fields are hidden.
func TestPostEditor_LocaleTabStrip(t *testing.T) {
	v := webtempl.PostFormView{
		ID:           "p1",
		Title:        "DE Titel",
		Body:         "<p>DE</p>",
		Status:       webtempl.PostStatusPublished,
		ActionURL:    "/admin/posts/p1",
		CSRFToken:    "tok",
		FieldErrors:  map[string]string{},
		BackURL:      "/admin/posts",
		ActiveLocale: "de",
		LocaleTabs: []webtempl.LocaleTab{
			{Label: "English", Code: "en", Href: "/admin/posts/p1/edit", Active: false},
			{Label: "Deutsch", Code: "de", Href: "/admin/posts/p1/edit?language=de", Active: true, HasTranslation: true},
			{Label: "Русский", Code: "ru", Href: "/admin/posts/p1/edit?language=ru", Active: false},
		},
		IsDefaultLocale: false,
	}
	html := renderStr(t, webtempl.PostEditor(v))
	mustContain(
		t, html,
		`data-testid="locale-tabs"`,
		`role="tablist"`,
		`data-testid="locale-tab-en"`,
		`data-testid="locale-tab-de"`,
		`data-testid="locale-tab-ru"`,
		`aria-selected="true"`,           // active de tab
		`data-testid="locale-dot-de"`,    // has-translation marker
		`role="tabpanel"`,                // the form is the panel
		`name="locale"`,                  // hidden active-locale field
		`data-testid="translation-note"`, // de tab shows the shared-fields note
	)
	// On a de tab the SHARED structural fields must NOT render.
	for _, absent := range []string{`data-testid="field-status"`, `data-testid="field-slug"`, `data-testid="action-publish"`} {
		if strings.Contains(html, absent) {
			t.Errorf("de translation tab should hide %q", absent)
		}
	}
}

// TestPostEditor_EnTabShowsStructuralFields asserts the default (en) tab keeps
// the shared structural fields + publish/schedule actions.
func TestPostEditor_EnTabShowsStructuralFields(t *testing.T) {
	v := webtempl.PostFormView{
		ID:           "p1",
		Title:        "EN Title",
		ActionURL:    "/admin/posts/p1",
		CSRFToken:    "tok",
		FieldErrors:  map[string]string{},
		BackURL:      "/admin/posts",
		Status:       webtempl.PostStatusDraft,
		ActiveLocale: "en",
		LocaleTabs: []webtempl.LocaleTab{
			{Label: "English", Code: "en", Href: "/admin/posts/p1/edit", Active: true},
			{Label: "Deutsch", Code: "de", Href: "/admin/posts/p1/edit?language=de", Active: false},
		},
		IsDefaultLocale: true,
	}
	html := renderStr(t, webtempl.PostEditor(v))
	mustContain(
		t, html,
		`data-testid="locale-tab-en"`,
		`data-testid="field-status"`, // structural fields present on en
		`data-testid="field-slug"`,
		`data-testid="action-publish"`, // publish action present on en
	)
}

func TestPublicPostDetail_ArticleProseBreadcrumbJSONLD(t *testing.T) {
	v := webtempl.PublicPostView{
		SiteName:     "Agentic CMS",
		HomeURL:      "/",
		Title:        "Hello World",
		Slug:         "hello-world",
		BodyHTML:     "<p>Body content here.</p>",
		Excerpt:      "An intro.",
		AuthorName:   "Ann Author",
		AuthorURL:    "/authors/abc",
		PublishedAt:  time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		ReadingTime:  3,
		LikeCount:    5,
		CanLike:      false,
		CanonicalURL: "https://site.test/blog/hello-world",
	}
	html := renderStr(t, webtempl.PublicPostDetail(v))
	mustContain(
		t, html,
		`data-testid="post-article"`,
		"<article",
		`class="prose`, // prose typography on the body
		`data-testid="post-breadcrumb"`,
		`aria-label="Breadcrumb"`,
		`aria-current="page"`,
		`application/ld+json`, // JSON-LD seam
		`BlogPosting`,
		"3 min read",
		`data-testid="like-signin"`, // anonymous like prompt
		"<p>Body content here.</p>", // sanitized body rendered verbatim
	)
}

func TestPublicPostDetail_JSONLDEscapesMarkup(t *testing.T) {
	v := webtempl.PublicPostView{
		Title:       `Evil </script><script>alert(1)</script>`,
		AuthorName:  "X",
		PublishedAt: time.Now(),
	}
	html := renderStr(t, webtempl.PublicPostDetail(v))
	// The raw closing script + injected script must not survive into the JSON-LD.
	if strings.Contains(html, "</script><script>alert(1)") {
		t.Errorf("JSON-LD did not escape script breakout: %s", html)
	}
}

func TestPublicPostIndex_CardsAndEmpty(t *testing.T) {
	withCards := webtempl.PublicPostIndexView{
		SiteName: "Agentic CMS",
		Cards: []webtempl.PublicPostCard{
			{Title: "Post A", URL: "/blog/post-a", Excerpt: "ex", AuthorName: "Ann", Date: "Jan 1, 2026", ReadingTime: 2},
		},
		Pager: webtempl.Pagination{Page: 1, PageSize: 9, Total: 1},
	}
	html := renderStr(t, webtempl.PublicPostIndex(withCards))
	mustContain(t, html, `data-testid="blog-grid"`, `data-testid="blog-card"`, "Post A", "2 min")

	empty := webtempl.PublicPostIndexView{SiteName: "Agentic CMS"}
	emptyHTML := renderStr(t, webtempl.PublicPostIndex(empty))
	mustContain(t, emptyHTML, `data-testid="blog-empty"`, "No posts yet")
}

func TestLikeIsland_SignedInTogglePressed(t *testing.T) {
	v := webtempl.PublicPostView{CanLike: true, Liked: true, LikeCount: 7, LikeURL: "/blog/x/like", CSRFToken: "t"}
	html := renderStr(t, webtempl.LikeIsland(v))
	mustContain(
		t, html,
		`data-testid="like-button"`,
		`aria-pressed="true"`,
		`hx-post="/blog/x/like"`,
		`data-testid="like-count"`,
		"7",
	)
}

func TestPostRevisions_DiffAndRestore(t *testing.T) {
	v := webtempl.RevisionsView{
		PostTitle: "T",
		Current:   webtempl.RevisionRow{Title: "T", Body: "<p>current</p>"},
		Rows: []webtempl.RevisionRow{
			{ID: "r1", AuthorName: "Ann", CreatedAt: "Jan 1", Title: "Old", Body: "<p>old</p>", RestoreURL: "/admin/posts/p/revisions/r1/restore"},
		},
		BackURL: "/admin/posts/p/edit",
	}
	html := renderStr(t, webtempl.PostRevisions(v))
	mustContain(t, html, `data-testid="revisions-list"`, `data-testid="revision-diff"`, `data-testid="restore-r1"`, "<p>old</p>", "<p>current</p>")
}

func TestPostTrash_ConfirmModal(t *testing.T) {
	v := webtempl.TrashView{
		Rows: []webtempl.TrashRow{
			{ID: "p1", Title: "Trashed", DeletedAt: "Jan 1", RestoreURL: "/admin/posts/trash/p1/restore", DeleteURL: "/admin/posts/trash/p1/delete"},
		},
	}
	html := renderStr(t, webtempl.PostTrash(v))
	mustContain(t, html, `data-testid="trash-table"`, `data-testid="delete-modal"`, `role="dialog"`, `aria-modal="true"`, `data-testid="confirm-delete"`)
}

func TestPostTrash_Empty(t *testing.T) {
	html := renderStr(t, webtempl.PostTrash(webtempl.TrashView{}))
	mustContain(t, html, `data-testid="trash-empty"`, "Trash is empty")
}
