// Package tui implements the interactive issue browser.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/putcho01/atlassian-cli/internal/jira"
)

// Source is the subset of the Jira client the browser needs. *jira.Client
// implements it.
type Source interface {
	SearchIssuesAll(ctx context.Context, jql string, fields []string, limit int) ([]jira.Issue, error)
	GetIssueRendered(ctx context.Context, key string) (*jira.Issue, error)
	GetTransitions(ctx context.Context, key string) ([]jira.Transition, error)
	DoTransition(ctx context.Context, key, transitionID string) error
	IssueURL(key string) string
}

// Options configures the issue browser.
type Options struct {
	Source Source
	JQL    string // query re-run on refresh
	Limit  int    // maximum number of issues fetched on refresh
	Issues []jira.Issue

	OpenURL func(url string) error  // opens a URL in the browser
	Copy    func(text string) error // copies text to the clipboard

	// Caption replaces the JQL in the title line.
	Caption string
	// Annotations adds a column (titled AnnotationTitle) with a value per
	// issue key, e.g. how each issue is linked to a feature.
	AnnotationTitle string
	Annotations     map[string]string

	// DetailFields limits the custom fields of the detail view (names or
	// IDs, in order); empty shows all non-empty ones.
	DetailFields []string

	// Bookmarks enables B (bookmark the issue) and S (bookmark the query).
	Bookmarks Bookmarker

	// OpenDetail starts in the detail view of this issue.
	OpenDetail string
}

type mode int

const (
	modeList mode = iota
	modeDetail
	modeTransition
)

var (
	styleTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleCursor = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
)

// Messages produced by asynchronous commands.
type (
	issuesLoadedMsg struct {
		jql    string // query the result belongs to; stale results are dropped
		issues []jira.Issue
		err    error
	}
	issueLoadedMsg struct {
		key   string
		issue *jira.Issue
		err   error
	}
	transitionsLoadedMsg struct {
		key         string
		transitions []jira.Transition
		err         error
	}
	transitionDoneMsg struct {
		key, name string
		err       error
	}
)

type model struct {
	ctx    context.Context
	opts   Options
	render markdownRenderer

	mode     mode
	prevMode mode // mode to return to from the transition picker

	issues  []jira.Issue // everything loaded
	visible []int        // indices into issues that match the filter
	table   table.Model

	filter    textinput.Model
	filtering bool

	detailKey string
	detail    viewport.Model
	cache     map[string]*jira.Issue

	transitionKey    string
	transitions      []jira.Transition
	transitionCursor int

	loading bool
	spinner spinner.Model
	help    help.Model

	status    string
	statusErr bool

	width, height int
	quitting      bool

	pendingSelect string        // issue to select once the next result arrives
	extraHelp     []key.Binding // bindings of an enclosing view (explorer)
	initCmd       tea.Cmd
}

func newModel(ctx context.Context, opts Options, render markdownRenderer) model {
	t := table.New(table.WithColumns(columnsFor(80, opts.AnnotationTitle)), table.WithFocused(true), table.WithKeyMap(tableKeyMap()))
	styles := table.DefaultStyles()
	styles.Header = styles.Header.Bold(true).BorderStyle(lipgloss.NormalBorder()).BorderBottom(true).BorderForeground(lipgloss.Color("8"))
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("0")).Background(lipgloss.Color("12"))
	t.SetStyles(styles)

	f := textinput.New()
	f.Prompt = "/"

	vp := viewport.New(0, 0)
	vp.KeyMap = viewportKeyMap()

	m := model{
		ctx:     ctx,
		opts:    opts,
		render:  render,
		table:   t,
		filter:  f,
		detail:  vp,
		cache:   map[string]*jira.Issue{},
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		help:    help.New(),
		issues:  opts.Issues,
		width:   80,
		height:  24,
	}
	m.applyFilter()
	m.layout()
	if opts.OpenDetail != "" {
		m.mode, m.detailKey = modeDetail, opts.OpenDetail
		m.refreshDetailContent()
		m.initCmd = tea.Batch(m.startLoading(), m.loadIssue(opts.OpenDetail))
	}
	return m
}

