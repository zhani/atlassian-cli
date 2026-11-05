package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/zhani/atlassian-cli/internal/bookmarks"
	"github.com/zhani/atlassian-cli/internal/browser"
	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/tui"
	"github.com/zhani/atlassian-cli/internal/xref"
)

// The issue browser and the page browser open each other for the links
// between Jira and Confluence (l on an issue lists its pages, L on a page
// its issues); each side is wired only when that service is configured.

// withPageLinks lets the issue browser open the Confluence pages an issue
// links to.
func withPageLinks(opts *tui.Options) {
	client, err := newConfluenceClient()
	if err != nil {
		return
	}
	opts.PageBases = []string{client.BaseURL()}
	opts.OpenPage = func(ctx context.Context, ref xref.PageRef) error {
		id, err := resolvePage(ctx, client, ref)
		if err != nil {
			return err
		}
		o := pageTreeOptions(client)
		o.PageID, o.Space = id, ref.Space
		return tui.RunPageTree(ctx, o)
	}
}

// resolvePage returns the ID of a referenced page, looking up a
// /display/SPACE/Title reference by title.
func resolvePage(ctx context.Context, client *confluence.Client, ref xref.PageRef) (string, error) {
	if ref.ID != "" {
		return ref.ID, nil
	}
	pages, err := client.SearchCQL(ctx, fmt.Sprintf("space = %s AND title = %s", cqlQuote(ref.Space), cqlQuote(ref.Title)), 1)
	if err != nil {
		return "", err
	}
	if len(pages) == 0 {
		return "", fmt.Errorf("no page %q in space %s", ref.Title, ref.Space)
	}
	return pages[0].ID, nil
}

func cqlQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// pageTreeOptions are the page browser's options, including opening the
// Jira issues a page shows or links to when Jira is configured.
func pageTreeOptions(client *confluence.Client) tui.TreeOptions {
	opts := tui.TreeOptions{Source: client, OpenURL: browser.Open, Copy: tui.CopyToClipboard}
	if store, err := bookmarks.Open(); err == nil {
		opts.Bookmarks = store
	}
	jc, err := newJiraClient()
	if err != nil {
		return opts
	}
	opts.IssueBases = []string{jc.BaseURL()}
	opts.SearchIssues = jc.SearchRaw
	opts.OpenIssue = func(ctx context.Context, key string) error {
		jql := fmt.Sprintf("key = %q", key)
		issues, err := jc.SearchIssuesAll(ctx, jql, nil, 1)
		if err != nil {
			return err
		}
		if len(issues) == 0 {
			return fmt.Errorf("%s not found", key)
		}
		o := browserOptions(jc, 50)
		o.JQL, o.Issues, o.Caption, o.OpenDetail = jql, issues, key, key
		return tui.RunIssueList(ctx, o)
	}
	return opts
}
