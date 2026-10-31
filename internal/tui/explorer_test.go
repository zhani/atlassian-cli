package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"
	"github.com/putcho01/atlassian-cli/internal/jira"
)

func issue(key, typ, summary string) jira.Issue {
	return jira.Issue{Key: key, Fields: jira.IssueFields{Summary: summary, IssueType: &jira.IssueType{Name: typ}}}
}

func newTestExplorer(t *testing.T) (*fakeSource, explorerModel) {
	t.Helper()
	src := &fakeSource{byJQL: map[string][]jira.Issue{
		"q1":       {issue("S-1", "Story", "first"), issue("E-1", "Epic", "an epic")},
		"q2":       {issue("S-2", "Story", "second")},
		"epic E-1": {issue("S-9", "Story", "in epic")},
	}}
	m := newExplorer(context.Background(), ExplorerOptions{
		Title: "Features",
		Items: []ExplorerItem{
			{Key: "F-1", Title: "One", JQL: "q1", Annotations: map[string]string{"S-1": "is implemented by"}},
			{Key: "F-2", Title: "Two", JQL: "q2"},
			{Key: "F-3", Title: "Empty"},
		},
		Issues: Options{Source: src, AnnotationTitle: "Link"},
		DrillJQL: func(is jira.Issue) (string, bool) {
			if is.Fields.IssueType.Name != "Epic" {
				return "", false
			}
			return "epic " + is.Key, true
		},
	}, plainRenderer)
	return src, drainExplorer(m, m.Init())
}

// drainExplorer is drain for the explorer model.
func drainExplorer(m explorerModel, cmd tea.Cmd) explorerModel {
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
			m = next.(explorerModel)
			queue = append(queue, nc)
		}
	}
	return m
}

func pressExplorer(m explorerModel, k string) explorerModel {
	var msg tea.KeyMsg
	switch k {
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	next, cmd := m.Update(msg)
	return drainExplorer(next.(explorerModel), cmd)
}

func rightKeys(m explorerModel) []string {
	var keys []string
	for _, i := range m.right.visible {
		keys = append(keys, m.right.issues[i].Key)
	}
	return keys
}

func TestExplorerSelectionLoadsIssues(t *testing.T) {
	t.Parallel()
	src, m := newTestExplorer(t)

	if diff := cmp.Diff([]string{"S-1", "E-1"}, rightKeys(m)); diff != "" {
		t.Fatalf("initial issues diff (-want +got):\n%s", diff)
	}
	if !strings.Contains(m.View(), "is implemented by") {
		t.Error("annotation column should show the link of S-1")
	}

	m = pressExplorer(m, "j")
	if diff := cmp.Diff([]string{"S-2"}, rightKeys(m)); diff != "" {
		t.Errorf("after j diff (-want +got):\n%s", diff)
	}

	// An item without a query shows nothing and searches nothing.
	m = pressExplorer(m, "j")
	if len(m.right.issues) != 0 {
		t.Errorf("empty item shows %v", rightKeys(m))
	}
	if diff := cmp.Diff([]string{"search q1", "search q2"}, src.calls); diff != "" {
		t.Errorf("calls diff (-want +got):\n%s", diff)
	}
}

func TestExplorerIgnoresStaleResults(t *testing.T) {
	t.Parallel()
	_, m := newTestExplorer(t)
	m = pressExplorer(m, "j") // now showing q2
	next, _ := m.Update(issuesLoadedMsg{jql: "q1", issues: []jira.Issue{issue("OLD-1", "Story", "late")}})
	m = next.(explorerModel)
	if diff := cmp.Diff([]string{"S-2"}, rightKeys(m)); diff != "" {
		t.Errorf("stale result was applied (-want +got):\n%s", diff)
	}
}

func TestExplorerFocusDrillAndBack(t *testing.T) {
	t.Parallel()
	_, m := newTestExplorer(t)

	m = pressExplorer(m, "tab")
	if !m.focusRight {
		t.Fatal("tab should focus the issue pane")
	}
	// Drilling into a story does nothing; into the epic shows its issues.
	m = pressExplorer(m, "l")
	if len(m.stack) != 0 || !strings.Contains(m.status, "no child issues") {
		t.Errorf("drill into story: stack=%d status=%q", len(m.stack), m.status)
	}
	m = pressExplorer(m, "j")
	m = pressExplorer(m, "l")
	if diff := cmp.Diff([]string{"S-9"}, rightKeys(m)); diff != "" {
		t.Fatalf("epic children diff (-want +got):\n%s", diff)
	}

	m = pressExplorer(m, "h")
	if diff := cmp.Diff([]string{"S-1", "E-1"}, rightKeys(m)); diff != "" {
		t.Fatalf("back diff (-want +got):\n%s", diff)
	}
	if got := m.right.selectedKey(); got != "E-1" {
		t.Errorf("back should reselect the epic, got %s", got)
	}

	m = pressExplorer(m, "h")
	if m.focusRight {
		t.Error("h at the top level should return to the sidebar")
	}
}

func TestExplorerKeysInFilterAndQuit(t *testing.T) {
	t.Parallel()
	_, m := newTestExplorer(t)
	m = pressExplorer(m, "tab")
	m = pressExplorer(m, "/")
	for _, r := range "quiet" {
		m = pressExplorer(m, string(r))
	}
	if m.quitting {
		t.Fatal("q typed into the filter must not quit")
	}
	m = pressExplorer(m, "esc")
	m = pressExplorer(m, "q")
	if !m.quitting {
		t.Error("q should quit")
	}
}

func TestExplorerSidebarActions(t *testing.T) {
	t.Parallel()
	_, m := newTestExplorer(t)
	var copied []string
	m.opts.Issues.Copy = func(s string) error { copied = append(copied, s); return nil }
	m.opts.Items[0].URL = "https://x/browse/F-1"

	m = pressExplorer(m, "c")
	if diff := cmp.Diff([]string{"https://x/browse/F-1"}, copied); diff != "" {
		t.Errorf("copy diff (-want +got):\n%s", diff)
	}
	m = pressExplorer(m, "j")
	m = pressExplorer(m, "c")
	if !strings.Contains(m.status, "No link") {
		t.Errorf("status = %q, want no-link message", m.status)
	}
}
