package bookmarks

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/jira"
)

// JiraLookup is what Describe needs from Jira; *jira.Client implements it.
type JiraLookup interface {
	GetIssue(ctx context.Context, key string) (*jira.Issue, error)
	SearchIssuesAll(ctx context.Context, jql string, fields []string, limit int) ([]jira.Issue, error)
	GetSprint(ctx context.Context, id int) (*jira.Sprint, error)
	IssueURL(key string) string
	BaseURL() string
}

// PageLookup is what Describe needs from Confluence; *confluence.Client
// implements it.
type PageLookup interface {
	GetPage(ctx context.Context, id string, expand []string) (*confluence.Page, error)
	WebURL(p *confluence.Page) string
}

// Describe looks the bookmarked item up, failing when it does not exist,
// and fills in its title, space and web link. The clients are created only
// for the product the bookmark belongs to.
func Describe(ctx context.Context, b *Bookmark, jiraClient func() (JiraLookup, error), pages func() (PageLookup, error)) error {
	if err := b.Validate(); err != nil {
		return err
	}
	if b.Kind == Page {
		client, err := pages()
		if err != nil {
			return err
		}
		p, err := client.GetPage(ctx, b.Key, []string{"space"})
		if err != nil {
			return err
		}
		b.Title, b.URL = p.Title, client.WebURL(p)
		if p.Space != nil {
			b.Space = p.Space.Key
		}
		return nil
	}

	client, err := jiraClient()
	if err != nil {
		return err
	}
	switch b.Kind {
	case Query:
		if _, err := client.SearchIssuesAll(ctx, b.JQL, []string{"summary"}, 1); err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		b.URL = strings.TrimRight(client.BaseURL(), "/") + "/issues/?jql=" + url.QueryEscape(b.JQL)
	case Sprint:
		id, _ := strconv.Atoi(b.Key) // checked by Validate
		sp, err := client.GetSprint(ctx, id)
		if err != nil {
			return err
		}
		b.Title = sp.Name
	default:
		is, err := client.GetIssue(ctx, strings.ToUpper(strings.TrimSpace(b.Key)))
		if err != nil {
			return err
		}
		b.Key, b.Title, b.URL = is.Key, is.Fields.Summary, client.IssueURL(is.Key)
	}
	return nil
}
