package tui

import (
	"strings"
	"testing"

	"github.com/putcho01/atlassian-cli/internal/jira"
)

func TestIssueMarkdown(t *testing.T) {
	t.Parallel()
	comments := make([]jira.Comment, 7)
	rendered := make([]jira.Comment, 7)
	for i := range comments {
		id := string(rune('a' + i))
		comments[i] = jira.Comment{ID: id, Body: "raw " + id, Author: &jira.User{DisplayName: "User " + id}, Created: "2024-03-0" + string(rune('1'+i)) + "T10:20:30.000+0000"}
		rendered[i] = jira.Comment{ID: id, Body: "<p>html <em>" + id + "</em></p>"}
	}
	issue := &jira.Issue{
		Key: "PROJ-7",
		Fields: jira.IssueFields{
			Summary:     "Pipes | in summary",
			Description: "h1. raw",
			Status:      &jira.Status{Name: "Open"},
			Assignee:    nil,
			Labels:      []string{"a", "b"},
			Created:     "2024-01-02T03:04:05.000+0000",
			IssueLinks: []jira.IssueLink{
				{Type: jira.IssueLinkType{Inward: "is implemented by", Outward: "implements"}, InwardIssue: &jira.Issue{Key: "PROJ-8", Fields: jira.IssueFields{Summary: "Impl", Status: &jira.Status{Name: "Done"}}}},
			},
			Comment: &jira.CommentPage{Comments: comments, Total: 7},
		},
		RenderedFields: &jira.RenderedFields{
			Description: "<h2>Rendered</h2>",
			Comment:     &jira.CommentPage{Comments: rendered},
		},
	}

	md := issueMarkdown(issue, nil)
	wants := []string{
		"# PROJ-7: Pipes | in summary",
		"**Assignee:** Unassigned",
		"**Labels:** a, b",
		"**Created:** 2024-01-02 03:04",
		"## Rendered",
		"- is implemented by **PROJ-8** Impl _(Done)_",
		"## Comments (5 of 7)",
		"### User g · 2024-03-07 10:20",
		"html *g*",
	}
	for _, w := range wants {
		if !strings.Contains(md, w) {
			t.Errorf("markdown missing %q:\n%s", w, md)
		}
	}
	if strings.Contains(md, "User a") || strings.Contains(md, "h1. raw") {
		t.Errorf("markdown should skip old comments and the raw description:\n%s", md)
	}
}

func TestIssueMarkdownFallsBackToRawText(t *testing.T) {
	t.Parallel()
	md := issueMarkdown(&jira.Issue{Key: "X-1", Fields: jira.IssueFields{Description: "plain text"}}, nil)
	if !strings.Contains(md, "plain text") {
		t.Errorf("expected raw description, got:\n%s", md)
	}
	md = issueMarkdown(&jira.Issue{Key: "X-1"}, nil)
	if !strings.Contains(md, "_No description._") {
		t.Errorf("expected placeholder, got:\n%s", md)
	}
}

func TestIssueMarkdownCustomFields(t *testing.T) {
	t.Parallel()
	issue := &jira.Issue{
		Key: "D-1",
		Fields: jira.IssueFields{Custom: map[string]any{
			"customfield_1": "*Preconditions:*\n- app 1.2\n- profile without passcode",
			"customfield_2": "<p>Wed Jun 10 13:21:12 GMT 2026: <br/>\nConsider splitting</p>",
			"customfield_3": []any{
				map[string]any{"name": "Done", "checked": true, "isHeader": false},
				map[string]any{"name": "Todo", "checked": false},
			},
			"customfield_4": map[string]any{"value": "SEVERE"},
			"customfield_5": map[string]any{"self": "x"},
			"customfield_6": "",
			"customfield_7": "{summaryBean=com.atlassian.jira.plugin.devstatus.rest.SummaryBean@2634afd0[summary={}]}",
			"customfield_9": "[ProjectComponentImpl { name='Security', id='34831' }]",
			"customfield_8": "com.atlassian.greenhopper.service.sprint.Sprint@1f[id=3,state=ACTIVE,name=Sprint 7,startDate=x]",
		}},
		RenderedFields: &jira.RenderedFields{Custom: map[string]string{
			"customfield_1": "<p><b>Preconditions:</b></p><ul><li>app 1.2</li><li>profile without passcode</li></ul>",
		}},
		Names: map[string]string{"customfield_1": "Steps to Reproduce", "customfield_2": "Comment log", "customfield_3": "DoD", "customfield_4": "Severity"},
	}

	md := issueMarkdown(issue, nil)
	wants := []string{
		"## Comment log\n\nWed Jun 10 13:21:12 GMT 2026:",
		"## DoD\n\n- [x] Done\n- [ ] Todo",
		"## Steps to Reproduce\n\n**Preconditions:**",
		"- profile without passcode",
		"## Fields\n\n- **customfield_8:** Sprint 7\n- **Severity:** SEVERE",
	}
	for _, w := range wants {
		if !strings.Contains(md, w) {
			t.Errorf("markdown missing %q:\n%s", w, md)
		}
	}
	for _, unwanted := range []string{"customfield_5", "customfield_6", "customfield_7", "summaryBean", "customfield_9", "<p>"} {
		if strings.Contains(md, unwanted) {
			t.Errorf("markdown should not contain %q:\n%s", unwanted, md)
		}
	}

	// Selected fields only, in the given order, by name or ID.
	md = issueMarkdown(issue, []string{"steps to reproduce", "customfield_2"})
	steps, log := strings.Index(md, "## Steps to Reproduce"), strings.Index(md, "## Comment log")
	if steps < 0 || log < 0 || steps > log || strings.Contains(md, "DoD") || strings.Contains(md, "Severity") {
		t.Errorf("DetailFields selection wrong:\n%s", md)
	}
}
