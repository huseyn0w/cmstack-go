package mcpserver

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listPostsInput is the filter/pagination input for list_posts.
type listPostsInput struct {
	Status         *string `json:"status,omitempty" jsonschema:"filter by status (DRAFT or PUBLISHED)"`
	Page           *int    `json:"page,omitempty" jsonschema:"1-based page number"`
	PerPage        *int    `json:"perPage,omitempty" jsonschema:"page size (max 100)"`
	IncludeTrashed *bool   `json:"includeTrashed,omitempty" jsonschema:"include soft-deleted posts"`
}

// createPostInput is the body for create_post.
type createPostInput struct {
	Title           string   `json:"title" jsonschema:"the post title (required)"`
	Slug            *string  `json:"slug,omitempty" jsonschema:"URL slug (optional; auto-generated from title)"`
	Excerpt         *string  `json:"excerpt,omitempty" jsonschema:"short summary"`
	Body            *string  `json:"body,omitempty" jsonschema:"the post content as HTML (sanitized server-side)"`
	Status          *string  `json:"status,omitempty" jsonschema:"DRAFT (default) or PUBLISHED"`
	CategoryIDs     []string `json:"categoryIds,omitempty" jsonschema:"category ids to attach"`
	TagIDs          []string `json:"tagIds,omitempty" jsonschema:"tag ids to attach"`
	MetaTitle       *string  `json:"metaTitle,omitempty" jsonschema:"SEO meta title"`
	MetaDescription *string  `json:"metaDescription,omitempty" jsonschema:"SEO meta description"`
}

// updatePostInput is the body for update_post (id + partial fields).
type updatePostInput struct {
	ID              string    `json:"id" jsonschema:"the post id"`
	Title           *string   `json:"title,omitempty" jsonschema:"new title"`
	Slug            *string   `json:"slug,omitempty" jsonschema:"new slug"`
	Excerpt         *string   `json:"excerpt,omitempty" jsonschema:"new excerpt"`
	Body            *string   `json:"body,omitempty" jsonschema:"new HTML body (sanitized server-side)"`
	Status          *string   `json:"status,omitempty" jsonschema:"DRAFT or PUBLISHED"`
	CategoryIDs     *[]string `json:"categoryIds,omitempty" jsonschema:"replacement category ids"`
	TagIDs          *[]string `json:"tagIds,omitempty" jsonschema:"replacement tag ids"`
	MetaTitle       *string   `json:"metaTitle,omitempty" jsonschema:"SEO meta title"`
	MetaDescription *string   `json:"metaDescription,omitempty" jsonschema:"SEO meta description"`
}

// listPagesInput is the input for list_pages.
type listPagesInput struct {
	IncludeTrashed *bool `json:"includeTrashed,omitempty" jsonschema:"include soft-deleted pages"`
}

// createPageInput is the body for create_page.
type createPageInput struct {
	Title           string  `json:"title" jsonschema:"the page title (required)"`
	Slug            *string `json:"slug,omitempty" jsonschema:"URL slug (optional; auto-generated)"`
	Body            *string `json:"body,omitempty" jsonschema:"the page content as HTML (sanitized server-side)"`
	Status          *string `json:"status,omitempty" jsonschema:"DRAFT (default) or PUBLISHED"`
	Template        *string `json:"template,omitempty" jsonschema:"page template id"`
	ParentID        *string `json:"parentId,omitempty" jsonschema:"parent page id for nesting"`
	MetaTitle       *string `json:"metaTitle,omitempty" jsonschema:"SEO meta title"`
	MetaDescription *string `json:"metaDescription,omitempty" jsonschema:"SEO meta description"`
}

// updatePageInput is the body for update_page.
type updatePageInput struct {
	ID              string  `json:"id" jsonschema:"the page id"`
	Title           *string `json:"title,omitempty" jsonschema:"new title"`
	Slug            *string `json:"slug,omitempty" jsonschema:"new slug"`
	Body            *string `json:"body,omitempty" jsonschema:"new HTML body (sanitized server-side)"`
	Status          *string `json:"status,omitempty" jsonschema:"DRAFT or PUBLISHED"`
	Template        *string `json:"template,omitempty" jsonschema:"page template id"`
	ParentID        *string `json:"parentId,omitempty" jsonschema:"parent page id"`
	MetaTitle       *string `json:"metaTitle,omitempty" jsonschema:"SEO meta title"`
	MetaDescription *string `json:"metaDescription,omitempty" jsonschema:"SEO meta description"`
}

