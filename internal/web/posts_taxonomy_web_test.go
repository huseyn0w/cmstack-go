package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/huseyn0w/agentic-cms-go/internal/accounts"
	"github.com/huseyn0w/agentic-cms-go/internal/content/categories"
	"github.com/huseyn0w/agentic-cms-go/internal/content/posts"
	"github.com/huseyn0w/agentic-cms-go/internal/content/tags"
	"github.com/huseyn0w/agentic-cms-go/internal/platform/security"
)

// stubCategoryReader / stubTagReader feed the post editor selectors.
type stubCategoryReader struct {
	tree []categories.TreeNode
	ids  []uuid.UUID
	cats []categories.Category
}

func (s stubCategoryReader) Tree(context.Context) ([]categories.TreeNode, error) { return s.tree, nil }

func (s stubCategoryReader) IDsForPost(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return s.ids, nil
}

func (s stubCategoryReader) CategoriesForPost(context.Context, uuid.UUID) ([]categories.Category, error) {
	return s.cats, nil
}

type stubTagReader struct {
	all  []tags.Tag
	ids  []uuid.UUID
	tags []tags.Tag
}

func (s stubTagReader) AllFlat(context.Context) ([]tags.Tag, error) { return s.all, nil }

func (s stubTagReader) IDsForPost(context.Context, uuid.UUID) ([]uuid.UUID, error) { return s.ids, nil }

func (s stubTagReader) TagsForPost(context.Context, uuid.UUID) ([]tags.Tag, error) {
	return s.tags, nil
}

// TestPostEditor_RendersTaxonomySelectors asserts the New post editor renders the
// category tree + tag selectors when the readers are wired, with the category's
// id as a checkbox option.
func TestPostEditor_RendersTaxonomySelectors(t *testing.T) {
	catID := uuid.New()
	tagID := uuid.New()
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{role: accounts.Role{Label: "Editor"}}, csrf: security.Token, siteURL: "/"}
	h := NewPostAdminHandler(stubPostAdmin{}, shell, nil, security.Token).
		WithTaxonomy(
			stubCategoryReader{tree: []categories.TreeNode{{Category: categories.Category{ID: catID, Name: "News"}, Depth: 0}}},
			stubTagReader{all: []tags.Tag{{ID: tagID, Name: "Go"}}},
		)

	req := httptest.NewRequest(http.MethodGet, "/admin/posts/new", nil)
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New(), Name: "Ed"}))
	rec := httptest.NewRecorder()
	h.New(rec, req)

	body := rec.Body.String()
	for _, want := range []string{"category-select", "tag-select", "category-option-" + catID.String(), "tag-option-" + tagID.String()} {
		if !strings.Contains(body, want) {
			t.Fatalf("editor body missing %q", want)
		}
	}
}

// TestPostCreate_PassesTaxonomyIDs asserts the create handler decodes the
// category_ids/tag_ids form fields and passes them to the service (the M2M seam).
func TestPostCreate_PassesTaxonomyIDs(t *testing.T) {
	catID := uuid.New()
	tagID := uuid.New()

	var gotCats, gotTags []uuid.UUID
	svc := captureCreateSvc{onCreate: func(in posts.CreateInput) {
		gotCats = in.CategoryIDs
		gotTags = in.TagIDs
	}}
	shell := adminShellDeps{authz: allowAllAuthz{}, roles: fakeRoles{}, csrf: security.Token, siteURL: "/"}
	h := NewPostAdminHandler(svc, shell, nil, security.Token)

	form := url.Values{
		"title":        {"Tagged"},
		"body":         {"<p>x</p>"},
		"status":       {"DRAFT"},
		"category_ids": {catID.String()},
		"tag_ids":      {tagID.String()},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/posts", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(withUser(req.Context(), accounts.User{ID: uuid.New()}))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if len(gotCats) != 1 || gotCats[0] != catID {
		t.Fatalf("category ids = %v, want [%s]", gotCats, catID)
	}
	if len(gotTags) != 1 || gotTags[0] != tagID {
		t.Fatalf("tag ids = %v, want [%s]", gotTags, tagID)
	}
}

// captureCreateSvc is a PostAdminService that records the create input.
type captureCreateSvc struct {
	stubPostAdmin
	onCreate func(posts.CreateInput)
}

func (s captureCreateSvc) Create(_ context.Context, _ uuid.UUID, in posts.CreateInput) (posts.Post, error) {
	if s.onCreate != nil {
		s.onCreate(in)
	}
	return posts.Post{ID: uuid.New()}, nil
}
