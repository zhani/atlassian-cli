package jira

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/zhani/atlassian-cli/internal/config"
	"github.com/zhani/atlassian-cli/internal/httpclient"
)

// Client is a Jira REST API v2 client.
type Client struct {
	http  *httpclient.Client
	cloud bool
}

// NewClient creates a new Client from the given configuration.
func NewClient(cfg *config.AtlassianConfig) *Client {
	return &Client{
		http:  httpclient.NewFromConfig(cfg.URL, cfg.Email, cfg.Token),
		cloud: isCloud(cfg),
	}
}

// isCloud reports whether the configuration points at Jira Cloud. Cloud uses
// Basic auth with an email, or is hosted under atlassian.net.
func isCloud(cfg *config.AtlassianConfig) bool {
	if cfg.Email != "" {
		return true
	}
	u, err := url.Parse(cfg.URL)
	return err == nil && strings.HasSuffix(u.Hostname(), ".atlassian.net")
}

func (c *Client) BaseURL() string { return c.http.BaseURL() }

// Raw performs an arbitrary authenticated REST request; see httpclient.Raw.
func (c *Client) Raw(ctx context.Context, method, path string, query url.Values, body []byte) (int, []byte, error) {
	return c.http.Raw(ctx, method, path, query, body)
}

// IssueURL returns the browser URL of an issue.
func (c *Client) IssueURL(key string) string {
	return c.http.BaseURL() + "/browse/" + key
}

func (c *Client) GetMyself(ctx context.Context) (*User, error) {
	var user User
	err := c.http.Do(ctx, "GET", "/rest/api/2/myself", nil, nil, &user)
	return &user, err
}

func (c *Client) GetIssue(ctx context.Context, key string) (*Issue, error) {
	var issue Issue
	err := c.http.Do(ctx, "GET", "/rest/api/2/issue/"+url.PathEscape(key), nil, nil, &issue)
	return &issue, err
}

// GetIssueRendered returns an issue together with the HTML-rendered versions
// of its text fields (description, comments, custom text fields) and the
// display names of its fields.
func (c *Client) GetIssueRendered(ctx context.Context, key string) (*Issue, error) {
	return c.GetIssueExpanded(ctx, key, "renderedFields", "names")
}

// GetIssueExpanded returns an issue with the given expansions, e.g.
// "renderedFields" or "names".
func (c *Client) GetIssueExpanded(ctx context.Context, key string, expand ...string) (*Issue, error) {
	q := url.Values{}
	if len(expand) > 0 {
		q.Set("expand", strings.Join(expand, ","))
	}
	var issue Issue
	err := c.http.Do(ctx, "GET", "/rest/api/2/issue/"+url.PathEscape(key), q, nil, &issue)
	return &issue, err
}

// searchPageSize is the page size requested from the search endpoints; Jira
// caps it at 100 on Cloud and usually lower on Server/DC.
const searchPageSize = 100

// SearchIssuesAll runs a JQL query and follows pagination until limit issues
// have been collected or the results are exhausted. A limit <= 0 means no
// limit. When fields is empty, all navigable fields are returned.
//
// Cloud uses the token-paginated /search/jql endpoint; Server/DC uses the
// offset-paginated /search endpoint.
func (c *Client) SearchIssuesAll(ctx context.Context, jql string, fields []string, limit int) ([]Issue, error) {
	return searchAll[Issue](ctx, c, jql, fields, limit, nil)
}

// SearchRaw is SearchIssuesAll with the issues as decoded JSON, every field
// as Jira sends it (for showing arbitrary columns), and expand (e.g.
// "renderedFields").
func (c *Client) SearchRaw(ctx context.Context, jql string, fields []string, limit int, expand ...string) ([]map[string]any, error) {
	return searchAll[map[string]any](ctx, c, jql, fields, limit, expand)
}

func searchAll[T any](ctx context.Context, c *Client, jql string, fields []string, limit int, expand []string) ([]T, error) {
	q := url.Values{}
	q.Set("jql", jql)
	if len(expand) > 0 {
		q.Set("expand", strings.Join(expand, ","))
	}
	if len(fields) > 0 {
		q.Set("fields", strings.Join(fields, ","))
	} else {
		q.Set("fields", "*navigable")
	}

	var issues []T
	for {
		pageSize := searchPageSize
		if limit > 0 && limit-len(issues) < pageSize {
			pageSize = limit - len(issues)
		}
		q.Set("maxResults", strconv.Itoa(pageSize))

		var page []T
		var done bool
		if c.cloud {
			var resp struct {
				Issues        []T    `json:"issues"`
				NextPageToken string `json:"nextPageToken"`
				IsLast        bool   `json:"isLast"`
			}
			if err := c.http.Do(ctx, "GET", "/rest/api/2/search/jql", q, nil, &resp); err != nil {
				return nil, err
			}
			page = resp.Issues
			done = resp.IsLast || resp.NextPageToken == ""
			q.Set("nextPageToken", resp.NextPageToken)
		} else {
			q.Set("startAt", strconv.Itoa(len(issues)))
			var resp struct {
				StartAt int `json:"startAt"`
				Total   int `json:"total"`
				Issues  []T `json:"issues"`
			}
			if err := c.http.Do(ctx, "GET", "/rest/api/2/search", q, nil, &resp); err != nil {
				return nil, err
			}
			page = resp.Issues
			done = resp.StartAt+len(resp.Issues) >= resp.Total
		}

		issues = append(issues, page...)
		if limit > 0 && len(issues) >= limit {
			return issues[:limit], nil
		}
		if done || len(page) == 0 {
			return issues, nil
		}
	}
}