// createCategoryInput is the body for create_category.
type createCategoryInput struct {
	Name        string  `json:"name" jsonschema:"the category name (required)"`
	Slug        *string `json:"slug,omitempty" jsonschema:"URL slug (optional)"`
	Description *string `json:"description,omitempty" jsonschema:"category description"`
	ParentID    *string `json:"parentId,omitempty" jsonschema:"parent category id for nesting"`
}

// updateCategoryInput is the body for update_category.
type updateCategoryInput struct {
	ID          string  `json:"id" jsonschema:"the category id"`
	Name        *string `json:"name,omitempty" jsonschema:"new name"`
	Slug        *string `json:"slug,omitempty" jsonschema:"new slug"`
	Description *string `json:"description,omitempty" jsonschema:"new description"`
	ParentID    *string `json:"parentId,omitempty" jsonschema:"new parent category id"`
}

// createTagInput is the body for create_tag.
type createTagInput struct {
	Name string  `json:"name" jsonschema:"the tag name (required)"`
	Slug *string `json:"slug,omitempty" jsonschema:"URL slug (optional)"`
}

// updateTagInput is the body for update_tag.
type updateTagInput struct {
	ID   string  `json:"id" jsonschema:"the tag id"`
	Name *string `json:"name,omitempty" jsonschema:"new name"`
	Slug *string `json:"slug,omitempty" jsonschema:"new slug"`
}

