package templ_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/huseyn0w/cmstack-go/internal/platform/render"
	webtempl "github.com/huseyn0w/cmstack-go/web/templ"
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
		`aria-label="Insert media"`, // media-insert stub button
		`:aria-pressed=`,            // toggle buttons bind aria-pressed
		`contenteditable="true"`,
		`role="textbox"`,
		`aria-multiline="true"`,
		`name="body"`, // hidden textarea carries the body
		`data-testid="action-save"`,
		`data-testid="action-publish"`,
		`data-testid="action-schedule"`,
	)
}

func TestPublicPostDetail_ArticleProseBreadcrumbJSONLD(t *testing.T) {
	v := webtempl.PublicPostView{
		SiteName:     "CMStack",
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
		SiteName: "CMStack",
		Cards: []webtempl.PublicPostCard{
			{Title: "Post A", URL: "/blog/post-a", Excerpt: "ex", AuthorName: "Ann", Date: "Jan 1, 2026", ReadingTime: 2},
		},
		Pager: webtempl.Pagination{Page: 1, PageSize: 9, Total: 1},
	}
	html := renderStr(t, webtempl.PublicPostIndex(withCards))
	mustContain(t, html, `data-testid="blog-grid"`, `data-testid="blog-card"`, "Post A", "2 min")

	empty := webtempl.PublicPostIndexView{SiteName: "CMStack"}
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