func (m model) Init() tea.Cmd { return m.initCmd }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.refreshDetailContent()
		return m, nil

	case spinner.TickMsg:
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case issuesLoadedMsg:
		if msg.jql != m.opts.JQL {
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			m.setStatus("Refresh failed: "+msg.err.Error(), true)
			return m, nil
		}
		selected := m.selectedKey()
		if m.pendingSelect != "" {
			selected, m.pendingSelect = m.pendingSelect, ""
		}
		m.issues = msg.issues
		m.applyFilter()
		m.selectKey(selected)
		switch {
		case m.status != "": // keep a message from the action that triggered the reload
		case msg.jql == "":
			m.setStatus("No issues to show", false)
		default:
			m.setStatus(fmt.Sprintf("Loaded %d issues", len(m.issues)), false)
		}
		return m, nil

	case issueLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.setStatus("Failed to load "+msg.key+": "+msg.err.Error(), true)
			return m, nil
		}
		m.cache[msg.key] = msg.issue
		if m.mode == modeDetail && m.detailKey == msg.key {
			m.refreshDetailContent()
		}
		return m, nil

	case transitionsLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.setStatus("Failed to load transitions: "+msg.err.Error(), true)
			return m, nil
		}
		if len(msg.transitions) == 0 {
			m.setStatus("No transitions available for "+msg.key, true)
			return m, nil
		}
		m.prevMode = m.mode
		m.mode = modeTransition
		m.transitionKey = msg.key
		m.transitions = msg.transitions
		m.transitionCursor = 0
		m.layout()
		return m, nil

	case transitionDoneMsg:
		m.loading = false
		if msg.err != nil {
			m.setStatus("Transition failed: "+msg.err.Error(), true)
			return m, nil
		}
		m.setStatus(fmt.Sprintf("%s → %s", msg.key, msg.name), false)
		delete(m.cache, msg.key)
		cmds := []tea.Cmd{m.startLoading(), m.loadIssues()}
		if m.mode == modeDetail && m.detailKey == msg.key {
			cmds = append(cmds, m.loadIssue(msg.key))
		}
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, keys.forceQuit) {
		m.quitting = true
		return m, tea.Quit
	}
	m.status = ""

	if m.filtering {
		return m.handleFilterKey(msg)
	}
	if m.mode == modeTransition {
		return m.handleTransitionKey(msg)
	}

	switch {
	case key.Matches(msg, keys.quit):
		m.quitting = true
		return m, tea.Quit
	case key.Matches(msg, keys.help):
		m.help.ShowAll = !m.help.ShowAll
		m.layout()
		return m, nil
	case key.Matches(msg, keys.refresh):
		m.cache = map[string]*jira.Issue{}
		cmds := []tea.Cmd{m.startLoading(), m.loadIssues()}
		if m.mode == modeDetail {
			cmds = append(cmds, m.loadIssue(m.detailKey))
		}
		return m, tea.Batch(cmds...)
	}

	if issueKey := m.currentKey(); issueKey != "" {
		switch {
		case key.Matches(msg, keys.open):
			m.reportAction(m.opts.OpenURL, m.opts.Source.IssueURL(issueKey), "Opened "+issueKey)
			return m, nil
		case key.Matches(msg, keys.copyURL):
			url := m.opts.Source.IssueURL(issueKey)
			m.reportAction(m.opts.Copy, url, "Copied "+url)
			return m, nil
		case key.Matches(msg, keys.copyKey):
			m.reportAction(m.opts.Copy, issueKey, "Copied "+issueKey)
			return m, nil
		case key.Matches(msg, keys.transition):
			cmd := tea.Batch(m.startLoading(), m.loadTransitions(issueKey))
			return m, cmd
		case m.opts.Bookmarks != nil && key.Matches(msg, bookmarkKeys.toggle):
			m.setStatus(toggleBookmark(m.opts.Bookmarks, m.issueBookmark(issueKey)))
			return m, nil
		}
	}
	if m.mode == modeList && m.opts.Bookmarks != nil && m.opts.JQL != "" && key.Matches(msg, bookmarkKeys.saveQuery) {
		m.setStatus(toggleBookmark(m.opts.Bookmarks, m.queryBookmark()))
		return m, nil
	}

	if m.mode == modeDetail {
		return m.handleDetailKey(msg)
	}
	return m.handleListKey(msg)
}

