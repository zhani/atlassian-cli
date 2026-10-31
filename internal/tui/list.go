package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/putcho01/atlassian-cli/internal/jira"
)

// Fixed column widths; Summary takes the remaining space.
const (
	colKey      = 12
	colType     = 14
	colStatus   = 14
	colAssignee = 18
	colMinSum   = 20
	cellPadding = 2 // the table style pads each cell by one on both sides
)

// columnsFor returns the table columns for a terminal width. Type and
// Assignee are hidden (width 0) on narrow terminals.
func columnsFor(width int) []table.Column {
	typeW, assigneeW := colType, colAssignee
	fixed := func() int {
		n := colKey + colStatus + typeW + assigneeW
		cols := 3 // key, status, summary
		if typeW > 0 {
			cols++
		}
		if assigneeW > 0 {
			cols++
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
		{Title: "Summary", Width: max(colMinSum, width-fixed())},
	}
}

func issueRow(issue jira.Issue) table.Row {
	f := issue.Fields
	return table.Row{issue.Key, safeType(f.IssueType), safeName(f.Status), safeUser(f.Assignee), f.Summary}
}

// applyFilter recomputes the visible rows from the filter text, matching it
// case-insensitively against every column.
func (m *model) applyFilter() {
	needle := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.visible = nil
	rows := make([]table.Row, 0, len(m.issues))
	for i, issue := range m.issues {
		row := issueRow(issue)
		if needle != "" && !strings.Contains(strings.ToLower(strings.Join(row, "\x00")), needle) {
			continue
		}
		m.visible = append(m.visible, i)
		rows = append(rows, row)
	}
	m.table.SetRows(rows)
	if m.table.Cursor() >= len(rows) {
		m.table.SetCursor(max(0, len(rows)-1))
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
