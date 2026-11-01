package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Fuzzy filtering of the issue list (/), as in the page tree and bookmark
// browser: the list narrows live to the issues matching the query, best
// first, with matched characters highlighted. enter opens the selected issue
// and keeps the list filtered (esc clears it); esc while typing returns to
// the full list.

// issueColumns are the table columns in display order; matching covers
// all of them.
const (
	icKey = iota
	icType
	icStatus
	icAssignee
	icAnnotation
	icSummary
	icCount
)

// matchOrder lists the columns in the order they are joined for matching:
// key and summary first, as they are what one usually types.
var matchOrder = []int{icKey, icSummary, icStatus, icType, icAssignee, icAnnotation}

// rowMatch is a fuzzy match of an issue row: score and matched rune
// positions per column.
type rowMatch struct {
	score int
	pos   [icCount][]int
}

func matchRow(q string, row table.Row) (rowMatch, bool) {
	var sb strings.Builder
	starts := [icCount]int{}
	n := 0
	for i, col := range matchOrder {
		if i > 0 {
			sb.WriteByte('\n')
			n++
		}
		starts[col] = n
		sb.WriteString(row[col])
		n += len([]rune(row[col]))
	}
	score, pos, ok := fuzzyMatch(q, sb.String())
	if !ok {
		return rowMatch{}, false
	}
	rm := rowMatch{score: score}
	for _, p := range pos {
		for _, col := range matchOrder {
			if l := len([]rune(row[col])); p >= starts[col] && p < starts[col]+l {
				rm.pos[col] = append(rm.pos[col], p-starts[col])
			}
		}
	}
	return rm, true
}

// filterQuery is the active filter, typed or kept after enter.
func (m model) filterQuery() string { return strings.TrimSpace(m.filter.Value()) }

// startFilter opens the filter input, keeping a filter kept from before.
func (m *model) startFilter() tea.Cmd {
	m.filtering = true
	m.filterFrom = m.selectedKey()
	m.layout()
	return m.filter.Focus()
}

func (m model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, filterKeys.cancel):
		m.filtering = false
		m.filter.Blur()
		m.filter.SetValue("")
		m.applyFilter()
		m.selectKey(m.filterFrom)
		m.layout()
		return m, nil
	case key.Matches(msg, filterKeys.accept):
		m.filtering = false
		m.filter.Blur()
		m.layout()
		if m.filterQuery() == "" || len(m.visible) == 0 {
			return m, nil
		}
		return m.openDetail() // the list stays filtered behind the issue
	case key.Matches(msg, filterKeys.prev):
		if n := len(m.visible); n > 0 {
			m.table.SetCursor((m.table.Cursor() - 1 + n) % n)
			m.keepFilterCursorVisible()
		}
		return m, nil
	case key.Matches(msg, filterKeys.next):
		if n := len(m.visible); n > 0 {
			m.table.SetCursor((m.table.Cursor() + 1) % n)
			m.keepFilterCursorVisible()
		}
		return m, nil
	}
	prev := m.filter.Value()
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() != prev {
		m.applyFilter()
		if m.filterQuery() == "" {
			m.selectKey(m.filterFrom)
		} else {
			m.table.SetCursor(0) // best match
		}
		m.keepFilterCursorVisible()
	}
	return m, cmd
}

// sortByScore orders the visible rows best match first (stable).
func (m *model) sortByScore(rows []table.Row) {
	idx := make([]int, len(m.visible))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return m.matches[m.visible[idx[a]]].score > m.matches[m.visible[idx[b]]].score
	})
	vis := make([]int, len(idx))
	sorted := make([]table.Row, len(idx))
	for i, j := range idx {
		vis[i], sorted[i] = m.visible[j], rows[j]
	}
	copy(m.visible, vis)
	copy(rows, sorted)
}

// filterRows is how many rows the filtered list shows.
func (m model) filterRows() int { return max(1, m.bodyHeight-2) } // header and rule

func (m *model) keepFilterCursorVisible() {
	c, rows := m.table.Cursor(), m.filterRows()
	if c < m.filterOffset {
		m.filterOffset = c
	}
	if c >= m.filterOffset+rows {
		m.filterOffset = c - rows + 1
	}
	m.filterOffset = max(0, min(m.filterOffset, len(m.visible)-1))
}

// filteredView renders the filtered list like the table, with the matched
// characters highlighted.
func (m model) filteredView() string {
	cols := columnsFor(m.width, m.opts.AnnotationTitle)
	var header strings.Builder
	for _, c := range cols {
		if c.Width > 0 {
			header.WriteString(" " + padRight(truncate(c.Title, c.Width), c.Width) + " ")
		}
	}
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(header.String()) + "\n")
	sb.WriteString(styleDim.Render(strings.Repeat("─", max(10, m.width))) + "\n")
	if len(m.visible) == 0 {
		sb.WriteString(styleDim.Render(" No issue matches"))
	}
	rows := m.filterRows()
	end := min(len(m.visible), m.filterOffset+rows)
	for i := m.filterOffset; i < end; i++ {
		idx := m.visible[i]
		row := m.issueRow(m.issues[idx])
		match := m.matches[idx]
		base := lipgloss.NewStyle()
		if i == m.table.Cursor() {
			base = styleRowSelected
		}
		var line strings.Builder
		for c, col := range cols {
			if col.Width == 0 {
				continue
			}
			line.WriteString(base.Render(" ") + cell(row[c], match.pos[c], col.Width, base) + base.Render(" "))
		}
		sb.WriteString(line.String() + "\n")
	}
	return lipgloss.NewStyle().Height(m.bodyHeight).MaxHeight(m.bodyHeight).Render(strings.TrimRight(sb.String(), "\n"))
}

func padRight(s string, w int) string {
	if pad := w - ansi.StringWidth(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// filterFooter is the filter box while typing, or the kept filter's line.
func (m model) filterFooter() string {
	if m.filtering {
		return filterBox(m.filter, fmt.Sprintf("%d/%d", len(m.visible), len(m.issues)), m.width)
	}
	return styleDim.Render(" filter: " + m.filter.Value() + "  (esc to clear, / to edit)")
}
