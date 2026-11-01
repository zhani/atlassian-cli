package mcptools

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/putcho01/atlassian-cli/internal/bookmarks"
)

type listBookmarksInput struct {
	Kind string `json:"kind,omitempty" jsonschema:"Only bookmarks of this kind: issue, query, feature, epic, sprint or page"`
}

type addBookmarkInput struct {
	Kind string `json:"kind" jsonschema:"issue, query, feature, epic, sprint or page"`
	Key  string `json:"key,omitempty" jsonschema:"Issue key, sprint ID or Confluence page ID (not for queries)"`
	JQL  string `json:"jql,omitempty" jsonschema:"JQL of a query bookmark"`
	Name string `json:"name,omitempty" jsonschema:"Name shown in lists (default: key and title; the JQL for queries)"`
}

type removeBookmarkInput struct {
	Refs []string `json:"refs" jsonschema:"Bookmarks to remove: list number (n), name, key/ID or JQL"`
}

// bookmarkStore is replaced in tests.
var bookmarkStore = bookmarks.Open

func registerBookmarkTools(s *mcp.Server, enabledGroups []string) {
	maybeAdd(s, enabledGroups, "bookmark_list",
		"List the user's bookmarks: the Jira issues, saved JQL queries, features, epics, sprints and Confluence pages they work with. "+
			"Use the key/JQL with the Jira and Confluence tools (e.g. jira_search_issues with a query's jql).",
		func(ctx context.Context, req *mcp.CallToolRequest, input listBookmarksInput) (*mcp.CallToolResult, any, error) {
			store, err := bookmarkStore()
			if err != nil {
				return nil, nil, err
			}
			list, err := store.List()
			if err != nil {
				return nil, nil, err
			}
			var want bookmarks.Kind
			if input.Kind != "" {
				if want, err = bookmarks.ParseKind(input.Kind); err != nil {
					return nil, nil, err
				}
			}
			type item struct {
				N int `json:"n"`
				bookmarks.Bookmark
			}
			out := []item{}
			for i, b := range list {
				if want == "" || b.Kind == want {
					b.Added = ""
					out = append(out, item{i + 1, b})
				}
			}
			return textResult(out)
		})

	maybeAdd(s, enabledGroups, "bookmark_add", "Bookmark a Jira issue, JQL query, feature, epic, sprint or Confluence page for the user",
		func(ctx context.Context, req *mcp.CallToolRequest, input addBookmarkInput) (*mcp.CallToolResult, any, error) {
			kind, err := bookmarks.ParseKind(input.Kind)
			if err != nil {
				return nil, nil, err
			}
			b := bookmarks.Bookmark{Kind: kind, Key: strings.TrimSpace(input.Key), JQL: strings.TrimSpace(input.JQL), Name: input.Name}
			if kind == bookmarks.Query {
				b.Key = ""
				if b.Name == "" {
					b.Name = b.JQL
				}
			}
			if err := bookmarks.Describe(ctx, &b,
				func() (bookmarks.JiraLookup, error) { return newJiraClient() },
				func() (bookmarks.PageLookup, error) { return newConfluenceClient() }); err != nil {
				return nil, nil, err
			}
			store, err := bookmarkStore()
			if err != nil {
				return nil, nil, err
			}
			if err := store.Add(b); err != nil {
				return nil, nil, err
			}
			return message("Bookmarked %s %s", b.Kind, b.Label())
		})

	maybeAdd(s, enabledGroups, "bookmark_remove", "Remove bookmarks",
		func(ctx context.Context, req *mcp.CallToolRequest, input removeBookmarkInput) (*mcp.CallToolResult, any, error) {
			store, err := bookmarkStore()
			if err != nil {
				return nil, nil, err
			}
			removed, err := store.Remove(input.Refs...)
			if err != nil {
				return nil, nil, err
			}
			names := make([]string, len(removed))
			for i, b := range removed {
				names[i] = b.Label()
			}
			return message("Removed %s", strings.Join(names, ", "))
		})
}
