package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/go-cmp/cmp"
	"github.com/putcho01/atlassian-cli/internal/jira"
)

func testIssues() []jira.Issue {
	return []jira.Issue{
		{Key: "PROJ-1", Fields: jira.IssueFields{Summary: "First issue", Status: &jira.Status{Name: "Open"}}},
		{Key: "PROJ-2", Fields: jira.IssueFields{Summary: "Second issue", Status: &jira.Status{Name: "In Progress"}}},
		{Key: "PROJ-3", Fields: jira.IssueFields{Summary: "Third issue", Status: &jira.Status{Name: "Done"}}},
	}
}

// fakeSource records calls and serves canned data.
type fakeSource struct {
	mu          sync.Mutex
	calls       []string
	issues      []jira.Issue
	transitions []jira.Transition
	byJQL       map[string][]jira.Issue // per-query results, when set
	err         error
}

func (f *fakeSource) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeSource) SearchIssuesAll(_ context.Context, jql string, _ []string, _ int) ([]jira.Issue, error) {
	f.record("search " + jql)
	if f.byJQL != nil {
		return f.byJQL[jql], f.err
	}
	return f.issues, f.err
}

func (f *fakeSource) GetIssueRendered(_ context.Context, key string) (*jira.Issue, error) {
	f.record("get " + key)
	for _, is := range f.issues {
		if is.Key == key {
			is := is
			is.RenderedFields = &jira.RenderedFields{Description: "<p>Rendered <b>" + key + "</b></p>"}
			return &is, f.err
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeSource) GetTransitions(_ context.Context, key string) ([]jira.Transition, error) {
	f.record("transitions " + key)
	return f.transitions, f.err
}

func (f *fakeSource) DoTransition(_ context.Context, key, id string) error {
	f.record("transition " + key + " " + id)
	return f.err
}

func (f *fakeSource) IssueURL(key string) string { return "https://jira.example.com/browse/" + key }

type harness struct {
	src    *fakeSource
	opened []string
	copied []string
}

func newHarness(t *testing.T) (*harness, model) {
	t.Helper()
	h := &harness{src: &fakeSource{
		issues: testIssues(),
		transitions: []jira.Transition{
			{ID: "11", Name: "Start", To: &jira.Status{Name: "In Progress"}},
			{ID: "21", Name: "Done", To: &jira.Status{Name: "Done"}},
		},
	}}
	m := newModel(context.Background(), Options{
		Source:  h.src,
		JQL:     "project = PROJ",
		Limit:   50,
		Issues:  testIssues(),
		OpenURL: func(u string) error { h.opened = append(h.opened, u); return nil },
		Copy:    func(s string) error { h.copied = append(h.copied, s); return nil },
	}, plainRenderer)
	return h, m
}

// drain runs cmd and every command it produces, feeding the resulting
// messages back into the model. Spinner ticks are dropped so it terminates.
func drain(m model, cmd tea.Cmd) model {
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
			m = next.(model)
			queue = append(queue, nc)
		}
	}
	return m
}

func press(m model, ks ...string) model {
	for _, k := range ks {
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
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		case "ctrl+r":
			msg = tea.KeyMsg{Type: tea.KeyCtrlR}
		case "ctrl+k":
			msg = tea.KeyMsg{Type: tea.KeyCtrlK}
		case "ctrl+c":
			msg = tea.KeyMsg{Type: tea.KeyCtrlC}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, cmd := m.Update(msg)
		m = drain(next.(model), cmd)
	}
	return m
}

func TestNavigation(t *testing.T) {
	t.Parallel()
	_, m := newHarness(t)

	steps := []struct {
		key  string
		want string
	}{
		{"down", "PROJ-2"},
		{"j", "PROJ-3"},
		{"j", "PROJ-3"}, // clamps at bottom
		{"k", "PROJ-2"},
		{"g", "PROJ-1"},
		{"G", "PROJ-3"},
		{"up", "PROJ-2"},
	}
	for _, s := range steps {
		m = press(m, s.key)
		if got := m.selectedKey(); got != s.want {
			t.Fatalf("after %q selected = %s, want %s", s.key, got, s.want)
		}
	}
}

func TestDetailLoadsRenderedIssue(t *testing.T) {
	t.Parallel()
	h, m := newHarness(t)

	m = press(m, "j")
	m = press(m, "enter")
	if m.mode != modeDetail || m.detailKey != "PROJ-2" {
		t.Fatalf("mode = %v key = %s, want detail of PROJ-2", m.mode, m.detailKey)
	}
	view := m.View()
	if !strings.Contains(view, "Rendered **PROJ-2**") {
		t.Errorf("detail view should show the rendered description converted to markdown, got:\n%s", view)
	}

	// Re-opening uses the cache.
	m = press(m, "esc")
	if m.mode != modeList {
		t.Fatalf("esc should return to the list, mode = %v", m.mode)
	}
	m = press(m, "v")
	if diff := cmp.Diff([]string{"get PROJ-2"}, h.src.calls); diff != "" {
		t.Errorf("calls diff (-want +got):\n%s", diff)
	}
}

