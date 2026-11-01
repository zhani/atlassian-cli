package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/putcho01/atlassian-cli/internal/confluence"
)

// Live fuzzy filtering of the page tree (/), like the fuzzy finder of
// neo-tree: the tree shows the loaded pages matching the query together with
// their ancestors, the cursor moves between matches, enter jumps to the
// page and esc goes back.

var filterKeys = struct {
	prev, next, accept, cancel key.Binding
}{
	prev:   key.NewBinding(key.WithKeys("up", "ctrl+p", "ctrl+k", "shift+tab"), key.WithHelp("↑/ctrl+p", "previous match")),
	next:   key.NewBinding(key.WithKeys("down", "ctrl+n", "ctrl+j", "tab"), key.WithHelp("↓/ctrl+n", "next match")),
	accept: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "go to page")),
	cancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

func newTreeFilterInput() textinput.Model {
	f := textinput.New()
	f.Prompt = "/ "
	f.Placeholder = "fuzzy find"
	return f
}

// filterPopupHeight is the height of the filter box under the tree.
const filterPopupHeight = 3

// filterSettle is how long the cursor rests on a match before its page is
// loaded, so typing does not fetch a page per keystroke.
const filterSettle = 250 * time.Millisecond

type nodeMatch struct {
	score int
	pos   []int // matched rune positions in the title
}

type filterSettledMsg struct{ seq int }

// SpaceTreeSource is implemented by page sources that list every page of a
// space with its ancestors (*confluence.Client). With it, opening the filter
// loads the whole page tree of the spaces being browsed, so pages in folders
// never opened are found too.
type SpaceTreeSource interface {
	SpacePages(ctx context.Context, spaceKey string, limit int) ([]confluence.Page, error)
}

// maxSpacePages caps how many pages of a space the filter loads.
const maxSpacePages = 10000

type spaceTreeMsg struct {
	space string
	pages []confluence.Page
	err   error
}

// loadSpaceTrees starts loading the whole page tree of each space that has
// pages in the tree and is not loaded yet.
func (m *treeModel) loadSpaceTrees() tea.Cmd {
	src, ok := m.opts.Source.(SpaceTreeSource)
	if !ok {
		return nil
	}
	var cmds []tea.Cmd
	for _, n := range m.allNodes() {
		space := n.space
		if space == "" || m.spaceLoaded[space] || m.spaceLoading[space] || (n.isSpace && !n.loaded) {
			continue // only spaces the user has opened
		}
		m.spaceLoading[space] = true
		ctx := m.ctx
		cmds = append(cmds, func() tea.Msg {
			pages, err := src.SpacePages(ctx, space, maxSpacePages)
			return spaceTreeMsg{space: space, pages: pages, err: err}
		})
	}
	return tea.Batch(cmds...)
}

// mergeSpaceTree adds the pages of a space that are not in the tree yet
// under their parents and marks the space's nodes as fully loaded. Nodes
// already in the tree keep their place and state.
func (m *treeModel) mergeSpaceTree(space string, pages []confluence.Page) {
	m.spaceLoaded[space] = true
	root := m.opts.PageID   // browsing one page's subtree
	var container *treeNode // gets the space's top-level pages; nil: m.roots
	byID := map[string]*treeNode{}
	for _, n := range m.allNodes() {
		switch {
		case n.isSpace && n.id == space:
			container = n
		case !n.isSpace:
			byID[n.id] = n
		}
	}
	if root != "" {
		container = byID[root]
		if container == nil {
			return
		}
	}

	sort.SliceStable(pages, func(i, j int) bool { return len(pages[i].Ancestors) < len(pages[j].Ancestors) })
	for i := range pages {
		p := &pages[i]
		if _, ok := byID[p.ID]; ok {
			continue
		}
		parentID := ""
		if a := p.Ancestors; len(a) > 0 {
			parentID = a[len(a)-1].ID
		}
		var parent *treeNode
		switch {
		case root != "" && !hasAncestor(p, root):
			continue // outside the browsed subtree
		case parentID == "":
			parent = container
		default:
			if parent = byID[parentID]; parent == nil {
				continue // parent outside the tree
			}
		}
		n := &treeNode{id: p.ID, title: p.Title, url: m.opts.Source.WebURL(p), space: space, parent: parent}
		if parent == nil {
			if m.opts.Space != space {
				continue
			}
			m.roots = append(m.roots, n)
		} else {
			n.depth = parent.depth + 1
			parent.children = append(parent.children, n)
		}
		byID[p.ID] = n
	}

	// Every node of the space now has all its children.
	var mark func([]*treeNode)
	mark = func(nodes []*treeNode) {
		for _, n := range nodes {
			if n.space == space || n.space == "" {
				n.loaded = true
				mark(n.children)
			}
		}
	}
	if container != nil {
		container.loaded = true
		mark(container.children)
	} else {
		mark(m.roots)
	}
}

func hasAncestor(p *confluence.Page, id string) bool {
	for _, a := range p.Ancestors {
		if a.ID == id {
			return true
		}
	}
	return false
}

// filterActive reports whether the tree is narrowed to filter matches.
func (m treeModel) filterActive() bool {
	return m.filtering && strings.TrimSpace(m.filter.Value()) != ""
}

// allNodes returns every loaded node in tree order, collapsed or not.
func (m treeModel) allNodes() []*treeNode {
	var out []*treeNode
	var walk func([]*treeNode)
	walk = func(nodes []*treeNode) {
		for _, n := range nodes {
			out = append(out, n)
			walk(n.children)
		}
	}
	walk(m.roots)
	return out
}

