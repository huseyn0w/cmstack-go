// Package mcpserver is a standalone MCP (Model Context Protocol) server that
// exposes the CMStack-Go REST API (/api/v1) as 48 scoped tools. It is a THIN,
// authenticated HTTP client of the REST API, not a second source of truth: it
// carries a service-account bearer API token and calls the existing endpoints,
// so every tool invocation is re-authorized SERVER-SIDE by the DB-backed RBAC on
// the REST routes. The MCP server itself holds no authorization logic — a tool
// is only as powerful as the token user's role.
//
// Authorization model: a service-account bearer token with per-call server-side
// permission re-check (the "OAuth 2.1 auth floor" as realized on a mature stack).
// Full interactive OAuth 2.1 is a DEFERRED enhancement.
package mcpserver

import (
	"fmt"
	"os"
	"strings"
)

// Config holds the MCP server's runtime configuration, read from the
// environment. BaseURL is the REST API origin (no /api/v1 suffix); Token is the
// bearer API token minted via cmd/apitoken.
type Config struct {
	// BaseURL is the REST API origin, e.g. "http://localhost:8090". The client
	// appends "/api/v1/..." to it. A trailing slash is trimmed.
	BaseURL string
	// Token is the plaintext bearer API token sent as "Authorization: Bearer ..."
	// on every request. It is never logged.
	Token string
}

// Environment variable names read by LoadConfig.
const (
	// EnvBaseURL names the env var carrying the REST API origin.
	EnvBaseURL = "MCP_API_BASE_URL"
	// EnvToken names the env var carrying the bearer API token.
	EnvToken = "MCP_API_TOKEN"
	// DefaultBaseURL is the fallback REST API origin when EnvBaseURL is unset.
	DefaultBaseURL = "http://localhost:8090"
)

// LoadConfig reads the MCP server configuration from the environment. BaseURL
// defaults to DefaultBaseURL. Token is REQUIRED: when it is empty LoadConfig
// fails fast with an actionable error pointing at cmd/apitoken, so the server
// never starts silently unauthenticated.
func LoadConfig() (Config, error) {
	base := strings.TrimSpace(os.Getenv(EnvBaseURL))
	if base == "" {
		base = DefaultBaseURL
	}
	base = strings.TrimRight(base, "/")

	token := strings.TrimSpace(os.Getenv(EnvToken))
	if token == "" {
		return Config{}, fmt.Errorf(
			"%s is required: mint one with `go run ./cmd/apitoken -email <user> -name mcp` and export it",
			EnvToken,
		)
	}

	return Config{BaseURL: base, Token: token}, nil
}
