package mcptools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/putcho01/atlassian-cli/internal/config"
	"github.com/putcho01/atlassian-cli/internal/jira"
	"github.com/putcho01/atlassian-cli/internal/mdconv"
)

// --- Input types ---

type emptyInput struct{}

type getIssueInput struct {
	Key  string `json:"key" jsonschema:"The Jira issue key (e.g. PROJ-123)"`
	Full bool   `json:"full,omitempty" jsonschema:"Return the raw API issue with every field instead of the compact view"`
}

type searchIssuesInput struct {
	JQL        string `json:"jql" jsonschema:"JQL query string"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Maximum number of results (default 50)"`
	Full       bool   `json:"full,omitempty" jsonschema:"Return raw API issues with all navigable fields instead of compact rows"`
}

type createIssueInput struct {
	ProjectKey  string `json:"project_key" jsonschema:"Project key (e.g. PROJ)"`
	Summary     string `json:"summary" jsonschema:"Issue summary"`
	IssueType   string `json:"issue_type,omitempty" jsonschema:"Issue type (default Task)"`
	Description string `json:"description,omitempty" jsonschema:"Issue description in Markdown (Jira wiki markup when raw is true)"`
	Raw         bool   `json:"raw,omitempty" jsonschema:"Send the description as Jira wiki markup without converting from Markdown"`
	Assignee    string `json:"assignee,omitempty" jsonschema:"Assignee username"`
	Priority    string `json:"priority,omitempty" jsonschema:"Priority name"`
}

type listCommentsInput struct {
	Key string `json:"key" jsonschema:"The Jira issue key"`
}

type addCommentInput struct {
	Key  string `json:"key" jsonschema:"The Jira issue key"`
	Body string `json:"body" jsonschema:"Comment text in Markdown (Jira wiki markup when raw is true)"`
	Raw  bool   `json:"raw,omitempty" jsonschema:"Send the body as Jira wiki markup without converting from Markdown"`
}

type apiRequestInput struct {
	Method string            `json:"method,omitempty" jsonschema:"HTTP method (default GET)"`
	Path   string            `json:"path" jsonschema:"REST path relative to the configured base URL, starting with / (e.g. /rest/api/2/field)"`
	Query  map[string]string `json:"query,omitempty" jsonschema:"Query parameters"`
	Body   string            `json:"body,omitempty" jsonschema:"JSON request body"`
}

type updateIssueInput struct {
	Key    string         `json:"key" jsonschema:"The Jira issue key"`
	Fields map[string]any `json:"fields" jsonschema:"Fields to update as key-value pairs"`
}

type deleteIssueInput struct {
	Key            string `json:"key" jsonschema:"The Jira issue key to delete"`
	DeleteSubtasks bool   `json:"delete_subtasks,omitempty" jsonschema:"Also delete its subtasks (required when it has any)"`
}

type transitionIssueInput struct {
	Key    string `json:"key" jsonschema:"The Jira issue key"`
	Status string `json:"status" jsonschema:"Target status name"`
}

type getTransitionsInput struct {
	Key string `json:"key" jsonschema:"The Jira issue key"`
}

type getSubtasksInput struct {
	Key string `json:"key" jsonschema:"The parent Jira issue key"`
}

type listAttachmentsInput struct {
	Key string `json:"key" jsonschema:"The Jira issue key"`
}

type getAttachmentInput struct {
	ID string `json:"id" jsonschema:"The attachment ID"`
}

type downloadAttachmentInput struct {
	Key  string `json:"key" jsonschema:"The Jira issue key"`
	Ref  string `json:"ref" jsonschema:"Attachment filename or ID to download"`
	Path string `json:"path" jsonschema:"Local file path to write the attachment to"`
}

// --- Registration ---

