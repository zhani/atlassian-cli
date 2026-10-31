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

	md := issueMarkdown(issue)
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
	md := issueMarkdown(&jira.Issue{Key: "X-1", Fields: jira.IssueFields{Description: "plain text"}})
	if !strings.Contains(md, "plain text") {
		t.Errorf("expected raw description, got:\n%s", md)
	}
	md = issueMarkdown(&jira.Issue{Key: "X-1"})
	if !strings.Contains(md, "_No description._") {
		t.Errorf("expected placeholder, got:\n%s", md)
	}
}
