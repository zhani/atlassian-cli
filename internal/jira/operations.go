package jira

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// --- users ---

// userRef references a user in a request body: by accountId on Cloud, by
// username on Server/DC. An empty id yields a JSON null (no user).
func userRef(id string, cloud bool) map[string]any {
	field := "name"
	if cloud {
		field = "accountId"
	}
	if id == "" {
		return map[string]any{field: nil}
	}
	return map[string]any{field: id}
}

// UserID returns the identifier the API expects for u: the account ID on
// Cloud, the username on Server/DC.
func (c *Client) UserID(u *User) string {
	if c.cloud {
		return u.AccountID
	}
	return u.Name
}

// SearchUsers finds users by name, username or email.
func (c *Client) SearchUsers(ctx context.Context, query string) ([]User, error) {
	q := url.Values{}
	if c.cloud {
		q.Set("query", query)
	} else {
		q.Set("username", query) // Server/DC matches username, name and email
	}
	var users []User
	err := c.http.Do(ctx, "GET", "/rest/api/2/user/search", q, nil, &users)
	return users, err
}

// ErrAmbiguousUser is returned by ResolveUser when a query matches several
// users; the message lists them.
var ErrAmbiguousUser = errors.New("ambiguous user")

// ResolveUser turns a user reference into a user: "me" is the current user;
// anything else is searched for, preferring an exact match on username,
// email or display name.
func (c *Client) ResolveUser(ctx context.Context, ref string) (*User, error) {
	if strings.EqualFold(ref, "me") {
		return c.GetMyself(ctx)
	}
	users, err := c.SearchUsers(ctx, ref)
	if err != nil {
		return nil, err
	}
	var exact []User
	for _, u := range users {
		for _, v := range []string{u.Name, u.EmailAddress, u.DisplayName, u.AccountID} {
			if v != "" && strings.EqualFold(v, ref) {
				exact = append(exact, u)
				break
			}
		}
	}
	switch {
	case len(exact) == 1:
		return &exact[0], nil
	case len(users) == 1:
		return &users[0], nil
	case len(users) == 0:
		return nil, fmt.Errorf("no user matches %q", ref)
	}
	names := make([]string, 0, len(users))
	for _, u := range users {
		names = append(names, fmt.Sprintf("%s (%s)", u.DisplayName, c.UserID(&u)))
	}
	return nil, fmt.Errorf("%w: %q matches %s", ErrAmbiguousUser, ref, strings.Join(names, ", "))
}

// AssignIssue sets the assignee. userID is an account ID (Cloud) or username
// (Server/DC); "" unassigns, and "-1" selects the project's default assignee.
func (c *Client) AssignIssue(ctx context.Context, key, userID string) error {
	return c.http.Do(ctx, "PUT", "/rest/api/2/issue/"+url.PathEscape(key)+"/assignee", nil, userRef(userID, c.cloud), nil)
}

// --- links ---

// GetLinkTypes returns the issue link types of the instance.
func (c *Client) GetLinkTypes(ctx context.Context) ([]IssueLinkType, error) {
	var resp struct {
		IssueLinkTypes []IssueLinkType `json:"issueLinkTypes"`
	}
	err := c.http.Do(ctx, "GET", "/rest/api/2/issueLinkType", nil, nil, &resp)
	return resp.IssueLinkTypes, err
}

// ResolveLinkType finds a link type by name ("Blocks") or by either of its
// phrases ("blocks", "is blocked by"). swap reports that the phrase was the
// inward one, so the issues must be swapped to read "a <phrase> b".
func ResolveLinkType(types []IssueLinkType, ref string) (lt IssueLinkType, swap bool, err error) {
	for _, t := range types {
		if strings.EqualFold(t.Name, ref) || strings.EqualFold(t.Outward, ref) {
			return t, false, nil
		}
	}
	for _, t := range types {
		if strings.EqualFold(t.Inward, ref) {
			return t, true, nil
		}
	}
	names := make([]string, 0, len(types))
	for _, t := range types {
		names = append(names, fmt.Sprintf("%s (%s / %s)", t.Name, t.Outward, t.Inward))
	}
	return IssueLinkType{}, false, fmt.Errorf("unknown link type %q, available: %s", ref, strings.Join(names, ", "))
}

