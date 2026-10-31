package mcptools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/putcho01/atlassian-cli/internal/config"
	"github.com/putcho01/atlassian-cli/internal/jira"
)

// --- Input types ---

type assignIssueInput struct {
	Key  string `json:"key" jsonschema:"The Jira issue key"`
	User string `json:"user" jsonschema:"me, x (unassign), default (project default), or a username, email or display name"`
}

type linkIssuesInput struct {
	Key      string `json:"key" jsonschema:"The issue the link reads from"`
	OtherKey string `json:"other_key" jsonschema:"The issue the link points to"`
	Type     string `json:"type" jsonschema:"Link type name (Blocks) or phrase (blocks, is blocked by); the link reads: key <type> other_key"`
}

type unlinkIssuesInput struct {
	Key      string `json:"key" jsonschema:"One of the linked issues"`
	OtherKey string `json:"other_key" jsonschema:"The other linked issue"`
	Type     string `json:"type,omitempty" jsonschema:"Only remove links of this type name"`
}

type remoteLinkInput struct {
	Key   string `json:"key" jsonschema:"The Jira issue key"`
	URL   string `json:"url" jsonschema:"Link URL"`
	Title string `json:"title,omitempty" jsonschema:"Link title (default: the URL)"`
}

type worklogInput struct {
	Key       string `json:"key" jsonschema:"The Jira issue key"`
	TimeSpent string `json:"time_spent" jsonschema:"Duration in Jira format, e.g. 30m, 2h 30m, 1d"`
	Comment   string `json:"comment,omitempty" jsonschema:"Worklog comment in Markdown"`
	Started   string `json:"started,omitempty" jsonschema:"Start time as a Jira timestamp, e.g. 2026-01-31T09:00:00.000+0000 (default: now)"`
}

type searchUsersInput struct {
	Query string `json:"query" jsonschema:"Name, username or email to search for"`
}

type listFieldsInput struct {
	Filter     string `json:"filter,omitempty" jsonschema:"Only fields whose name or ID contains this text"`
	CustomOnly bool   `json:"custom_only,omitempty" jsonschema:"Only custom fields"`
}

type issueKeyInput struct {
	Key string `json:"key" jsonschema:"The Jira issue key"`
}

type cloneIssueInput struct {
	Key      string   `json:"key" jsonschema:"The issue to clone"`
	Summary  string   `json:"summary,omitempty" jsonschema:"Summary of the clone (default: same)"`
	Priority string   `json:"priority,omitempty" jsonschema:"Priority of the clone"`
	Assignee string   `json:"assignee,omitempty" jsonschema:"Assignee of the clone: me or a user"`
	Labels   []string `json:"labels,omitempty" jsonschema:"Labels of the clone (replaces the original labels)"`
}

type uploadInput struct {
	Target string `json:"target" jsonschema:"Jira issue key or Confluence page ID"`
	Path   string `json:"path" jsonschema:"Local file to upload"`
}

type deleteAttachmentInput struct {
	Target string `json:"target" jsonschema:"Jira issue key or Confluence page ID"`
	Ref    string `json:"ref" jsonschema:"Attachment filename/title or ID"`
}

type copyPageInput struct {
	PageID   string `json:"page_id" jsonschema:"Page to copy"`
	Title    string `json:"title,omitempty" jsonschema:"Title of the copy (default: Copy of <title>)"`
	Space    string `json:"space,omitempty" jsonschema:"Target space key (default: same space)"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"Parent page ID (default: the original's parent)"`
}

type movePageInput struct {
	PageID   string `json:"page_id" jsonschema:"Page to move"`
	ParentID string `json:"parent_id" jsonschema:"New parent page ID (same space)"`
}

type featureChildrenInput struct {
	Key       string   `json:"key" jsonschema:"The feature issue key"`
	LinkTypes []string `json:"link_types,omitempty" jsonschema:"Link phrases from the feature to its issues (default: JIRA_FEATURE_LINK_TYPES, else is implemented by / gets values from)"`
}

type featureChild struct {
	Relation string `json:"relation"`
	compactSearchHit
}

func message(format string, args ...any) (*mcp.CallToolResult, any, error) {
	return textResult(map[string]string{"message": fmt.Sprintf(format, args...)})
}

// --- Registration ---