func (m model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.detail):
		issueKey := m.selectedKey()
		if issueKey == "" {
			return m, nil
		}
		m.mode = modeDetail
		m.detailKey = issueKey
		m.detail.GotoTop()
		m.refreshDetailContent()
		if _, ok := m.cache[issueKey]; ok {
			return m, nil
		}
		cmd := tea.Batch(m.startLoading(), m.loadIssue(issueKey))
		return m, cmd
	case key.Matches(msg, keys.filter):
		m.filtering = true
		m.layout()
		cmd := m.filter.Focus()
		return m, cmd
	case key.Matches(msg, keys.back):
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.applyFilter()
			m.layout()
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.back):
		m.mode = modeList
		return m, nil
	case key.Matches(msg, keys.top):
		m.detail.GotoTop()
		return m, nil
	case key.Matches(msg, keys.bottom):
		m.detail.GotoBottom()
		return m, nil
	}
	var cmd tea.Cmd
	m.detail, cmd = m.detail.Update(msg)
	return m, cmd
}

func (m model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m model) handleTransitionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.back), key.Matches(msg, keys.quit):
		m.mode = m.prevMode
		m.layout()
	case key.Matches(msg, keys.up):
		m.transitionCursor = max(0, m.transitionCursor-1)
	case key.Matches(msg, keys.down):
		m.transitionCursor = min(len(m.transitions)-1, m.transitionCursor+1)
	case key.Matches(msg, keys.choose):
		t := m.transitions[m.transitionCursor]
		m.mode = m.prevMode
		m.layout()
		cmd := tea.Batch(m.startLoading(), m.doTransition(m.transitionKey, t))
		return m, cmd
	}
	return m, nil
}

// --- commands ---

func (m *model) startLoading() tea.Cmd {
	m.loading = true
	return m.spinner.Tick
}

func (m model) loadIssues() tea.Cmd {
	ctx, src, jql, limit := m.ctx, m.opts.Source, m.opts.JQL, m.opts.Limit
	if jql == "" { // nothing to show (e.g. a feature without linked issues)
		return func() tea.Msg { return issuesLoadedMsg{jql: jql} }
	}
	return func() tea.Msg {
		issues, err := src.SearchIssuesAll(ctx, jql, nil, limit)
		return issuesLoadedMsg{jql: jql, issues: issues, err: err}
	}
}

// showQuery switches the browser to another query and starts loading it.
func (m *model) showQuery(jql, caption string, annotations map[string]string) tea.Cmd {
	m.opts.JQL, m.opts.Caption, m.opts.Annotations = jql, caption, annotations
	m.status = ""
	m.issues = nil
	m.mode = modeList
	m.filter.SetValue("")
	m.applyFilter()
	m.table.SetCursor(0)
	m.layout()
	return tea.Batch(m.startLoading(), m.loadIssues())
}

func (m model) loadIssue(issueKey string) tea.Cmd {
	ctx, src := m.ctx, m.opts.Source
	return func() tea.Msg {
		issue, err := src.GetIssueRendered(ctx, issueKey)
		return issueLoadedMsg{key: issueKey, issue: issue, err: err}
	}
}

func (m model) loadTransitions(issueKey string) tea.Cmd {
	ctx, src := m.ctx, m.opts.Source
	return func() tea.Msg {
		ts, err := src.GetTransitions(ctx, issueKey)
		return transitionsLoadedMsg{key: issueKey, transitions: ts, err: err}
	}
}

func (m model) doTransition(issueKey string, t jira.Transition) tea.Cmd {
	ctx, src := m.ctx, m.opts.Source
	name := t.Name
	if t.To != nil && t.To.Name != "" {
		name = t.To.Name
	}
	return func() tea.Msg {
		err := src.DoTransition(ctx, issueKey, t.ID)
		return transitionDoneMsg{key: issueKey, name: name, err: err}
	}
}

// --- helpers ---

func (m *model) setStatus(s string, isErr bool) {
	m.status, m.statusErr = s, isErr
}

// reportAction runs a synchronous side effect (open, copy) and reports the
// outcome in the status line.
func (m *model) reportAction(fn func(string) error, arg, success string) {
	if fn == nil {
		return
	}
	if err := fn(arg); err != nil {
		m.setStatus(err.Error(), true)
		return
	}
	m.setStatus(success, false)
}

