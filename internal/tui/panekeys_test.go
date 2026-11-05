package tui

import (
	"context"
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	ctrlH = tea.KeyMsg{Type: tea.KeyCtrlH}
	ctrlL = tea.KeyMsg{Type: tea.KeyCtrlL}
	ctrlJ = tea.KeyMsg{Type: tea.KeyCtrlJ}
	ctrlK = tea.KeyMsg{Type: tea.KeyCtrlK}
)

func TestPaneFocusKeys(t *testing.T) {
	t.Parallel()
	tree := newTree(context.Background(), TreeOptions{Source: &fakePages{}, Space: "DEV"}, plainRenderer)
	tree = drainTree(tree, tree.Init())
	for _, step := range []struct {
		msg   tea.KeyMsg
		right bool
	}{{ctrlL, true}, {ctrlL, true}, {ctrlH, false}, {ctrlH, false}} {
		next, _ := tree.Update(step.msg)
		tree = next.(treeModel)
		if tree.focusContent != step.right {
			t.Errorf("tree after %s: right = %v", step.msg, tree.focusContent)
		}
	}

	_, ex := newTestExplorer(t)
	for _, step := range []struct {
		msg   tea.KeyMsg
		right bool
	}{{ctrlL, true}, {ctrlL, true}, {ctrlH, false}} {
		next, _ := ex.Update(step.msg)
		ex = next.(explorerModel)
		if ex.focusRight != step.right {
			t.Errorf("explorer after %s: right = %v", step.msg, ex.focusRight)
		}
	}
}

func TestFilterCtrlJK(t *testing.T) {
	t.Parallel()
	// Page tree.
	tree := filterTree(t)
	tree, _ = typeTree(tree, "/", "o") // Development, Home, Runbook, Release notes
	first := tree.selected()
	next, _ := tree.Update(ctrlJ)
	tree = next.(treeModel)
	if tree.selected() == first {
		t.Error("tree: ctrl+j did not move")
	}
	next, _ = tree.Update(ctrlK)
	if next.(treeModel).selected() != first {
		t.Error("tree: ctrl+k did not move back")
	}

	// Bookmark browser.
	bm := newBookmarkModel(BookmarkOptions{Store: seededStore(t)})
	bm = pressBM(bm, "/")
	nb, _ := bm.Update(ctrlJ)
	bm = nb.(bookmarkModel)
	if bm.cursor != 1 {
		t.Errorf("bookmarks: ctrl+j cursor %d", bm.cursor)
	}
	nb, _ = bm.Update(ctrlK)
	if nb.(bookmarkModel).cursor != 0 || nb.(bookmarkModel).filter.Value() != "" {
		t.Errorf("bookmarks: ctrl+k cursor %d value %q", nb.(bookmarkModel).cursor, nb.(bookmarkModel).filter.Value())
	}

	// Issue list.
	_, m := newHarness(t)
	m = press(m, "/", "i", "s", "s", "u", "e")
	nm, _ := m.Update(ctrlJ)
	m = nm.(model)
	if m.table.Cursor() != 1 {
		t.Errorf("issues: ctrl+j cursor %d", m.table.Cursor())
	}
	nm, _ = m.Update(ctrlK)
	m = nm.(model)
	if m.table.Cursor() != 0 || m.filter.Value() != "issue" {
		t.Errorf("issues: ctrl+k cursor %d value %q", m.table.Cursor(), m.filter.Value())
	}
}

// Not parallel: it swaps tmuxSelectPane.
func TestPaneKeysLeaveAtTheEdge(t *testing.T) {
	var moves []string
	orig := tmuxSelectPane
	t.Cleanup(func() { tmuxSelectPane = orig })
	tmuxSelectPane = func(dir string) tea.Cmd {
		moves = append(moves, dir)
		return nil
	}
	check := func(what string, want ...string) {
		t.Helper()
		if fmt.Sprint(moves) != fmt.Sprint(want) {
			t.Errorf("%s: tmux moves %v, want %v", what, moves, want)
		}
		moves = nil
	}

	tree := newTree(context.Background(), TreeOptions{Source: &fakePages{}, Space: "DEV"}, plainRenderer)
	tree = drainTree(tree, tree.Init())
	for _, k := range []tea.KeyMsg{ctrlL, ctrlL, ctrlH, ctrlH, ctrlK, ctrlJ} {
		next, _ := tree.Update(k)
		tree = next.(treeModel)
	}
	check("page tree", "R", "L", "U", "D") // the second press of ctrl+h/l leaves

	_, ex := newTestExplorer(t)
	for _, k := range []tea.KeyMsg{ctrlL, ctrlL, ctrlH, ctrlH, ctrlK, ctrlJ} {
		next, _ := ex.Update(k)
		ex = next.(explorerModel)
	}
	check("explorer", "R", "L", "U", "D")

	// The issue browser alone is one pane: both keys leave at once.
	_, m := newHarness(t)
	for _, k := range []tea.KeyMsg{ctrlL, ctrlH, ctrlK, ctrlJ} {
		next, _ := m.Update(k)
		m = next.(model)
	}
	check("issue browser", "R", "L", "U", "D")

	// In a filter ctrl+h stays a backspace.
	m = press(m, "/", "a", "b")
	next, _ := m.Update(ctrlH)
	if m = next.(model); m.filter.Value() != "a" {
		t.Errorf("filter after ctrl+h: %q", m.filter.Value())
	}
	check("issue filter")

	// The bookmark browser alone passes all four on; as a picker none.
	bm := newBookmarkModel(BookmarkOptions{Store: seededStore(t)})
	for _, k := range []tea.KeyMsg{ctrlL, ctrlH, ctrlK, ctrlJ} {
		nb, _ := bm.Update(k)
		bm = nb.(bookmarkModel)
	}
	check("bookmarks", "R", "L", "U", "D")
	bm.embedded = true
	nb, _ := bm.Update(ctrlJ)
	_ = nb
	check("bookmark picker")
}
