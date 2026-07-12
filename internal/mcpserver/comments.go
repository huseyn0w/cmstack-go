package mcpserver

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listCommentsInput is the filter/pagination input for list_comments.
type listCommentsInput struct {
	Status  *string `json:"status,omitempty" jsonschema:"filter by status (PENDING|APPROVED|SPAM|TRASH)"`
	Page    *int    `json:"page,omitempty" jsonschema:"1-based page number"`
	PerPage *int    `json:"perPage,omitempty" jsonschema:"page size (max 100)"`
}

// registerCommentTools registers the 5 comment moderation tools. Gated by the
// Comment RBAC subject (Editor + Administrator). New comments arrive PENDING and
// become public only once approved.
func registerCommentTools(s *mcp.Server, client *APIClient) {
	register(s, "agentic_cms_go_list_comments", "List comments",
		"List comments for moderation with optional status filter (PENDING|APPROVED|SPAM|TRASH) and pagination. Returns { items, total, page, perPage }; each item includes the post id and author email (admin view).",
		readAnn, func(ctx context.Context, in listCommentsInput) (json.RawMessage, error) {
			q := url.Values{}
			setIfNotNil(q, "status", in.Status)
			setIfNotNil(q, "page", in.Page)
			setIfNotNil(q, "perPage", in.PerPage)
			return client.do(ctx, "GET", "/comments", q, nil)
		})

	register(s, "agentic_cms_go_approve_comment", "Approve a comment",
		"Approve a comment by id (status APPROVED), making it publicly visible. Returns the updated comment.",
		updateAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "POST", "/comments/"+in.ID+"/approve", nil, nil)
		})

	register(s, "agentic_cms_go_mark_comment_spam", "Mark a comment as spam",
		"Mark a comment by id as SPAM (hides it from the public site). Returns the updated comment.",
		updateAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "POST", "/comments/"+in.ID+"/spam", nil, nil)
		})

	register(s, "agentic_cms_go_trash_comment", "Trash a comment",
		"Move a comment by id to TRASH. Returns the updated comment.",
		updateAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "POST", "/comments/"+in.ID+"/trash", nil, nil)
		})

	register(s, "agentic_cms_go_delete_comment", "Delete a comment",
		"Permanently delete a comment by id.",
		destructiveAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "DELETE", "/comments/"+in.ID, nil, nil)
		})
}