// currentKey is the issue that actions apply to: the open issue in the detail
// view, otherwise the selected row.
func (m model) currentKey() string {
	if m.mode == modeDetail {
		return m.detailKey
	}
	return m.selectedKey()
}

func (m model) selectedKey() string {
	c := m.table.Cursor()
	if c < 0 || c >= len(m.visible) {
		return ""
	}
	return m.issues[m.visible[c]].Key
}

func (m *model) selectKey(issueKey string) {
	for i, idx := range m.visible {
		if m.issues[idx].Key == issueKey {
			m.table.SetCursor(i)
			return
		}
	}
}

// layout sizes the components to the window.
func (m *model) layout() {
	footer := lipgloss.Height(m.footerView())
	bodyHeight := max(3, m.height-1-footer) // 1 line for the title
	m.table.SetColumns(columnsFor(m.width, m.opts.AnnotationTitle))
	m.table.SetWidth(m.width)
	m.table.SetHeight(bodyHeight)
	m.detail.Width = m.width
	m.detail.Height = bodyHeight
	m.help.Width = m.width
}

func (m *model) refreshDetailContent() {
	if m.detailKey == "" {
		return
	}
	issue, ok := m.cache[m.detailKey]
	if !ok {
		m.detail.SetContent(styleDim.Render("  Loading " + m.detailKey + "…"))
		return
	}
	m.detail.SetContent(m.render(issueMarkdown(issue, m.opts.DetailFields), max(20, m.width-2)))
}

// --- views ---

func (m model) View() string {
	if m.quitting {
		return ""
	}
	var body string
	switch {
	case m.mode == modeDetail || (m.mode == modeTransition && m.prevMode == modeDetail):
		body = m.detail.View()
	default:
		body = m.table.View()
	}
	if m.mode == modeTransition {
		body = m.transitionView()
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.titleView(), body, m.footerView())
}

func (m model) titleView() string {
	var title string
	switch m.mode {
	case modeDetail:
		title = m.detailKey
	case modeTransition:
		title = "Move " + m.transitionKey
	default:
		title = fmt.Sprintf("Issues (%d)", len(m.visible))
		if len(m.visible) != len(m.issues) {
			title = fmt.Sprintf("Issues (%d of %d)", len(m.visible), len(m.issues))
		}
	}
	s := styleTitle.Render(" " + title)
	caption := m.opts.Caption
	if caption == "" {
		caption = m.opts.JQL
	}
	if m.mode == modeList && caption != "" {
		s += styleDim.Render("  " + truncate(caption, max(10, m.width-lipgloss.Width(s)-6)))
	}
	if m.loading {
		s += " " + m.spinner.View()
	}
	return s
}

// footerView renders the status line (always present, so the layout does
// not shift when a message appears) and the help.
func (m model) footerView() string {
	status := ""
	switch {
	case m.filtering:
		status = m.filter.View()
	case m.status != "":
		style := styleOK
		if m.statusErr {
			style = styleErr
		}
		status = style.Render(" " + truncate(m.status, max(10, m.width-2)))
	case m.mode == modeList && m.filter.Value() != "":
		status = styleDim.Render(" filter: " + m.filter.Value() + "  (esc to clear)")
	}
	return status + "\n " + m.help.View(m.helpKeys())
}

func (m model) transitionView() string {
	var sb strings.Builder
	sb.WriteString(styleDim.Render(" Choose a transition:") + "\n\n")
	for i, t := range m.transitions {
		label := t.Name
		if t.To != nil && t.To.Name != "" && !strings.EqualFold(t.To.Name, t.Name) {
			label += styleDim.Render(" → " + t.To.Name)
		}
		if i == m.transitionCursor {
			sb.WriteString(styleCursor.Render(" ▶ ") + label + "\n")
		} else {
			sb.WriteString("   " + label + "\n")
		}
	}
	return lipgloss.NewStyle().Height(m.detail.Height).Render(sb.String())
}

// RunIssueList launches the interactive issue browser and blocks until the
// user quits.
func RunIssueList(ctx context.Context, opts Options) error {
	m := newModel(ctx, opts, glamourRenderer())
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	return err
}
