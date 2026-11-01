package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/putcho01/atlassian-cli/internal/bookmarks"
)

func tempStore(t *testing.T) *bookmarks.Store {
	t.Helper()
	return &bookmarks.Store{Path: filepath.Join(t.TempDir(), "bm.yaml")}
}

func ids(t *testing.T, s *bookmarks.Store) []string {
	t.Helper()
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, b := range list {
		out = append(out, b.ID())
	}
	return out
}

func TestIssueBrowserBookmarks(t *testing.T) {
	t.Parallel()
	store := tempStore(t)
	_, m := newHarness(t)
	m.opts.Bookmarks = store
	m.opts.Caption = "My work"

	m = press(m, "down")
	m = press(m, "B")
	if m.status != "Bookmarked PROJ-2 Second issue" || m.statusErr {
		t.Errorf("status %q", m.status)
	}
	m = press(m, "S")
	if got := strings.Join(ids(t, store), ","); got != "issue:PROJ-2,query:project = PROJ" {
		t.Errorf("bookmarks %s", got)
	}
	list, _ := store.List()
	if list[0].Title != "Second issue" || list[0].URL != "https://jira.example.com/browse/PROJ-2" || list[1].Name != "My work" {
		t.Errorf("stored %+v", list)
	}

	// B again removes it, also from the detail view.
	m = press(m, "enter")
	m = press(m, "B")
	if !strings.HasPrefix(m.status, "Removed bookmark PROJ-2") {
		t.Errorf("status %q", m.status)
	}
	if got := ids(t, store); len(got) != 1 {
		t.Errorf("left %v", got)
	}
}

func TestIssueBrowserWithoutBookmarks(t *testing.T) {
	t.Parallel()
	_, m := newHarness(t)
	m = press(m, "B")
	if m.status != "" {
		t.Errorf("B without a store: %q", m.status)
	}
}

func TestOpenDetail(t *testing.T) {
	t.Parallel()
	h, m := newHarness(t)
	m.opts.OpenDetail = "PROJ-3"
	m = newModel(context.Background(), m.opts, plainRenderer)
	if m.mode != modeDetail || m.detailKey != "PROJ-3" {
		t.Fatalf("mode %v key %q", m.mode, m.detailKey)
	}
	m = drain(m, m.Init())
	if !strings.Contains(m.detail.View(), "Rendered **PROJ-3**") {
		t.Errorf("detail:\n%s", m.detail.View())
	}
	if !strings.Contains(strings.Join(h.src.calls, ","), "get PROJ-3") {
		t.Errorf("calls %v", h.src.calls)
	}
}

func TestExplorerBookmarksSidebarItem(t *testing.T) {
	t.Parallel()
	store := tempStore(t)
	_, m := newTestExplorer(t)
	m.opts.Kind = bookmarks.Feature
	m.opts.Issues.Bookmarks = store
	m = pressExplorer(m, "B")
	if !m.statusOK || !strings.Contains(m.status, "Bookmarked F-1") {
		t.Errorf("status %q ok=%v", m.status, m.statusOK)
	}
	if got := ids(t, store); len(got) != 1 || got[0] != "feature:F-1" {
		t.Errorf("bookmarks %v", got)
	}
}

func TestPageTreeBookmarksPage(t *testing.T) {
	t.Parallel()
	store := tempStore(t)
	m := newTree(context.Background(), TreeOptions{Source: &fakePages{}, Space: "DEV", Bookmarks: store}, plainRenderer)
	m = drainTree(m, m.Init())
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("B")})
	m = next.(treeModel)
	if m.statusErr || !strings.Contains(m.status, "Bookmarked") {
		t.Errorf("status %q", m.status)
	}
	list, _ := store.List()
	if len(list) != 1 || list[0].Kind != bookmarks.Page || list[0].Key != "1" || list[0].Space != "DEV" || list[0].URL != "https://wiki/p/1" {
		t.Errorf("stored %+v", list)
	}
}

func TestPageTreeStartsAtPage(t *testing.T) {
	t.Parallel()
	src := &fakePages{}
	m := newTree(context.Background(), TreeOptions{Source: src, PageID: "7"}, plainRenderer)
	m = drainTree(m, m.Init())
	if len(m.roots) != 1 || m.roots[0].id != "7" || m.roots[0].title != "Page 7" {
		t.Fatalf("roots %+v", m.roots)
	}
	if !strings.Contains(m.content.View(), "Body of") {
		t.Errorf("content:\n%s", m.content.View())
	}
}

func pressBM(m bookmarkModel, keys ...string) bookmarkModel {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, _ := m.Update(msg)
		m = next.(bookmarkModel)
	}
	return m
}

