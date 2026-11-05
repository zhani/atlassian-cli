package tui

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zhani/atlassian-cli/internal/bookmarks"
)

// BookmarkStore is the bookmark file as the bookmark browser uses it;
// *bookmarks.Store implements it.
type BookmarkStore interface {
	List() ([]bookmarks.Bookmark, error)
	Delete(b bookmarks.Bookmark) error
	SetName(b bookmarks.Bookmark, name string) error
}

// BookmarkOptions configures the bookmark browser.
type BookmarkOptions struct {
	Store    BookmarkStore
	Selected string           // ID of the bookmark selected at start
	Kinds    []bookmarks.Kind // only these kinds; none: all

	// Message is shown in the status line at start (e.g. why opening the
	// last bookmark failed).
	Message    string
	MessageErr bool

	OpenURL func(url string) error
	Copy    func(text string) error
}

var bmKeys = struct {
	open, web, del, rename, confirm key.Binding
}{
	open:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open")),
	web:     key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open in browser")),
	del:     key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d", "delete")),
	rename:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rename")),
	confirm: key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "confirm")),
}

var (
	styleRowSelected = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("12"))
	styleKind        = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
)

// bookmarkMatch is a fuzzy match of a bookmark: matched rune positions in
// its kind, key/query and name columns.
type bookmarkMatch struct {
	score              int
	kind, target, name []int
}

type bookmarkModel struct {
	opts    BookmarkOptions
	all     []bookmarks.Bookmark
	visible []int // indices into all; ordered by match score while filtering
	matches map[int]bookmarkMatch
	cursor  int
	offset  int

	filter     textinput.Model
	filtering  bool
	filterFrom string // ID selected when the filter opened, for esc
	rename     textinput.Model
	renaming   bool
	deleting   bool

	help      help.Model
	status    string
	statusErr bool

	chosen        *bookmarks.Bookmark
	width, height int
	quitting      bool
	embedded      bool // a picker inside another view: closing does not quit
}

func newBookmarkModel(opts BookmarkOptions) bookmarkModel {
	r := textinput.New()
	r.Prompt = " Name: "
	m := bookmarkModel{opts: opts, filter: newTreeFilterInput(), rename: r, help: help.New(), width: 100, height: 24}
	m.reload(opts.Selected)
	if opts.Message != "" {
		m.setStatus(opts.Message, opts.MessageErr)
	}
	m.layout()
	return m
}

// reload reads the bookmarks and selects the one with the given ID.
func (m *bookmarkModel) reload(selectID string) {
	list, err := m.opts.Store.List()
	if err != nil {
		m.setStatus(err.Error(), true)
	}
	m.all = list[:0:0]
	for _, b := range list {
		if len(m.opts.Kinds) == 0 || slices.Contains(m.opts.Kinds, b.Kind) {
			m.all = append(m.all, b)
		}
	}
	m.applyFilter()
	m.selectID(selectID)
}

func (m *bookmarkModel) selectID(id string) {
	for i, idx := range m.visible {
		if m.all[idx].ID() == id {
			m.cursor = i
		}
	}
	m.keepCursorVisible()
}

// applyFilter shows every bookmark, or while filtering the fuzzy matches of
// the query, best first.
func (m *bookmarkModel) applyFilter() {
	q := strings.TrimSpace(m.filter.Value())
	m.visible = m.visible[:0]
	m.matches = nil
	if !m.filtering || q == "" {
		for i := range m.all {
			m.visible = append(m.visible, i)
		}
		m.cursor = min(m.cursor, max(0, len(m.visible)-1))
		return
	}
	m.matches = map[int]bookmarkMatch{}
	for i, b := range m.all {
		if bm, ok := matchBookmark(q, b); ok {
			m.matches[i] = bm
			m.visible = append(m.visible, i)
		}
	}
	sort.SliceStable(m.visible, func(a, b int) bool { return m.matches[m.visible[a]].score > m.matches[m.visible[b]].score })
	m.cursor, m.offset = 0, 0
}

