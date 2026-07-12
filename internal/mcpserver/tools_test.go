package mcpserver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// wantToolIDs is the canonical, sorted set of the 48 tool ids RegisterAll must
// register. Any drift (missing, extra, or misnamed) fails the test.
var wantToolIDs = []string{
	// content (23)
	"agentic_cms_go_create_category",
	"agentic_cms_go_create_page",
	"agentic_cms_go_create_post",
	"agentic_cms_go_create_tag",
	"agentic_cms_go_delete_category",
	"agentic_cms_go_delete_page",
	"agentic_cms_go_delete_post",
	"agentic_cms_go_delete_tag",
	"agentic_cms_go_get_page",
	"agentic_cms_go_get_post",
	"agentic_cms_go_get_post_revisions",
	"agentic_cms_go_list_categories",
	"agentic_cms_go_list_pages",
	"agentic_cms_go_list_posts",
	"agentic_cms_go_list_tags",
	"agentic_cms_go_publish_post",
	"agentic_cms_go_restore_page",
	"agentic_cms_go_restore_post",
	"agentic_cms_go_unpublish_post",
	"agentic_cms_go_update_category",
	"agentic_cms_go_update_page",
	"agentic_cms_go_update_post",
	"agentic_cms_go_update_tag",
	// media (4)
	"agentic_cms_go_delete_media",
	"agentic_cms_go_get_media",
	"agentic_cms_go_list_media",
	"agentic_cms_go_update_media",
	// comments (5)
	"agentic_cms_go_approve_comment",
	"agentic_cms_go_delete_comment",
	"agentic_cms_go_list_comments",
	"agentic_cms_go_mark_comment_spam",
	"agentic_cms_go_trash_comment",
	// settings (2)
	"agentic_cms_go_get_active_theme",
	"agentic_cms_go_set_active_theme",
	// seo (10)
	"agentic_cms_go_create_faq",
	"agentic_cms_go_create_service",
	"agentic_cms_go_delete_faq",
	"agentic_cms_go_delete_service",
	"agentic_cms_go_get_site_profile",
	"agentic_cms_go_list_faqs",
	"agentic_cms_go_list_services",
	"agentic_cms_go_update_faq",
	"agentic_cms_go_update_service",
	"agentic_cms_go_update_site_profile",
	// users (4)
	"agentic_cms_go_get_user",
	"agentic_cms_go_list_roles",
	"agentic_cms_go_list_users",
	"agentic_cms_go_update_user",
}

func init() { sort.Strings(wantToolIDs) }

// connect wires an in-memory MCP client to a server with all tools registered
// against the given REST base URL and token.
func connect(t *testing.T, baseURL, token string) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	RegisterAll(server, New(baseURL, token, http.DefaultClient))

	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestRegisterAllRegistersExactly48Tools(t *testing.T) {
	cs := connect(t, "http://unused", "tok")

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	got := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
		if !strings.HasPrefix(tool.Name, "agentic_cms_go_") {
			t.Errorf("tool %q does not start with agentic_cms_go_", tool.Name)
		}
	}
	sort.Strings(got)

	if len(got) != 48 {
		t.Fatalf("registered %d tools, want 48\n%s", len(got), strings.Join(got, "\n"))
	}
	if len(wantToolIDs) != 48 {
		t.Fatalf("wantToolIDs has %d entries, want 48", len(wantToolIDs))
	}
	for i := range got {
		if got[i] != wantToolIDs[i] {
			t.Errorf("tool id mismatch at %d: got %q want %q", i, got[i], wantToolIDs[i])
		}
	}
}

// callRecorder records the method/path/body of the last REST call and replies
// with a fixed success envelope.
type callRecorder struct {
	method, path, query, body string
}

func newAPIStub(t *testing.T, rec *callRecorder, respBody string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.RawQuery
		b, _ := io.ReadAll(r.Body)
		rec.body = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// textOf extracts the first text-content string from a tool result.
func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res.IsError {
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				t.Fatalf("tool returned error: %s", tc.Text)
			}
		}
		t.Fatal("tool returned error")
	}
	if len(res.Content) == 0 {
		t.Fatal("no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is not text: %T", res.Content[0])
	}
	return tc.Text
}

func TestRepresentativeHandlers(t *testing.T) {
	rec := &callRecorder{}
	srv := newAPIStub(t, rec, `{"data":{"ok":true}}`)
	cs := connect(t, srv.URL, "tok")
	ctx := context.Background()

	cases := []struct {
		name       string
		tool       string
		args       map[string]any
		wantMethod string
		wantPath   string
		wantBody   string // "" means no body assertion
		wantQuery  string // "" means no query assertion
	}{
		{
			name: "list_posts", tool: "agentic_cms_go_list_posts",
			args:       map[string]any{"status": "PUBLISHED", "perPage": 5},
			wantMethod: "GET", wantPath: "/api/v1/posts",
			wantQuery: "perPage=5&status=PUBLISHED",
		},
		{
			name: "update_media", tool: "agentic_cms_go_update_media",
			args:       map[string]any{"id": "m1", "alt": "logo"},
			wantMethod: "PATCH", wantPath: "/api/v1/media/m1",
			wantBody: `{"alt":"logo"}`,
		},
		{
			name: "mark_comment_spam", tool: "agentic_cms_go_mark_comment_spam",
			args:       map[string]any{"id": "c9"},
			wantMethod: "POST", wantPath: "/api/v1/comments/c9/spam",
		},
		{
			name: "set_active_theme", tool: "agentic_cms_go_set_active_theme",
			args:       map[string]any{"theme": "editorial"},
			wantMethod: "PUT", wantPath: "/api/v1/settings/theme",
			wantBody: `{"theme":"editorial"}`,
		},
		{
			name: "create_faq", tool: "agentic_cms_go_create_faq",
			args:       map[string]any{"serviceId": "s1", "question": "Q?", "answer": "A."},
			wantMethod: "POST", wantPath: "/api/v1/services/s1/faqs",
			wantBody: `{"question":"Q?","answer":"A."}`,
		},
		{
			name: "update_user", tool: "agentic_cms_go_update_user",
			args:       map[string]any{"id": "u1", "roleId": "r2"},
			wantMethod: "PATCH", wantPath: "/api/v1/users/u1",
			wantBody: `{"roleId":"r2"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			*rec = callRecorder{}
			res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
			if err != nil {
				t.Fatalf("CallTool: %v", err)
			}
			txt := textOf(t, res)
			if !strings.Contains(txt, `"ok": true`) {
				t.Errorf("result text did not contain unwrapped data: %s", txt)
			}
			if rec.method != tc.wantMethod {
				t.Errorf("method = %q, want %q", rec.method, tc.wantMethod)
			}
			if rec.path != tc.wantPath {
				t.Errorf("path = %q, want %q", rec.path, tc.wantPath)
			}
			if tc.wantQuery != "" && rec.query != tc.wantQuery {
				t.Errorf("query = %q, want %q", rec.query, tc.wantQuery)
			}
			if tc.wantBody != "" && rec.body != tc.wantBody {
				t.Errorf("body = %q, want %q", rec.body, tc.wantBody)
			}
		})
	}
}