func (c *Client) SearchIssues(ctx context.Context, jql string, maxResults int) (*SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	q := url.Values{}
	q.Set("jql", jql)
	q.Set("maxResults", strconv.Itoa(maxResults))

	var result SearchResult
	err := c.http.Do(ctx, "GET", "/rest/api/2/search", q, nil, &result)
	return &result, err
}

func (c *Client) CreateIssue(ctx context.Context, input *CreateIssueInput) (*Issue, error) {
	body := map[string]any{
		"fields": buildCreateFields(input, c.cloud),
	}

	var issue Issue
	err := c.http.Do(ctx, "POST", "/rest/api/2/issue", nil, body, &issue)
	return &issue, err
}

func (c *Client) UpdateIssue(ctx context.Context, key string, fields map[string]any) error {
	body := map[string]any{"fields": fields}
	return c.http.Do(ctx, "PUT", "/rest/api/2/issue/"+url.PathEscape(key), nil, body, nil)
}

// DeleteIssue deletes an issue. Issues with subtasks can only be deleted
// together with them (deleteSubtasks).
func (c *Client) DeleteIssue(ctx context.Context, key string, deleteSubtasks bool) error {
	var q url.Values
	if deleteSubtasks {
		q = url.Values{"deleteSubtasks": {"true"}}
	}
	return c.http.Do(ctx, "DELETE", "/rest/api/2/issue/"+url.PathEscape(key), q, nil, nil)
}

func (c *Client) GetTransitions(ctx context.Context, key string) ([]Transition, error) {
	var resp transitionsResponse
	err := c.http.Do(ctx, "GET", "/rest/api/2/issue/"+url.PathEscape(key)+"/transitions", nil, nil, &resp)
	return resp.Transitions, err
}

func (c *Client) TransitionIssue(ctx context.Context, key, statusName string) error {
	transitions, err := c.GetTransitions(ctx, key)
	if err != nil {
		return fmt.Errorf("get transitions: %w", err)
	}

	var transitionID string
	for _, t := range transitions {
		if strings.EqualFold(t.Name, statusName) || (t.To != nil && strings.EqualFold(t.To.Name, statusName)) {
			transitionID = t.ID
			break
		}
	}
	if transitionID == "" {
		available := make([]string, len(transitions))
		for i, t := range transitions {
			available[i] = t.Name
		}
		return fmt.Errorf("transition %q not found, available: %s", statusName, strings.Join(available, ", "))
	}
	return c.DoTransition(ctx, key, transitionID)
}

// DoTransition applies the transition with the given ID to an issue.
func (c *Client) DoTransition(ctx context.Context, key, transitionID string) error {
	body := map[string]any{
		"transition": map[string]string{"id": transitionID},
	}
	return c.http.Do(ctx, "POST", "/rest/api/2/issue/"+url.PathEscape(key)+"/transitions", nil, body, nil)
}

func (c *Client) ListComments(ctx context.Context, key string) ([]Comment, error) {
	q := url.Values{}
	q.Set("expand", "renderedBody")
	var resp CommentPage
	err := c.http.Do(ctx, "GET", "/rest/api/2/issue/"+url.PathEscape(key)+"/comment", q, nil, &resp)
	return resp.Comments, err
}

func (c *Client) AddComment(ctx context.Context, key, body string) (*Comment, error) {
	payload := map[string]any{"body": body}
	var comment Comment
	if err := c.http.Do(ctx, "POST", "/rest/api/2/issue/"+url.PathEscape(key)+"/comment", nil, payload, &comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

func (c *Client) GetSubtasks(ctx context.Context, key string) ([]Issue, error) {
	issue, err := c.GetIssue(ctx, key)
	if err != nil {
		return nil, err
	}
	return issue.Fields.Subtasks, nil
}

// GetAttachments returns the attachments on an issue.
func (c *Client) GetAttachments(ctx context.Context, key string) ([]Attachment, error) {
	q := url.Values{}
	q.Set("fields", "attachment")
	var issue Issue
	if err := c.http.Do(ctx, "GET", "/rest/api/2/issue/"+url.PathEscape(key), q, nil, &issue); err != nil {
		return nil, err
	}
	return issue.Fields.Attachment, nil
}

// GetAttachment returns metadata for a single attachment by ID.
func (c *Client) GetAttachment(ctx context.Context, id string) (*Attachment, error) {
	var att Attachment
	err := c.http.Do(ctx, "GET", "/rest/api/2/attachment/"+url.PathEscape(id), nil, nil, &att)
	return &att, err
}

// DownloadAttachment streams the raw content of an attachment from its content URL.
func (c *Client) DownloadAttachment(ctx context.Context, contentURL string) (io.ReadCloser, error) {
	resp, err := c.http.Get(ctx, contentURL, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// buildCreateFields builds the fields of a create request. Users are
// referenced by accountId on Cloud and by username on Server/DC.
func buildCreateFields(input *CreateIssueInput, cloud bool) map[string]any {
	fields := map[string]any{
		"project":   map[string]string{"key": input.ProjectKey},
		"summary":   input.Summary,
		"issuetype": map[string]string{"name": input.IssueType},
	}
	if input.Description != "" {
		fields["description"] = input.Description
	}
	if input.Assignee != "" {
		fields["assignee"] = userRef(input.Assignee, cloud)
	}
	if input.Priority != "" {
		fields["priority"] = map[string]string{"name": input.Priority}
	}
	if len(input.Labels) > 0 {
		fields["labels"] = input.Labels
	}
	for k, v := range input.CustomFields {
		fields[k] = v
	}
	return fields
}
