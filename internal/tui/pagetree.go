package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/putcho01/atlassian-cli/internal/bookmarks"
	"github.com/putcho01/atlassian-cli/internal/confluence"
	"github.com/putcho01/atlassian-cli/internal/htmlconv"
	"github.com/putcho01/atlassian-cli/internal/termimg"
)

// PageSource is the subset of the Confluence client the page browser needs.
// *confluence.Client implements it.
type PageSource interface {
	ListSpaces(ctx context.Context, limit int) ([]confluence.Space, error)
	RootPages(ctx context.Context, spaceKey string, limit int) ([]confluence.Page, error)
	GetChildren(ctx context.Context, pageID string, limit int) ([]confluence.Page, error)
	GetPage(ctx context.Context, id string, expand []string) (*confluence.Page, error)
	WebURL(p *confluence.Page) string
}

// TreeOptions configures the Confluence page browser.
type TreeOptions struct {
	Source PageSource
	Space  string // start in this space; "" lists all spaces

	// PageID starts at this page (its subtree) instead of a space.
	PageID string

	OpenURL func(url string) error
	Copy    func(text string) error

	// Bookmarks enables B (bookmark the page).
	Bookmarks Bookmarker
}

// treeLimit caps how many children are listed per node.
const treeLimit = 500

type treeNode struct {
	id, title, url string
	space          string // key of the space the node is in
	isSpace        bool
	depth          int
	parent         *treeNode
	children       []*treeNode
	expanded       bool
	loaded         bool
}

type (
	childrenLoadedMsg struct {
		node   *treeNode // nil for the roots
		spaces []confluence.Space
		pages  []confluence.Page
		err    error
	}
	pageContentMsg struct {
		id   string
		page *confluence.Page
		err  error
	}
)

var treeKeys = struct {
	expand, collapse, focus, refresh, images key.Binding
}{
	expand:   key.NewBinding(key.WithKeys("enter", "l", "right"), key.WithHelp("enter/l", "expand")),
	collapse: key.NewBinding(key.WithKeys("h", "left", "backspace"), key.WithHelp("h", "collapse")),
	focus:    key.NewBinding(key.WithKeys("tab", "w"), key.WithHelp("tab", "switch pane")),
	refresh:  key.NewBinding(key.WithKeys("ctrl+r", "f5"), key.WithHelp("ctrl+r", "reload page")),
	images:   key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "view images")),
}

type treeModel struct {
	ctx    context.Context
	opts   TreeOptions
	render markdownRenderer

	roots   []*treeNode
	cursor  int
	offset  int
	content viewport.Model
	pages   map[string]*confluence.Page // loaded page content by ID
	shownID string                      // page in the content pane

	gfx      *graphics               // image drawing; nil shows captions only
	markdown map[string]string       // page markdown with image markers, by ID
	images   map[string][]*pageImage // images of each page, by ID
	rendered map[string]string       // glamour output by "id/width"
	imageAt  []int                   // content line of each image of the shown page

	focusContent bool

	// Fuzzy filter (/), see pagefilter.go.
	filtering  bool
	filter     textinput.Model
	filterFrom *treeNode // selection to return to on esc
	matches    map[*treeNode]nodeMatch
	filterSeq  int

	spaceLoaded  map[string]bool // spaces whose whole page tree is loaded
	spaceLoading map[string]bool
	loading      bool
	spinner      spinner.Model
	help         help.Model
	status       string
	statusErr    bool

	width, height int
	quitting      bool
	initCmd       tea.Cmd
}

func newTree(ctx context.Context, opts TreeOptions, render markdownRenderer) treeModel {
	vp := viewport.New(0, 0)
	vp.KeyMap = viewportKeyMap()
	m := treeModel{
		ctx: ctx, opts: opts, render: render,
		content:  vp,
		pages:    map[string]*confluence.Page{},
		markdown: map[string]string{}, images: map[string][]*pageImage{}, rendered: map[string]string{},
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
		filter:  newTreeFilterInput(),

		spaceLoaded: map[string]bool{}, spaceLoading: map[string]bool{},
		help:  help.New(),
		width: 100, height: 24,
	}
	m.layout()
	m.loading = true
	m.initCmd = tea.Batch(m.spinner.Tick, m.loadChildren(nil))
	return m
}

func (m treeModel) Init() tea.Cmd { return m.initCmd }

// --- commands ---