// LinkIssues links two issues so that the relation reads
// "from <outward phrase> to", e.g. "A-1 blocks A-2" for type Blocks.
func (c *Client) LinkIssues(ctx context.Context, typeName, from, to string) error {
	body := map[string]any{
		"type":         map[string]string{"name": typeName},
		"inwardIssue":  map[string]string{"key": from},
		"outwardIssue": map[string]string{"key": to},
	}
	return c.http.Do(ctx, "POST", "/rest/api/2/issueLink", nil, body, nil)
}

// UnlinkIssues removes the links between two issues, optionally only those
// of one link type, and returns how many were removed.
func (c *Client) UnlinkIssues(ctx context.Context, a, b, typeName string) (int, error) {
	q := url.Values{"fields": {"issuelinks"}}
	var issue Issue
	if err := c.http.Do(ctx, "GET", "/rest/api/2/issue/"+url.PathEscape(a), q, nil, &issue); err != nil {
		return 0, err
	}
	removed := 0
	for _, l := range issue.Fields.IssueLinks {
		_, other := l.Describe()
		if other == nil || !strings.EqualFold(other.Key, b) {
			continue
		}
		if typeName != "" && !strings.EqualFold(l.Type.Name, typeName) {
			continue
		}
		if err := c.http.Do(ctx, "DELETE", "/rest/api/2/issueLink/"+url.PathEscape(l.ID), nil, nil, nil); err != nil {
			return removed, err
		}
		removed++
	}
	if removed == 0 {
		return 0, fmt.Errorf("%s is not linked to %s", a, b)
	}
	return removed, nil
}

// AddRemoteLink adds a web link to an issue.
func (c *Client) AddRemoteLink(ctx context.Context, key, linkURL, title string) error {
	if title == "" {
		title = linkURL
	}
	body := map[string]any{"object": map[string]string{"url": linkURL, "title": title}}
	return c.http.Do(ctx, "POST", "/rest/api/2/issue/"+url.PathEscape(key)+"/remotelink", nil, body, nil)
}

// --- worklog ---

// Worklog is a time log entry.
type Worklog struct {
	ID        string `json:"id"`
	Author    *User  `json:"author,omitempty"`
	Comment   string `json:"comment,omitempty"`
	Started   string `json:"started,omitempty"`
	TimeSpent string `json:"timeSpent"`
}

// AddWorklog logs time on an issue. timeSpent uses Jira's duration format
// ("1d 2h 30m"); started is a Jira timestamp or "" for now.
func (c *Client) AddWorklog(ctx context.Context, key, timeSpent, comment, started string) (*Worklog, error) {
	body := map[string]any{"timeSpent": timeSpent}
	if comment != "" {
		body["comment"] = comment
	}
	if started != "" {
		body["started"] = started
	}
	var w Worklog
	err := c.http.Do(ctx, "POST", "/rest/api/2/issue/"+url.PathEscape(key)+"/worklog", nil, body, &w)
	return &w, err
}

// --- fields ---

// Field describes a Jira field.
type Field struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Custom bool   `json:"custom"`
	Schema *struct {
		Type   string `json:"type"`
		Items  string `json:"items,omitempty"`
		Custom string `json:"custom,omitempty"`
	} `json:"schema,omitempty"`
}

// ListFields returns all system and custom fields.
func (c *Client) ListFields(ctx context.Context) ([]Field, error) {
	var fields []Field
	err := c.http.Do(ctx, "GET", "/rest/api/2/field", nil, nil, &fields)
	return fields, err
}

