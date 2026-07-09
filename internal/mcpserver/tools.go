package mcpserver

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// characterLimit caps a tool's text result so a large list never floods the
// client's context window; over it, the tool returns narrowing guidance instead.
const characterLimit = 25_000

// bp returns a pointer to b. The MCP SDK's ToolAnnotations use *bool so an unset
// hint is distinguishable from an explicit false.
func bp(b bool) *bool { return &b }

// Annotation presets describing how each kind of tool behaves, ported from the
// TypeScript reference (read-only / create / update / destructive hints).
var (
	// readAnn marks a read-only, idempotent tool.
	readAnn = &mcp.ToolAnnotations{
		ReadOnlyHint:    true,
		DestructiveHint: bp(false),
		IdempotentHint:  true,
		OpenWorldHint:   bp(true),
	}
	// createAnn marks a non-idempotent creating tool.
	createAnn = &mcp.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: bp(false),
		IdempotentHint:  false,
		OpenWorldHint:   bp(true),
	}
	// updateAnn marks an idempotent, non-destructive mutating tool.
	updateAnn = &mcp.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: bp(false),
		IdempotentHint:  true,
		OpenWorldHint:   bp(true),
	}
	// destructiveAnn marks an idempotent destructive tool (delete/trash).
	destructiveAnn = &mcp.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: bp(true),
		IdempotentHint:  true,
		OpenWorldHint:   bp(true),
	}
)

// idInput is the shared single-id input used by get/delete/action tools.
type idInput struct {
	ID string `json:"id" jsonschema:"the resource id"`
}

// emptyInput is used by tools that take no parameters.
type emptyInput struct{}

// toolResult wraps raw API JSON into an MCP text-content result. A nil result
// (e.g. a 204 from a delete) reports success; an oversized result returns
// narrowing guidance instead of the full payload.
func toolResult(raw json.RawMessage) (*mcp.CallToolResult, error) {
	if len(raw) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Done. The API returned no content (success)."}},
		}, nil
	}

	var pretty json.RawMessage
	if buf, err := json.MarshalIndent(raw, "", "  "); err == nil {
		pretty = buf
	} else {
		pretty = raw
	}

	if len(pretty) > characterLimit {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{
			Text: "The response is too large to return in full. Narrow the request with filters or pagination (e.g. a smaller perPage, a status filter, or a specific id).",
		}}}, nil
	}

	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(pretty)}}}, nil
}

// errorResult turns a thrown error into an isError tool result so the model can
// see the failure and self-correct (mirrors the TS errors.ts behavior).
func errorResult(err error) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}, nil
}

// respond runs an API call and formats its result (or error) as a
// CallToolResult. The (result, output, error) shape matches ToolHandlerFor;
// output is always nil since these tools return unstructured JSON text.
func respond(raw json.RawMessage, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		res, e := errorResult(err)
		return res, nil, e
	}
	res, e := toolResult(raw)
	return res, nil, e
}

// register is a small helper that adds a typed tool to the server with the given
// name, title, description, and annotations. It keeps each registration a single
// readable line at the call site.
func register[In any](
	s *mcp.Server,
	name, title, description string,
	ann *mcp.ToolAnnotations,
	handler func(ctx context.Context, in In) (json.RawMessage, error),
) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        name,
		Title:       title,
		Description: description,
		Annotations: ann,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		return respond(handler(ctx, in))
	})
}

// setIfNotNil sets q[key] = *v when v is non-nil (used to build optional query
// strings without emitting empty params).
func setIfNotNil[T any](q url.Values, key string, v *T) {
	if v == nil {
		return
	}
	q.Set(key, toStr(*v))
}

// toStr renders a supported scalar into its query-string form.
func toStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return itoa(t)
	default:
		return ""
	}
}

// itoa is a tiny int→string without pulling strconv into every call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// RegisterAll registers all 48 CMStack-Go MCP tools onto server, each backed by
// client. The tools map 1:1 to the REST API; every id is prefixed cmstack_go_.
func RegisterAll(server *mcp.Server, client *APIClient) {
	registerContentTools(server, client)
	registerMediaTools(server, client)
	registerCommentTools(server, client)
	registerSettingsTools(server, client)
	registerSeoTools(server, client)
	registerUserTools(server, client)
}
