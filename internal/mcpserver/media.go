package mcpserver

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listMediaInput is the pagination input for list_media.
type listMediaInput struct {
	Page    *int `json:"page,omitempty" jsonschema:"1-based page number"`
	PerPage *int `json:"perPage,omitempty" jsonschema:"page size (max 100)"`
}

// updateMediaInput is the body for update_media (id + partial metadata).
type updateMediaInput struct {
	ID      string  `json:"id" jsonschema:"the media asset id"`
	Alt     *string `json:"alt,omitempty" jsonschema:"alt text"`
	Title   *string `json:"title,omitempty" jsonschema:"title"`
	Caption *string `json:"caption,omitempty" jsonschema:"caption"`
}

// registerMediaTools registers the 4 media tools. Binary upload is intentionally
// NOT exposed over MCP (it needs a multipart file body and byte-level
// validation); these list and manage existing assets and their metadata.
func registerMediaTools(s *mcp.Server, client *APIClient) {
	register(s, "agentic_cms_go_list_media", "List media",
		"List uploaded media assets with pagination. Returns { items, total, page, perPage }; each item includes its public url, dimensions, and metadata.",
		readAnn, func(ctx context.Context, in listMediaInput) (json.RawMessage, error) {
			q := url.Values{}
			setIfNotNil(q, "page", in.Page)
			setIfNotNil(q, "perPage", in.PerPage)
			return client.do(ctx, "GET", "/media", q, nil)
		})

	register(s, "agentic_cms_go_get_media", "Get a media asset",
		"Fetch a single media asset by id.",
		readAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "GET", "/media/"+in.ID, nil, nil)
		})

	register(s, "agentic_cms_go_update_media", "Update media metadata",
		"Update a media asset's editorial metadata by id. Any subset of: alt, title, caption. Returns the updated asset.",
		updateAnn, func(ctx context.Context, in updateMediaInput) (json.RawMessage, error) {
			return client.do(ctx, "PATCH", "/media/"+in.ID, nil, bodyWithoutID(in))
		})

	register(s, "agentic_cms_go_delete_media", "Delete a media asset",
		"Permanently delete a media asset (and its stored file) by id.",
		destructiveAnn, func(ctx context.Context, in idInput) (json.RawMessage, error) {
			return client.do(ctx, "DELETE", "/media/"+in.ID, nil, nil)
		})
}
