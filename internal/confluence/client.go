package confluence

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/zhani/atlassian-cli/internal/config"
	"github.com/zhani/atlassian-cli/internal/httpclient"
)

type Client struct {
	http *httpclient.Client
}

func NewClient(cfg *config.AtlassianConfig) *Client {
	return &Client{http: httpclient.NewFromConfig(cfg.URL, cfg.Email, cfg.Token)}
}

func (c *Client) GetPage(ctx context.Context, id string, expand []string) (*Page, error) {
	q := url.Values{}
	if len(expand) > 0 {
		q.Set("expand", strings.Join(expand, ","))
	} else {
		q.Set("expand", "body.storage,version")
	}

	var page Page
	err := c.http.Do(ctx, "GET", contentPath(id), q, nil, &page)
	return &page, err
}

// listPageSize is the page size requested from paginated endpoints.
const listPageSize = 50

// collect follows start/limit pagination of a v1 list endpoint until limit
// results are gathered (limit <= 0: all) or the results run out.
func collect[T any](ctx context.Context, c *Client, path string, q url.Values, limit int) ([]T, error) {
	return collectSized[T](ctx, c, path, q, limit, listPageSize)
}

// collectSized is collect with a given page size.
func collectSized[T any](ctx context.Context, c *Client, path string, q url.Values, limit, pageSize int) ([]T, error) {
	var all []T
	for {
		size := pageSize
		if limit > 0 && limit-len(all) < size {
			size = limit - len(all)
		}
		q.Set("start", strconv.Itoa(len(all)))
		q.Set("limit", strconv.Itoa(size))
		var resp pageResponse[T]
		if err := c.http.Do(ctx, "GET", path, q, nil, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Results...)
		if limit > 0 && len(all) >= limit {
			return all[:limit], nil
		}
		if resp.Links.Next == "" || len(resp.Results) == 0 {
			return all, nil
		}
	}
}

// SearchCQL returns content matching a CQL query.
func (c *Client) SearchCQL(ctx context.Context, cql string, limit int) ([]Page, error) {
	q := url.Values{}
	q.Set("cql", cql)
	q.Set("expand", "space,version")
	return collect[Page](ctx, c, "/rest/api/content/search", q, limit)
}

// GetChildren returns the child pages of a page.
func (c *Client) GetChildren(ctx context.Context, pageID string, limit int) ([]Page, error) {
	q := url.Values{}
	q.Set("expand", "space,version")
	return collect[Page](ctx, c, contentPath(pageID)+"/child/page", q, limit)
}

// ListPages returns the pages of a space.
func (c *Client) ListPages(ctx context.Context, spaceKey string, limit int) ([]Page, error) {
	q := url.Values{}
	q.Set("spaceKey", spaceKey)
	q.Set("type", "page")
	q.Set("expand", "space,version")
	return collect[Page](ctx, c, "/rest/api/content", q, limit)
}

// SpacePages returns every page of a space with its ancestors (root
// first), from which the whole page tree can be built.
func (c *Client) SpacePages(ctx context.Context, spaceKey string, limit int) ([]Page, error) {
	q := url.Values{}
	q.Set("spaceKey", spaceKey)
	q.Set("type", "page")
	q.Set("expand", "ancestors")
	return collectSized[Page](ctx, c, "/rest/api/content", q, limit, 200)
}

// RootPages returns the top-level pages of a space.
func (c *Client) RootPages(ctx context.Context, spaceKey string, limit int) ([]Page, error) {
	q := url.Values{}
	q.Set("depth", "root")
	q.Set("expand", "space,version")
	return collect[Page](ctx, c, "/rest/api/space/"+url.PathEscape(spaceKey)+"/content/page", q, limit)
}

// ListSpaces returns the spaces visible to the user.
func (c *Client) ListSpaces(ctx context.Context, limit int) ([]Space, error) {
	return collect[Space](ctx, c, "/rest/api/space", url.Values{}, limit)
}

// GetVersions returns the version history of a page, newest first. Cloud
// serves it under /rest/api; Server/DC only under /rest/experimental.
func (c *Client) GetVersions(ctx context.Context, pageID string, limit int) ([]Version, error) {
	versions, err := collect[Version](ctx, c, contentPath(pageID)+"/version", url.Values{}, limit)
	var apiErr *httpclient.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		return collect[Version](ctx, c, "/rest/experimental/content/"+url.PathEscape(pageID)+"/version", url.Values{}, limit)
	}
	return versions, err
}

