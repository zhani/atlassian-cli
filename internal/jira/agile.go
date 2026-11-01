package jira

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Board is an agile (Scrum or Kanban) board.
type Board struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// Sprint is a sprint of a Scrum board.
type Sprint struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"` // future, active or closed
	StartDate string `json:"startDate,omitempty"`
	EndDate   string `json:"endDate,omitempty"`
	Goal      string `json:"goal,omitempty"`
}

// agilePage is a page of the startAt/maxResults/isLast paginated Agile API.
type agilePage[T any] struct {
	Values []T  `json:"values"`
	IsLast bool `json:"isLast"`
}

func collectAgile[T any](ctx context.Context, c *Client, path string, q url.Values) ([]T, error) {
	var all []T
	for {
		q.Set("startAt", strconv.Itoa(len(all)))
		q.Set("maxResults", "50")
		var page agilePage[T]
		if err := c.http.Do(ctx, "GET", path, q, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Values...)
		if page.IsLast || len(page.Values) == 0 {
			return all, nil
		}
	}
}

// ListBoards returns the boards of a project ("" for all boards).
func (c *Client) ListBoards(ctx context.Context, project string) ([]Board, error) {
	q := url.Values{}
	if project != "" {
		q.Set("projectKeyOrId", project)
	}
	return collectAgile[Board](ctx, c, "/rest/agile/1.0/board", q)
}

// GetSprint returns a sprint by ID.
func (c *Client) GetSprint(ctx context.Context, id int) (*Sprint, error) {
	var sp Sprint
	if err := c.http.Do(ctx, "GET", "/rest/agile/1.0/sprint/"+strconv.Itoa(id), nil, nil, &sp); err != nil {
		return nil, err
	}
	return &sp, nil
}

// ListSprints returns the sprints of a board in the given states (future,
// active, closed; none means all), ordered by start date.
func (c *Client) ListSprints(ctx context.Context, boardID int, states ...string) ([]Sprint, error) {
	q := url.Values{}
	if len(states) > 0 {
		q.Set("state", strings.Join(states, ","))
	}
	sprints, err := collectAgile[Sprint](ctx, c, "/rest/agile/1.0/board/"+strconv.Itoa(boardID)+"/sprint", q)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(sprints, func(i, j int) bool {
		return sprintOrder(sprints[i]) < sprintOrder(sprints[j])
	})
	return sprints, nil
}

// sprintOrder sorts closed sprints before active before future ones, each
// by start date (future sprints usually have none and keep board order).
func sprintOrder(s Sprint) string {
	rank := map[string]string{"closed": "0", "active": "1", "future": "2"}[s.State]
	return rank + s.StartDate
}

// AddToSprint moves issues into a sprint.
func (c *Client) AddToSprint(ctx context.Context, sprintID int, keys []string) error {
	body := map[string]any{"issues": keys}
	return c.http.Do(ctx, "POST", "/rest/agile/1.0/sprint/"+strconv.Itoa(sprintID)+"/issue", nil, body, nil)
}

// AddToEpic sets the epic of issues; epicKey "" removes them from their epic.
func (c *Client) AddToEpic(ctx context.Context, epicKey string, keys []string) error {
	target := epicKey
	if target == "" {
		target = "none"
	}
	body := map[string]any{"issues": keys}
	return c.http.Do(ctx, "POST", "/rest/agile/1.0/epic/"+url.PathEscape(target)+"/issue", nil, body, nil)
}

// IsCloud reports whether the client talks to Jira Cloud.
func (c *Client) IsCloud() bool { return c.cloud }

// EpicChildrenJQL returns the JQL for the issues of an epic. field is the
// JQL field that links issues to epics; when empty it is "parent" on Cloud
// and "Epic Link" on Server/DC.
func (c *Client) EpicChildrenJQL(epicKey, field string) string {
	if field == "" {
		field = "Epic Link"
		if c.cloud {
			field = "parent"
		}
	}
	return quoteField(field) + " = " + quote(epicKey) + " ORDER BY rank ASC"
}

// quoteField quotes JQL field names that contain spaces.
func quoteField(f string) string {
	if strings.ContainsAny(f, " -") {
		return quote(f)
	}
	return f
}

// LinkedChildren returns, for an issue's links, the linked issues whose
// relation (as seen from the issue) is one of phrases, keyed by issue key,
// with the matching phrase. Phrases match case-insensitively.
func LinkedChildren(issue *Issue, phrases []string) (keys []string, relation map[string]string) {
	relation = map[string]string{}
	for _, l := range issue.Fields.IssueLinks {
		phrase, other := l.Describe()
		if other == nil {
			continue
		}
		for _, p := range phrases {
			if strings.EqualFold(phrase, p) {
				if _, seen := relation[other.Key]; !seen {
					keys = append(keys, other.Key)
				}
				relation[other.Key] = phrase
			}
		}
	}
	return keys, relation
}

// KeysJQL returns a JQL query matching exactly the given issue keys, or ""
// for none.
func KeysJQL(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = quote(k)
	}
	return fmt.Sprintf("key in (%s) ORDER BY key ASC", strings.Join(quoted, ", "))
}
