package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/jira"
	"github.com/zhani/atlassian-cli/internal/xref"
)

const wiki = "https://wiki.example.com"

// runCross makes execCross run the opener right away, as tea.Exec would.
func runCross(t *testing.T) {
	t.Helper()
	orig := execCross
	execCross = func(ctx context.Context, what string, open func(context.Context) error) tea.Cmd {
		err := open(ctx)
		return func() tea.Msg { return crossDoneMsg{what: what, err: err} }
	}
	t.Cleanup(func() { execCross = orig })
}

func pageLinkedIssue(t *testing.T) *jira.Issue {
	t.Helper()
	var is jira.Issue
	raw := `{"key":"P-1","fields":{"summary":"root",
		"description":"Spec: [spec|` + wiki + `/pages/viewpage.action?pageId=10], style: ` + wiki + `/display/DEV/Code+Style",
		"comment":{"comments":[{"body":"Same spec: ` + wiki + `/x/Cg"}],"total":1}},
		"renderedFields":{"description":"<a href=\"` + wiki + `/pages/viewpage.action?pageId=10\">spec</a>"}}`
	if err := json.Unmarshal([]byte(raw), &is); err != nil {
		t.Fatal(err)
	}
	var rl []jira.RemoteLink
	raw = `[{"relationship":"Wiki Page","application":{"type":"com.atlassian.confluence"},"object":{"url":"` + wiki + `/pages/viewpage.action?pageId=20","title":"Release Plan"}},
		{"application":{"type":"com.atlassian.confluence"},"globalId":"appId=abc&pageId=30","object":{"url":"https://wiki-alias.example.com/x/y","title":"Alias"}},
		{"object":{"url":"https://example.org/doc","title":"Web page"}}]`
	if err := json.Unmarshal([]byte(raw), &rl); err != nil {
		t.Fatal(err)
	}
	is.RemoteLinks = rl
	return &is
}