func (m treeModel) loadChildren(n *treeNode) tea.Cmd {
	ctx, src, space := m.ctx, m.opts.Source, m.opts.Space
	pageID := m.opts.PageID
	return func() tea.Msg {
		switch {
		case n == nil && pageID != "":
			p, err := src.GetPage(ctx, pageID, []string{"space"})
			if err != nil {
				return childrenLoadedMsg{err: err}
			}
			return childrenLoadedMsg{pages: []confluence.Page{*p}}
		case n == nil && space == "":
			spaces, err := src.ListSpaces(ctx, treeLimit)
			return childrenLoadedMsg{spaces: spaces, err: err}
		case n == nil:
			pages, err := src.RootPages(ctx, space, treeLimit)
			return childrenLoadedMsg{pages: pages, err: err}
		case n.isSpace:
			pages, err := src.RootPages(ctx, n.id, treeLimit)
			return childrenLoadedMsg{node: n, pages: pages, err: err}
		default:
			pages, err := src.GetChildren(ctx, n.id, treeLimit)
			return childrenLoadedMsg{node: n, pages: pages, err: err}
		}
	}
}

func (m treeModel) loadPage(id string) tea.Cmd {
	ctx, src := m.ctx, m.opts.Source
	return func() tea.Msg {
		p, err := src.GetPage(ctx, id, []string{"body.storage", "version", "space"})
		return pageContentMsg{id: id, page: p, err: err}
	}
}

// --- update ---

func (m treeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.showContent()
		return m, nil

	case spinner.TickMsg:
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case childrenLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			return m, nil
		}
		depth, parent := 0, msg.node
		if parent != nil {
			depth = parent.depth + 1
		}
		var nodes []*treeNode
		if parent != nil && parent.loaded {
			return m, nil // filled meanwhile (from the whole space tree)
		}
		for _, s := range msg.spaces {
			nodes = append(nodes, &treeNode{id: s.Key, title: s.Name, space: s.Key, isSpace: true, depth: depth, parent: parent})
		}
		for i := range msg.pages {
			p := &msg.pages[i]
			space := m.opts.Space
			switch {
			case p.Space != nil:
				space = p.Space.Key
			case parent != nil:
				space = parent.space
			}
			nodes = append(nodes, &treeNode{id: p.ID, title: p.Title, url: m.opts.Source.WebURL(p), space: space, depth: depth, parent: parent})
		}
		if parent == nil {
			m.roots = nodes
			return m, m.selectionChanged()
		}
		parent.children, parent.loaded, parent.expanded = nodes, true, true
		if len(nodes) == 0 {
			m.setStatus(parent.title+" has no child pages", false)
		}
		return m, nil

	case spaceTreeMsg:
		delete(m.spaceLoading, msg.space)
		if msg.err != nil {
			m.setStatus("Loading all pages of "+msg.space+" failed: "+msg.err.Error(), true)
			return m, nil
		}
		m.mergeSpaceTree(msg.space, msg.pages)
		if m.filtering {
			sel := m.selected()
			m.applyTreeFilter()
			if _, ok := m.matches[sel]; ok && sel != nil {
				m.selectNode(sel) // keep the user's choice
			}
		}
		return m, nil

	case filterSettledMsg:
		if !m.filtering || msg.seq != m.filterSeq {
			return m, nil
		}
		if n := m.selected(); n != nil && n.id != m.shownID {
			return m, m.selectionChanged()
		}
		return m, nil

	case pageContentMsg:
		m.loading = false
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			return m, nil
		}
		m.pages[msg.id] = msg.page
		m.preparePage(msg.page)
		if m.shownID == msg.id {
			m.showContent()
		}
		return m, m.loadImages(msg.id)

	case attachmentsMsg:
		if msg.err != nil {
			for _, im := range m.images[msg.pageID] {
				if isAttachmentRef(im.src) {
					im.err, im.loaded = msg.err, true
				}
			}
		}
		return m, m.fetchImages(msg.pageID, msg.atts)

	case imageLoadedMsg:
		imgs := m.images[msg.pageID]
		if msg.idx >= len(imgs) {
			return m, nil // page reloaded meanwhile
		}
		im := imgs[msg.idx]
		im.img, im.err, im.loaded = msg.img, msg.err, true
		if m.shownID == msg.pageID {
			m.showContent()
		}
		return m, m.transmitKitty(msg.pageID, msg.idx, im)

	case kittyReadyMsg:
		imgs := m.images[msg.pageID]
		if msg.err != nil || msg.idx >= len(imgs) {
			return m, nil
		}
		imgs[msg.idx].kittyID, imgs[msg.idx].placed = msg.id, [2]int{msg.cols, msg.rows}
		if m.shownID == msg.pageID {
			m.showContent()
		}
		return m, nil

	case previewDoneMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m treeModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, keys.forceQuit) {
		m.quitting = true
		return m, tea.Quit
	}
	if m.filtering {
		return m.handleFilterKey(msg)
	}
	if key.Matches(msg, keys.quit) {
		m.quitting = true
		return m, tea.Quit
	}
	m.status = ""
	if key.Matches(msg, keys.filter) {
		return m, m.startFilter()
	}

	switch {
	case key.Matches(msg, treeKeys.focus):
		m.focusContent = !m.focusContent
		return m, nil
	case key.Matches(msg, keys.help):
		m.help.ShowAll = !m.help.ShowAll
		m.layout()
		return m, nil
	case key.Matches(msg, keys.open), key.Matches(msg, keys.copyURL):
		n := m.selected()
		if n == nil || n.url == "" {
			m.setStatus("No link for this entry", true)
			return m, nil
		}
		fn, done := m.opts.OpenURL, "Opened "+n.title
		if key.Matches(msg, keys.copyURL) {
			fn, done = m.opts.Copy, "Copied "+n.url
		}
		if fn == nil {
			return m, nil
		}
		if err := fn(n.url); err != nil {
			m.setStatus(err.Error(), true)
		} else {
			m.setStatus(done, false)
		}
		return m, nil
	case key.Matches(msg, treeKeys.images):
		return m.previewImages()
	case m.opts.Bookmarks != nil && key.Matches(msg, bookmarkKeys.toggle):
		n := m.selected()
		if n == nil || n.isSpace {
			m.setStatus("Select a page to bookmark", true)
			return m, nil
		}
		m.setStatus(toggleBookmark(m.opts.Bookmarks, bookmarks.Bookmark{
			Kind: bookmarks.Page, Key: n.id, Title: n.title, URL: n.url, Space: m.spaceOf(n),
		}))
		return m, nil
	case key.Matches(msg, treeKeys.refresh):
		if n := m.selected(); n != nil && !n.isSpace {
			m.forgetPage(n.id)
			m.showContent()
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.loadPage(n.id))
		}
		return m, nil
	}

	if m.focusContent {
		if key.Matches(msg, keys.back) {
			m.focusContent = false
			return m, nil
		}
		switch {
		case key.Matches(msg, keys.top):
			m.content.GotoTop()
		case key.Matches(msg, keys.bottom):
			m.content.GotoBottom()
		default:
			var cmd tea.Cmd
			m.content, cmd = m.content.Update(msg)
			return m, cmd
		}
		return m, nil
	}
	return m.handleTreeKey(msg)
}

