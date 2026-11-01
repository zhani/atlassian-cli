package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/zhani/atlassian-cli/internal/jira"
)

// Fixed column widths; Summary takes the remaining space.
const (
	colKey        = 12
	colType       = 14
	colStatus     = 14
	colAssignee   = 18
	colAnnotation = 18
	colMinSum     = 20
	cellPadding   = 2 // the table style pads each cell by one on both sides
)

// columnsFor returns the table columns for a terminal width. The annotation
// column only exists when titled; on narrow terminals Type, then Assignee
// are hidden (width 0).
func columnsFor(width int, annotationTitle string) []table.Column {
	typeW, assigneeW, annotationW := colType, colAssignee, 0
	if annotationTitle != "" {
		annotationW = colAnnotation
	}
	fixed := func() int {
		n, cols := colKey+colStatus, 3 // key, status, summary
		for _, w := range []int{typeW, assigneeW, annotationW} {
			if w > 0 {
				n += w
				cols++
			}
		}
		return n + cols*cellPadding
	}
	if width-fixed() < colMinSum {
		typeW = 0
	}
	if width-fixed() < colMinSum {
		assigneeW = 0
	}
	return []table.Column{
		{Title: "Key", Width: colKey},
		{Title: "Type", Width: typeW},
		{Title: "Status", Width: colStatus},
		{Title: "Assignee", Width: assigneeW},
		{Title: annotationTitle, Width: annotationW},
		{Title: "Summary", Width: max(colMinSum, width-fixed())},
	}
}

func (m *model) issueRow(issue jira.Issue) table.Row {
	f := issue.Fields
	return table.Row{issue.Key, safeType(f.IssueType), safeName(f.Status), safeUser(f.Assignee), m.opts.Annotations[issue.Key], f.Summary}
}

// applyFilter recomputes the visible rows: every issue, or the fuzzy
// matches of the filter across all columns, best first.
func (m *model) applyFilter() {
	q := strings.TrimSpace(m.filter.Value())
	m.visible = nil
	m.matches = nil
	rows := make([]table.Row, 0, len(m.issues))
	if q != "" {
		m.matches = map[int]rowMatch{}
	}
	for i, issue := range m.issues {
		row := m.issueRow(issue)
		if q != "" {
			rm, ok := matchRow(q, row)
			if !ok {
				continue
			}
			m.matches[i] = rm
		}
		m.visible = append(m.visible, i)
		rows = append(rows, row)
	}
	if q != "" {
		m.sortByScore(rows)
	}
	m.table.SetRows(rows)
	// Keep the cursor on a row; it is -1 after the table was empty.
	switch c := m.table.Cursor(); {
	case c >= len(rows):
		m.table.SetCursor(len(rows) - 1)
	case c < 0 && len(rows) > 0:
		m.table.SetCursor(0)
	}
	if q != "" {
		m.keepFilterCursorVisible()
	}
}

func safeName(s *jira.Status) string {
	if s == nil {
		return ""
	}
	return s.Name
}

func safeType(t *jira.IssueType) string {
	if t == nil {
		return ""
	}
	return t.Name
}

func safePriority(p *jira.Priority) string {
	if p == nil {
		return ""
	}
	return p.Name
}

func safeUser(u *jira.User) string {
	if u == nil {
		return "Unassigned"
	}
	return u.DisplayName
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-3]) + "..."
}