// GetUser looks a user up by "key" (Server/DC user key), "username" or
// "accountId" (Cloud).
func (c *Client) GetUser(ctx context.Context, by, value string) (*User, error) {
	var u User
	err := c.http.Do(ctx, "GET", "/rest/api/user", url.Values{by: {value}}, nil, &u)
	return &u, err
}

// BaseURL returns the Confluence base URL.
func (c *Client) BaseURL() string { return c.http.BaseURL() }

// Raw performs an arbitrary authenticated REST request; see httpclient.Raw.
func (c *Client) Raw(ctx context.Context, method, path string, query url.Values, body []byte) (int, []byte, error) {
	return c.http.Raw(ctx, method, path, query, body)
}

// WebURL returns the browser URL of a page from its webui link.
func (c *Client) WebURL(p *Page) string {
	if p.Links.WebUI == "" {
		return ""
	}
	return c.http.BaseURL() + p.Links.WebUI
}

func (c *Client) GetLabels(ctx context.Context, pageID string) ([]Label, error) {
	var resp LabelResponse
	err := c.http.Do(ctx, "GET", contentPath(pageID)+"/label", nil, nil, &resp)
	return resp.Results, err
}

func (c *Client) AddLabels(ctx context.Context, pageID string, labels []string) error {
	body := make([]map[string]string, len(labels))
	for i, l := range labels {
		body[i] = map[string]string{"prefix": "global", "name": l}
	}
	return c.http.Do(ctx, "POST", contentPath(pageID)+"/label", nil, body, nil)
}

func (c *Client) RemoveLabel(ctx context.Context, pageID, label string) error {
	q := url.Values{}
	q.Set("name", label)
	return c.http.Do(ctx, "DELETE", contentPath(pageID)+"/label", q, nil, nil)
}

func (c *Client) GetRestrictions(ctx context.Context, pageID string) ([]Restriction, error) {
	var result []Restriction
	err := c.http.Do(ctx, "GET", contentPath(pageID)+"/restriction", nil, nil, &result)
	return result, err
}

func (c *Client) AddRestriction(ctx context.Context, pageID string, input *RestrictionInput) error {
	body := buildRestrictionBody(input)
	return c.http.Do(ctx, "PUT", contentPath(pageID)+"/restriction", nil, body, nil)
}

func (c *Client) RemoveRestriction(ctx context.Context, pageID string, input *RestrictionInput) error {
	q := url.Values{}
	var path string
	if input.Type == "user" {
		path = contentPath(pageID) + "/restriction/byOperation/" + url.PathEscape(input.Operation) + "/user"
		q.Set("accountId", input.Name)
	} else {
		path = contentPath(pageID) + "/restriction/byOperation/" + url.PathEscape(input.Operation) + "/group/" + url.PathEscape(input.Name)
	}
	return c.http.Do(ctx, "DELETE", path, q, nil, nil)
}

