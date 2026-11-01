package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/zhani/atlassian-cli/internal/confluence"
)

// filterTree is a page tree with a loaded but collapsed subtree:
//
//	DEV
//	  Home
//	    Architecture        (collapsed, children loaded)
//	      API guide
//	      Runbook
//	    Release notes
func filterTree(t *testing.T) treeModel {
	t.Helper()
	m := newTree(context.Background(), TreeOptions{Source: &fakePages{}}, plainRenderer)
	dev := &treeNode{id: "DEV", title: "Development", isSpace: true, loaded: true, expanded: true}
	home := &treeNode{id: "1", title: "Home", depth: 1, parent: dev, loaded: true, expanded: true}
	arch := &treeNode{id: "10", title: "Architecture", depth: 2, parent: home, loaded: true}
	api := &treeNode{id: "11", title: "API guide", depth: 3, parent: arch, loaded: true}
	run := &treeNode{id: "12", title: "Runbook", depth: 3, parent: arch, loaded: true}
	rel := &treeNode{id: "20", title: "Release notes", depth: 2, parent: home, loaded: true}
	dev.children = []*treeNode{home}
	home.children = []*treeNode{arch, rel}
	arch.children = []*treeNode{api, run}
	m.roots = []*treeNode{dev}
	m.cursor = 1 // Home
	return m
}

func typeTree(m treeModel, keys ...string) (treeModel, []tea.Cmd) {
	var cmds []tea.Cmd
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "ctrl+n":
			msg = tea.KeyMsg{Type: tea.KeyCtrlN}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, cmd := m.Update(msg)
		m = next.(treeModel)
		cmds = append(cmds, cmd)
	}
	return m, cmds
}

func titles(nodes []*treeNode) string {
	var out []string
	for _, n := range nodes {
		out = append(out, n.title)
	}
	return strings.Join(out, ",")
}

func TestTreeFilterFindsCollapsedPages(t *testing.T) {
	t.Parallel()
	m := filterTree(t)
	m, _ = typeTree(m, "/", "r", "u", "n")
	if !m.filtering {
		t.Fatal("filter not open")
	}
	// The match inside the collapsed subtree is shown with its ancestors.
	if got := titles(m.visible()); got != "Development,Home,Architecture,Runbook" {
		t.Errorf("visible = %s", got)
	}
	if n := m.selected(); n == nil || n.title != "Runbook" {
		t.Errorf("selected %v", n)
	}
	view := m.treeView()
	if !strings.Contains(view, "/ run") || !strings.Contains(view, "1/6") {
		t.Errorf("popup missing:\n%s", view)
	}
	if q := m.helpKeys().short; len(q) == 0 || q[0].Help().Key != "↓/ctrl+j" {
		t.Errorf("filter help %v", q)
	}
}

func TestTreeFilterMovesBetweenMatchesAndAccepts(t *testing.T) {
	t.Parallel()
	m := filterTree(t)
	m, _ = typeTree(m, "/", "e")
	// Every title but "Runbook" has an e; the best match comes first.
	matched := 0
	for range m.matches {
		matched++
	}
	if matched != 5 {
		t.Fatalf("%d matches", matched)
	}
	m, _ = typeTree(m, "backspace", "r", "e", "l")
	if n := m.selected(); n.title != "Release notes" {
		t.Fatalf("selected %q", n.title)
	}
	m, _ = typeTree(m, "backspace", "backspace", "backspace", "g", "u", "i")
	if n := m.selected(); n.title != "API guide" {
		t.Fatalf("selected %q", n.title)
	}

	m, cmds := typeTree(m, "enter")
	if m.filtering || m.filter.Value() != "" {
		t.Error("filter still open")
	}
	// The page is revealed in the full tree: its collapsed parent expanded.
	if n := m.selected(); n == nil || n.title != "API guide" || !n.parent.expanded {
		t.Errorf("selected %v", n)
	}
	if got := titles(m.visible()); got != "Development,Home,Architecture,API guide,Runbook,Release notes" {
		t.Errorf("visible = %s", got)
	}
	if cmds[0] == nil || m.shownID != "11" {
		t.Errorf("page not loaded: shown %q", m.shownID)
	}
}

func TestTreeFilterNextWrapsAndSkipsAncestors(t *testing.T) {
	t.Parallel()
	m := filterTree(t)
	m, _ = typeTree(m, "/", "o") // Development, Home, Runbook, Release notes
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		seen[m.selected().title] = true
		if _, ok := m.matches[m.selected()]; !ok {
			t.Fatalf("cursor on a non-match %q", m.selected().title)
		}
		m, _ = typeTree(m, "ctrl+n")
	}
	if len(seen) != 4 || seen["Architecture"] {
		t.Errorf("visited %v", seen)
	}
}

