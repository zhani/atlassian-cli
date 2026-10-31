package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/putcho01/atlassian-cli/internal/htmlconv"
	"github.com/putcho01/atlassian-cli/internal/jira"
)

// maxComments is how many of the most recent comments the detail view shows.
const maxComments = 5

// issueMarkdown builds the markdown document shown in the detail view. Text
// fields prefer their HTML-rendered versions (converted to markdown) and fall
// back to the raw wiki markup.
func issueMarkdown(issue *jira.Issue) string {
	f := issue.Fields
	var sb strings.Builder

	fmt.Fprintf(&sb, "# %s: %s\n\n", issue.Key, f.Summary)

	// Header fields, a few per list item (glamour has no hard line breaks).
	lines := [][]string{
		{field("Status", safeName(f.Status)), field("Type", safeType(f.IssueType)), field("Priority", safePriority(f.Priority))},
		{field("Assignee", safeUser(f.Assignee)), field("Reporter", safeUser(f.Reporter))},
		{field("Created", formatTime(f.Created)), field("Updated", formatTime(f.Updated))},
	}
	if len(f.Labels) > 0 {
		lines = append(lines, []string{field("Labels", strings.Join(f.Labels, ", "))})
	}
	for _, l := range lines {
		sb.WriteString("- " + strings.Join(l, " · ") + "\n")
	}
	sb.WriteString("\n")

	description := f.Description
	if issue.RenderedFields != nil && issue.RenderedFields.Description != "" {
		description = htmlconv.Convert(issue.RenderedFields.Description)
	}
	sb.WriteString("\n## Description\n\n")
	if strings.TrimSpace(description) == "" {
		sb.WriteString("_No description._\n")
	} else {
		sb.WriteString(description + "\n")
	}

	if len(f.IssueLinks) > 0 {
		sb.WriteString("\n## Linked issues\n\n")
		for _, l := range f.IssueLinks {
			phrase, linked := l.Describe()
			if linked == nil {
				continue
			}
			fmt.Fprintf(&sb, "- %s **%s** %s _(%s)_\n", phrase, linked.Key, linked.Fields.Summary, safeName(linked.Fields.Status))
		}
	}

	if len(f.Subtasks) > 0 {
		sb.WriteString("\n## Subtasks\n\n")
		for _, s := range f.Subtasks {
			fmt.Fprintf(&sb, "- **%s** %s _(%s)_\n", s.Key, s.Fields.Summary, safeName(s.Fields.Status))
		}
	}

	if f.Comment != nil && len(f.Comment.Comments) > 0 {
		comments := f.Comment.Comments
		var rendered []jira.Comment
		if issue.RenderedFields != nil && issue.RenderedFields.Comment != nil {
			rendered = issue.RenderedFields.Comment.Comments
		}
		start := max(0, len(comments)-maxComments)
		total := max(f.Comment.Total, len(comments))
		fmt.Fprintf(&sb, "\n## Comments (%d of %d)\n", len(comments)-start, total)
		for i := start; i < len(comments); i++ {
			c := comments[i]
			body := c.Body
			if i < len(rendered) && rendered[i].ID == c.ID && rendered[i].Body != "" {
				body = htmlconv.Convert(rendered[i].Body)
			}
			fmt.Fprintf(&sb, "\n### %s · %s\n\n%s\n", safeUser(c.Author), formatTime(c.Created), body)
		}
	}

	return sb.String()
}

// formatTime shortens Jira timestamps (2024-01-02T15:04:05.000+0000) to
// "2024-01-02 15:04", leaving unparseable values unchanged.
func formatTime(s string) string {
	t, err := time.Parse("2006-01-02T15:04:05.000-0700", s)
	if err != nil {
		return s
	}
	return t.Format("2006-01-02 15:04")
}

func field(label, value string) string {
	if value == "" {
		value = "–"
	}
	return "**" + label + ":** " + value
}

// markdownRenderer renders markdown for a given wrap width.
type markdownRenderer func(md string, width int) string

// glamourRenderer returns a renderer using a glamour style picked once, before
// the program starts: querying the terminal background from inside the
// running TUI would interfere with its input.
func glamourRenderer() markdownRenderer {
	style := os.Getenv("GLAMOUR_STYLE")
	if style == "" {
		style = "light"
		if lipgloss.HasDarkBackground() {
			style = "dark"
		}
	}
	cache := map[int]*glamour.TermRenderer{}
	return func(md string, width int) string {
		r, ok := cache[width]
		if !ok {
			var err error
			r, err = glamour.NewTermRenderer(glamour.WithStylePath(style), glamour.WithWordWrap(width))
			if err != nil {
				return md
			}
			cache[width] = r
		}
		out, err := r.Render(md)
		if err != nil {
			return md
		}
		return out
	}
}

// plainRenderer returns the markdown unchanged; used in tests.
func plainRenderer(md string, _ int) string { return md }