// filteredNodes returns the matches and their ancestors in tree order.
func (m treeModel) filteredNodes() []*treeNode {
	keep := map[*treeNode]bool{}
	for n := range m.matches {
		for p := n; p != nil && !keep[p]; p = p.parent {
			keep[p] = true
		}
	}
	var out []*treeNode
	for _, n := range m.allNodes() {
		if keep[n] {
			out = append(out, n)
		}
	}
	return out
}

// startFilter opens the filter popup.
func (m *treeModel) startFilter() tea.Cmd {
	m.filtering = true
	m.filterFrom = m.selected()
	m.filter.SetValue("")
	m.matches = nil
	m.focusContent = false
	return tea.Batch(m.filter.Focus(), m.loadSpaceTrees())
}

// applyTreeFilter matches the query against the loaded pages and puts the
// cursor on the best match.
func (m *treeModel) applyTreeFilter() tea.Cmd {
	m.matches = map[*treeNode]nodeMatch{}
	q := m.filter.Value()
	if strings.TrimSpace(q) == "" {
		m.matches = nil
		m.cursor, m.offset = 0, 0
		m.selectNode(m.filterFrom)
		return nil
	}
	var best *treeNode
	for _, n := range m.allNodes() {
		if score, pos, ok := fuzzyMatch(q, n.title); ok {
			m.matches[n] = nodeMatch{score, pos}
			if best == nil || score > m.matches[best].score {
				best = n
			}
		}
	}
	m.offset = 0
	m.selectNode(best)
	return m.settleCmd()
}

// selectNode puts the cursor on n in the visible tree (top when absent).
func (m *treeModel) selectNode(n *treeNode) {
	m.cursor = 0
	for i, v := range m.visible() {
		if v == n {
			m.cursor = i
		}
	}
	m.keepCursorVisible()
}

// moveMatch moves the cursor to the next (dir 1) or previous (-1) match,
// skipping ancestors shown only for context; it wraps around.
func (m *treeModel) moveMatch(dir int) tea.Cmd {
	vis := m.visible()
	for i := 1; i <= len(vis); i++ {
		j := ((m.cursor+dir*i)%len(vis) + len(vis)) % len(vis)
		if _, ok := m.matches[vis[j]]; ok {
			m.cursor = j
			m.keepCursorVisible()
			return m.settleCmd()
		}
	}
	return nil
}

// settleCmd loads the selected match's page once the cursor rests.
func (m *treeModel) settleCmd() tea.Cmd {
	m.filterSeq++
	seq := m.filterSeq
	return tea.Tick(filterSettle, func(time.Time) tea.Msg { return filterSettledMsg{seq} })
}

// closeFilter leaves filter mode. With accept, the selected page is revealed
// in the full tree (its ancestors expanded); otherwise the cursor returns to
// where it was.
func (m *treeModel) closeFilter(accept bool) tea.Cmd {
	target := m.filterFrom
	if accept {
		if n := m.selected(); n != nil {
			target = n
		}
	}
	m.filtering = false
	m.filter.Blur()
	m.filter.SetValue("")
	m.matches = nil
	if target != nil {
		for p := target.parent; p != nil; p = p.parent {
			p.expanded = true
		}
	}
	m.selectNode(target)
	if target == nil || target.id == m.shownID {
		return nil
	}
	return m.selectionChanged()
}

func (m treeModel) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, filterKeys.cancel):
		return m, m.closeFilter(false)
	case key.Matches(msg, filterKeys.accept):
		if m.filterActive() && len(m.matches) == 0 {
			return m, nil // nothing to go to
		}
		return m, m.closeFilter(true)
	case key.Matches(msg, filterKeys.prev):
		return m, m.moveMatch(-1)
	case key.Matches(msg, filterKeys.next):
		return m, m.moveMatch(1)
	}
	prev := m.filter.Value()
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() == prev {
		return m, cmd
	}
	return m, tea.Batch(cmd, m.applyTreeFilter())
}

// highlight renders a tree label with the matched title runes emphasised;
// prefix (indent and marker) comes before the title. The label is cut to
// width runes.
func highlight(prefix, title string, match nodeMatch, width int, base lipgloss.Style) string {
	label := truncate(prefix+title, width)
	runes := []rune(label)
	offset := len([]rune(prefix))
	hit := map[int]bool{}
	for _, p := range match.pos {
		hit[p+offset] = true
	}
	var sb strings.Builder
	run := []rune{}
	flush := func() {
		if len(run) > 0 {
			sb.WriteString(base.Render(string(run)))
			run = run[:0]
		}
	}
	for i, r := range runes {
		if hit[i] && r != '…' {
			flush()
			sb.WriteString(styleMatch.Inherit(base).Render(string(r)))
			continue
		}
		run = append(run, r)
	}
	flush()
	return sb.String()
}

var styleMatch = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")).Underline(true)

// filterPopup renders the filter box shown under the tree.
func (m treeModel) filterPopup(width int) string {
	count := fmt.Sprintf("%d/%d", len(m.matches), len(m.allNodes()))
	if !m.filterActive() {
		count = fmt.Sprintf("%d pages", len(m.allNodes()))
	}
	if len(m.spaceLoading) > 0 {
		count = "loading… " + count
	}
	m.filter.Width = max(4, width-lipgloss.Width(count)-6)
	line := m.filter.View()
	gap := max(1, width-2-lipgloss.Width(line)-lipgloss.Width(count))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("11")).Width(width - 2).
		Render(line + strings.Repeat(" ", gap) + styleDim.Render(count))
}
