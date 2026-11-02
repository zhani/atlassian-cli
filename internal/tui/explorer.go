package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zhani/atlassian-cli/internal/bookmarks"
	"github.com/zhani/atlassian-cli/internal/jira"
)

// ExplorerItem is an entry of the explorer sidebar (a feature, epic or
// sprint) together with the query for the issues shown next to it.
type ExplorerItem struct {
	Key    string // issue key or sprint ID
	Title  string
	Status string
	URL    string // opened with o; "" when the item has no page

	JQL         string            // issues of the item; "" for none
	Annotations map[string]string // per-issue value for the annotation column
}

// ExplorerOptions configures the two-pane explorer.
type ExplorerOptions struct {
	Title    string // sidebar title, e.g. "Features"
	Items    []ExplorerItem
	Selected int // item selected at start

	// Issues configures the issue pane. Its JQL, Caption and Annotations
	// are set from the selected item; AnnotationTitle names the column.
	Issues Options

	// DrillJQL returns the query for the children of an issue in the issue
	// pane (e.g. the stories of an epic), or false when it has none.
	DrillJQL func(issue jira.Issue) (string, bool)

	// Kind is the bookmark kind of the sidebar items (feature, epic,
	// sprint); b bookmarks them when Issues.Bookmarks is set.
	Kind bookmarks.Kind
}

var explorerKeys = struct {
	focus, drill, back key.Binding
}{
	focus: key.NewBinding(key.WithKeys("tab", "w"), key.WithHelp("tab", "switch pane")),
	drill: key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l", "open children")),
	back:  key.NewBinding(key.WithKeys("h", "left", "backspace"), key.WithHelp("h", "back")),
}

var (
	styleSidebarFocused = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12"))
	styleSidebarBlurred = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("8"))
)

// drillFrame remembers the issue pane query to return to with "back".
type drillFrame struct {
	jql, caption string
	annotations  map[string]string
	selected     string
}

type explorerModel struct {
	opts       ExplorerOptions
	cursor     int
	offset     int // first visible sidebar row
	focusRight bool
	right      model
	stack      []drillFrame
	status     string
	statusOK   bool // status is a success message rather than an error

	width, height int
	quitting      bool
	initCmd       tea.Cmd // loads the first item's issues
}

func newExplorer(ctx context.Context, opts ExplorerOptions, render markdownRenderer) explorerModel {
	m := explorerModel{opts: opts, width: 100, height: 24}
	if opts.Selected > 0 && opts.Selected < len(opts.Items) {
		m.cursor = opts.Selected
	}
	m.right = newModel(ctx, opts.Issues, render)
	m.right.extraHelp = []key.Binding{explorerKeys.focus}
	if opts.DrillJQL != nil {
		m.right.extraHelp = append(m.right.extraHelp, explorerKeys.drill, explorerKeys.back)
	}
	m.resize()
	m.keepCursorVisible()
	m.initCmd = m.selectItem()
	return m
}

func (m explorerModel) Init() tea.Cmd { return m.initCmd }

// selectItem shows the issues of the item under the cursor.
func (m *explorerModel) selectItem() tea.Cmd {
	m.stack = nil
	if len(m.opts.Items) == 0 {
		return nil
	}
	it := m.opts.Items[m.cursor]
	return m.right.showQuery(it.JQL, it.Key+"  "+it.Title, it.Annotations)
}

func (m explorerModel) sidebarWidth() int {
	return min(44, max(26, m.width*3/10))
}

// resize gives the issue pane the space right of the sidebar.
func (m *explorerModel) resize() {
	next, _ := m.right.Update(tea.WindowSizeMsg{Width: m.width - m.sidebarWidth(), Height: m.height})
	m.right = next.(model)
}

func (m explorerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m.forward(msg)
}

// forward passes a message to the issue pane.
func (m explorerModel) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.right.Update(msg)
	m.right = next.(model)
	return m, cmd
}

func (m explorerModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, keys.forceQuit) {
		m.quitting = true
		return m, tea.Quit
	}
	m.status, m.statusOK = "", false

	// While typing a filter, choosing a transition or picking a bookmark,
	// the issue pane owns every key.
	if m.right.picker != nil || m.focusRight && (m.right.filtering || m.right.mode == modeTransition) {
		return m.forward(msg)
	}
	// B picks a bookmark to show in the issue pane, from either pane.
	if m.opts.Issues.Bookmarks != nil && key.Matches(msg, bookmarkKeys.picker) {
		m.focusRight = true
		return m.forward(msg)
	}

	switch {
	case key.Matches(msg, keys.quit):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, explorerKeys.focus):
		m.focusRight = !m.focusRight
		return m, nil
	case key.Matches(msg, paneKeys.left):
		m.focusRight = false
		return m, nil
	case key.Matches(msg, paneKeys.right):
		m.focusRight = true
		return m, nil
	}

	if m.focusRight {
		return m.handleRightKey(msg)
	}
	return m.handleSidebarKey(msg)
}

