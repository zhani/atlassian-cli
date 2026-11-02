package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zhani/atlassian-cli/internal/jira"
)

func linkedIssue(t *testing.T) *jira.Issue {
	t.Helper()
	var is jira.Issue
	raw := `{"key":"P-1","names":{"customfield_5":"Epic Link"},"fields":{"summary":"root",
		"description":"Depends on PX-9 and P-2 (UTF-8, SHA-256); see also P-1 itself.",
		"comment":{"comments":[{"body":"Also PX-10."}],"total":1},
		"parent":{"key":"PX-0","fields":{"summary":"the parent","status":{"name":"Open"}}},
		"customfield_5":"P-7",
		"issuelinks":[{"type":{"outward":"blocks"},"outwardIssue":{"key":"P-2","fields":{"summary":"second","status":{"name":"Done"}}}},
		              {"type":{"inward":"is blocked by"},"inwardIssue":{"key":"P-3","fields":{"summary":"third"}}}],
		"subtasks":[{"key":"P-4","fields":{"summary":"sub"}}],
		"customfield_8":[{"issueKey":"P-1","okJql":"issueKey in (P-5)","notRunJql":"issueKey in ()"}]}}`
	if err := json.Unmarshal([]byte(raw), &is); err != nil {
		t.Fatal(err)
	}
	return &is
}

func TestIssueLinkItems(t *testing.T) {
	known := map[string]*jira.Issue{"P-7": {Key: "P-7", Fields: jira.IssueFields{Summary: "the epic"}}}
	items := issueLinkItems(linkedIssue(t), func(k string) *jira.Issue { return known[k] })
	var got []string
	for _, it := range items {
		got = append(got, it.rel+" "+it.key+" "+it.summary+" "+it.status)
	}
	want := []string{
		"parent PX-0 the parent Open", "epic P-7 the epic ", "blocks P-2 second Done",
		"is blocked by P-3 third ", "subtask P-4 sub ", "covered by test P-5  ",
		"mentioned PX-9  ", "mentioned PX-10  ",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func key1(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func step(t *testing.T, m model, keys ...string) model {
	t.Helper()
	for _, k := range keys {
		next, cmd := m.Update(key1(k))
		m = drainIssues(next.(model), cmd)
	}
	return m
}

func TestFollowLinksAndBack(t *testing.T) {
	root := linkedIssue(t)
	second := jira.Issue{Key: "P-2", Fields: jira.IssueFields{Summary: "second"}}
	src := &fakeSource{issues: []jira.Issue{*root, second}}
	m := newModel(context.Background(), Options{Source: src, Issues: []jira.Issue{*root, second}}, plainRenderer)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(model)
	m = step(t, m, "enter") // detail of P-1
	if m.mode != modeDetail || m.detailKey != "P-1" {
		t.Fatalf("mode %v key %s", m.mode, m.detailKey)
	}

	m = step(t, m, "L")
	if m.mode != modeLinks || !strings.Contains(m.View(), "Links of P-1") || !strings.Contains(m.View(), "blocks") {
		t.Fatalf("link picker not shown:\n%s", m.View())
	}
	m = step(t, m, "down", "down", "enter") // parent, epic, → blocks P-2
	if m.mode != modeDetail || m.detailKey != "P-2" || !strings.Contains(m.titleView(), "P-2 ← P-1") {
		t.Fatalf("followed to %s (mode %v), title %q", m.detailKey, m.mode, m.titleView())
	}
	m = step(t, m, "esc") // back to P-1
	if m.mode != modeDetail || m.detailKey != "P-1" || len(m.detailTrail) != 0 {
		t.Fatalf("back: %s %v %v", m.detailKey, m.mode, m.detailTrail)
	}
	m = step(t, m, "esc") // then to the list
	if m.mode != modeList {
		t.Fatalf("second esc should return to the list, mode %v", m.mode)
	}

	// A link that can't be opened (e.g. not in the mirror) leaves you where you were.
	m = step(t, m, "enter", "L", "down", "down", "down", "enter") // → is blocked by P-3 (unknown)
	if m.detailKey != "P-1" || m.mode != modeDetail || !strings.Contains(m.status, "Can't open P-3") {
		t.Errorf("unopenable link: key %s mode %v status %q", m.detailKey, m.mode, m.status)
	}

	// esc in the picker closes it without moving.
	m = step(t, m, "L", "esc")
	if m.mode != modeDetail || m.detailKey != "P-1" {
		t.Errorf("esc in picker: %v %s", m.mode, m.detailKey)
	}
}

func TestNoLinks(t *testing.T) {
	is := jira.Issue{Key: "P-1", Fields: jira.IssueFields{Summary: "lonely"}}
	m := newModel(context.Background(), Options{Source: &fakeSource{issues: []jira.Issue{is}}, Issues: []jira.Issue{is}}, plainRenderer)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = step(t, next.(model), "enter", "L")
	if m.mode != modeDetail || !strings.Contains(m.status, "No links on P-1") {
		t.Errorf("mode %v status %q", m.mode, m.status)
	}
}