func (m treeModel) handleTreeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visible := m.visible()
	if len(visible) == 0 {
		return m, nil
	}
	prev := m.cursor
	switch {
	case key.Matches(msg, keys.up):
		m.cursor = max(0, m.cursor-1)
	case key.Matches(msg, keys.down):
		m.cursor = min(len(visible)-1, m.cursor+1)
	case key.Matches(msg, keys.top):
		m.cursor = 0
	case key.Matches(msg, keys.bottom):
		m.cursor = len(visible) - 1
	case key.Matches(msg, keys.pageUp):
		m.cursor = max(0, m.cursor-m.treeRows())
	case key.Matches(msg, keys.pageDown):
		m.cursor = min(len(visible)-1, m.cursor+m.treeRows())
	case key.Matches(msg, treeKeys.expand):
		n := visible[m.cursor]
		switch {
		case n.expanded:
			if len(n.children) > 0 {
				m.cursor++ // step into the children
			}
		case n.loaded:
			n.expanded = true
			if len(n.children) == 0 {
				m.setStatus(n.title+" has no child pages", false)
			}
		default:
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, m.loadChildren(n))
		}
	case key.Matches(msg, treeKeys.collapse):
		n := visible[m.cursor]
		if n.expanded {
			n.expanded = false
		} else if n.parent != nil {
			for i, v := range visible {
				if v == n.parent {
					m.cursor = i
				}
			}
		}
	}
	if m.cursor != prev {
		m.keepCursorVisible()
		return m, m.selectionChanged()
	}
	return m, nil
}