func (m explorerModel) handleRightKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	inList := m.right.mode == modeList
	switch {
	case inList && key.Matches(msg, explorerKeys.drill) && m.opts.DrillJQL != nil:
		c := m.right.table.Cursor()
		if c < 0 || c >= len(m.right.visible) {
			return m, nil
		}
		issue := m.right.issues[m.right.visible[c]]
		jql, ok := m.opts.DrillJQL(issue)
		if !ok {
			m.status = issue.Key + " has no child issues to open"
			return m, nil
		}
		m.stack = append(m.stack, drillFrame{m.right.opts.JQL, m.right.opts.Caption, m.right.opts.Annotations, issue.Key})
		cmd := m.right.showQuery(jql, issue.Key+"  "+issue.Fields.Summary, nil)
		return m, cmd
	case inList && key.Matches(msg, explorerKeys.back):
		if len(m.stack) == 0 {
			m.focusRight = false
			return m, nil
		}
		f := m.stack[len(m.stack)-1]
		m.stack = m.stack[:len(m.stack)-1]
		cmd := m.right.showQuery(f.jql, f.caption, f.annotations)
		m.right.pendingSelect = f.selected
		return m, cmd
	case inList && key.Matches(msg, keys.back) && m.right.filter.Value() == "":
		m.focusRight = false
		return m, nil
	}
	return m.forward(msg)
}

func (m explorerModel) handleSidebarKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.opts.Items)
	if n == 0 {
		return m, nil
	}
	prev := m.cursor
	switch {
	case key.Matches(msg, keys.up):
		m.cursor = max(0, m.cursor-1)
	case key.Matches(msg, keys.down):
		m.cursor = min(n-1, m.cursor+1)
	case key.Matches(msg, keys.top):
		m.cursor = 0
	case key.Matches(msg, keys.bottom):
		m.cursor = n - 1
	case key.Matches(msg, keys.pageUp):
		m.cursor = max(0, m.cursor-m.sidebarRows())
	case key.Matches(msg, keys.pageDown):
		m.cursor = min(n-1, m.cursor+m.sidebarRows())
	case key.Matches(msg, keys.detail), key.Matches(msg, explorerKeys.drill):
		m.focusRight = true
		return m, nil
	case key.Matches(msg, keys.open), key.Matches(msg, keys.copyURL), key.Matches(msg, keys.copyKey):
		it := m.opts.Items[m.cursor]
		fn, arg, done := m.opts.Issues.OpenURL, it.URL, "Opened "+it.Key
		switch {
		case key.Matches(msg, keys.copyURL):
			fn, done = m.opts.Issues.Copy, "Copied "+it.URL
		case key.Matches(msg, keys.copyKey):
			fn, arg, done = m.opts.Issues.Copy, it.Key, "Copied "+it.Key
		}
		if arg == "" || fn == nil {
			m.status = "No link for " + it.Key
			return m, nil
		}
		if err := fn(arg); err != nil {
			m.status = err.Error()
		} else {
			m.status = done
		}
		return m, nil
	case key.Matches(msg, bookmarkKeys.toggle) && m.opts.Issues.Bookmarks != nil && m.opts.Kind != "":
		it := m.opts.Items[m.cursor]
		b := bookmarks.Bookmark{Kind: m.opts.Kind, Key: it.Key, Title: it.Title, URL: it.URL}
		var isErr bool
		m.status, isErr = toggleBookmark(m.opts.Issues.Bookmarks, b)
		m.statusOK = !isErr
		return m, nil
	case key.Matches(msg, keys.refresh), key.Matches(msg, keys.help):
		return m.forward(msg)
	}
	if m.cursor != prev {
		m.keepCursorVisible()
		return m, m.selectItem()
	}
	return m, nil
}

// sidebarRows is the number of items that fit in the sidebar.
func (m explorerModel) sidebarRows() int {
	return max(1, m.height-5) // border (2), title, blank line, footer
}

func (m *explorerModel) keepCursorVisible() {
	rows := m.sidebarRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
}

func (m explorerModel) View() string {
	if m.quitting {
		return ""
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, m.sidebarView(), m.right.View())
}

func (m explorerModel) sidebarView() string {
	w := m.sidebarWidth() - 2 // border
	var sb strings.Builder
	sb.WriteString(styleTitle.Render(fmt.Sprintf(" %s (%d)", m.opts.Title, len(m.opts.Items))) + "\n\n")
	if len(m.opts.Items) == 0 {
		sb.WriteString(styleDim.Render(" Nothing found") + "\n")
	}
	end := min(len(m.opts.Items), m.offset+m.sidebarRows())
	for i := m.offset; i < end; i++ {
		it := m.opts.Items[i]
		label := truncate(it.Key+" "+it.Title, max(5, w-3))
		if i == m.cursor {
			sb.WriteString(styleCursor.Render("▶ "+label) + "\n")
		} else {
			sb.WriteString("  " + label + "\n")
		}
	}

	footer := ""
	switch {
	case m.status != "":
		style := styleErr
		if m.statusOK {
			style = styleOK
		}
		footer = style.Render(" " + truncate(m.status, max(5, w-2)))
	case len(m.opts.Items) > 0 && m.opts.Items[m.cursor].Status != "":
		footer = styleDim.Render(" " + truncate(m.opts.Items[m.cursor].Status, max(5, w-2)))
	}
	if m.focusRight && len(m.stack) > 0 {
		footer = styleDim.Render(" " + truncate("h: back to "+m.stack[len(m.stack)-1].selected, max(5, w-2)))
	}

	body := lipgloss.NewStyle().Width(w).Height(m.height - 3).Render(sb.String())
	style := styleSidebarBlurred
	if !m.focusRight {
		style = styleSidebarFocused
	}
	return style.Render(lipgloss.JoinVertical(lipgloss.Left, body, footer))
}

// RunExplorer launches the two-pane explorer and blocks until the user
// quits.
func RunExplorer(ctx context.Context, opts ExplorerOptions) error {
	m := newExplorer(ctx, opts, glamourRenderer())
	m.right.gfx = newGraphics()
	final, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx), tea.WithOutput(m.right.gfx.out)).Run()
	if fm, ok := final.(explorerModel); ok {
		fm.right.clearInline()
	}
	return err
}