// matchBookmark matches the query against a bookmark's name, key or query,
// kind and title together, so terms can match different columns.
func matchBookmark(q string, b bookmarks.Bookmark) (bookmarkMatch, bool) {
	name, target, kind := b.DisplayName(), b.Target(), string(b.Kind)
	text := name + "\n" + target + "\n" + kind
	if b.Title != "" && b.Title != name {
		text += "\n" + b.Title // searchable, not shown
	}
	score, pos, ok := fuzzyMatch(q, text)
	if !ok {
		return bookmarkMatch{}, false
	}
	bm := bookmarkMatch{score: score}
	nameEnd := len([]rune(name))
	targetEnd := nameEnd + 1 + len([]rune(target))
	kindEnd := targetEnd + 1 + len([]rune(kind))
	for _, p := range pos {
		switch {
		case p < nameEnd:
			bm.name = append(bm.name, p)
		case p > nameEnd && p < targetEnd:
			bm.target = append(bm.target, p-nameEnd-1)
		case p > targetEnd && p < kindEnd:
			bm.kind = append(bm.kind, p-targetEnd-1)
		}
	}
	return bm, true
}

func (m bookmarkModel) selected() (bookmarks.Bookmark, bool) {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return bookmarks.Bookmark{}, false
	}
	return m.all[m.visible[m.cursor]], true
}

func (m *bookmarkModel) setStatus(s string, isErr bool) { m.status, m.statusErr = s, isErr }

func (m *bookmarkModel) layout() {
	m.help.Width = m.width
	m.keepCursorVisible()
}

// listRows is the number of bookmarks that fit on screen.
func (m bookmarkModel) listRows() int {
	rows := m.height - 3 - lipgloss.Height(m.footerView()) // title, header, rule
	if m.filtering {
		rows -= filterPopupHeight
	}
	return max(1, rows)
}

func (m *bookmarkModel) keepCursorVisible() {
	rows := m.listRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	m.offset = max(0, min(m.offset, len(m.visible)-1))
}

func (m *bookmarkModel) move(to int) {
	m.cursor = max(0, min(len(m.visible)-1, to))
	m.keepCursorVisible()
}

func (m bookmarkModel) Init() tea.Cmd { return nil }

// quit ends the bookmark browser, or just closes it as a picker.
func (m bookmarkModel) quit() tea.Cmd {
	if m.embedded {
		return nil
	}
	return tea.Quit
}

