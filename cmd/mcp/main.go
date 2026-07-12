// Command mcp is the standalone MCP (Model Context Protocol) server for
// Agentic CMS-Go. It exposes the REST API (/api/v1) as 48 scoped tools over stdio,
// acting as a THIN, authenticated HTTP client: it carries a service-account
// bearer API token and calls the existing endpoints, so every tool invocation is
// re-authorized SERVER-SIDE by the DB-backed RBAC on the REST routes. The MCP
// server holds no authorization logic of its own.
//
// Run:
//
//	MCP_API_BASE_URL=http://localhost:8090 \
//	MCP_API_TOKEN=$(go run ./cmd/apitoken -email admin@example.com -name mcp) \
//	go run ./cmd/mcp
//
// Logs go to STDERR (stdout is the MCP JSON-RPC transport). The token is never
// logged. Full interactive OAuth 2.1 is a deferred enhancement.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/huseyn0w/agentic-cms-go/internal/mcpserver"
)

// version is the reported MCP server version.
const version = "0.1.0"

func main() {
	if err := run(); err != nil {
		slog.Error("mcp server exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// Logs MUST go to stderr: stdout carries the MCP JSON-RPC stdio transport.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := mcpserver.LoadConfig()
	if err != nil {
		return err
	}

	// Graceful shutdown: cancel the server run on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := mcpserver.New(cfg.BaseURL, cfg.Token, nil)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "agentic-cms-go-mcp",
		Version: version,
	}, nil)
	mcpserver.RegisterAll(server, client)

	// The token is deliberately NOT logged.
	logger.Info("mcp server starting", "apiBaseURL", cfg.BaseURL, "transport", "stdio")

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return err
	}
	logger.Info("mcp server stopped cleanly")
	return nil
}
