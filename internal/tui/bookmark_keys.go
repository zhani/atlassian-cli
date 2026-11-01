package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/zhani/atlassian-cli/internal/bookmarks"
)

// Bookmarker adds and removes bookmarks and lists them for the bookmark
// picker; *bookmarks.Store implements it. Views without one have no
// bookmark keys.
type Bookmarker interface {
	Toggle(b bookmarks.Bookmark) (added bool, err error)
	BookmarkStore
}

var bookmarkKeys = struct {
	toggle, saveQuery, picker key.Binding
}{
	toggle:    key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "bookmark")),
	saveQuery: key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "bookmark query")),
	picker:    key.NewBinding(key.WithKeys("B"), key.WithHelp("B", "bookmarks")),
}

// newPicker returns the bookmark browser as a picker inside another view,
// listing the given kinds.
func newPicker(store BookmarkStore, kinds []bookmarks.Kind, width, height int) *bookmarkModel {
	p := newBookmarkModel(BookmarkOptions{Store: store, Kinds: kinds})
	p.embedded = true
	next, _ := p.Update(tea.WindowSizeMsg{Width: width, Height: height})
	pm := next.(bookmarkModel)
	return &pm
}

// updatePicker passes a message to an open picker. done reports that the
// picker closed; chosen is the bookmark picked with enter, if any.
func updatePicker(p *bookmarkModel, msg tea.Msg) (done bool, chosen *bookmarks.Bookmark, cmd tea.Cmd) {
	next, cmd := p.Update(msg)
	*p = next.(bookmarkModel)
	if p.quitting {
		return true, p.chosen, nil
	}
	return false, nil, cmd
}

// toggleBookmark bookmarks b, or removes its bookmark, and describes the
// outcome for the status line.
func toggleBookmark(bm Bookmarker, b bookmarks.Bookmark) (status string, isErr bool) {
	added, err := bm.Toggle(b)
	switch {
	case err != nil:
		return "Bookmark failed: " + err.Error(), true
	case added:
		return "Bookmarked " + b.Label(), false
	}
	return "Removed bookmark " + b.Label(), false
}

// issueBookmark is the bookmark of an issue shown in the browser.
func (m model) issueBookmark(issueKey string) bookmarks.Bookmark {
	b := bookmarks.Bookmark{Kind: bookmarks.Issue, Key: issueKey, URL: m.opts.Source.IssueURL(issueKey)}
	if is, ok := m.cache[issueKey]; ok {
		b.Title = is.Fields.Summary
	}
	for _, is := range m.issues {
		if is.Key == issueKey {
			b.Title = is.Fields.Summary
		}
	}
	return b
}

// queryBookmark is the bookmark of the browser's query, named after its
// caption.
func (m model) queryBookmark() bookmarks.Bookmark {
	name := m.opts.Caption
	if name == "" {
		name = m.opts.JQL
	}
	return bookmarks.Bookmark{Kind: bookmarks.Query, Name: name, JQL: m.opts.JQL}
}

type bookmarkViewMsg struct {
	b    bookmarks.Bookmark
	view BookmarkView
	err  error
}

// handlePickerKey passes keys to the bookmark picker and opens the bookmark
// chosen there: an issue in the detail view, a query, feature, epic or
// sprint as the list.
func (m model) handlePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	done, chosen, cmd := updatePicker(m.picker, msg)
	if !done {
		return m, cmd
	}
	m.picker = nil
	if chosen == nil {
		return m, nil
	}
	b := *chosen
	switch b.Kind {
	case bookmarks.Issue:
		m.mode, m.detailKey = modeDetail, b.Key
		m.detail.GotoTop()
		m.refreshDetailContent()
		if _, ok := m.cache[b.Key]; ok {
			return m, nil
		}
		return m, tea.Batch(m.startLoading(), m.loadIssue(b.Key))
	case bookmarks.Query:
		return m, m.showView(BookmarkView{JQL: b.JQL, Caption: b.Label()})
	}
	if m.opts.ResolveBookmark == nil {
		m.setStatus("Cannot open "+string(b.Kind)+" bookmarks here", true)
		return m, nil
	}
	ctx, resolve := m.ctx, m.opts.ResolveBookmark
	return m, tea.Batch(m.startLoading(), func() tea.Msg {
		v, err := resolve(ctx, b)
		return bookmarkViewMsg{b: b, view: v, err: err}
	})
}