func TestTreeFilterCancelRestores(t *testing.T) {
	t.Parallel()
	m := filterTree(t)
	m, _ = typeTree(m, "/", "a", "p", "i", "esc")
	if m.filtering || m.selected().title != "Home" {
		t.Errorf("after esc: filtering=%v selected %q", m.filtering, m.selected().title)
	}
	if m.roots[0].children[0].children[0].expanded {
		t.Error("cancel expanded the matched subtree")
	}
	// q types into the filter instead of quitting.
	m, _ = typeTree(m, "/", "q")
	if m.quitting || m.filter.Value() != "q" {
		t.Errorf("q: quitting=%v value %q", m.quitting, m.filter.Value())
	}
	// enter without matches keeps the filter open.
	m, _ = typeTree(m, "z", "z", "enter")
	if !m.filtering || !strings.Contains(m.treeView(), "No page matches") {
		t.Error("enter with no matches closed the filter")
	}
}

func TestTreeFilterSettleLoadsPage(t *testing.T) {
	t.Parallel()
	m := filterTree(t)
	m, cmds := typeTree(m, "/", "r", "u", "n")
	if m.shownID == "12" {
		t.Fatal("page loaded before the cursor settled")
	}
	stale := filterSettledMsg{seq: m.filterSeq - 1}
	next, _ := m.Update(stale)
	if next.(treeModel).shownID == "12" {
		t.Error("stale settle loaded the page")
	}
	_ = cmds
	next, cmd := m.Update(filterSettledMsg{seq: m.filterSeq})
	if next.(treeModel).shownID != "12" || cmd == nil {
		t.Errorf("settled: shown %q", next.(treeModel).shownID)
	}
}

func TestHighlight(t *testing.T) {
	t.Parallel()
	_, pos, _ := fuzzyMatch("rb", "Runbook")
	got := highlight("  ", "Runbook", nodeMatch{pos: pos}, 20, styleDim)
	if !strings.Contains(ansi.Strip(got), "  Runbook") {
		t.Errorf("text changed: %q", ansi.Strip(got))
	}
}

// spacePages serves the fake tree plus a whole-space listing in which
// "Deep dive" sits under Child (2), which was never expanded.
type spacePages struct {
	fakePages
}

func (f *spacePages) SpacePages(_ context.Context, space string, _ int) ([]confluence.Page, error) {
	f.record("space " + space)
	anc := func(ids ...string) []confluence.Page {
		var out []confluence.Page
		for _, id := range ids {
			out = append(out, confluence.Page{ID: id})
		}
		return out
	}
	return []confluence.Page{
		{ID: "3", Title: "Deep dive", Ancestors: anc("1", "2")},
		{ID: "1", Title: "Home"},
		{ID: "2", Title: "Child", Ancestors: anc("1")},
		{ID: "4", Title: "Second root"},
		{ID: "5", Title: "Elsewhere", Ancestors: anc("99")}, // parent unknown
	}, nil
}

func TestTreeFilterLoadsWholeSpace(t *testing.T) {
	t.Parallel()
	src := &spacePages{}
	m := newTree(context.Background(), TreeOptions{Source: src, Space: "DEV"}, plainRenderer)
	m = drainTree(m, m.Init()) // roots: Home (not expanded)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = drainTree(next.(treeModel), cmd)
	if !strings.Contains(strings.Join(src.calls, ","), "space DEV") {
		t.Fatalf("calls %v", src.calls)
	}
	if !m.spaceLoaded["DEV"] || len(m.spaceLoading) != 0 {
		t.Errorf("loaded %v loading %v", m.spaceLoaded, m.spaceLoading)
	}
	m, _ = typeTree(m, "d", "e", "e", "p")
	if got := titles(m.visible()); got != "Home,Child,Deep dive" {
		t.Errorf("visible = %s", got)
	}
	m, _ = typeTree(m, "enter")
	if got := titles(m.visible()); got != "Home,Child,Deep dive,Second root" {
		t.Errorf("full tree = %s", got)
	}
	// Nodes from the space listing count as loaded: no fetch on expand.
	home := m.roots[0]
	if !home.loaded || !home.children[0].loaded || home.children[0].children[0].depth != 2 {
		t.Errorf("home %+v", home)
	}
	// A second filter does not load the space again.
	src.calls = nil
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	_ = drainTree(next.(treeModel), cmd)
	if strings.Contains(strings.Join(src.calls, ","), "space") {
		t.Errorf("reloaded: %v", src.calls)
	}
}

func TestMergeSpaceTreeInPageSubtree(t *testing.T) {
	t.Parallel()
	src := &spacePages{}
	m := newTree(context.Background(), TreeOptions{Source: src, PageID: "2"}, plainRenderer)
	m = drainTree(m, m.Init())
	m.roots[0].space = "DEV"
	pages, _ := src.SpacePages(context.Background(), "DEV", 0)
	m.mergeSpaceTree("DEV", pages)
	if got := titles(m.allNodes()); got != "Page 2,Deep dive" {
		t.Errorf("subtree = %s", got)
	}
}

func (f *spacePages) GetPage(ctx context.Context, id string, expand []string) (*confluence.Page, error) {
	p, err := f.fakePages.GetPage(ctx, id, expand)
	if p != nil {
		p.Space = &confluence.Space{Key: "DEV"}
	}
	return p, err
}
