package jira

import (
	"encoding/json"
	"strings"
)

// User represents a Jira user account.
type User struct {
	Self         string `json:"self"`
	Name         string `json:"name,omitempty"` // username (Server/DC)
	Key          string `json:"key,omitempty"`  // user key (Server/DC)
	AccountID    string `json:"accountId"`
	EmailAddress string `json:"emailAddress"`
	DisplayName  string `json:"displayName"`
	Active       bool   `json:"active"`
	TimeZone     string `json:"timeZone"`
}

// Issue represents a Jira issue with its key and fields.
type Issue struct {
	ID     string      `json:"id"`
	Key    string      `json:"key"`
	Self   string      `json:"self"`
	Fields IssueFields `json:"fields"`

	// RenderedFields is only populated when the issue is fetched with
	// expand=renderedFields (see GetIssueRendered).
	RenderedFields *RenderedFields `json:"renderedFields,omitempty"`

	// Names maps field IDs to display names; populated with expand=names.
	Names map[string]string `json:"names,omitempty"`
}

// RenderedFields holds the HTML-rendered versions of an issue's text fields.
type RenderedFields struct {
	Description string       `json:"description"`
	Comment     *CommentPage `json:"comment,omitempty"`

	// Custom holds the rendered HTML of custom text fields (customfield_*).
	Custom map[string]string `json:"-"`
}

// UnmarshalJSON decodes the known rendered fields and captures rendered
// custom text fields into Custom.
func (r *RenderedFields) UnmarshalJSON(data []byte) error {
	type alias RenderedFields // avoid recursion into this method
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = RenderedFields(a)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for k, v := range raw {
		var html string
		if !strings.HasPrefix(k, "customfield_") || json.Unmarshal(v, &html) != nil || html == "" {
			continue // not a custom field, or not rendered text
		}
		if r.Custom == nil {
			r.Custom = map[string]string{}
		}
		r.Custom[k] = html
	}
	return nil
}

// MarshalJSON emits the known rendered fields plus the rendered custom
// fields flattened into the object.
func (r RenderedFields) MarshalJSON() ([]byte, error) {
	type alias RenderedFields
	base, err := json.Marshal(alias(r))
	if err != nil || len(r.Custom) == 0 {
		return base, err
	}
	var m map[string]any
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, err
	}
	for k, v := range r.Custom {
		m[k] = v
	}
	return json.Marshal(m)
}

// IssueLink connects an issue to another issue. Exactly one of InwardIssue
// and OutwardIssue is set, depending on the direction of the link.
type IssueLink struct {
	ID           string        `json:"id"`
	Type         IssueLinkType `json:"type"`
	InwardIssue  *Issue        `json:"inwardIssue,omitempty"`
	OutwardIssue *Issue        `json:"outwardIssue,omitempty"`
}

type IssueLinkType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

// Describe returns the link phrase as seen from the issue that holds the link
// (e.g. "is blocked by") together with the linked issue.
func (l IssueLink) Describe() (string, *Issue) {
	if l.OutwardIssue != nil {
		return l.Type.Outward, l.OutwardIssue
	}
	return l.Type.Inward, l.InwardIssue
}

type IssueFields struct {
	Summary     string       `json:"summary"`
	Description string       `json:"description"`
	Status      *Status      `json:"status"`
	IssueType   *IssueType   `json:"issuetype"`
	Priority    *Priority    `json:"priority"`
	Resolution  *Resolution  `json:"resolution"`
	Assignee    *User        `json:"assignee"`
	Reporter    *User        `json:"reporter"`
	Project     *Project     `json:"project"`
	Created     string       `json:"created"`
	Updated     string       `json:"updated"`
	Labels      []string     `json:"labels"`
	Subtasks    []Issue      `json:"subtasks"`
	Attachment  []Attachment `json:"attachment"`
	IssueLinks  []IssueLink  `json:"issuelinks,omitempty"`
	Comment     *CommentPage `json:"comment,omitempty"`

	// Custom holds any customfield_* values returned by the API. It is
	// populated on unmarshal and flattened back into the object on marshal,
	// so custom fields survive a JSON round-trip.
	Custom map[string]any `json:"-"`
}

