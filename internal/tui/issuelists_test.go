package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zhani/atlassian-cli/internal/confluence"
)

type listPages struct{ fakePages }

func (f *listPages) GetPage(_ context.Context, id string, _ []string) (*confluence.Page, error) {
	return &confluence.Page{ID: id, Title: "Page " + id, Body: confluence.PageBody{Storage: &confluence.BodyContent{Value: `<h2>Development</h2>` +
		`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="server">PSS Jira</ac:parameter>` +
		`<ac:parameter ac:name="columnIds">issuekey,summary,issuetype,status,fixVersions,issuelinks,customfield_7,updated</ac:parameter>` +
		`<ac:parameter ac:name="columns">key,summary,type,status,fixVersions,issuelinks,Test Status,updated</ac:parameter>` +
		`<ac:parameter ac:name="maximumIssues">50</ac:parameter>` +
		`<ac:parameter ac:name="jqlQuery">issue in linkedIssues(&quot;DEV-1&quot;)   </ac:parameter></ac:structured-macro>` +
		`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="jqlQuery">project = OTHER</ac:parameter></ac:structured-macro>` +
		`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="columnIds">description</ac:parameter><ac:parameter ac:name="columns">description</ac:parameter>` +
		`<ac:parameter ac:name="jqlQuery">key = DEV-7</ac:parameter></ac:structured-macro>` +
		`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="columnIds">issuekey,description</ac:parameter><ac:parameter ac:name="columns">key,description</ac:parameter>` +
		`<ac:parameter ac:name="jqlQuery">project = DEV</ac:parameter></ac:structured-macro>` +
		`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="key">DEV-1</ac:parameter></ac:structured-macro>`}}}, nil
}

func TestPageIssueLists(t *testing.T) {
	var issues []map[string]any
	raw := `[{"key":"DEV-7","fields":{"summary":"Emergency call | priority","issuetype":{"name":"Story"},"status":{"name":"In Progress"},
		"fixVersions":[{"name":"2.1"},{"name":"2.2"}],"issuelinks":[{"outwardIssue":{"key":"DEV-1"}},{"inwardIssue":{"key":"DEV-9"}}],
		"customfield_7":{"value":"PASS"},"updated":"2026-10-30T10:00:00.000+0200","description":"Emergency *starts*"},
		"renderedFields":{"description":"<p>Emergency <b>starts</b> with:</p><ul><li>SOS button</li></ul>"}},
		{"key":"DEV-1","fields":{"summary":"Root","issuetype":{"name":"Epic"},"status":{"name":"Open"},"description":"Line one\nline two"}}]`
	if err := json.Unmarshal([]byte(raw), &issues); err != nil {
		t.Fatal(err)
	}
	type call struct {
		jql    string
		fields []string
		limit  int
		expand []string
	}
	var calls []call
	search := func(_ context.Context, jql string, fields []string, limit int, expand ...string) ([]map[string]any, error) {
		calls = append(calls, call{jql, fields, limit, expand})
		switch {
		case strings.Contains(jql, "OTHER"):
			return nil, errors.New("project OTHER does not exist")
		case jql == "key = DEV-7":
			return issues[:1], nil
		}
		return issues, nil
	}
	runCross(t)
	opts := TreeOptions{Source: &listPages{}, PageID: "5", SearchIssues: search, IssueBases: []string{"https://jira.example.com"},
		OpenIssue: func(context.Context, string) error { return nil }}
	m := newTree(context.Background(), opts, plainRenderer)
	m = drainTree(m, m.Init())

	if len(calls) != 4 {
		t.Fatalf("searches: %+v", calls)
	}
	for _, c := range calls {
		if strings.Contains(c.jql, "linkedIssues") && (c.limit != 50 || c.fields[0] != "summary" || c.fields[len(c.fields)-1] != "updated") {
			t.Errorf("search: %+v", c)
		}
		// Rendered fields only where a column may be rich text.
		if want := c.jql != "project = OTHER"; (len(c.expand) == 1 && c.expand[0] == "renderedFields") != want {
			t.Errorf("search %q expands %v", c.jql, c.expand)
		}
	}
	md := m.markdown["5"]
	for _, want := range []string{
		"**Jira issues** (2 issues) `issue in linkedIssues(\"DEV-1\")`",
		"| key | summary | type | status | fixVersions | issuelinks | Test Status | updated |",
		"| DEV-7 | Emergency call / priority | Story | In Progress | 2.1, 2.2 | DEV-1, DEV-9 | PASS | 2026-10-30 |",
		"| DEV-1 | Root | Epic | Open |",
		"**Jira issues** `project = OTHER`: project OTHER does not exist",
		"**DEV-1**",
		// A description-only list shows the description itself, formatted.
		"**Jira issues** (1 issue) `key = DEV-7`\n\nEmergency **starts** with:",
		"- SOS button",
		// Next to other columns it follows the table, under each key.
		"| key |\n|---|\n| DEV-7 |\n| DEV-1 |",
		"**DEV-1** · description\n\nLine one",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("page markdown lacks %q:\n%s", want, md)
		}
	}

	// L offers the listed issues after the ones the page names.
	m = pressTree(m, "L")
	if len(m.links) != 2 || m.links[0].issue.Key != "DEV-1" || m.links[1].issue.Key != "DEV-7" || !m.links[1].issue.List ||
		!strings.Contains(m.View(), "issue list") {
		t.Fatalf("picker: %+v\n%s", m.links, m.View())
	}

	// Without a searcher (no Jira) the lists show their queries only.
	opts.SearchIssues = nil
	m = newTree(context.Background(), opts, plainRenderer)
	m = drainTree(m, m.Init())
	if md := m.markdown["5"]; !strings.Contains(md, "Jira issues: `issue in linkedIssues(\"DEV-1\")`") {
		t.Errorf("without Jira:\n%s", md)
	}
}
