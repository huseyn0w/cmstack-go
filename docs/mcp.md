# MCP Server (`cmd/mcp`)

`cmd/mcp` is a standalone MCP (Model Context Protocol) server that exposes the
CMStack-Go REST API (`/api/v1`) as **48 scoped tools** mapping 1:1 to the REST
endpoints. It is a **thin, authenticated HTTP client** of the API, not a second
source of truth.

## Authorization model (read this)

The MCP server carries a **service-account bearer API token** and calls the
existing REST endpoints. Every tool invocation is therefore **re-authorized
server-side** by the DB-backed RBAC on the REST routes. The MCP server holds no
authorization logic of its own — a tool is only as powerful as the token user's
role (e.g. an Editor token cannot switch the active theme, which requires
Administrator).

This realizes the "OAuth 2.1 auth floor" on a mature stack: a service-account
bearer token with a per-call server-side permission re-check. **Full interactive
OAuth 2.1 is a deferred enhancement.**

The token is sent only as the `Authorization: Bearer …` header and is **never
logged** (startup logs only the API base URL). Logs go to **stderr** because
stdout is the MCP JSON-RPC stdio transport.

## 1. Mint a token

Use `cmd/apitoken` to mint a bearer token for an existing user. The plaintext is
printed exactly once — copy it immediately.

```sh
go run ./cmd/apitoken -email admin@example.com -name mcp
# optional expiry: -days 90 (0 = never expires)
```

The token inherits the RBAC permissions of that user, so pick the account whose
role should bound what the AI client can do.

## 2. Configure the environment

| Env var            | Required | Default                   | Meaning                                  |
| ------------------ | -------- | ------------------------- | ---------------------------------------- |
| `MCP_API_TOKEN`    | yes      | —                         | The bearer API token from step 1.        |
| `MCP_API_BASE_URL` | no       | `http://localhost:8090`   | REST API origin (no `/api/v1` suffix).   |

If `MCP_API_TOKEN` is unset the server **fails fast** with a message pointing at
`cmd/apitoken`.

## 3. Run

```sh
MCP_API_BASE_URL=http://localhost:8090 \
MCP_API_TOKEN=<paste-token> \
go run ./cmd/mcp
```

The server speaks the MCP **stdio** transport (standard for a locally-launched
MCP server).

## 4. Connect from an MCP client

A sample client config snippet (command + env):

```json
{
  "mcpServers": {
    "cmstack-go": {
      "command": "go",
      "args": ["run", "./cmd/mcp"],
      "env": {
        "MCP_API_BASE_URL": "http://localhost:8090",
        "MCP_API_TOKEN": "<paste-token>"
      }
    }
  }
}
```

For a built binary, replace `command`/`args` with the path to the compiled
`mcp` binary. Run the command from the repository root (or use an absolute
binary path) so `go run ./cmd/mcp` resolves.

## The 48 tools

Grouped by concern (all ids are prefixed `cmstack_go_`):

- **content (23):** `list_posts`, `get_post`, `get_post_revisions`,
  `create_post`, `update_post`, `publish_post`, `unpublish_post`, `delete_post`,
  `restore_post`, `list_pages`, `get_page`, `create_page`, `update_page`,
  `delete_page`, `restore_page`, `list_categories`, `create_category`,
  `update_category`, `delete_category`, `list_tags`, `create_tag`, `update_tag`,
  `delete_tag`
- **media (4):** `list_media`, `get_media`, `update_media`, `delete_media`
  (binary upload is intentionally not exposed over MCP)
- **comments (5):** `list_comments`, `approve_comment`, `mark_comment_spam`,
  `trash_comment`, `delete_comment`
- **settings (2):** `get_active_theme`, `set_active_theme`
- **seo (10):** `get_site_profile`, `update_site_profile`, `list_services`,
  `create_service`, `update_service`, `delete_service`, `list_faqs`,
  `create_faq`, `update_faq`, `delete_faq` (FAQs are service-scoped, so the FAQ
  tools take a `serviceId`)
- **users (4):** `list_users`, `list_roles`, `get_user`, `update_user` (no user
  delete over MCP)

Each tool carries the read-only / create / update / destructive annotation
hints, so clients can flag mutating and destructive operations.

## Implementation

The MCP library lives in `internal/mcpserver` (unit-testable, kept out of
`cmd`):

- `config.go` — reads `MCP_API_BASE_URL` / `MCP_API_TOKEN`.
- `client.go` — `APIClient`: sets the Bearer header + `Content-Type`, encodes
  the body, unwraps the `{"data":…}` success envelope, and maps a non-2xx
  `{"error":{code,message}}` into an `*APIError` (status + code + message).
- `tools.go` + per-group files (`content.go`, `media.go`, `comments.go`,
  `settings.go`, `seo.go`, `users.go`) — `RegisterAll` registers all 48 tools;
  each tool is a thin `input → HTTP → result` mapping.

Built with the official Go MCP SDK
[`github.com/modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk).