func (m bookmarkModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m bookmarkModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, keys.forceQuit) {
		m.quitting = true
		return m, tea.Quit // ctrl+c leaves the whole program
	}
	if m.filtering {
		return m.handleFilterKey(msg)
	}
	if m.renaming {
		switch msg.Type {
		case tea.KeyEsc:
			m.renaming = false
			m.layout()
			return m, nil
		case tea.KeyEnter:
			m.renaming = false
			if b, ok := m.selected(); ok {
				if err := m.opts.Store.SetName(b, m.rename.Value()); err != nil {
					m.setStatus(err.Error(), true)
				} else {
					m.reload(b.ID())
					m.setStatus("Renamed", false)
				}
			}
			m.layout()
			return m, nil
		}
		var cmd tea.Cmd
		m.rename, cmd = m.rename.Update(msg)
		return m, cmd
	}
	if m.deleting {
		m.deleting = false
		b, ok := m.selected()
		if ok && key.Matches(msg, bmKeys.confirm) {
			if err := m.opts.Store.Delete(b); err != nil {
				m.setStatus(err.Error(), true)
			} else {
				m.reload("")
				m.setStatus("Deleted "+b.Label(), false)
			}
		} else {
			m.setStatus("", false)
		}
		return m, nil
	}

	m.status = ""
	if !m.embedded { // as a picker it is an overlay: no tmux moves from there
		for k, dir := range map[*key.Binding]string{&paneKeys.left: "L", &paneKeys.right: "R", &paneKeys.up: "U", &paneKeys.down: "D"} {
			if key.Matches(msg, *k) {
				return m, tmuxSelectPane(dir)
			}
		}
	}
	b, ok := m.selected()
	switch {
	case key.Matches(msg, keys.quit), m.embedded && key.Matches(msg, keys.back):
		m.quitting = true
		return m, m.quit()
	case key.Matches(msg, keys.help):
		m.help.ShowAll = !m.help.ShowAll
		m.layout()
		return m, nil
	case key.Matches(msg, keys.filter):
		m.filtering = true
		m.filterFrom = b.ID()
		m.filter.SetValue("")
		m.layout()
		return m, m.filter.Focus()
	case key.Matches(msg, keys.up):
		m.move(m.cursor - 1)
	case key.Matches(msg, keys.down):
		m.move(m.cursor + 1)
	case key.Matches(msg, keys.top):
		m.move(0)
	case key.Matches(msg, keys.bottom):
		m.move(len(m.visible) - 1)
	case key.Matches(msg, keys.pageUp):
		m.move(m.cursor - m.listRows())
	case key.Matches(msg, keys.pageDown):
		m.move(m.cursor + m.listRows())
	case !ok:
	case key.Matches(msg, bmKeys.open):
		m.chosen = &b
		m.quitting = true
		return m, m.quit()
	case key.Matches(msg, bmKeys.web), key.Matches(msg, keys.copyURL):
		if b.URL == "" {
			m.setStatus("No web link for "+b.Label(), true)
			return m, nil
		}
		fn, done := m.opts.OpenURL, "Opened "+b.Label()
		if key.Matches(msg, keys.copyURL) {
			fn, done = m.opts.Copy, "Copied "+b.URL
		}
		if fn != nil {
			if err := fn(b.URL); err != nil {
				m.setStatus(err.Error(), true)
			} else {
				m.setStatus(done, false)
			}
		}
	case key.Matches(msg, keys.copyKey):
		if m.opts.Copy != nil {
			if err := m.opts.Copy(b.Target()); err != nil {
				m.setStatus(err.Error(), true)
			} else {
				m.setStatus("Copied "+b.Target(), false)
			}
		}
	case key.Matches(msg, bmKeys.del):
		m.deleting = true
		m.setStatus(fmt.Sprintf("Delete bookmark %q? y to confirm", b.Label()), true)
	case key.Matches(msg, bmKeys.rename):
		m.renaming = true
		m.rename.SetValue(b.Name)
		m.rename.CursorEnd()
		m.layout()
		return m, m.rename.Focus()
	}
	return m, nil
}

// handleFilterKey handles keys while the fuzzy filter is open: typing
// narrows the list live, the arrows (ctrl+j/ctrl+k, ctrl+n/ctrl+p, tab) move, enter opens
// the selected bookmark and esc returns to the full list.
func (m bookmarkModel) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, filterKeys.cancel):
		m.filtering = false
		m.filter.Blur()
		m.filter.SetValue("")
		m.applyFilter()
		m.selectID(m.filterFrom)
		m.layout()
		return m, nil
	case key.Matches(msg, filterKeys.accept):
		b, ok := m.selected()
		if !ok {
			return m, nil
		}
		m.chosen = &b
		m.quitting = true
		return m, m.quit()
	case key.Matches(msg, filterKeys.prev):
		if len(m.visible) > 0 {
			m.move((m.cursor - 1 + len(m.visible)) % len(m.visible))
		}
		return m, nil
	case key.Matches(msg, filterKeys.next):
		if len(m.visible) > 0 {
			m.move((m.cursor + 1) % len(m.visible))
		}
		return m, nil
	}
	prev := m.filter.Value()
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() != prev {
		m.applyFilter()
		if strings.TrimSpace(m.filter.Value()) == "" {
			m.selectID(m.filterFrom)
		}
	}
	return m, cmd
}

func (m bookmarkModel) View() string {
	if m.quitting {
		return ""
	}
	name := "Bookmarks"
	if len(m.opts.Kinds) == 1 {
		name = "Bookmarked " + string(m.opts.Kinds[0]) + "s"
	}
	title := styleTitle.Render(fmt.Sprintf(" %s (%d)", name, len(m.all)))
	if len(m.visible) != len(m.all) {
		title = styleTitle.Render(fmt.Sprintf(" %s (%d of %d)", name, len(m.visible), len(m.all)))
	}
	rows := m.listRows()
	var body string
	switch {
	case len(m.all) == 0:
		body = styleDim.Render("\n No bookmarks yet. Press b in the issue, explorer or page browsers,\n" +
			" S in an issue list to save its query, or use `atlassian-cli bookmark add`.")
	default:
		body = m.listView(rows)
	}
	body = lipgloss.NewStyle().Height(rows + 2).MaxHeight(rows + 2).Render(body)
	parts := []string{title, body}
	if m.filtering {
		parts = append(parts, filterBox(m.filter, fmt.Sprintf("%d/%d", len(m.visible), len(m.all)), m.width))
	}
	return lipgloss.JoinVertical(lipgloss.Left, append(parts, m.footerView())...)
}