func registerJiraTools(s *mcp.Server, enabledGroups []string) {
	maybeAdd(s, enabledGroups, "jira_get_myself", "Get the currently authenticated Jira user",
		func(ctx context.Context, req *mcp.CallToolRequest, input emptyInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			user, err := client.GetMyself(ctx)
			if err != nil {
				return nil, nil, err
			}
			return textResult(user)
		})

	maybeAdd(s, enabledGroups, "jira_get_issue", "Get a Jira issue by key: summary, status, people, description (Markdown), custom fields by name, links, subtasks, attachments and latest comments",
		func(ctx context.Context, req *mcp.CallToolRequest, input getIssueInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			if input.Full {
				issue, err := client.GetIssue(ctx, input.Key)
				if err != nil {
					return nil, nil, err
				}
				return textResult(issue)
			}
			issue, err := client.GetIssueExpanded(ctx, input.Key, "renderedFields", "names")
			if err != nil {
				return nil, nil, err
			}
			return textResult(compactIssueFrom(issue))
		})

	maybeAdd(s, enabledGroups, "jira_search_issues", "Search Jira issues using JQL query language",
		func(ctx context.Context, req *mcp.CallToolRequest, input searchIssuesInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			limit := input.MaxResults
			if limit <= 0 {
				limit = 50
			}
			if input.Full {
				issues, err := client.SearchIssuesAll(ctx, input.JQL, nil, limit)
				if err != nil {
					return nil, nil, err
				}
				return textResult(issues)
			}
			issues, err := client.SearchIssuesAll(ctx, input.JQL, compactSearchFields, limit)
			if err != nil {
				return nil, nil, err
			}
			hits := make([]compactSearchHit, len(issues))
			for i, is := range issues {
				hits[i] = compactSearchHitFrom(is)
			}
			return textResult(hits)
		})

	maybeAdd(s, enabledGroups, "jira_create_issue", "Create a new Jira issue",
		func(ctx context.Context, req *mcp.CallToolRequest, input createIssueInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			issueType := input.IssueType
			if issueType == "" {
				issueType = "Task"
			}
			createInput := &jira.CreateIssueInput{
				ProjectKey:  input.ProjectKey,
				Summary:     input.Summary,
				IssueType:   issueType,
				Description: jiraMarkup(input.Description, input.Raw),
				Assignee:    input.Assignee,
				Priority:    input.Priority,
			}
			issue, err := client.CreateIssue(ctx, createInput)
			if err != nil {
				return nil, nil, err
			}
			return textResult(issue)
		})

	maybeAdd(s, enabledGroups, "jira_update_issue", "Update fields on an existing Jira issue",
		func(ctx context.Context, req *mcp.CallToolRequest, input updateIssueInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			if err := client.UpdateIssue(ctx, input.Key, input.Fields); err != nil {
				return nil, nil, err
			}
			return textResult(map[string]string{"message": "Issue " + input.Key + " updated successfully"})
		})

	maybeAdd(s, enabledGroups, "jira_delete_issue", "Delete a Jira issue",
		func(ctx context.Context, req *mcp.CallToolRequest, input deleteIssueInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			if err := client.DeleteIssue(ctx, input.Key, input.DeleteSubtasks); err != nil {
				return nil, nil, err
			}
			return textResult(map[string]string{"message": "Issue " + input.Key + " deleted"})
		})

	maybeAdd(s, enabledGroups, "jira_get_transitions", "Get available transitions for a Jira issue",
		func(ctx context.Context, req *mcp.CallToolRequest, input getTransitionsInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			transitions, err := client.GetTransitions(ctx, input.Key)
			if err != nil {
				return nil, nil, err
			}
			return textResult(transitions)
		})

	maybeAdd(s, enabledGroups, "jira_transition_issue", "Transition a Jira issue to a new status",
		func(ctx context.Context, req *mcp.CallToolRequest, input transitionIssueInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			if err := client.TransitionIssue(ctx, input.Key, input.Status); err != nil {
				return nil, nil, err
			}
			return textResult(map[string]string{"message": "Issue " + input.Key + " transitioned to " + input.Status})
		})

	maybeAdd(s, enabledGroups, "jira_get_subtasks", "Get subtasks of a Jira issue",
		func(ctx context.Context, req *mcp.CallToolRequest, input getSubtasksInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			subtasks, err := client.GetSubtasks(ctx, input.Key)
			if err != nil {
				return nil, nil, err
			}
			return textResult(subtasks)
		})

	maybeAdd(s, enabledGroups, "jira_list_comments", "List the comments on a Jira issue (bodies as Markdown)",
		func(ctx context.Context, req *mcp.CallToolRequest, input listCommentsInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			comments, err := client.ListComments(ctx, input.Key)
			if err != nil {
				return nil, nil, err
			}
			out := make([]compactComment, len(comments))
			for i, c := range comments {
				out[i] = compactCommentFrom(c)
			}
			return textResult(out)
		})

	maybeAdd(s, enabledGroups, "jira_add_comment", "Add a comment to a Jira issue; the body is Markdown",
		func(ctx context.Context, req *mcp.CallToolRequest, input addCommentInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			comment, err := client.AddComment(ctx, input.Key, jiraMarkup(input.Body, input.Raw))
			if err != nil {
				return nil, nil, err
			}
			return textResult(map[string]string{"message": "Comment added to " + input.Key, "id": comment.ID})
		})

	maybeAdd(s, enabledGroups, "jira_api_request", "Send an authenticated request to any Jira REST endpoint (escape hatch for operations without a dedicated tool)",
		func(ctx context.Context, req *mcp.CallToolRequest, input apiRequestInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			return apiRequest(ctx, client, input)
		})

	maybeAdd(s, enabledGroups, "jira_list_attachments", "List attachments on a Jira issue",
		func(ctx context.Context, req *mcp.CallToolRequest, input listAttachmentsInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			attachments, err := client.GetAttachments(ctx, input.Key)
			if err != nil {
				return nil, nil, err
			}
			return textResult(attachments)
		})

	maybeAdd(s, enabledGroups, "jira_get_attachment", "Get metadata for a single Jira attachment by ID",
		func(ctx context.Context, req *mcp.CallToolRequest, input getAttachmentInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			attachment, err := client.GetAttachment(ctx, input.ID)
			if err != nil {
				return nil, nil, err
			}
			return textResult(attachment)
		})

	maybeAdd(s, enabledGroups, "jira_download_attachment", "Download a Jira issue attachment to a local file",
		func(ctx context.Context, req *mcp.CallToolRequest, input downloadAttachmentInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			attachments, err := client.GetAttachments(ctx, input.Key)
			if err != nil {
				return nil, nil, err
			}
			var match *jira.Attachment
			for i := range attachments {
				if attachments[i].ID == input.Ref || attachments[i].Filename == input.Ref {
					match = &attachments[i]
					break
				}
			}
			if match == nil {
				return nil, nil, fmt.Errorf("no attachment matching %q found on %s", input.Ref, input.Key)
			}
			body, err := client.DownloadAttachment(ctx, match.Content)
			if err != nil {
				return nil, nil, err
			}
			defer body.Close()
			f, err := os.Create(input.Path)
			if err != nil {
				return nil, nil, err
			}
			defer f.Close()
			n, err := io.Copy(f, body)
			if err != nil {
				return nil, nil, err
			}
			return textResult(map[string]any{
				"message":  "Attachment downloaded",
				"filename": match.Filename,
				"path":     input.Path,
				"bytes":    n,
			})
		})
}

