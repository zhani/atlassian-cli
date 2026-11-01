package tui

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"
	"github.com/zhani/atlassian-cli/internal/confluence"
)

type fakePages struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakePages) record(c string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
}

func (f *fakePages) ListSpaces(context.Context, int) ([]confluence.Space, error) {
	f.record("spaces")
	return []confluence.Space{{Key: "DEV", Name: "Development"}, {Key: "OPS", Name: "Operations"}}, nil
}

func (f *fakePages) RootPages(_ context.Context, space string, _ int) ([]confluence.Page, error) {
	f.record("roots " + space)
	if space != "DEV" {
		return nil, nil
	}
	return []confluence.Page{{ID: "1", Title: "Home", Links: confluence.Links{WebUI: "/p/1"}}}, nil
}

func (f *fakePages) GetChildren(_ context.Context, id string, _ int) ([]confluence.Page, error) {
	f.record("children " + id)
	if id != "1" {
		return nil, nil
	}
	return []confluence.Page{{ID: "2", Title: "Child"}}, nil
}

func (f *fakePages) GetPage(_ context.Context, id string, _ []string) (*confluence.Page, error) {
	f.record("page " + id)
	return &confluence.Page{ID: id, Title: "Page " + id, Body: confluence.PageBody{
		Storage: &confluence.BodyContent{Value: "<p>Body of <strong>" + id + "</strong></p>"},
	}}, nil
}

func (f *fakePages) WebURL(p *confluence.Page) string {
	if p.Links.WebUI == "" {
		return ""
	}
	return "https://wiki" + p.Links.WebUI
}

func drainTree(m treeModel, cmd tea.Cmd) treeModel {
	queue := []tea.Cmd{cmd}
	for i := 0; len(queue) > 0 && i < 50; i++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case spinner.TickMsg, tea.QuitMsg, nil:
		default:
			next, nc := m.Update(msg)
			m = next.(treeModel)
			queue = append(queue, nc)
		}
	}
	return m
}

func pressTree(m treeModel, ks ...string) treeModel {
	for _, k := range ks {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		}
		next, cmd := m.Update(msg)
		m = drainTree(next.(treeModel), cmd)
	}
	return m
}

func treeLabels(m treeModel) []string {
	var out []string
	for _, n := range m.visible() {
		out = append(out, strings.Repeat(" ", n.depth)+n.title)
	}
	return out
}

func TestPageTreeNavigation(t *testing.T) {
	t.Parallel()
	src := &fakePages{}
	var opened []string
	m := newTree(context.Background(), TreeOptions{Source: src, OpenURL: func(u string) error { opened = append(opened, u); return nil }}, plainRenderer)
	m = drainTree(m, m.Init())

	if diff := cmp.Diff([]string{"Development", "Operations"}, treeLabels(m)); diff != "" {
		t.Fatalf("roots diff (-want +got):\n%s", diff)
	}
	if !strings.Contains(m.View(), "Press enter to list its pages") {
		t.Error("a selected space should explain how to expand it")
	}

	m = pressTree(m, "enter") // expand DEV
	m = pressTree(m, "j")     // select Home → loads it
	if !strings.Contains(m.View(), "Body of **1**") {
		t.Errorf("page content not shown:\n%s", m.View())
	}
	m = pressTree(m, "l") // expand Home
	m = pressTree(m, "l") // step into its children
	if diff := cmp.Diff([]string{"Development", " Home", "  Child", "Operations"}, treeLabels(m)); diff != "" {
		t.Errorf("tree diff (-want +got):\n%s", diff)
	}
	if m.selected().title != "Child" {
		t.Errorf("selected = %s, want Child", m.selected().title)
	}

	m = pressTree(m, "h") // to parent
	m = pressTree(m, "o")
	if diff := cmp.Diff([]string{"https://wiki/p/1"}, opened); diff != "" {
		t.Errorf("opened diff (-want +got):\n%s", diff)
	}
	m = pressTree(m, "h") // collapse Home
	if diff := cmp.Diff([]string{"Development", " Home", "Operations"}, treeLabels(m)); diff != "" {
		t.Errorf("collapsed tree diff (-want +got):\n%s", diff)
	}

	// Re-expanding uses the loaded children and re-selecting uses the cache.
	m = pressTree(m, "l")
	m = pressTree(m, "k")
	m = pressTree(m, "j")
	want := []string{"spaces", "roots DEV", "page 1", "children 1", "page 2"}
	if diff := cmp.Diff(want, src.calls); diff != "" {
		t.Errorf("calls diff (-want +got):\n%s", diff)
	}
}

func TestPageTreeSpaceRoot(t *testing.T) {
	t.Parallel()
	src := &fakePages{}
	m := newTree(context.Background(), TreeOptions{Source: src, Space: "DEV"}, plainRenderer)
	m = drainTree(m, m.Init())
	if diff := cmp.Diff([]string{"Home"}, treeLabels(m)); diff != "" {
		t.Errorf("roots diff (-want +got):\n%s", diff)
	}
	if !strings.Contains(m.View(), "Body of **1**") {
		t.Error("first page should be shown right away")
	}
	m = pressTree(m, "q")
	if !m.quitting {
		t.Error("q should quit")
	}
}
