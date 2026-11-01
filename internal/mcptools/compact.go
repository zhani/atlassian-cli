package mcptools

import (
	"github.com/putcho01/atlassian-cli/internal/confluence"
	"github.com/putcho01/atlassian-cli/internal/htmlconv"
	"github.com/putcho01/atlassian-cli/internal/jira"
)

// The compact types are what the MCP tools return by default: the fields an
// agent acts on, text as Markdown, custom fields under their display names,
// and none of the API's self links, avatars or empty values. Tools take
// `full: true` to get the raw API objects instead.

type compactIssue struct {
	Key           string            `json:"key"`
	Summary       string            `json:"summary"`
	Status        string            `json:"status,omitempty"`
	Resolution    string            `json:"resolution,omitempty"`
	Type          string            `json:"type,omitempty"`
	Priority      string            `json:"priority,omitempty"`
	Assignee      string            `json:"assignee,omitempty"`
	Reporter      string            `json:"reporter,omitempty"`
	Project       string            `json:"project,omitempty"`
	Created       string            `json:"created,omitempty"`
	Updated       string            `json:"updated,omitempty"`
	Labels        []string          `json:"labels,omitempty"`
	Description   string            `json:"description,omitempty"`
	Fields        map[string]any    `json:"fields,omitempty"`
	Links         []compactLink     `json:"links,omitempty"`
	Subtasks      []compactIssueRef `json:"subtasks,omitempty"`
	Attachments   []compactFile     `json:"attachments,omitempty"`
	Comments      []compactComment  `json:"comments,omitempty"`
	CommentsTotal int               `json:"comments_total,omitempty"`
}

type compactIssueRef struct {
	Key     string `json:"key"`
	Summary string `json:"summary,omitempty"`
	Status  string `json:"status,omitempty"`
}

type compactLink struct {
	Relation string `json:"relation"`
	compactIssueRef
}

type compactFile struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

type compactComment struct {
	ID      string `json:"id"`
	Author  string `json:"author,omitempty"`
	Created string `json:"created,omitempty"`
	Body    string `json:"body"`
}

// compactSearchHit is one row of a search result.
type compactSearchHit struct {
	Key      string `json:"key"`
	Summary  string `json:"summary"`
	Status   string `json:"status,omitempty"`
	Type     string `json:"type,omitempty"`
	Priority string `json:"priority,omitempty"`
	Assignee string `json:"assignee,omitempty"`
	Updated  string `json:"updated,omitempty"`
}

// compactSearchFields are the fields requested for compact search results.
var compactSearchFields = []string{"summary", "status", "issuetype", "priority", "assignee", "updated"}

// maxCompactComments is how many of the latest comments compact issues carry.
const maxCompactComments = 10

func compactIssueFrom(is *jira.Issue) compactIssue {
	f := is.Fields
	out := compactIssue{
		Key:        is.Key,
		Summary:    f.Summary,
		Status:     statusName(f.Status),
		Resolution: f.ResolutionName(),
		Priority:   priorityName(f.Priority),
		Assignee:   userName(f.Assignee),
		Reporter:   userName(f.Reporter),
		Created:    f.Created,
		Updated:    f.Updated,
		Labels:     f.Labels,
	}
	if f.IssueType != nil {
		out.Type = f.IssueType.Name
	}
	if f.Project != nil {
		out.Project = f.Project.Key
	}

	out.Description = f.Description
	if is.RenderedFields != nil && is.RenderedFields.Description != "" {
		out.Description = htmlconv.Convert(is.RenderedFields.Description)
	}

	for id, v := range f.Custom {
		sv := jira.SimplifyValue(v)
		if sv == nil {
			continue
		}
		name := id
		if n := is.Names[id]; n != "" {
			name = n
		}
		if out.Fields == nil {
			out.Fields = map[string]any{}
		}
		if _, taken := out.Fields[name]; taken {
			name += " (" + id + ")"
		}
		out.Fields[name] = sv
	}

	for _, l := range f.IssueLinks {
		phrase, linked := l.Describe()
		if linked == nil {
			continue
		}
		out.Links = append(out.Links, compactLink{Relation: phrase, compactIssueRef: issueRef(linked)})
	}
	for i := range f.Subtasks {
		out.Subtasks = append(out.Subtasks, issueRef(&f.Subtasks[i]))
	}
	for _, a := range f.Attachment {
		out.Attachments = append(out.Attachments, compactFile{ID: a.ID, Filename: a.Filename, Size: a.Size})
	}

	if f.Comment != nil {
		var rendered []jira.Comment
		if is.RenderedFields != nil && is.RenderedFields.Comment != nil {
			rendered = is.RenderedFields.Comment.Comments
		}
		comments := f.Comment.Comments
		for i := max(0, len(comments)-maxCompactComments); i < len(comments); i++ {
			c := comments[i]
			if i < len(rendered) && rendered[i].ID == c.ID {
				c.RenderedBody = rendered[i].Body
			}
			out.Comments = append(out.Comments, compactCommentFrom(c))
		}
		out.CommentsTotal = max(f.Comment.Total, len(comments))
	}
	return out
}

func compactCommentFrom(c jira.Comment) compactComment {
	body := c.Body
	if c.RenderedBody != "" {
		body = htmlconv.Convert(c.RenderedBody)
	}
	return compactComment{ID: c.ID, Author: userName(c.Author), Created: c.Created, Body: body}
}

func compactSearchHitFrom(is jira.Issue) compactSearchHit {
	f := is.Fields
	hit := compactSearchHit{
		Key:      is.Key,
		Summary:  f.Summary,
		Status:   statusName(f.Status),
		Priority: priorityName(f.Priority),
		Assignee: userName(f.Assignee),
		Updated:  f.Updated,
	}
	if f.IssueType != nil {
		hit.Type = f.IssueType.Name
	}
	return hit
}

func issueRef(is *jira.Issue) compactIssueRef {
	return compactIssueRef{Key: is.Key, Summary: is.Fields.Summary, Status: statusName(is.Fields.Status)}
}

func statusName(s *jira.Status) string {
	if s == nil {
		return ""
	}
	return s.Name
}

func priorityName(p *jira.Priority) string {
	if p == nil {
		return ""
	}
	return p.Name
}

func userName(u *jira.User) string {
	if u == nil {
		return ""
	}
	return u.DisplayName
}

// --- Confluence ---

type compactPage struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Space   string `json:"space,omitempty"`
	Version int    `json:"version,omitempty"`
	Updated string `json:"updated,omitempty"`
	URL     string `json:"url,omitempty"`
}

func compactPages(c *confluence.Client, pages []confluence.Page) []compactPage {
	out := make([]compactPage, 0, len(pages))
	for i := range pages {
		p := &pages[i]
		cp := compactPage{ID: p.ID, Title: p.Title, Version: p.Version.Number, Updated: p.Version.When, URL: c.WebURL(p)}
		if p.Space != nil {
			cp.Space = p.Space.Key
		}
		out = append(out, cp)
	}
	return out
}