// showView lists the issues of a bookmarked query, feature, epic or sprint.
func (m *model) showView(v BookmarkView) tea.Cmd {
	m.filtering = false
	m.filter.Blur()
	if m.opts.AnnotationTitle != v.AnnotationTitle {
		m.opts.AnnotationTitle = v.AnnotationTitle
		m.layout()
	}
	return m.showQuery(v.JQL, v.Caption, v.Annotations)
}

type pageSpaceMsg struct {
	id, space string
	err       error
}

// handleTreePickerKey passes keys to the page bookmark picker and goes to
// the page chosen there.
func (m treeModel) handleTreePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	done, chosen, cmd := updatePicker(m.picker, msg)
	if !done {
		return m, cmd
	}
	m.picker = nil
	if chosen == nil {
		return m, nil
	}
	return m, m.revealPage(*chosen)
}

// revealPage selects a bookmarked page in the tree and shows it. A page
// that is not loaded yet is found by loading its space's whole page tree;
// a page in another space (or outside the browsed subtree) switches the
// tree to that space.
func (m *treeModel) revealPage(b bookmarks.Bookmark) tea.Cmd {
	if n := m.findNode(b.Key); n != nil {
		return m.revealNode(n)
	}
	m.pendingReveal = b.Key
	if b.Space != "" {
		return m.revealInSpace(b.Space)
	}
	ctx, src, id := m.ctx, m.opts.Source, b.Key
	m.loading = true
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		p, err := src.GetPage(ctx, id, []string{"space"})
		if err == nil && p.Space == nil {
			err = fmt.Errorf("page %s has no space", id)
		}
		if err != nil {
			return pageSpaceMsg{id: id, err: err}
		}
		return pageSpaceMsg{id: id, space: p.Space.Key}
	})
}

func (m *treeModel) revealInSpace(space string) tea.Cmd {
	src, ok := m.opts.Source.(SpaceTreeSource)
	if !ok {
		m.pendingReveal = ""
		m.setStatus("Cannot locate the page in the tree", true)
		return nil
	}
	sn := m.spaceNode(space)
	inTree := m.opts.PageID == "" && (m.opts.Space == space || sn != nil)
	switch {
	case !inTree:
		m.opts.Space, m.opts.PageID = space, ""
		m.roots, m.cursor, m.offset = nil, 0, 0
		m.spaceLoaded, m.spaceLoading = map[string]bool{}, map[string]bool{}
	case m.spaceLoaded[space]:
		m.setStatus("Page "+m.pendingReveal+" was not found in "+space, true)
		m.pendingReveal = ""
		return nil
	}
	if sn != nil && inTree {
		sn.expanded = true
	}
	m.loading = true
	return tea.Batch(m.spinner.Tick, m.loadSpaceTree(src, space))
}

func (m treeModel) findNode(id string) *treeNode {
	for _, n := range m.allNodes() {
		if !n.isSpace && n.id == id {
			return n
		}
	}
	return nil
}

func (m treeModel) spaceNode(key string) *treeNode {
	for _, n := range m.roots {
		if n.isSpace && n.id == key {
			return n
		}
	}
	return nil
}

// revealNode expands the ancestors of n, selects it and shows its page.
func (m *treeModel) revealNode(n *treeNode) tea.Cmd {
	for p := n.parent; p != nil; p = p.parent {
		p.expanded = true
	}
	m.selectNode(n)
	m.focusContent = false
	return m.selectionChanged()
}
