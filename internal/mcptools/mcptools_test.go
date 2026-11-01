package mcptools

import (
	"context"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/putcho01/atlassian-cli/internal/jira"
)

// listTools starts a server with the given groups and returns the names of
// the tools a client sees.
func listTools(t *testing.T, groups []string) []string {
	t.Helper()
	ctx := context.Background()
	clientT, serverT := mcp.NewInMemoryTransports()
	if _, err := NewServer("test", groups).Connect(ctx, serverT, nil); err != nil {
		t.Fatalf("server connect: %v", err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer session.Close()
	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func TestToolGroups(t *testing.T) {
	t.Parallel()

	var all, optIn []string
	for g, tools := range toolGroups {
		if optInGroups[g] {
			optIn = append(optIn, tools...)
		} else {
			all = append(all, tools...)
		}
	}
	sort.Strings(all)
	sort.Strings(optIn)

	// Every grouped tool is registered, and the default set excludes opt-in groups.
	if diff := cmp.Diff(all, listTools(t, nil)); diff != "" {
		t.Errorf("default tools diff (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(optIn, listTools(t, OptInGroups())); diff != "" {
		t.Errorf("opt-in tools diff (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"jira_add_comment", "jira_list_comments"}, listTools(t, []string{"jira_comment"})); diff != "" {
		t.Errorf("jira_comment tools diff (-want +got):\n%s", diff)
	}
}

func TestCompactIssue(t *testing.T) {
	t.Parallel()
	is := &jira.Issue{
		Key: "P-1",
		Fields: jira.IssueFields{
			Summary:     "Sum",
			Description: "raw *wiki*",
			Status:      &jira.Status{Name: "Open", Self: "http://x/status/1"},
			Assignee:    &jira.User{DisplayName: "Ann", Self: "http://x/user"},
			IssueType:   &jira.IssueType{Name: "Bug"},
			Custom: map[string]any{
				"customfield_1": map[string]any{"value": "High"},
				"customfield_2": map[string]any{"self": "http://x"},
				"customfield_3": "unnamed",
			},
			IssueLinks: []jira.IssueLink{{Type: jira.IssueLinkType{Outward: "blocks"}, OutwardIssue: &jira.Issue{Key: "P-2"}}},
			Comment:    &jira.CommentPage{Comments: []jira.Comment{{ID: "9", Body: "raw", Author: &jira.User{DisplayName: "Bob"}}}, Total: 1},
		},
		RenderedFields: &jira.RenderedFields{
			Description: "<p><b>bold</b></p>",
			Comment:     &jira.CommentPage{Comments: []jira.Comment{{ID: "9", Body: "<p>html</p>"}}},
		},
		Names: map[string]string{"customfield_1": "Severity"},
	}

	got := compactIssueFrom(is)
	want := compactIssue{
		Key:           "P-1",
		Summary:       "Sum",
		Status:        "Open",
		Resolution:    "Unresolved",
		Type:          "Bug",
		Assignee:      "Ann",
		Description:   "**bold**",
		Fields:        map[string]any{"Severity": "High", "customfield_3": "unnamed"},
		Links:         []compactLink{{Relation: "blocks", compactIssueRef: compactIssueRef{Key: "P-2"}}},
		Comments:      []compactComment{{ID: "9", Author: "Bob", Body: "html"}},
		CommentsTotal: 1,
	}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(compactLink{})); diff != "" {
		t.Errorf("compactIssueFrom() diff (-want +got):\n%s", diff)
	}
}

func TestStorageBody(t *testing.T) {
	t.Parallel()
	if got, _ := storageBody("**b**", ""); got != "<p><strong>b</strong></p>" {
		t.Errorf("markdown body = %q", got)
	}
	if got, _ := storageBody("<p>x</p>", "storage"); got != "<p>x</p>" {
		t.Errorf("storage body = %q", got)
	}
	if _, err := storageBody("x", "wiki"); err == nil {
		t.Error("unknown format should fail")
	}
}

func TestJiraMarkup(t *testing.T) {
	t.Parallel()
	if got := jiraMarkup("**b**", false); got != "*b*\n" {
		t.Errorf("converted = %q", got)
	}
	if got := jiraMarkup("*b*", true); got != "*b*" {
		t.Errorf("raw = %q", got)
	}
}