// column widths: kind, key/query; the name takes the rest.
func (m bookmarkModel) columns() (kindW, targetW, nameW int) {
	kindW = 8
	targetW = max(12, (m.width-14)*35/100)
	nameW = max(10, m.width-kindW-targetW-4)
	return kindW, targetW, nameW
}

func (m bookmarkModel) listView(rows int) string {
	kindW, targetW, nameW := m.columns()
	header := fmt.Sprintf(" %-*s %-*s %s", kindW, "Kind", targetW, "Key / query", "Name")
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render(header) + "\n")
	sb.WriteString(styleDim.Render(strings.Repeat("─", max(10, m.width))) + "\n")
	if len(m.visible) == 0 {
		sb.WriteString(styleDim.Render(" No bookmark matches"))
		return sb.String()
	}
	end := min(len(m.visible), m.offset+rows)
	for i := m.offset; i < end; i++ {
		idx := m.visible[i]
		b := m.all[idx]
		match := m.matches[idx]
		base, kindStyle := lipgloss.NewStyle(), styleKind
		if i == m.cursor {
			base, kindStyle = styleRowSelected, styleRowSelected
		}
		line := base.Render(" ") +
			cell(string(b.Kind), match.kind, kindW, kindStyle) + base.Render(" ") +
			cell(b.Target(), match.target, targetW, base) + base.Render(" ") +
			cell(b.DisplayName(), match.name, nameW, base)
		sb.WriteString(line + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// cell renders text cut and padded to width, with the matched runes
// highlighted.
func cell(text string, pos []int, width int, base lipgloss.Style) string {
	s := highlight("", text, nodeMatch{pos: pos}, width, base)
	if pad := width - ansi.StringWidth(s); pad > 0 {
		s += base.Render(strings.Repeat(" ", pad))
	}
	return s
}

func (m bookmarkModel) footerView() string {
	status := ""
	switch {
	case m.renaming:
		status = m.rename.View()
	case m.status != "":
		style := styleOK
		if m.statusErr {
			style = styleErr
		}
		status = style.Render(" " + truncate(m.status, max(10, m.width-2)))
	}
	return status + "\n " + m.help.View(m.helpKeys())
}

func (m bookmarkModel) helpKeys() helpKeys {
	if m.filtering {
		b := []key.Binding{filterKeys.next, filterKeys.prev, bmKeys.open, filterKeys.cancel}
		return helpKeys{short: b, full: [][]key.Binding{b}}
	}
	quit := keys.quit
	if m.embedded {
		quit = key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q/esc", "close"))
	}
	short := []key.Binding{bmKeys.open, bmKeys.web, bmKeys.rename, bmKeys.del, keys.filter, keys.help, quit}
	return helpKeys{short: short, full: [][]key.Binding{
		{keys.up, keys.down, keys.pageUp, keys.pageDown, keys.top, keys.bottom},
		{bmKeys.open, bmKeys.web, keys.copyURL, keys.copyKey, bmKeys.rename, bmKeys.del},
		{keys.filter, keys.help, quit},
	}}
}

// RunBookmarks shows the bookmark browser; it returns the bookmark chosen
// with enter, or nil when the user quit.
func RunBookmarks(ctx context.Context, opts BookmarkOptions) (*bookmarks.Bookmark, error) {
	final, err := tea.NewProgram(newBookmarkModel(opts), tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	if err != nil {
		return nil, err
	}
	if m, ok := final.(bookmarkModel); ok {
		return m.chosen, nil
	}
	return nil, nil
}
