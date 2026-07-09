package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// setThemeInput is the body for set_active_theme.
type setThemeInput struct {
	Theme string `json:"theme" jsonschema:"the theme slug to activate (e.g. \"editorial\")"`
}

// registerSettingsTools registers the 2 settings tools. The active public theme
// is gated by the Setting RBAC subject (Administrator only).
func registerSettingsTools(s *mcp.Server, client *APIClient) {
	register(s, "cmstack_go_get_active_theme", "Get the active theme",
		"Get the active public theme id (the activeTheme setting). Returns { activeTheme }.",
		readAnn, func(ctx context.Context, _ emptyInput) (json.RawMessage, error) {
			return client.do(ctx, "GET", "/settings/theme", nil, nil)
		})

	register(s, "cmstack_go_set_active_theme", "Set the active theme",
		"Set the active public theme by id (a slug, e.g. \"editorial\"). Requires Administrator. Returns the updated { activeTheme }.",
		updateAnn, func(ctx context.Context, in setThemeInput) (json.RawMessage, error) {
			return client.do(ctx, "PUT", "/settings/theme", nil, in)
		})
}