func TestIssuePageItems(t *testing.T) {
	var got []string
	for _, it := range issuePageItems(pageLinkedIssue(t), []string{wiki}) {
		got = append(got, it.rel+" "+it.key+" "+it.status+" "+it.page.Key())
	}
	want := []string{
		"wiki page Release Plan  id:20",
		"wiki page Alias  id:30",
		"page page 10  id:10", // once, though linked from the comment's tiny link and both descriptions
		"page Code Style DEV title:DEV/Code Style",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestOpenLinkedPage(t *testing.T) {
	runCross(t)
	is := pageLinkedIssue(t)
	var opened []xref.PageRef
	src := &fakeSource{issues: []jira.Issue{*is}}
	m := newModel(context.Background(), Options{Source: src, Issues: []jira.Issue{*is}, PageBases: []string{wiki},
		OpenPage: func(_ context.Context, ref xref.PageRef) error {
			opened = append(opened, ref)
			return errors.New("gone")
		}}, plainRenderer)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(model)
	m = step(t, m, "enter")
	m.cache["P-1"] = is // remote links come from LoadRemoteLinks, which fakeSource lacks
	m = step(t, m, "l")
	if m.mode != modeLinks || len(m.links) != 4 || !strings.Contains(m.View(), "Release Plan") {
		t.Fatalf("link picker: mode %v, %d links\n%s", m.mode, len(m.links), m.View())
	}
	m = step(t, m, "enter")
	if len(opened) != 1 || opened[0].ID != "20" || m.detailKey != "P-1" || m.mode != modeDetail {
		t.Fatalf("opened %+v, detail %s mode %v", opened, m.detailKey, m.mode)
	}
	if !strings.Contains(m.status, "Can't open Release Plan: gone") {
		t.Errorf("status = %q", m.status)
	}

	// Without OpenPage (no Confluence) pages are not offered.
	m = newModel(context.Background(), Options{Source: src, Issues: []jira.Issue{*is}}, plainRenderer)
	m = step(t, m, "enter")
	m.cache["P-1"] = is
	m = step(t, m, "l")
	if m.mode == modeLinks {
		t.Errorf("pages listed without OpenPage: %+v", m.links)
	}
}

type issuePages struct{ fakePages }

func (f *issuePages) GetPage(_ context.Context, id string, _ []string) (*confluence.Page, error) {
	return &confluence.Page{ID: id, Title: "Page " + id, Body: confluence.PageBody{Storage: &confluence.BodyContent{Value: `<p>` +
		`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="key">DEV-1</ac:parameter></ac:structured-macro> and ` +
		`<a href="https://jira.example.com/browse/DEV-2">DEV-2</a> <a href="https://elsewhere.example.com/browse/X-9">x</a></p>`}}}, nil
}

func TestPageIssueLinks(t *testing.T) {
	runCross(t)
	var opened []string
	opts := TreeOptions{Source: &issuePages{}, PageID: "5", IssueBases: []string{"https://jira.example.com"},
		OpenIssue: func(_ context.Context, key string) error { opened = append(opened, key); return nil }}
	m := newTree(context.Background(), opts, plainRenderer)
	m = drainTree(m, m.Init())
	m = pressTree(m, "L")
	if len(m.links) != 2 || !strings.Contains(m.View(), "DEV-2") || strings.Contains(m.View(), "X-9") {
		t.Fatalf("issue picker: %+v\n%s", m.links, m.View())
	}
	m = pressTree(m, "j", "enter")
	if len(opened) != 1 || opened[0] != "DEV-2" || m.links != nil {
		t.Fatalf("opened %v, picker %v", opened, m.links)
	}
	m = pressTree(m, "L", "esc")
	if m.links != nil || len(opened) != 1 {
		t.Fatalf("esc: picker %v, opened %v", m.links, opened)
	}

	// Without OpenIssue (no Jira) L does nothing.
	opts.OpenIssue = nil
	m = newTree(context.Background(), opts, plainRenderer)
	m = drainTree(m, m.Init())
	if m = pressTree(m, "L"); m.links != nil {
		t.Errorf("picker opened without OpenIssue")
	}
}

// linkedPages: Child links to Home by title, and to itself.
type linkedPages struct {
	fakePages
	cql string
}

func (f *linkedPages) GetPage(_ context.Context, id string, _ []string) (*confluence.Page, error) {
	body := ""
	if id == "2" {
		body = `<p>See <ac:link><ri:page ri:content-title="Home" /></ac:link> and <ac:link><ri:page ri:content-title="Page 2" /></ac:link></p>`
	}
	return &confluence.Page{ID: id, Title: "Page " + id, Space: &confluence.Space{Key: "DEV"},
		Body: confluence.PageBody{Storage: &confluence.BodyContent{Value: body}}}, nil
}

func (f *linkedPages) SearchCQL(_ context.Context, cql string, _ int) ([]confluence.Page, error) {
	f.cql = cql
	return []confluence.Page{{ID: "1", Title: "Home"}}, nil
}

func TestPageLinksFollowed(t *testing.T) {
	src := &linkedPages{}
	m := newTree(context.Background(), TreeOptions{Source: src}, plainRenderer) // no Jira: L still lists pages
	m = drainTree(m, m.Init())
	m = pressTree(m, "enter", "j", "l", "l") // expand DEV, select Home, step into Child
	if m.selected().id != "2" {
		t.Fatalf("selected %s, want Child", m.selected().id)
	}
	m = pressTree(m, "L")
	if len(m.links) != 1 || m.links[0].page == nil || m.links[0].page.Title != "Home" || !strings.Contains(m.View(), "Home  page") {
		t.Fatalf("picker: %+v\n%s", m.links, m.View())
	}
	m = pressTree(m, "enter")
	if m.links != nil || m.selected().id != "1" || m.shownID != "1" {
		t.Fatalf("after following: picker %v, selected %s, shown %s", m.links, m.selected().id, m.shownID)
	}
	if src.cql != `space = "DEV" AND type = page AND title = "Home"` {
		t.Errorf("lookup: %s", src.cql)
	}
}