// UnmarshalJSON decodes the known issue fields and captures every
// customfield_* key into Custom.
func (f *IssueFields) UnmarshalJSON(data []byte) error {
	type alias IssueFields // avoid recursion into this method
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*f = IssueFields(a)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for k, v := range raw {
		if !strings.HasPrefix(k, "customfield_") {
			continue
		}
		var val any
		if err := json.Unmarshal(v, &val); err != nil {
			return err
		}
		if val == nil {
			continue // skip unset custom fields
		}
		if f.Custom == nil {
			f.Custom = make(map[string]any)
		}
		f.Custom[k] = val
	}
	return nil
}

// MarshalJSON emits the known issue fields plus every captured custom field
// flattened into the top-level object.
func (f IssueFields) MarshalJSON() ([]byte, error) {
	type alias IssueFields // avoid recursion into this method
	base, err := json.Marshal(alias(f))
	if err != nil {
		return nil, err
	}
	if len(f.Custom) == 0 {
		return base, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, err
	}
	for k, v := range f.Custom {
		vb, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		m[k] = vb
	}
	return json.Marshal(m)
}

// Attachment represents a file attached to a Jira issue.
type Attachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Author   *User  `json:"author"`
	Created  string `json:"created"`
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	Content  string `json:"content"`
}

type Status struct {
	Self        string `json:"self"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type IssueType struct {
	Self        string `json:"self"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Subtask     bool   `json:"subtask"`
}

type Priority struct {
	Self string `json:"self"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Resolution is how an issue was resolved; nil while it is unresolved.
type Resolution struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ResolutionName returns the resolution of an issue, or "Unresolved".
func (f *IssueFields) ResolutionName() string {
	if f.Resolution == nil || f.Resolution.Name == "" {
		return "Unresolved"
	}
	return f.Resolution.Name
}

type Project struct {
	Self string `json:"self"`
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type Transition struct {
	ID   string  `json:"id"`
	Name string  `json:"name"`
	To   *Status `json:"to"`
}

// SearchResult holds a paginated list of issues returned by a JQL query.
type SearchResult struct {
	StartAt    int     `json:"startAt"`
	MaxResults int     `json:"maxResults"`
	Total      int     `json:"total"`
	Issues     []Issue `json:"issues"`
}

// CreateIssueInput holds the parameters for creating a new Jira issue.
type CreateIssueInput struct {
	ProjectKey   string         `json:"projectKey"`
	Summary      string         `json:"summary"`
	IssueType    string         `json:"issueType"`
	Description  string         `json:"description,omitempty"`
	Assignee     string         `json:"assignee,omitempty"`
	Priority     string         `json:"priority,omitempty"`
	Labels       []string       `json:"labels,omitempty"`
	CustomFields map[string]any `json:"customFields,omitempty"`
}

type transitionsResponse struct {
	Transitions []Transition `json:"transitions"`
}

// Comment represents a comment on a Jira issue.
type Comment struct {
	ID      string `json:"id"`
	Author  *User  `json:"author"`
	Body    string `json:"body"`
	Created string `json:"created"`
	Updated string `json:"updated"`

	// RenderedBody is the HTML rendering of Body, when requested.
	RenderedBody string `json:"renderedBody,omitempty"`
}

// CommentPage is a page of comments, as returned by the comment endpoint and
// embedded in an issue's fields.
type CommentPage struct {
	Comments   []Comment `json:"comments"`
	Total      int       `json:"total"`
	StartAt    int       `json:"startAt"`
	MaxResults int       `json:"maxResults"`
}

// searchJQLResponse is the response of the token-paginated Cloud search
// endpoint (/rest/api/2/search/jql).
type searchJQLResponse struct {
	Issues        []Issue `json:"issues"`
	NextPageToken string  `json:"nextPageToken"`
	IsLast        bool    `json:"isLast"`
}
