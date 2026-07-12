package mcpserver

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listUsersInput is the pagination input for list_users.
type listUsersInput struct {
	Page    *int `json:"page,omitempty" jsonschema:"1-based page number"`
	PerPage *int `json:"perPage,omitempty" jsonschema:"page size (max 100)"`
}

// updateUserInput is the body for update_user (id + partial fields).
type updateUserInput struct {
	ID     string  `json:"id" jsonschema:"the user id"`
	Name   *string `json:"name,omitempty" jsonschema:"new display name"`
	RoleID *string `json:"roleId,omitempty" jsonschema:"role id to assign (use list_roles for valid ids)"`
}

// registerUserTools registers the 4 user tools. Gated by the User RBAC subject.
// Deliberately scoped to listing and role assignment; user deletion is NOT
// exposed over MCP. Email and other sensitive fields are never returned.
func registerUserTools(s *mcp.Server, client *APIClient) {
	register(s, "agentic_cms_go_list_users", "List users",
		"List users with pagination. Returns { items, total, page, perPage }; each item includes id, email, username, name, roleId, roleName, createdAt.",
		readAnn, func(ctx context.Context, in listUsersInput) (json.RawMessage, error) {
			q := url.Values{}
			setIfNotNil(q, "page", in.Page)
			setIfNotNil(q, "perPage", in.PerPage)
			return client.do(ctx, "GET", "/users", q, nil)
		})

	register(s, "agentic_cms_go_list_roles", "List roles",
		"List the available roles ({ id, key, label }) for assignment with agentic_cms_go_update_user.",
		readAnn, func(ctx context.Context, _ emptyInput) (json.RawMessage, error) {
			return client.do(ctx, "GET", "/roles", nil, nil)
		})

	register(s, "agentic_cms_go_get_user", "Get a user",
		"Fetch a single user by id (id, email, username, name, roleId, roleName, createdAt).",
		readAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "GET", "/users/"+in.ID, nil, nil)
		})

	register(s, "agentic_cms_go_update_user", "Update a user",
		"Update a user by id. Any subset of: name, roleId (assign a role; use agentic_cms_go_list_roles for valid ids). Returns the updated user.",
		updateAnn, func(ctx context.Context, in updateUserInput) (json.RawMessage, error) {
			return client.do(ctx, "PATCH", "/users/"+in.ID, nil, bodyWithoutID(in))
		})
}
