package mcptools

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zhani/atlassian-cli/internal/config"
	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/htmlconv"
	"github.com/zhani/atlassian-cli/internal/mdconv"
)

// --- Input types ---

type getPageInput struct {
	ID     string `json:"id" jsonschema:"The Confluence page ID"`
	Expand string `json:"expand,omitempty" jsonschema:"Comma-separated list of properties to expand (default: body.storage,version)"`
}

type createPageInput struct {
	SpaceKey   string `json:"space_key" jsonschema:"Space key"`
	Title      string `json:"title" jsonschema:"Page title"`
	Body       string `json:"body" jsonschema:"Page content in Markdown (storage-format XHTML when body_format is storage)"`
	BodyFormat string `json:"body_format,omitempty" jsonschema:"markdown (default) or storage"`
	ParentID   string `json:"parent_id,omitempty" jsonschema:"Parent page ID"`
}

type updatePageInput struct {
	PageID     string `json:"page_id" jsonschema:"The Confluence page ID"`
	Title      string `json:"title,omitempty" jsonschema:"New title (default: keep the current title)"`
	Body       string `json:"body" jsonschema:"New page content in Markdown (storage-format XHTML when body_format is storage); replaces the whole page"`
	BodyFormat string `json:"body_format,omitempty" jsonschema:"markdown (default) or storage"`
	Message    string `json:"message,omitempty" jsonschema:"Version message"`
}

type searchContentInput struct {
	CQL   string `json:"cql,omitempty" jsonschema:"CQL query; alternatively use text and/or space"`
	Text  string `json:"text,omitempty" jsonschema:"Full-text search terms"`
	Space string `json:"space,omitempty" jsonschema:"Space key to search in"`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 25)"`
}

type getChildrenInput struct {
	PageID string `json:"page_id" jsonschema:"The Confluence page ID"`
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 100)"`
}

type listLabelsInput struct {
	PageID string `json:"page_id" jsonschema:"The Confluence page ID"`
}

type addLabelsInput struct {
	PageID string   `json:"page_id" jsonschema:"The Confluence page ID"`
	Labels []string `json:"labels" jsonschema:"Labels to add"`
}

type removeLabelInput struct {
	PageID string `json:"page_id" jsonschema:"The Confluence page ID"`
	Label  string `json:"label" jsonschema:"Label name to remove"`
}

type listRestrictionsInput struct {
	PageID string `json:"page_id" jsonschema:"The Confluence page ID"`
}

type restrictionInput struct {
	PageID    string `json:"page_id" jsonschema:"The Confluence page ID"`
	Operation string `json:"operation" jsonschema:"Operation type: read or update"`
	Type      string `json:"type" jsonschema:"Restriction type: user or group"`
	Name      string `json:"name" jsonschema:"Username or group name"`
}

type listAttachmentsPageInput struct {
	PageID string `json:"page_id" jsonschema:"The Confluence page ID"`
}

type downloadAttachmentPageInput struct {
	PageID string `json:"page_id" jsonschema:"The Confluence page ID"`
	Ref    string `json:"ref" jsonschema:"Attachment title or ID to download"`
	Path   string `json:"path" jsonschema:"Local file path to write the attachment to"`
}

// --- Registration ---