// ResolveFieldIDs maps field references (IDs or display names,
// case-insensitive) to field IDs. Unknown references are returned in an
// error.
func ResolveFieldIDs(fields []Field, refs []string) ([]string, error) {
	byID := map[string]string{}
	byName := map[string]string{}
	for _, f := range fields {
		byID[strings.ToLower(f.ID)] = f.ID
		if _, dup := byName[strings.ToLower(f.Name)]; !dup {
			byName[strings.ToLower(f.Name)] = f.ID
		}
	}
	ids := make([]string, 0, len(refs))
	var unknown []string
	for _, r := range refs {
		k := strings.ToLower(strings.TrimSpace(r))
		switch {
		case byID[k] != "":
			ids = append(ids, byID[k])
		case byName[k] != "":
			ids = append(ids, byName[k])
		default:
			unknown = append(unknown, r)
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("unknown field(s): %s (see `jira field list`)", strings.Join(unknown, ", "))
	}
	return ids, nil
}

// GetIssueFields returns the raw values of the given fields of an issue.
func (c *Client) GetIssueFields(ctx context.Context, key string, fieldIDs []string) (map[string]any, error) {
	q := url.Values{"fields": {strings.Join(fieldIDs, ",")}}
	var raw struct {
		Fields map[string]any `json:"fields"`
	}
	err := c.http.Do(ctx, "GET", "/rest/api/2/issue/"+url.PathEscape(key), q, nil, &raw)
	return raw.Fields, err
}

// --- clone ---

// CloneOptions overrides fields of a cloned issue. Replace pairs are applied
// to the summary and description (find → replace, case-sensitive).
type CloneOptions struct {
	Summary  string
	Priority string
	Assignee string // user ID (see UserID)
	Labels   []string
	Replace  [][2]string
}

// CloneIssue creates a copy of an issue in the same project: summary,
// description, type, priority, labels, components, assignee and (for
// subtasks) parent. Custom fields are not copied.
func (c *Client) CloneIssue(ctx context.Context, key string, o CloneOptions) (*Issue, error) {
	q := url.Values{"fields": {"project,summary,description,issuetype,priority,labels,components,assignee,parent"}}
	var src struct {
		Fields map[string]any `json:"fields"`
	}
	if err := c.http.Do(ctx, "GET", "/rest/api/2/issue/"+url.PathEscape(key), q, nil, &src); err != nil {
		return nil, err
	}
	fields := cloneFields(src.Fields, o, c.cloud)
	var created Issue
	if err := c.http.Do(ctx, "POST", "/rest/api/2/issue", nil, map[string]any{"fields": fields}, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// cloneFields builds the create request fields of a clone from the source
// issue's raw fields.
func cloneFields(src map[string]any, o CloneOptions, cloud bool) map[string]any {
	ref := func(name, key string) {
		if m, ok := src[name].(map[string]any); ok {
			if v, ok := m[key].(string); ok && v != "" {
				src[name] = map[string]string{key: v}
				return
			}
		}
		delete(src, name)
	}
	ref("project", "key")
	ref("issuetype", "id")
	ref("priority", "name")
	ref("parent", "key")

	fields := map[string]any{}
	for _, k := range []string{"project", "issuetype", "priority", "parent", "labels"} {
		if v, ok := src[k]; ok && v != nil {
			fields[k] = v
		}
	}
	if comps, ok := src["components"].([]any); ok && len(comps) > 0 {
		var out []map[string]string
		for _, c := range comps {
			if m, ok := c.(map[string]any); ok {
				if id, ok := m["id"].(string); ok {
					out = append(out, map[string]string{"id": id})
				}
			}
		}
		fields["components"] = out
	}
	if a, ok := src["assignee"].(map[string]any); ok {
		id, _ := a["name"].(string)
		if cloud {
			id, _ = a["accountId"].(string)
		}
		if id != "" {
			fields["assignee"] = userRef(id, cloud)
		}
	}

	summary, _ := src["summary"].(string)
	description, _ := src["description"].(string)
	for _, r := range o.Replace {
		summary = strings.ReplaceAll(summary, r[0], r[1])
		description = strings.ReplaceAll(description, r[0], r[1])
	}
	if o.Summary != "" {
		summary = o.Summary
	}
	fields["summary"] = summary
	if description != "" {
		fields["description"] = description
	}
	if o.Priority != "" {
		fields["priority"] = map[string]string{"name": o.Priority}
	}
	if o.Assignee != "" {
		fields["assignee"] = userRef(o.Assignee, cloud)
	}
	if len(o.Labels) > 0 {
		fields["labels"] = o.Labels
	}
	return fields
}

// --- attachments ---

// UploadAttachment attaches a file to an issue.
func (c *Client) UploadAttachment(ctx context.Context, key, filename string, r io.Reader) ([]Attachment, error) {
	var atts []Attachment
	err := c.http.Upload(ctx, "/rest/api/2/issue/"+url.PathEscape(key)+"/attachments", filename, r, &atts)
	return atts, err
}

// DeleteAttachment deletes an attachment by ID.
func (c *Client) DeleteAttachment(ctx context.Context, id string) error {
	return c.http.Do(ctx, "DELETE", "/rest/api/2/attachment/"+url.PathEscape(id), nil, nil, nil)
}

// FindAttachment returns the attachment of an issue whose ID or filename is
// ref.
func (c *Client) FindAttachment(ctx context.Context, key, ref string) (*Attachment, error) {
	atts, err := c.GetAttachments(ctx, key)
	if err != nil {
		return nil, err
	}
	for i := range atts {
		if atts[i].ID == ref || atts[i].Filename == ref {
			return &atts[i], nil
		}
	}
	return nil, fmt.Errorf("no attachment matching %q found on %s", ref, key)
}

// --- development panel ---

// DevInfo is the development information (pull requests and branches) that
// connected tools such as Bitbucket or GitHub report for an issue.
type DevInfo struct {
	PullRequests []PullRequest `json:"pullRequests"`
	Branches     []Branch      `json:"branches"`
}

type PullRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	URL         string `json:"url"`
	Author      string `json:"author,omitempty"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
	LastUpdate  string `json:"lastUpdate,omitempty"`
	Repository  string `json:"repository,omitempty"`
}

type Branch struct {
	Name       string `json:"name"`
	URL        string `json:"url,omitempty"`
	Repository string `json:"repository,omitempty"`
}

// GetDevInfo returns the pull requests and branches linked to an issue. It
// uses the (unofficial but long-standing) dev-status API behind the issue
// view's development panel, on both Cloud and Server/DC.
func (c *Client) GetDevInfo(ctx context.Context, key string) (*DevInfo, error) {
	var issue Issue
	if err := c.http.Do(ctx, "GET", "/rest/api/2/issue/"+url.PathEscape(key), url.Values{"fields": {"summary"}}, nil, &issue); err != nil {
		return nil, err
	}

	var summary struct {
		Summary map[string]struct {
			ByInstanceType map[string]struct {
				Count int `json:"count"`
			} `json:"byInstanceType"`
		} `json:"summary"`
	}
	q := url.Values{"issueId": {issue.ID}}
	if err := c.http.Do(ctx, "GET", "/rest/dev-status/latest/issue/summary", q, nil, &summary); err != nil {
		return nil, err
	}

	// Collect the tools (applicationType) reporting branches or pull
	// requests; the "branch" data type returns both.
	apps := map[string]bool{}
	for _, kind := range []string{"branch", "pullrequest"} {
		for app, s := range summary.Summary[kind].ByInstanceType {
			if s.Count > 0 {
				apps[app] = true
			}
		}
	}

	info := &DevInfo{PullRequests: []PullRequest{}, Branches: []Branch{}}
	for app := range apps {
		var detail struct {
			Detail []struct {
				PullRequests []struct {
					ID     string `json:"id"`
					Name   string `json:"name"`
					Status string `json:"status"`
					URL    string `json:"url"`
					Author struct {
						Name string `json:"name"`
					} `json:"author"`
					Source struct {
						Branch string `json:"branch"`
					} `json:"source"`
					Destination struct {
						Branch string `json:"branch"`
					} `json:"destination"`
					LastUpdate     string `json:"lastUpdate"`
					RepositoryName string `json:"repositoryName"`
				} `json:"pullRequests"`
				Branches []struct {
					Name       string `json:"name"`
					URL        string `json:"url"`
					Repository struct {
						Name string `json:"name"`
					} `json:"repository"`
				} `json:"branches"`
			} `json:"detail"`
		}
		dq := url.Values{"issueId": {issue.ID}, "applicationType": {app}, "dataType": {"branch"}}
		if err := c.http.Do(ctx, "GET", "/rest/dev-status/latest/issue/detail", dq, nil, &detail); err != nil {
			return nil, err
		}
		for _, d := range detail.Detail {
			for _, p := range d.PullRequests {
				info.PullRequests = append(info.PullRequests, PullRequest{
					ID: p.ID, Name: p.Name, Status: p.Status, URL: p.URL, Author: p.Author.Name,
					Source: p.Source.Branch, Destination: p.Destination.Branch, LastUpdate: p.LastUpdate,
					Repository: p.RepositoryName,
				})
			}
			for _, b := range d.Branches {
				info.Branches = append(info.Branches, Branch{Name: b.Name, URL: b.URL, Repository: b.Repository.Name})
			}
		}
	}
	return info, nil
}
