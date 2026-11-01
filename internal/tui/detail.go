package tui

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/zhani/atlassian-cli/internal/htmlconv"
	"github.com/zhani/atlassian-cli/internal/jira"
)

// maxComments is how many of the most recent comments the detail view shows.
const maxComments = 5

// issueMarkdown builds the markdown document shown in the detail view. Text
// fields prefer their HTML-rendered versions (converted to markdown) and fall
// back to the raw wiki markup. detailFields limits the custom fields shown
// (names or IDs, in order); empty shows all non-empty ones.
func issueMarkdown(issue *jira.Issue, detailFields []string) string {
	f := issue.Fields
	var sb strings.Builder

	fmt.Fprintf(&sb, "# %s: %s\n\n", issue.Key, f.Summary)

	// Header fields, a few per list item (glamour has no hard line breaks).
	lines := [][]string{
		{field("Status", safeName(f.Status)), field("Resolution", f.ResolutionName())},
		{field("Type", safeType(f.IssueType)), field("Priority", safePriority(f.Priority))},
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

	sections, short := customFields(issue, detailFields)
	for _, sec := range sections {
		fmt.Fprintf(&sb, "\n## %s\n\n%s\n", sec[0], sec[1])
	}
	if len(short) > 0 {
		sb.WriteString("\n## Fields\n\n")
		for _, fv := range short {
			sb.WriteString("- " + field(fv[0], fv[1]) + "\n")
		}
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

// shortValueLimit is the length above which a text field gets its own
// section instead of a line in the Fields list.
const shortValueLimit = 80

// customFields renders the custom fields of an issue: long text (HTML is
// converted to markdown) and checklists as sections, other values as short
// name/value pairs. Each is [name, markdown]. Empty values and objects
// without a readable form are left out.
func customFields(issue *jira.Issue, only []string) (sections, short [][2]string) {
	f := issue.Fields
	name := func(id string) string {
		if n := issue.Names[id]; n != "" {
			return n
		}
		return id
	}

	ids := make([]string, 0, len(f.Custom))
	for id := range f.Custom {
		ids = append(ids, id)
	}
	if len(only) > 0 {
		ids = selectFields(ids, only, name)
	} else {
		sort.Slice(ids, func(i, j int) bool { return strings.ToLower(name(ids[i])) < strings.ToLower(name(ids[j])) })
	}

	for _, id := range ids {
		v := f.Custom[id]
		if items, ok := checklist(v); ok {
			sections = append(sections, [2]string{name(id), items})
			continue
		}
		if text, ok := v.(string); ok {
			text = strings.TrimSpace(text)
			if javaObject.MatchString(text) {
				// Internal state such as the Development field's summary
				// beans; keep only what simplifies (legacy sprint strings).
				if sv, ok := jira.SimplifyValue(text).(string); ok && sv != text {
					short = append(short, [2]string{name(id), sv})
				}
				continue
			}
			if rendered := renderedCustom(issue, id); rendered != "" {
				text = htmlconv.Convert(rendered)
			} else if strings.HasPrefix(text, "<") {
				text = htmlconv.Convert(text)
			}
			if text == "" {
				continue
			}
			if len(text) > shortValueLimit || strings.Contains(text, "\n") {
				sections = append(sections, [2]string{name(id), text})
			} else {
				short = append(short, [2]string{name(id), text})
			}
			continue
		}
		if jira.SimplifyValue(v) == nil {
			continue // empty, or an object without a readable form
		}
		short = append(short, [2]string{name(id), jira.DisplayValue(v)})
	}
	return sections, short
}

// javaObject matches Java toString() output that some fields return
// (com.atlassian.…Bean@1a2b3c4d[...], ProjectComponentImpl { name='…' }).
var javaObject = regexp.MustCompile(`com\.atlassian\.[\w.]+@[0-9a-f]+|\b\w+(?:Impl|Bean) ?\{ ?\w+=`)

// selectFields returns the IDs matching refs (IDs or names, case-insensitive)
// in the order of refs.
func selectFields(ids, refs []string, name func(string) string) []string {
	var out []string
	for _, ref := range refs {
		for _, id := range ids {
			if strings.EqualFold(id, ref) || strings.EqualFold(name(id), ref) {
				out = append(out, id)
			}
		}
	}
	return out
}

func renderedCustom(issue *jira.Issue, id string) string {
	if issue.RenderedFields == nil {
		return ""
	}
	return issue.RenderedFields.Custom[id]
}

// checklist renders checklist field values (lists of items with a name and
// a checked flag, as stored by checklist apps) as a markdown task list.
func checklist(v any) (string, bool) {
	items, ok := v.([]any)
	if !ok || len(items) == 0 {
		return "", false
	}
	var sb strings.Builder
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			return "", false
		}
		label, okName := m["name"].(string)
		checked, okChecked := m["checked"].(bool)
		if !okName || !okChecked {
			return "", false
		}
		switch {
		case m["isHeader"] == true:
			sb.WriteString("\n**" + label + "**\n\n")
		case checked:
			sb.WriteString("- [x] " + label + "\n")
		default:
			sb.WriteString("- [ ] " + label + "\n")
		}
	}
	return strings.TrimSpace(sb.String()), true
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