func TestOpenAndCopy(t *testing.T) {
	t.Parallel()
	h, m := newHarness(t)

	m = press(m, "j")
	m = press(m, "o")
	m = press(m, "c")
	m = press(m, "ctrl+k")

	if diff := cmp.Diff([]string{"https://jira.example.com/browse/PROJ-2"}, h.opened); diff != "" {
		t.Errorf("opened diff (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"https://jira.example.com/browse/PROJ-2", "PROJ-2"}, h.copied); diff != "" {
		t.Errorf("copied diff (-want +got):\n%s", diff)
	}
	if m.quitting {
		t.Error("open should not quit the browser")
	}
	if !strings.Contains(m.View(), "Copied PROJ-2") {
		t.Error("status line should confirm the copy")
	}
}

func TestActionErrorShownInStatus(t *testing.T) {
	t.Parallel()
	_, m := newHarness(t)
	m.opts.Copy = func(string) error { return errors.New("no clipboard") }

	m = press(m, "c")
	if !m.statusErr || !strings.Contains(m.View(), "no clipboard") {
		t.Errorf("expected error status, got %q", m.status)
	}
}

func TestTransitionFlow(t *testing.T) {
	t.Parallel()
	h, m := newHarness(t)

	m = press(m, "m")
	if m.mode != modeTransition {
		t.Fatalf("mode = %v, want transition picker", m.mode)
	}
	if !strings.Contains(m.View(), "Start") || !strings.Contains(m.View(), "In Progress") {
		t.Errorf("picker should list transitions, got:\n%s", m.View())
	}

	m = press(m, "j")
	m = press(m, "enter")
	if m.mode != modeList {
		t.Fatalf("mode = %v, want list after transition", m.mode)
	}
	want := []string{"transitions PROJ-1", "transition PROJ-1 21", "search project = PROJ"}
	if diff := cmp.Diff(want, h.src.calls); diff != "" {
		t.Errorf("calls diff (-want +got):\n%s", diff)
	}
	if !strings.Contains(m.status, "PROJ-1 → Done") {
		t.Errorf("status = %q, want transition confirmation", m.status)
	}
}

func TestTransitionCancel(t *testing.T) {
	t.Parallel()
	h, m := newHarness(t)

	m = press(m, "m")
	m = press(m, "esc")
	if m.mode != modeList {
		t.Fatalf("mode = %v, want list after cancel", m.mode)
	}
	if diff := cmp.Diff([]string{"transitions PROJ-1"}, h.src.calls); diff != "" {
		t.Errorf("calls diff (-want +got):\n%s", diff)
	}
}

func TestRefreshKeepsSelection(t *testing.T) {
	t.Parallel()
	h, m := newHarness(t)

	m = press(m, "j") // PROJ-2
	// The refreshed result has a new issue in front.
	h.src.issues = append([]jira.Issue{{Key: "PROJ-0", Fields: jira.IssueFields{Summary: "New"}}}, testIssues()...)
	m = press(m, "ctrl+r")

	if len(m.issues) != 4 {
		t.Fatalf("issues = %d, want 4 after refresh", len(m.issues))
	}
	if got := m.selectedKey(); got != "PROJ-2" {
		t.Errorf("selected = %s, want PROJ-2 kept after refresh", got)
	}
	if m.loading {
		t.Error("loading should be cleared after refresh")
	}
}

func TestRefreshError(t *testing.T) {
	t.Parallel()
	h, m := newHarness(t)
	h.src.err = errors.New("boom")

	m = press(m, "ctrl+r")
	if !m.statusErr || !strings.Contains(m.status, "boom") {
		t.Errorf("status = %q, want refresh error", m.status)
	}
	if len(m.issues) != 3 {
		t.Errorf("issues = %d, want previous results kept", len(m.issues))
	}
}

func TestFilter(t *testing.T) {
	t.Parallel()
	_, m := newHarness(t)

	m = press(m, "/")
	if !m.filtering {
		t.Fatal("expected filtering mode")
	}
	for _, r := range "progress" {
		m = press(m, string(r))
	}
	if diff := cmp.Diff([]int{1}, m.visible); diff != "" {
		t.Errorf("visible diff (-want +got):\n%s", diff)
	}
	// Typing in the filter must not trigger actions bound to letters.
	if m.quitting {
		t.Fatal("typing in the filter should not quit")
	}

	// enter opens the issue; the list behind it stays filtered.
	m = press(m, "enter")
	if m.filtering || m.mode != modeDetail || m.detailKey != "PROJ-2" {
		t.Errorf("filtering = %v mode = %v detail = %s, want PROJ-2 opened", m.filtering, m.mode, m.detailKey)
	}
	m = press(m, "esc")
	if m.mode != modeList || len(m.visible) != 1 {
		t.Errorf("back to the filtered list: mode %v visible %v", m.mode, m.visible)
	}

	m = press(m, "esc")
	if len(m.visible) != 3 {
		t.Errorf("esc should clear the filter, visible = %v", m.visible)
	}
}

func TestFuzzyFilterOrderAndKeys(t *testing.T) {
	t.Parallel()
	_, m := newHarness(t)
	m = press(m, "down", "down") // PROJ-3
	m = press(m, "/")
	for _, r := range "issue" {
		m = press(m, string(r))
	}
	if len(m.visible) != 3 || m.table.Cursor() != 0 {
		t.Fatalf("visible %v cursor %d", m.visible, m.table.Cursor())
	}
	view := m.View()
	if !strings.Contains(view, "/ issue") || !strings.Contains(view, "3/3") {
		t.Errorf("filter box missing:\n%s", view)
	}
	// "sec" ranks Second issue (word start) first.
	for range "issue" {
		m = press(m, "backspace")
	}
	for _, r := range "sec" {
		m = press(m, string(r))
	}
	if m.selectedKey() != "PROJ-2" {
		t.Errorf("best match %s (visible %v)", m.selectedKey(), m.visible)
	}
	// Movement wraps; letters are typed, not actions.
	for range "sec" {
		m = press(m, "backspace")
	}
	m = press(m, "p", "r", "o", "j")
	m = press(m, "up")
	if m.table.Cursor() != len(m.visible)-1 {
		t.Errorf("up from the first match should wrap, cursor %d", m.table.Cursor())
	}
	m = press(m, "q", "m")
	if m.quitting || m.mode == modeTransition || m.filter.Value() != "projqm" {
		t.Errorf("value %q", m.filter.Value())
	}
	// esc while typing restores the full list and the selection.
	m = press(m, "esc")
	if m.filtering || len(m.visible) != 3 || m.selectedKey() != "PROJ-3" {
		t.Errorf("after esc: filtering %v visible %v selected %s", m.filtering, m.visible, m.selectedKey())
	}
}

func TestMatchRowColumns(t *testing.T) {
	t.Parallel()
	row := table.Row{"PROJ-7", "Bug", "In Progress", "Ann Lee", "", "Crash on start"}
	rm, ok := matchRow("proj crash", row)
	if !ok || len(rm.pos[icKey]) != 4 || len(rm.pos[icSummary]) != 5 {
		t.Errorf("match %+v %v", rm, ok)
	}
	if _, ok := matchRow("zzz", row); ok {
		t.Error("zzz matched")
	}
}

func TestQuit(t *testing.T) {
	t.Parallel()
	for _, k := range []string{"q", "ctrl+c"} {
		_, m := newHarness(t)
		m = press(m, k)
		if !m.quitting {
			t.Errorf("%s should quit", k)
		}
	}
}

func TestHelpToggle(t *testing.T) {
	t.Parallel()
	_, m := newHarness(t)
	short := m.View()
	m = press(m, "?")
	if !m.help.ShowAll {
		t.Fatal("? should expand help")
	}
	if !strings.Contains(m.View(), "copy key") || strings.Contains(short, "copy key") {
		t.Error("full help should list extra bindings that the short help hides")
	}
}

func TestColumnsFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		width            int
		typeW, assigneeW int
	}{
		{width: 120, typeW: colType, assigneeW: colAssignee},
		{width: 75, typeW: 0, assigneeW: colAssignee},
		{width: 50, typeW: 0, assigneeW: 0},
	}
	for _, tt := range tests {
		cols := columnsFor(tt.width, "")
		if cols[1].Width != tt.typeW || cols[3].Width != tt.assigneeW {
			t.Errorf("width %d: type=%d assignee=%d, want %d %d", tt.width, cols[1].Width, cols[3].Width, tt.typeW, tt.assigneeW)
		}
		total := 0
		for _, c := range cols {
			if c.Width > 0 {
				total += c.Width + cellPadding
			}
		}
		if tt.width >= 60 && total > tt.width {
			t.Errorf("width %d: columns take %d", tt.width, total)
		}
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello..."},
		{"日本語テスト", 5, "日本..."},
	}
	for _, tt := range tests {
		if got := truncate(tt.input, tt.n); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.n, got, tt.want)
		}
	}
}