func newJiraClient() (*jira.Client, error) {
	cfg, err := config.LoadJiraConfig()
	if err != nil {
		return nil, err
	}
	return jira.NewClient(cfg), nil
}

// jiraMarkup converts Markdown to Jira wiki markup unless raw is set.
func jiraMarkup(text string, raw bool) string {
	if raw || strings.TrimSpace(text) == "" {
		return text
	}
	return mdconv.ToJiraWiki(text)
}

// rawRequester is implemented by the Jira and Confluence clients.
type rawRequester interface {
	Raw(ctx context.Context, method, path string, query url.Values, body []byte) (int, []byte, error)
}

// apiRequest performs a raw REST request and returns the status with the
// response body (decoded when it is JSON).
func apiRequest(ctx context.Context, c rawRequester, input apiRequestInput) (*mcp.CallToolResult, any, error) {
	if !strings.HasPrefix(input.Path, "/") {
		return nil, nil, fmt.Errorf("path must start with / and be relative to the configured URL")
	}
	method := strings.ToUpper(input.Method)
	if method == "" {
		method = "GET"
	}
	query := url.Values{}
	for k, v := range input.Query {
		query.Set(k, v)
	}
	status, body, err := c.Raw(ctx, method, input.Path, query, []byte(input.Body))
	if err != nil {
		return nil, nil, err
	}
	var decoded any
	if json.Unmarshal(body, &decoded) != nil {
		decoded = string(body)
	}
	return textResult(map[string]any{"status": status, "body": decoded})
}

func textResult(v any) (*mcp.CallToolResult, any, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}, nil, nil
}