func registerOperationTools(s *mcp.Server, enabledGroups []string) {
	maybeAdd(s, enabledGroups, "jira_assign_issue", "Assign a Jira issue to a user, or unassign it",
		func(ctx context.Context, req *mcp.CallToolRequest, input assignIssueInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			id, name := "", "nobody"
			switch strings.ToLower(input.User) {
			case "x", "":
			case "default":
				id, name = "-1", "the default assignee"
			default:
				user, err := client.ResolveUser(ctx, input.User)
				if err != nil {
					return nil, nil, err
				}
				id, name = client.UserID(user), user.DisplayName
			}
			if err := client.AssignIssue(ctx, input.Key, id); err != nil {
				return nil, nil, err
			}
			return message("Assigned %s to %s", input.Key, name)
		})

	maybeAdd(s, enabledGroups, "jira_search_users", "Search Jira users by name, username or email",
		func(ctx context.Context, req *mcp.CallToolRequest, input searchUsersInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			users, err := client.SearchUsers(ctx, input.Query)
			if err != nil {
				return nil, nil, err
			}
			type user struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Email string `json:"email,omitempty"`
			}
			out := make([]user, len(users))
			for i := range users {
				out[i] = user{ID: client.UserID(&users[i]), Name: users[i].DisplayName, Email: users[i].EmailAddress}
			}
			return textResult(out)
		})

	maybeAdd(s, enabledGroups, "jira_get_link_types", "List the issue link types (name, outward and inward phrase)",
		func(ctx context.Context, req *mcp.CallToolRequest, input emptyInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			types, err := client.GetLinkTypes(ctx)
			if err != nil {
				return nil, nil, err
			}
			return textResult(types)
		})

	maybeAdd(s, enabledGroups, "jira_link_issues", "Link two Jira issues so that the link reads: key <type> other_key",
		func(ctx context.Context, req *mcp.CallToolRequest, input linkIssuesInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			types, err := client.GetLinkTypes(ctx)
			if err != nil {
				return nil, nil, err
			}
			lt, swap, err := jira.ResolveLinkType(types, input.Type)
			if err != nil {
				return nil, nil, err
			}
			from, to, phrase := input.Key, input.OtherKey, lt.Outward
			if swap {
				from, to, phrase = to, from, lt.Inward
			}
			if err := client.LinkIssues(ctx, lt.Name, from, to); err != nil {
				return nil, nil, err
			}
			return message("Linked: %s %s %s", input.Key, phrase, input.OtherKey)
		})

	maybeAdd(s, enabledGroups, "jira_unlink_issues", "Remove the links between two Jira issues",
		func(ctx context.Context, req *mcp.CallToolRequest, input unlinkIssuesInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			n, err := client.UnlinkIssues(ctx, input.Key, input.OtherKey, input.Type)
			if err != nil {
				return nil, nil, err
			}
			return message("Removed %d link(s) between %s and %s", n, input.Key, input.OtherKey)
		})

	maybeAdd(s, enabledGroups, "jira_add_remote_link", "Add a web link to a Jira issue",
		func(ctx context.Context, req *mcp.CallToolRequest, input remoteLinkInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			if err := client.AddRemoteLink(ctx, input.Key, input.URL, input.Title); err != nil {
				return nil, nil, err
			}
			return message("Added link %s to %s", input.URL, input.Key)
		})

	maybeAdd(s, enabledGroups, "jira_add_worklog", "Log time spent on a Jira issue",
		func(ctx context.Context, req *mcp.CallToolRequest, input worklogInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			w, err := client.AddWorklog(ctx, input.Key, input.TimeSpent, jiraMarkup(input.Comment, false), input.Started)
			if err != nil {
				return nil, nil, err
			}
			return message("Logged %s on %s", w.TimeSpent, input.Key)
		})

	maybeAdd(s, enabledGroups, "jira_list_fields", "List Jira fields with their IDs (custom field IDs are needed to update custom fields)",
		func(ctx context.Context, req *mcp.CallToolRequest, input listFieldsInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			fields, err := client.ListFields(ctx)
			if err != nil {
				return nil, nil, err
			}
			type field struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Type string `json:"type,omitempty"`
			}
			var out []field
			filter := strings.ToLower(input.Filter)
			for _, f := range fields {
				if (input.CustomOnly && !f.Custom) || (filter != "" && !strings.Contains(strings.ToLower(f.Name+" "+f.ID), filter)) {
					continue
				}
				ff := field{ID: f.ID, Name: f.Name}
				if f.Schema != nil {
					ff.Type = f.Schema.Type
				}
				out = append(out, ff)
			}
			return textResult(out)
		})

	maybeAdd(s, enabledGroups, "jira_get_development_info", "Get the pull requests and branches linked to a Jira issue (development panel)",
		func(ctx context.Context, req *mcp.CallToolRequest, input issueKeyInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			info, err := client.GetDevInfo(ctx, input.Key)
			if err != nil {
				return nil, nil, err
			}
			return textResult(info)
		})

	maybeAdd(s, enabledGroups, "jira_get_feature_children", "List the issues linked to a feature (by its implementing link types), with how each is linked",
		func(ctx context.Context, req *mcp.CallToolRequest, input featureChildrenInput) (*mcp.CallToolResult, any, error) {
			cfg, err := config.LoadJiraConfig()
			if err != nil {
				return nil, nil, err
			}
			client := jira.NewClient(cfg)
			phrases := input.LinkTypes
			if len(phrases) == 0 {
				phrases = cfg.FeatureLinkTypes
			}
			if len(phrases) == 0 {
				phrases = config.DefaultFeatureLinkTypes
			}
			feature, err := client.GetIssue(ctx, input.Key)
			if err != nil {
				return nil, nil, err
			}
			keys, relation := jira.LinkedChildren(feature, phrases)
			out := []featureChild{}
			if jql := jira.KeysJQL(keys); jql != "" {
				issues, err := client.SearchIssuesAll(ctx, jql, compactSearchFields, 0)
				if err != nil {
					return nil, nil, err
				}
				for _, is := range issues {
					out = append(out, featureChild{Relation: relation[is.Key], compactSearchHit: compactSearchHitFrom(is)})
				}
			}
			return textResult(map[string]any{"feature": input.Key, "summary": feature.Fields.Summary, "link_types": phrases, "issues": out})
		})

	maybeAdd(s, enabledGroups, "jira_clone_issue", "Clone a Jira issue (summary, description, type, priority, labels, components, assignee)",
		func(ctx context.Context, req *mcp.CallToolRequest, input cloneIssueInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			o := jira.CloneOptions{Summary: input.Summary, Priority: input.Priority, Labels: input.Labels}
			if input.Assignee != "" {
				user, err := client.ResolveUser(ctx, input.Assignee)
				if err != nil {
					return nil, nil, err
				}
				o.Assignee = client.UserID(user)
			}
			issue, err := client.CloneIssue(ctx, input.Key, o)
			if err != nil {
				return nil, nil, err
			}
			return textResult(map[string]string{"message": "Cloned " + input.Key, "key": issue.Key})
		})

	maybeAdd(s, enabledGroups, "jira_upload_attachment", "Attach a local file to a Jira issue",
		func(ctx context.Context, req *mcp.CallToolRequest, input uploadInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			f, err := os.Open(input.Path)
			if err != nil {
				return nil, nil, err
			}
			defer f.Close()
			atts, err := client.UploadAttachment(ctx, input.Target, filepath.Base(input.Path), f)
			if err != nil {
				return nil, nil, err
			}
			return textResult(atts)
		})

	maybeAdd(s, enabledGroups, "jira_delete_attachment", "Delete an attachment from a Jira issue",
		func(ctx context.Context, req *mcp.CallToolRequest, input deleteAttachmentInput) (*mcp.CallToolResult, any, error) {
			client, err := newJiraClient()
			if err != nil {
				return nil, nil, err
			}
			att, err := client.FindAttachment(ctx, input.Target, input.Ref)
			if err != nil {
				return nil, nil, err
			}
			if err := client.DeleteAttachment(ctx, att.ID); err != nil {
				return nil, nil, err
			}
			return message("Deleted %s from %s", att.Filename, input.Target)
		})

	maybeAdd(s, enabledGroups, "confluence_upload_attachment", "Attach a local file to a Confluence page (same name: new version)",
		func(ctx context.Context, req *mcp.CallToolRequest, input uploadInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			f, err := os.Open(input.Path)
			if err != nil {
				return nil, nil, err
			}
			defer f.Close()
			att, err := client.UploadAttachment(ctx, input.Target, filepath.Base(input.Path), f)
			if err != nil {
				return nil, nil, err
			}
			return textResult(map[string]string{"message": "Attached " + att.Title, "id": att.ID})
		})

	maybeAdd(s, enabledGroups, "confluence_delete_attachment", "Delete an attachment from a Confluence page",
		func(ctx context.Context, req *mcp.CallToolRequest, input deleteAttachmentInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			att, err := client.FindAttachment(ctx, input.Target, input.Ref)
			if err != nil {
				return nil, nil, err
			}
			if err := client.DeleteAttachment(ctx, att.ID); err != nil {
				return nil, nil, err
			}
			return message("Deleted %s from page %s", att.Title, input.Target)
		})

	maybeAdd(s, enabledGroups, "confluence_copy_page", "Copy a Confluence page (content only; not attachments, labels or children)",
		func(ctx context.Context, req *mcp.CallToolRequest, input copyPageInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			page, err := client.CopyPage(ctx, input.PageID, input.Title, input.Space, input.ParentID)
			if err != nil {
				return nil, nil, err
			}
			return textResult(map[string]string{"message": "Page copied", "id": page.ID, "title": page.Title, "url": client.WebURL(page)})
		})

	maybeAdd(s, enabledGroups, "confluence_move_page", "Move a Confluence page under another parent page in the same space",
		func(ctx context.Context, req *mcp.CallToolRequest, input movePageInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			page, err := client.MovePage(ctx, input.PageID, input.ParentID)
			if err != nil {
				return nil, nil, err
			}
			return message("Moved %q under page %s", page.Title, input.ParentID)
		})
}