func seededStore(t *testing.T) *bookmarks.Store {
	t.Helper()
	s := tempStore(t)
	for _, b := range []bookmarks.Bookmark{
		{Kind: bookmarks.Issue, Key: "A-1", Title: "Fix login", URL: "https://j/A-1"},
		{Kind: bookmarks.Query, Name: "Mine", JQL: "assignee = currentUser()"},
		{Kind: bookmarks.Page, Key: "5", Title: "Runbook"},
	} {
		if err := s.Add(b); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestBookmarkBrowserOpenAndWeb(t *testing.T) {
	t.Parallel()
	var opened []string
	m := newBookmarkModel(BookmarkOptions{Store: seededStore(t), OpenURL: func(u string) error { opened = append(opened, u); return nil }})
	m = pressBM(m, "o")
	if len(opened) != 1 || opened[0] != "https://j/A-1" {
		t.Errorf("opened %v", opened)
	}
	m = pressBM(m, "down", "o")
	if !m.statusErr || !strings.Contains(m.status, "No web link") {
		t.Errorf("status %q", m.status)
	}
	m = pressBM(m, "enter")
	if m.chosen == nil || m.chosen.Name != "Mine" || !m.quitting {
		t.Errorf("chosen %+v", m.chosen)
	}
}

func TestBookmarkBrowserSelectedAndMessage(t *testing.T) {
	t.Parallel()
	m := newBookmarkModel(BookmarkOptions{Store: seededStore(t), Selected: "page:5", Message: "boom", MessageErr: true})
	if b, _ := m.selected(); b.Key != "5" {
		t.Errorf("selected %+v", b)
	}
	if m.status != "boom" || !m.statusErr {
		t.Errorf("status %q", m.status)
	}
}

func TestBookmarkBrowserDeleteRenameFilter(t *testing.T) {
	t.Parallel()
	store := seededStore(t)
	m := newBookmarkModel(BookmarkOptions{Store: store})

	m = pressBM(m, "d", "n") // cancelled
	if len(m.all) != 3 {
		t.Fatalf("deleted without confirmation")
	}
	m = pressBM(m, "d", "y")
	if len(m.all) != 2 || !strings.HasPrefix(m.status, "Deleted A-1") {
		t.Errorf("after delete: %d %q", len(m.all), m.status)
	}

	m = pressBM(m, "r")
	for range "Mine" {
		m = pressBM(m, "backspace")
	}
	m = pressBM(m, "M", "y", " ", "w", "o", "r", "k", "enter")
	if b, _ := m.selected(); b.Name != "My work" {
		t.Errorf("renamed %+v (status %q)", b, m.status)
	}
	if list, _ := store.List(); list[0].Name != "My work" {
		t.Errorf("stored %+v", list)
	}

	m = pressBM(m, "/", "r", "u", "n", "b")
	if len(m.visible) != 1 || !m.filtering {
		t.Errorf("filter matched %d", len(m.visible))
	}
	if b, _ := m.selected(); b.Key != "5" {
		t.Errorf("filtered selection %+v", b)
	}
	m = pressBM(m, "esc")
	if len(m.visible) != 2 || m.filtering {
		t.Errorf("esc did not close the filter")
	}
}

func TestBookmarkBrowserFuzzyFilter(t *testing.T) {
	t.Parallel()
	m := newBookmarkModel(BookmarkOptions{Store: seededStore(t)})
	m = pressBM(m, "down") // Mine
	m = pressBM(m, "/", "p", "a", "g", "e")
	// "page" matches the page's kind best; other bookmarks may match across
	// columns but rank lower.
	if b, _ := m.selected(); b.Kind != "page" {
		t.Errorf("best match %+v", b)
	}
	if bm := m.matches[m.visible[0]]; len(bm.kind) != 4 {
		t.Errorf("kind highlight %v", bm)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "/ page") || !strings.Contains(view, "/3") {
		t.Errorf("filter box missing:\n%s", view)
	}
	// q and d are typed, not actions.
	m = pressBM(m, "backspace", "backspace", "backspace", "backspace", "f", "i", "x", "q")
	if m.quitting || m.filter.Value() != "fixq" {
		t.Errorf("value %q quitting %v", m.filter.Value(), m.quitting)
	}
	m = pressBM(m, "backspace", "down", "up")
	if b, _ := m.selected(); b.Key != "A-1" {
		t.Errorf("selected %+v", b)
	}
	// esc restores the selection from before the filter.
	m = pressBM(m, "esc")
	if b, _ := m.selected(); b.Name != "Mine" {
		t.Errorf("after esc %+v", b)
	}
	// enter opens the match directly.
	m = pressBM(m, "/", "r", "u", "n", "b", "o", "o", "k", "enter")
	if m.chosen == nil || m.chosen.Key != "5" || !m.quitting {
		t.Errorf("chosen %+v", m.chosen)
	}
}

func TestMatchBookmarkColumns(t *testing.T) {
	t.Parallel()
	b := bookmarks.Bookmark{Kind: bookmarks.Epic, Key: "PAY-7", Title: "Payments"}
	bm, ok := matchBookmark("epic pay", b)
	if !ok || len(bm.kind) != 4 || len(bm.name)+len(bm.target) < 3 {
		t.Errorf("match %+v %v", bm, ok)
	}
	if _, ok := matchBookmark("zzz", b); ok {
		t.Error("zzz matched")
	}
}

func TestBookmarkBrowserEmpty(t *testing.T) {
	t.Parallel()
	m := newBookmarkModel(BookmarkOptions{Store: tempStore(t)})
	if v := m.View(); !strings.Contains(v, "No bookmarks yet") {
		t.Errorf("view:\n%s", v)
	}
	m = pressBM(m, "enter", "d")
	if m.chosen != nil || m.deleting {
		t.Error("actions on an empty list")
	}
}