// selectionChanged shows the selected page, loading it when needed.
func (m *treeModel) selectionChanged() tea.Cmd {
	n := m.selected()
	if n == nil {
		return nil
	}
	m.shownID = n.id
	m.content.GotoTop()
	m.showContent()
	if n.isSpace {
		return nil
	}
	if _, ok := m.pages[n.id]; ok {
		return nil
	}
	m.loading = true
	return tea.Batch(m.spinner.Tick, m.loadPage(n.id))
}

// --- helpers ---

// spaceOf returns the key of the space a page is in, when known.
func (m treeModel) spaceOf(n *treeNode) string {
	for p := n; p != nil; p = p.parent {
		if p.isSpace {
			return p.id
		}
	}
	if pg, ok := m.pages[n.id]; ok && pg.Space != nil {
		return pg.Space.Key
	}
	return m.opts.Space
}

func (m *treeModel) setStatus(s string, isErr bool) { m.status, m.statusErr = s, isErr }

func (m treeModel) visible() []*treeNode {
	if m.filterActive() {
		return m.filteredNodes()
	}
	var out []*treeNode
	var walk func(nodes []*treeNode)
	walk = func(nodes []*treeNode) {
		for _, n := range nodes {
			out = append(out, n)
			if n.expanded {
				walk(n.children)
			}
		}
	}
	walk(m.roots)
	return out
}

func (m treeModel) selected() *treeNode {
	v := m.visible()
	if m.cursor < 0 || m.cursor >= len(v) {
		return nil
	}
	return v[m.cursor]
}

func (m treeModel) treeWidth() int { return min(50, max(26, m.width*35/100)) }

func (m treeModel) footerHeight() int { return lipgloss.Height(m.footerView()) }

func (m treeModel) treeRows() int {
	rows := m.height - m.footerHeight() - 4
	if m.filtering {
		rows -= filterPopupHeight
	}
	return max(1, rows)
}

func (m *treeModel) keepCursorVisible() {
	rows := m.treeRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
}

func (m *treeModel) layout() {
	m.help.Width = m.width
	m.content.Width = max(10, m.width-m.treeWidth()-2)
	m.content.Height = max(3, m.height-m.footerHeight()-2)
}

func (m *treeModel) showContent() {
	n := m.selected()
	switch {
	case n == nil:
		m.content.SetContent(styleDim.Render(" Nothing selected"))
	case n.isSpace:
		m.content.SetContent(m.render(fmt.Sprintf("# %s\n\nSpace **%s**. Press enter to list its pages.", n.title, n.id), m.content.Width-2))
	default:
		p, ok := m.pages[n.id]
		if !ok {
			m.content.SetContent(styleDim.Render(" Loading " + n.title + "…"))
			return
		}
		width := m.content.Width - 2
		key := fmt.Sprintf("%s/%d", n.id, width)
		rendered, ok := m.rendered[key]
		if !ok {
			md, ok := m.markdown[n.id]
			if !ok {
				md = m.preparePage(p)
			}
			rendered = m.render(md, width)
			m.rendered[key] = rendered
		}
		var content string
		content, m.imageAt = m.placeImages(rendered, m.images[n.id])
		m.content.SetContent(content)
	}
}

// preparePage converts a page to markdown, replacing its images by markers.
func (m *treeModel) preparePage(p *confluence.Page) string {
	body := ""
	if p.Body.Storage != nil {
		body = htmlconv.Convert(p.Body.Storage.Value)
	}
	body, imgs := extractImages(body)
	md := fmt.Sprintf("# %s\n\n_Version %d · %s_\n\n", p.Title, p.Version.Number, p.Version.When) + body
	m.markdown[p.ID], m.images[p.ID] = md, imgs
	return md
}

// forgetPage drops a loaded page and everything derived from it.
func (m *treeModel) forgetPage(id string) {
	delete(m.pages, id)
	delete(m.markdown, id)
	if m.gfx.kitty() {
		for _, im := range m.images[id] {
			if im.kittyID != 0 {
				m.gfx.out.WriteSeqs(m.gfx.caps.Wrap(termimg.KittyDelete(im.kittyID), false)) //nolint:errcheck
			}
		}
	}
	delete(m.images, id)
	for k := range m.rendered {
		if strings.HasPrefix(k, id+"/") {
			delete(m.rendered, k)
		}
	}
}

// --- view ---

func (m treeModel) View() string {
	if m.quitting {
		return ""
	}
	tree := m.treeView()
	contentStyle := styleSidebarBlurred
	if m.focusContent {
		contentStyle = styleSidebarFocused
	}
	content := contentStyle.Render(m.content.View())
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinHorizontal(lipgloss.Top, tree, content), m.footerView())
}