func (c *Client) CreatePage(ctx context.Context, input *CreatePageInput) (*Page, error) {
	body := map[string]any{
		"type":  "page",
		"title": input.Title,
		"space": map[string]string{"key": input.SpaceKey},
		"body":  storageBody(input.Body),
	}
	if input.ParentID != "" {
		body["ancestors"] = []map[string]string{{"id": input.ParentID}}
	}
	var page Page
	if err := c.http.Do(ctx, "POST", "/rest/api/content", nil, body, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (c *Client) UpdatePage(ctx context.Context, id string, input *UpdatePageInput) (*Page, error) {
	version := input.Version
	if version == 0 {
		current, err := c.GetPage(ctx, id, []string{"version"})
		if err != nil {
			return nil, fmt.Errorf("fetch current version: %w", err)
		}
		version = current.Version.Number + 1
	}
	body := map[string]any{
		"type":  "page",
		"title": input.Title,
		"version": map[string]any{
			"number":  version,
			"message": input.Message,
		},
		"body": storageBody(input.Body),
	}
	if input.ParentID != "" {
		body["ancestors"] = []map[string]string{{"id": input.ParentID}}
	}
	var page Page
	if err := c.http.Do(ctx, "PUT", contentPath(id), nil, body, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// MovePage moves a page under a new parent in the same space, keeping its
// title and content.
func (c *Client) MovePage(ctx context.Context, id, parentID string) (*Page, error) {
	page, err := c.GetPage(ctx, id, []string{"body.storage", "version"})
	if err != nil {
		return nil, err
	}
	return c.UpdatePage(ctx, id, &UpdatePageInput{
		Title:    page.Title,
		Body:     storageValue(page),
		Version:  page.Version.Number + 1,
		ParentID: parentID,
		Message:  "Moved",
	})
}

// CopyPage creates a new page with the content of an existing one. The copy
// goes to space (default: the source's space) under parentID (default: the
// source's parent) and is titled title (default: "Copy of <title>").
// Attachments, labels and child pages are not copied.
func (c *Client) CopyPage(ctx context.Context, id, title, space, parentID string) (*Page, error) {
	src, err := c.GetPage(ctx, id, []string{"body.storage", "space", "ancestors"})
	if err != nil {
		return nil, err
	}
	if title == "" {
		title = "Copy of " + src.Title
	}
	if space == "" && src.Space != nil {
		space = src.Space.Key
	}
	if parentID == "" && len(src.Ancestors) > 0 && (src.Space == nil || space == src.Space.Key) {
		parentID = src.Ancestors[len(src.Ancestors)-1].ID
	}
	return c.CreatePage(ctx, &CreatePageInput{SpaceKey: space, Title: title, Body: storageValue(src), ParentID: parentID})
}

func storageValue(p *Page) string {
	if p.Body.Storage == nil {
		return ""
	}
	return p.Body.Storage.Value
}

// UploadAttachment attaches a file to a page, or adds a new version of the
// attachment when the page already has one with the same name.
func (c *Client) UploadAttachment(ctx context.Context, pageID, filename string, r io.Reader) (*Attachment, error) {
	existing, err := c.GetAttachments(ctx, pageID)
	if err != nil {
		return nil, err
	}
	path := contentPath(pageID) + "/child/attachment"
	for _, a := range existing {
		if a.Title == filename {
			path += "/" + url.PathEscape(a.ID) + "/data"
			var att Attachment
			if err := c.http.Upload(ctx, path, filename, r, &att); err != nil {
				return nil, err
			}
			return &att, nil
		}
	}
	var resp AttachmentResponse
	if err := c.http.Upload(ctx, path, filename, r, &resp); err != nil {
		return nil, err
	}
	if len(resp.Results) == 0 {
		return nil, fmt.Errorf("upload of %s returned no attachment", filename)
	}
	return &resp.Results[0], nil
}

// DeleteAttachment deletes an attachment (attachments are content items).
func (c *Client) DeleteAttachment(ctx context.Context, id string) error {
	return c.http.Do(ctx, "DELETE", contentPath(id), nil, nil, nil)
}

// FindAttachment returns the attachment of a page whose ID or title is ref.
func (c *Client) FindAttachment(ctx context.Context, pageID, ref string) (*Attachment, error) {
	atts, err := c.GetAttachments(ctx, pageID)
	if err != nil {
		return nil, err
	}
	for i := range atts {
		if atts[i].ID == ref || atts[i].Title == ref {
			return &atts[i], nil
		}
	}
	return nil, fmt.Errorf("no attachment matching %q found on page %s", ref, pageID)
}

func (c *Client) GetAttachments(ctx context.Context, pageID string) ([]Attachment, error) {
	var resp AttachmentResponse
	err := c.http.Do(ctx, "GET", contentPath(pageID)+"/child/attachment", nil, nil, &resp)
	return resp.Results, err
}

func (c *Client) DownloadAttachment(ctx context.Context, downloadPath string) (io.ReadCloser, error) {
	resp, err := c.http.Get(ctx, downloadPath, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func contentPath(id string) string {
	return "/rest/api/content/" + url.PathEscape(id)
}

func storageBody(value string) map[string]any {
	return map[string]any{
		"storage": map[string]string{
			"value":          value,
			"representation": "storage",
		},
	}
}

func buildRestrictionBody(input *RestrictionInput) []map[string]any {
	subject := map[string]any{}
	if input.Type == "user" {
		subject["user"] = map[string]any{
			"type":      "known",
			"accountId": input.Name,
		}
	} else {
		subject["group"] = map[string]any{
			"type": "group",
			"name": input.Name,
		}
	}
	return []map[string]any{
		{
			"operation":    input.Operation,
			"restrictions": subject,
		},
	}
}
