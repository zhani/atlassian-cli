package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/putcho01/atlassian-cli/internal/bookmarks"
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
	Selected string // ID of the bookmark selected at start

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

type bookmarkModel struct {
	opts    BookmarkOptions
	all     []bookmarks.Bookmark
	visible []int
	table   table.Model

	filter    textinput.Model
	filtering bool
	rename    textinput.Model
	renaming  bool
	deleting  bool

	help      help.Model
	status    string
	statusErr bool

	chosen        *bookmarks.Bookmark
	width, height int
	quitting      bool
}

func newBookmarkModel(opts BookmarkOptions) bookmarkModel {
	t := table.New(table.WithFocused(true), table.WithKeyMap(tableKeyMap()))
	styles := table.DefaultStyles()
	styles.Header = styles.Header.Bold(true).BorderStyle(lipgloss.NormalBorder()).BorderBottom(true).BorderForeground(lipgloss.Color("8"))
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("0")).Background(lipgloss.Color("12"))
	t.SetStyles(styles)
	f := textinput.New()
	f.Prompt = "/"
	r := textinput.New()
	r.Prompt = " Name: "
	m := bookmarkModel{opts: opts, table: t, filter: f, rename: r, help: help.New(), width: 100, height: 24}
	m.layout() // the table needs its columns before rows
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
	m.all = list
	m.applyFilter()
	for i, idx := range m.visible {
		if m.all[idx].ID() == selectID {
			m.table.SetCursor(i)
		}
	}
}

func (m *bookmarkModel) applyFilter() {
	q := strings.ToLower(m.filter.Value())
	m.visible = m.visible[:0]
	rows := []table.Row{}
	for i, b := range m.all {
		if q != "" && !strings.Contains(strings.ToLower(string(b.Kind)+" "+b.Label()+" "+b.Target()+" "+b.Title), q) {
			continue
		}
		m.visible = append(m.visible, i)
		rows = append(rows, table.Row{string(b.Kind), b.Target(), b.DisplayName()})
	}
	m.table.SetRows(rows)
	if c := m.table.Cursor(); c >= len(rows) || c < 0 {
		m.table.SetCursor(max(0, len(rows)-1))
	}
}

func (m bookmarkModel) selected() (bookmarks.Bookmark, bool) {
	c := m.table.Cursor()
	if c < 0 || c >= len(m.visible) {
		return bookmarks.Bookmark{}, false
	}
	return m.all[m.visible[c]], true
}

func (m *bookmarkModel) setStatus(s string, isErr bool) { m.status, m.statusErr = s, isErr }

func (m *bookmarkModel) layout() {
	targetW := max(12, (m.width-14)*35/100)
	m.table.SetColumns([]table.Column{
		{Title: "Kind", Width: 8},
		{Title: "Key / query", Width: targetW},
		{Title: "Name", Width: max(10, m.width-targetW-16)},
	})
	m.table.SetWidth(m.width)
	m.table.SetHeight(max(3, m.height-1-lipgloss.Height(m.footerView())))
	m.help.Width = m.width
}

func (m bookmarkModel) Init() tea.Cmd { return nil }

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
		return m, tea.Quit
	}
	if m.filtering {
		switch msg.Type {
		case tea.KeyEsc:
			m.filter.SetValue("")
			fallthrough
		case tea.KeyEnter:
			m.filtering = false
			m.filter.Blur()
			m.applyFilter()
			m.layout()
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.applyFilter()
		return m, cmd
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
	b, ok := m.selected()
	switch {
	case key.Matches(msg, keys.quit):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, keys.help):
		m.help.ShowAll = !m.help.ShowAll
		m.layout()
		return m, nil
	case key.Matches(msg, keys.filter):
		m.filtering = true
		m.layout()
		return m, m.filter.Focus()
	case key.Matches(msg, keys.back):
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.applyFilter()
		}
		return m, nil
	case !ok:
	case key.Matches(msg, bmKeys.open):
		m.chosen = &b
		m.quitting = true
		return m, tea.Quit
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
		return m, nil
	case key.Matches(msg, keys.copyKey):
		if m.opts.Copy != nil {
			if err := m.opts.Copy(b.Target()); err != nil {
				m.setStatus(err.Error(), true)
			} else {
				m.setStatus("Copied "+b.Target(), false)
			}
		}
		return m, nil
	case key.Matches(msg, bmKeys.del):
		m.deleting = true
		m.setStatus(fmt.Sprintf("Delete bookmark %q? y to confirm", b.Label()), true)
		return m, nil
	case key.Matches(msg, bmKeys.rename):
		m.renaming = true
		m.rename.SetValue(b.Name)
		m.rename.CursorEnd()
		m.layout()
		return m, m.rename.Focus()
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m bookmarkModel) View() string {
	if m.quitting {
		return ""
	}
	title := styleTitle.Render(fmt.Sprintf(" Bookmarks (%d)", len(m.all)))
	if len(m.visible) != len(m.all) {
		title = styleTitle.Render(fmt.Sprintf(" Bookmarks (%d of %d)", len(m.visible), len(m.all)))
	}
	body := m.table.View()
	if len(m.all) == 0 {
		body = lipgloss.NewStyle().Height(m.table.Height()).Render(styleDim.Render(
			"\n No bookmarks yet. Press B in the issue, explorer or page browsers,\n" +
				" S in an issue list to save its query, or use `atlassian-cli bookmark add`."))
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, body, m.footerView())
}

func (m bookmarkModel) footerView() string {
	status := ""
	switch {
	case m.filtering:
		status = m.filter.View()
	case m.renaming:
		status = m.rename.View()
	case m.status != "":
		style := styleOK
		if m.statusErr {
			style = styleErr
		}
		status = style.Render(" " + truncate(m.status, max(10, m.width-2)))
	case m.filter.Value() != "":
		status = styleDim.Render(" filter: " + m.filter.Value() + "  (esc to clear)")
	}
	return status + "\n " + m.help.View(m.helpKeys())
}

func (m bookmarkModel) helpKeys() helpKeys {
	short := []key.Binding{bmKeys.open, bmKeys.web, bmKeys.rename, bmKeys.del, keys.filter, keys.help, keys.quit}
	return helpKeys{short: short, full: [][]key.Binding{
		{keys.up, keys.down, keys.pageUp, keys.pageDown, keys.top, keys.bottom},
		{bmKeys.open, bmKeys.web, keys.copyURL, keys.copyKey, bmKeys.rename, bmKeys.del},
		{keys.filter, keys.back, keys.help, keys.quit},
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
