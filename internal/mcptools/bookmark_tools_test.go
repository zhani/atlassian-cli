package mcptools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

func TestBookmarkTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/rest/api/2/issue/A-1":
			w.Write([]byte(`{"key":"A-1","fields":{"summary":"Fix login"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errorMessages":["Issue does not exist"]}`))
		}
	}))
	defer srv.Close()
	t.Setenv("JIRA_URL", srv.URL)
	t.Setenv("JIRA_PERSONAL_TOKEN", "x")
	t.Setenv("ATLASSIAN_BOOKMARKS", filepath.Join(t.TempDir(), "bm.yaml"))

	ctx := context.Background()
	clientT, serverT := mcp.NewInMemoryTransports()
	if _, err := NewServer("test", []string{"bookmarks"}).Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	if out, isErr := callTool(t, session, "bookmark_add", map[string]any{"kind": "issue", "key": "a-1"}); isErr || !strings.Contains(out, "A-1 Fix login") {
		t.Fatalf("add: %s", out)
	}
	if out, isErr := callTool(t, session, "bookmark_add", map[string]any{"kind": "issue", "key": "NOPE-1"}); !isErr {
		t.Errorf("adding a missing issue succeeded: %s", out)
	}
	out, _ := callTool(t, session, "bookmark_list", map[string]any{"kind": "issue"})
	if !strings.Contains(out, `"key": "A-1"`) || !strings.Contains(out, `"title": "Fix login"`) || !strings.Contains(out, `"n": 1`) {
		t.Errorf("list: %s", out)
	}
	if out, isErr := callTool(t, session, "bookmark_remove", map[string]any{"refs": []string{"A-1"}}); isErr || !strings.Contains(out, "Removed") {
		t.Errorf("remove: %s", out)
	}
	if out, _ := callTool(t, session, "bookmark_list", nil); strings.TrimSpace(out) != "[]" {
		t.Errorf("list after remove: %s", out)
	}
}