// registerContentTools registers the 23 content tools (posts, pages,
// categories, tags). Every call hits an RBAC-gated authoring endpoint, so the
// token user must hold the matching content permission (Editor or Administrator).
func registerContentTools(s *mcp.Server, client *APIClient) {
	// --- Posts ----------------------------------------------------------------

	register(s, "cmstack_go_list_posts", "List posts",
		"List posts (drafts and published) with optional filters and pagination. Returns { items, total, page, perPage }. Filters: status (DRAFT|PUBLISHED), includeTrashed, page, perPage.",
		readAnn, func(ctx context.Context, in listPostsInput) (json.RawMessage, error) {
			q := url.Values{}
			setIfNotNil(q, "status", in.Status)
			setIfNotNil(q, "page", in.Page)
			setIfNotNil(q, "perPage", in.PerPage)
			setIfNotNil(q, "includeTrashed", in.IncludeTrashed)
			return client.do(ctx, "GET", "/posts", q, nil)
		})

	register(s, "cmstack_go_get_post", "Get a post",
		"Fetch a single post by id, including its full content.",
		readAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "GET", "/posts/"+in.ID, nil, nil)
		})

	register(s, "cmstack_go_get_post_revisions", "Get post revisions",
		"List the saved revisions (scalar field snapshots) of a post by id, newest first.",
		readAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "GET", "/posts/"+in.ID+"/revisions", nil, nil)
		})

	register(s, "cmstack_go_create_post", "Create a post",
		"Create a post. Fields: title (required), slug (optional, auto-generated from title), excerpt, body (HTML; sanitized server-side), status (DRAFT default), categoryIds, tagIds. Returns the created post.",
		createAnn, func(ctx context.Context, in createPostInput) (json.RawMessage, error) {
			return client.do(ctx, "POST", "/posts", nil, in)
		})

	register(s, "cmstack_go_update_post", "Update a post",
		"Update a post by id. Any subset of: title, slug, excerpt, body, status, categoryIds, tagIds. Returns the updated post.",
		updateAnn, func(ctx context.Context, in updatePostInput) (json.RawMessage, error) {
			return client.do(ctx, "PATCH", "/posts/"+in.ID, nil, bodyWithoutID(in))
		})

	register(s, "cmstack_go_publish_post", "Publish a post",
		"Publish a post by id (sets status to PUBLISHED). Returns the updated post.",
		updateAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "POST", "/posts/"+in.ID+"/publish", nil, nil)
		})

	register(s, "cmstack_go_unpublish_post", "Unpublish a post",
		"Unpublish a post by id (sets status back to DRAFT, hiding it from the public site). Returns the updated post.",
		updateAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "POST", "/posts/"+in.ID+"/unpublish", nil, nil)
		})

	register(s, "cmstack_go_delete_post", "Delete a post",
		"Soft-delete a post by id (moves it to trash; restorable with cmstack_go_restore_post).",
		destructiveAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "DELETE", "/posts/"+in.ID, nil, nil)
		})

	register(s, "cmstack_go_restore_post", "Restore a post",
		"Restore a soft-deleted (trashed) post by id. Returns the restored post.",
		updateAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "POST", "/posts/"+in.ID+"/restore", nil, nil)
		})

	// --- Pages ----------------------------------------------------------------

	register(s, "cmstack_go_list_pages", "List pages",
		"List all pages. Set includeTrashed to also return soft-deleted pages.",
		readAnn, func(ctx context.Context, in listPagesInput) (json.RawMessage, error) {
			q := url.Values{}
			setIfNotNil(q, "includeTrashed", in.IncludeTrashed)
			return client.do(ctx, "GET", "/pages", q, nil)
		})

	register(s, "cmstack_go_get_page", "Get a page",
		"Fetch a single page by id, including its full content.",
		readAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "GET", "/pages/"+in.ID, nil, nil)
		})

	register(s, "cmstack_go_create_page", "Create a page",
		"Create a page. Fields: title (required), slug (optional), body (HTML; sanitized server-side), status (DRAFT default), template, parentId. Returns the created page.",
		createAnn, func(ctx context.Context, in createPageInput) (json.RawMessage, error) {
			return client.do(ctx, "POST", "/pages", nil, in)
		})

	register(s, "cmstack_go_update_page", "Update a page",
		"Update a page by id. Any subset of: title, slug, body, status, template, parentId. Returns the updated page.",
		updateAnn, func(ctx context.Context, in updatePageInput) (json.RawMessage, error) {
			return client.do(ctx, "PATCH", "/pages/"+in.ID, nil, bodyWithoutID(in))
		})

	register(s, "cmstack_go_delete_page", "Delete a page",
		"Soft-delete a page by id (moves it to trash; restorable).",
		destructiveAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "DELETE", "/pages/"+in.ID, nil, nil)
		})

	register(s, "cmstack_go_restore_page", "Restore a page",
		"Restore a soft-deleted (trashed) page by id. Returns the restored page.",
		updateAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "POST", "/pages/"+in.ID+"/restore", nil, nil)
		})

	// --- Categories -----------------------------------------------------------

	register(s, "cmstack_go_list_categories", "List categories",
		"List all categories (a self-referential tree; each item carries its parentId).",
		readAnn, func(ctx context.Context, _ emptyInput) (json.RawMessage, error) {
			return client.do(ctx, "GET", "/categories", nil, nil)
		})

	register(s, "cmstack_go_create_category", "Create a category",
		"Create a category. Fields: name (required), slug (optional), description, parentId (optional, for nesting). Returns the created category.",
		createAnn, func(ctx context.Context, in createCategoryInput) (json.RawMessage, error) {
			return client.do(ctx, "POST", "/categories", nil, in)
		})

	register(s, "cmstack_go_update_category", "Update a category",
		"Update a category by id. Any subset of: name, slug, description, parentId. Returns the updated category.",
		updateAnn, func(ctx context.Context, in updateCategoryInput) (json.RawMessage, error) {
			return client.do(ctx, "PATCH", "/categories/"+in.ID, nil, bodyWithoutID(in))
		})

	register(s, "cmstack_go_delete_category", "Delete a category",
		"Permanently delete a category by id.",
		destructiveAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "DELETE", "/categories/"+in.ID, nil, nil)
		})

	// --- Tags -----------------------------------------------------------------

	register(s, "cmstack_go_list_tags", "List tags",
		"List all tags.",
		readAnn, func(ctx context.Context, _ emptyInput) (json.RawMessage, error) {
			return client.do(ctx, "GET", "/tags", nil, nil)
		})

	register(s, "cmstack_go_create_tag", "Create a tag",
		"Create a tag. Fields: name (required), slug (optional). Returns the created tag.",
		createAnn, func(ctx context.Context, in createTagInput) (json.RawMessage, error) {
			return client.do(ctx, "POST", "/tags", nil, in)
		})

	register(s, "cmstack_go_update_tag", "Update a tag",
		"Update a tag by id. Any subset of: name, slug. Returns the updated tag.",
		updateAnn, func(ctx context.Context, in updateTagInput) (json.RawMessage, error) {
			return client.do(ctx, "PATCH", "/tags/"+in.ID, nil, bodyWithoutID(in))
		})

	register(s, "cmstack_go_delete_tag", "Delete a tag",
		"Permanently delete a tag by id.",
		destructiveAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "DELETE", "/tags/"+in.ID, nil, nil)
		})
}

// bodyWithoutID marshals an update-input struct to a JSON object with the "id"
// key removed, so a route-param id is never duplicated into the PATCH body. It
// round-trips through a generic map; on any encode error it returns the value
// unchanged (the API ignores an unknown/extra id field regardless).
func bodyWithoutID(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return v
	}
	delete(m, "id")
	return m
}