func (m treeModel) treeView() string {
	w := m.treeWidth() - 2
	title := "Spaces"
	switch {
	case m.opts.PageID != "":
		title = "Page " + m.opts.PageID
	case m.opts.Space != "":
		title = m.opts.Space
	}
	if m.loading {
		title += " " + m.spinner.View()
	}
	var sb strings.Builder
	sb.WriteString(styleTitle.Render(" "+title) + "\n")
	visible := m.visible()
	end := min(len(visible), m.offset+m.treeRows())
	for i := m.offset; i < end; i++ {
		n := visible[i]
		marker := "  "
		switch {
		case m.filterActive() && i+1 < len(visible) && visible[i+1].parent == n:
			marker = "▾ " // shows the matches below it
		case n.expanded:
			marker = "▾ "
		case !n.loaded || len(n.children) > 0:
			marker = "▸ "
		}
		prefix := strings.Repeat("  ", n.depth) + marker
		width := max(5, w-2)
		if m.filterActive() {
			base := lipgloss.NewStyle()
			match, matched := m.matches[n]
			switch {
			case i == m.cursor:
				base = styleCursor
			case !matched:
				base = styleDim // shown only as the path to a match
			}
			cursor := " "
			if i == m.cursor {
				cursor = styleCursor.Render("▶")
			}
			sb.WriteString(cursor + highlight(prefix, n.title, match, width, base) + "\n")
			continue
		}
		label := truncate(prefix+n.title, width)
		if i == m.cursor {
			sb.WriteString(styleCursor.Render("▶"+label) + "\n")
		} else {
			sb.WriteString(" " + label + "\n")
		}
	}
	if m.filterActive() && len(m.matches) == 0 {
		sb.WriteString(styleDim.Render(" No page matches") + "\n")
	}
	style := styleSidebarBlurred
	if !m.focusContent {
		style = styleSidebarFocused
	}
	height := m.content.Height
	body := sb.String()
	if m.filtering {
		body = lipgloss.NewStyle().Height(height - filterPopupHeight).MaxHeight(height - filterPopupHeight).Render(body)
		body = lipgloss.JoinVertical(lipgloss.Left, body, m.filterPopup(w))
	}
	return style.Render(lipgloss.NewStyle().Width(w).Height(height).Render(body))
}

func (m treeModel) footerView() string {
	line := ""
	if m.status != "" {
		style := styleOK
		if m.statusErr {
			style = styleErr
		}
		line = style.Render(" "+m.status) + "\n"
	}
	return line + " " + m.help.View(m.helpKeys())
}

func (m treeModel) helpKeys() helpKeys {
	if m.filtering {
		b := []key.Binding{filterKeys.next, filterKeys.prev, filterKeys.accept, filterKeys.cancel}
		return helpKeys{short: b, full: [][]key.Binding{b}}
	}
	short := []key.Binding{treeKeys.expand, treeKeys.collapse, keys.filter, treeKeys.focus, treeKeys.images, keys.open, keys.help, keys.quit}
	return helpKeys{short: short, full: [][]key.Binding{
		{keys.up, keys.down, keys.pageUp, keys.pageDown, keys.top, keys.bottom},
		{treeKeys.expand, treeKeys.collapse, treeKeys.focus, treeKeys.refresh, treeKeys.images, bookmarkKeys.toggle},
		{keys.filter, keys.open, keys.copyURL, keys.help, keys.quit},
	}}
}

// RunPageTree launches the Confluence page browser and blocks until the
// user quits.
func RunPageTree(ctx context.Context, opts TreeOptions) error {
	caps, err := termimg.Detect()
	if err != nil {
		return err
	}
	out := termimg.NewOutput(os.Stdout)
	cellW, cellH := termimg.CellSize()
	m := newTree(ctx, opts, glamourRenderer())
	m.gfx = &graphics{caps: caps, out: out, cellW: cellW, cellH: cellH}
	if hint := caps.Hint(); hint != "" {
		m.setStatus(hint, false)
	}
	final, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx), tea.WithOutput(out)).Run()
	if fm, ok := final.(treeModel); ok && m.gfx.kitty() {
		for _, imgs := range fm.images {
			for _, im := range imgs {
				if im.kittyID != 0 {
					out.WriteSeqs(caps.Wrap(termimg.KittyDelete(im.kittyID), false)) //nolint:errcheck
				}
			}
		}
	}
	return err
}