func registerConfluenceTools(s *mcp.Server, enabledGroups []string) {
	maybeAdd(s, enabledGroups, "confluence_get_page", "Get a Confluence page by ID, including its content",
		func(ctx context.Context, req *mcp.CallToolRequest, input getPageInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			var expand []string
			if input.Expand != "" {
				expand = strings.Split(input.Expand, ",")
			}
			page, err := client.GetPage(ctx, input.ID, expand)
			if err != nil {
				return nil, nil, err
			}
			if page.Body.Storage != nil {
				page.Body.Storage.Value = htmlconv.Convert(page.Body.Storage.Value)
			}
			return textResult(page)
		})

	maybeAdd(s, enabledGroups, "confluence_create_page", "Create a Confluence page; the body is Markdown (code blocks and > **Note:** quotes become Confluence macros)",
		func(ctx context.Context, req *mcp.CallToolRequest, input createPageInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			body, err := storageBody(input.Body, input.BodyFormat)
			if err != nil {
				return nil, nil, err
			}
			page, err := client.CreatePage(ctx, &confluence.CreatePageInput{
				SpaceKey: input.SpaceKey,
				Title:    input.Title,
				Body:     body,
				ParentID: input.ParentID,
			})
			if err != nil {
				return nil, nil, err
			}
			return textResult(map[string]any{"message": "Page created", "id": page.ID, "version": page.Version.Number, "url": client.WebURL(page)})
		})

	maybeAdd(s, enabledGroups, "confluence_update_page", "Replace the content of a Confluence page; the body is Markdown. Fetch the page first: content not in body is removed",
		func(ctx context.Context, req *mcp.CallToolRequest, input updatePageInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			body, err := storageBody(input.Body, input.BodyFormat)
			if err != nil {
				return nil, nil, err
			}
			title := input.Title
			if title == "" {
				current, err := client.GetPage(ctx, input.PageID, []string{"version"})
				if err != nil {
					return nil, nil, err
				}
				title = current.Title
			}
			page, err := client.UpdatePage(ctx, input.PageID, &confluence.UpdatePageInput{Title: title, Body: body, Message: input.Message})
			if err != nil {
				return nil, nil, err
			}
			return textResult(map[string]any{"message": "Page updated", "id": page.ID, "version": page.Version.Number, "url": client.WebURL(page)})
		})

	maybeAdd(s, enabledGroups, "confluence_search", "Search Confluence pages by CQL, or by text and/or space",
		func(ctx context.Context, req *mcp.CallToolRequest, input searchContentInput) (*mcp.CallToolResult, any, error) {
			cql := input.CQL
			if cql == "" {
				if input.Text == "" && input.Space == "" {
					return nil, nil, fmt.Errorf("cql, text or space is required")
				}
				cql = confluence.BuildCQL(confluence.SearchOptions{Text: input.Text, Space: input.Space})
			}
			limit := input.Limit
			if limit <= 0 {
				limit = 25
			}
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			pages, err := client.SearchCQL(ctx, cql, limit)
			if err != nil {
				return nil, nil, err
			}
			return textResult(compactPages(client, pages))
		})

	maybeAdd(s, enabledGroups, "confluence_get_children", "List the child pages of a Confluence page",
		func(ctx context.Context, req *mcp.CallToolRequest, input getChildrenInput) (*mcp.CallToolResult, any, error) {
			limit := input.Limit
			if limit <= 0 {
				limit = 100
			}
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			pages, err := client.GetChildren(ctx, input.PageID, limit)
			if err != nil {
				return nil, nil, err
			}
			return textResult(compactPages(client, pages))
		})

	maybeAdd(s, enabledGroups, "confluence_api_request", "Send an authenticated request to any Confluence REST endpoint (escape hatch for operations without a dedicated tool)",
		func(ctx context.Context, req *mcp.CallToolRequest, input apiRequestInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			return apiRequest(ctx, client, input)
		})

	maybeAdd(s, enabledGroups, "confluence_list_labels", "List labels on a Confluence page",
		func(ctx context.Context, req *mcp.CallToolRequest, input listLabelsInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			labels, err := client.GetLabels(ctx, input.PageID)
			if err != nil {
				return nil, nil, err
			}
			return textResult(labels)
		})

	maybeAdd(s, enabledGroups, "confluence_add_labels", "Add labels to a Confluence page",
		func(ctx context.Context, req *mcp.CallToolRequest, input addLabelsInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			if err := client.AddLabels(ctx, input.PageID, input.Labels); err != nil {
				return nil, nil, err
			}
			return textResult(map[string]string{"message": "Labels added successfully"})
		})

	maybeAdd(s, enabledGroups, "confluence_remove_label", "Remove a label from a Confluence page",
		func(ctx context.Context, req *mcp.CallToolRequest, input removeLabelInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			if err := client.RemoveLabel(ctx, input.PageID, input.Label); err != nil {
				return nil, nil, err
			}
			return textResult(map[string]string{"message": "Label removed successfully"})
		})

	maybeAdd(s, enabledGroups, "confluence_list_restrictions", "List restrictions on a Confluence page",
		func(ctx context.Context, req *mcp.CallToolRequest, input listRestrictionsInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			restrictions, err := client.GetRestrictions(ctx, input.PageID)
			if err != nil {
				return nil, nil, err
			}
			return textResult(restrictions)
		})

	maybeAdd(s, enabledGroups, "confluence_add_restriction", "Add a restriction to a Confluence page",
		func(ctx context.Context, req *mcp.CallToolRequest, input restrictionInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			ri := &confluence.RestrictionInput{
				Operation: input.Operation,
				Type:      input.Type,
				Name:      input.Name,
			}
			if err := client.AddRestriction(ctx, input.PageID, ri); err != nil {
				return nil, nil, err
			}
			return textResult(map[string]string{"message": "Restriction added successfully"})
		})

	maybeAdd(s, enabledGroups, "confluence_remove_restriction", "Remove a restriction from a Confluence page",
		func(ctx context.Context, req *mcp.CallToolRequest, input restrictionInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			ri := &confluence.RestrictionInput{
				Operation: input.Operation,
				Type:      input.Type,
				Name:      input.Name,
			}
			if err := client.RemoveRestriction(ctx, input.PageID, ri); err != nil {
				return nil, nil, err
			}
			return textResult(map[string]string{"message": "Restriction removed successfully"})
		})

	maybeAdd(s, enabledGroups, "confluence_list_attachments", "List attachments on a Confluence page",
		func(ctx context.Context, req *mcp.CallToolRequest, input listAttachmentsPageInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			attachments, err := client.GetAttachments(ctx, input.PageID)
			if err != nil {
				return nil, nil, err
			}
			return textResult(attachments)
		})

	maybeAdd(s, enabledGroups, "confluence_download_attachment", "Download a Confluence page attachment to a local file",
		func(ctx context.Context, req *mcp.CallToolRequest, input downloadAttachmentPageInput) (*mcp.CallToolResult, any, error) {
			client, err := newConfluenceClient()
			if err != nil {
				return nil, nil, err
			}
			attachments, err := client.GetAttachments(ctx, input.PageID)
			if err != nil {
				return nil, nil, err
			}
			var match *confluence.Attachment
			for i := range attachments {
				if attachments[i].ID == input.Ref || attachments[i].Title == input.Ref {
					match = &attachments[i]
					break
				}
			}
			if match == nil {
				return nil, nil, fmt.Errorf("no attachment matching %q found on page %s", input.Ref, input.PageID)
			}
			downloadPath := match.Links["download"]
			if downloadPath == "" {
				return nil, nil, fmt.Errorf("attachment %q has no download link", match.Title)
			}
			body, err := client.DownloadAttachment(ctx, downloadPath)
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
				"filename": match.Title,
				"path":     input.Path,
				"bytes":    n,
			})
		})
}

func newConfluenceClient() (*confluence.Client, error) {
	cfg, err := config.LoadConfluenceConfig()
	if err != nil {
		return nil, err
	}
	return confluence.NewClient(cfg), nil
}

// storageBody converts a page body to storage format according to format
// ("markdown" by default, or "storage" for no conversion).
func storageBody(body, format string) (string, error) {
	switch strings.ToLower(format) {
	case "", "markdown", "md":
		return mdconv.ToStorage(body)
	case "storage", "xhtml":
		return body, nil
	}
	return "", fmt.Errorf("unknown body_format %q (use markdown or storage)", format)
}
