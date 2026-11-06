package tui

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zhani/atlassian-cli/internal/bookmarks"
	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/jira"
	"github.com/zhani/atlassian-cli/internal/xref"
)

// Links between Jira and Confluence: the issue browser's l also lists the
// pages an issue links to and opens one in the page browser; the page
// browser's L lists the issues a page shows or links to and opens one in
// the issue browser (and the pages it links to, which it selects itself). The other browser runs in this process, in place of
// this one (tea.Exec), and q/esc there comes back here.

// crossDoneMsg reports how opening the other browser went.
type crossDoneMsg struct {
	what string
	err  error
}

// runExec runs a function as a tea.ExecCommand: the program gives up the
// terminal, run starts another program on it, and the first resumes after.
type runExec func() error

func (r runExec) Run() error        { return r() }
func (runExec) SetStdin(io.Reader)  {}
func (runExec) SetStdout(io.Writer) {}
func (runExec) SetStderr(io.Writer) {}

// execCross runs open (another browser) in place of the current program.
// A variable so tests can see what would be opened.
var execCross = func(ctx context.Context, what string, open func(ctx context.Context) error) tea.Cmd {
	return tea.Exec(runExec(func() error { return open(ctx) }),
		func(err error) tea.Msg { return crossDoneMsg{what: what, err: err} })
}

// issuePageItems lists the Confluence pages an issue links to: its remote
// links to Confluence first (titled by Jira), then pages linked from the
// description, comments and other fields. bases are the Confluence base
// URLs.
func issuePageItems(issue *jira.Issue, bases []string) []linkItem {
	var out []linkItem
	seen := map[string]bool{}
	add := func(rel string, r xref.PageRef) {
		if seen[r.Key()] {
			return
		}
		seen[r.Key()] = true
		ref := r
		it := linkItem{rel: rel, key: ref.Label(), page: &ref}
		if ref.Space != "" {
			it.status = ref.Space
		}
		out = append(out, it)
	}
	for _, rl := range issue.RemoteLinks {
		refs := xref.PageRefs(rl.Object.URL, bases...)
		if len(refs) == 0 && strings.Contains(rl.Application.Type, "confluence") {
			// Application links name the page in the global ID even when the
			// URL is on another host name of the same Confluence.
			if q, err := url.ParseQuery(rl.GlobalID); err == nil && q.Get("pageId") != "" {
				refs = []xref.PageRef{{ID: q.Get("pageId"), URL: rl.Object.URL}}
			}
		}
		for _, r := range refs {
			if rl.Object.Title != "" {
				r.Title = rl.Object.Title
			}
			rel := strings.ToLower(rl.Relationship)
			if rel == "" {
				rel = "wiki page"
			}
			add(rel, r)
		}
	}
	var text strings.Builder
	f := issue.Fields
	text.WriteString(f.Description + "\n")
	if f.Comment != nil {
		for _, c := range f.Comment.Comments {
			text.WriteString(c.Body + "\n")
		}
	}
	if rf := issue.RenderedFields; rf != nil {
		text.WriteString(rf.Description + "\n")
		if rf.Comment != nil {
			for _, c := range rf.Comment.Comments {
				text.WriteString(c.Body + "\n")
			}
		}
	}
	for _, id := range sortedCustom(f.Custom) {
		if s, ok := f.Custom[id].(string); ok {
			text.WriteString(s + "\n")
		}
	}
	for _, r := range xref.PageRefs(text.String(), bases...) {
		add("page", r)
	}
	return out
}

// treeLink is an entry of the page browser's link picker (L): an issue the
// page shows or links to, or a page it links to.
type treeLink struct {
	issue *xref.IssueRef
	page  *xref.PageRef
}

// pageFoundMsg answers the title lookup of a followed page link.
type pageFoundMsg struct {
	id, space string
	err       error
}

// PageFinder looks pages up by CQL; *confluence.Client implements it. The
// page browser needs it to follow links that name a page by title.
type PageFinder interface {
	SearchCQL(ctx context.Context, cql string, limit int) ([]confluence.Page, error)
}

// openIssueLinks opens the picker of the issues the shown page shows (Jira
// issue macro), links to or lists (issue lists, after the others), then the
// pages it links to.
func (m treeModel) openIssueLinks() (tea.Model, tea.Cmd) {
	p := m.pages[m.shownID]
	if p == nil {
		if n := m.selected(); n != nil && !n.isSpace {
			m.setStatus("Still loading "+n.title, false)
		} else {
			m.setStatus("Select a page", false)
		}
		return m, nil
	}
	storage := ""
	if p.Body.Storage != nil {
		storage = p.Body.Storage.Value
	}
	var links []treeLink
	if m.opts.OpenIssue != nil {
		refs := xref.IssueRefs(storage, m.opts.IssueBases...)
		named := map[string]bool{}
		for _, r := range refs {
			named[r.Key] = true
		}
		for _, k := range listedIssues(m.lists[p.ID]) {
			if !named[k] {
				refs = append(refs, xref.IssueRef{Key: k, List: true})
			}
		}
		for i := range refs {
			links = append(links, treeLink{issue: &refs[i]})
		}
	}
	space := ""
	if p.Space != nil {
		space = p.Space.Key
	}
	for _, r := range xref.StoragePageLinks(storage, space, m.opts.PageBases...) {
		if r.ID == p.ID || (r.ID == "" && r.Space == space && r.Title == p.Title) {
			continue // a link to itself
		}
		links = append(links, treeLink{page: &r})
	}
	if len(links) == 0 {
		m.setStatus("No links on "+p.Title, false)
		return m, nil
	}
	m.links, m.linkCursor = links, 0
	return m, nil
}

func (m treeModel) handleIssueLinksKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.back), key.Matches(msg, keys.quit), key.Matches(msg, treeKeys.issues):
		m.links = nil
	case key.Matches(msg, keys.up):
		m.linkCursor = max(0, m.linkCursor-1)
	case key.Matches(msg, keys.down):
		m.linkCursor = min(len(m.links)-1, m.linkCursor+1)
	case key.Matches(msg, keys.top):
		m.linkCursor = 0
	case key.Matches(msg, keys.bottom):
		m.linkCursor = len(m.links) - 1
	case key.Matches(msg, keys.choose):
		l := m.links[m.linkCursor]
		m.links = nil
		if l.page != nil {
			return m, m.followPage(*l.page)
		}
		k, open := l.issue.Key, m.opts.OpenIssue
		return m, execCross(m.ctx, k, func(ctx context.Context) error { return open(ctx, k) })
	}
	return m, nil
}

// followPage selects a linked page in the tree (loading its space when
// needed), looking it up by title first when the link names no ID.
func (m *treeModel) followPage(r xref.PageRef) tea.Cmd {
	if r.ID != "" {
		return m.revealPage(bookmarks.Bookmark{Key: r.ID, Space: r.Space})
	}
	finder, ok := m.opts.Source.(PageFinder)
	if !ok {
		m.setStatus("Cannot look up "+r.Title, true)
		return nil
	}
	ctx := m.ctx
	m.loading = true
	return tea.Batch(m.spinner.Tick, func() tea.Msg {
		cql := "type = page AND title = " + cqlQuote(r.Title)
		if r.Space != "" {
			cql = "space = " + cqlQuote(r.Space) + " AND " + cql
		}
		pages, err := finder.SearchCQL(ctx, cql, 1)
		switch {
		case err != nil:
			return pageFoundMsg{err: err}
		case len(pages) == 0:
			return pageFoundMsg{err: fmt.Errorf("no page %q in space %s", r.Title, r.Space)}
		}
		return pageFoundMsg{id: pages[0].ID, space: r.Space}
	})
}

func cqlQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// issueLinksView renders the issue picker in place of the page content.
func (m treeModel) issueLinksView() string {
	var sb strings.Builder
	sb.WriteString(styleDim.Render(" Open an issue or page this page links to (enter), esc to go back:") + "\n\n")
	rows := max(1, m.content.Height-2)
	start := max(0, min(m.linkCursor-rows/2, len(m.links)-rows))
	for i := start; i < min(len(m.links), start+rows); i++ {
		var line string
		if pg := m.links[i].page; pg != nil {
			name, how := pg.Title, "page"
			if name == "" {
				name = "page " + pg.ID
			}
			if p := m.pages[m.shownID]; pg.Space != "" && p != nil && p.Space != nil && p.Space.Key != pg.Space {
				how = "page in " + pg.Space
			}
			line = styleTitle.Render(name) + "  " + styleDim.Render(how)
		} else {
			r := m.links[i].issue
			how := "issue macro"
			switch {
			case r.List:
				how = "issue list"
			case r.URL != "":
				how = "link"
			}
			line = styleTitle.Render(r.Key) + "  " + styleDim.Render(how)
		}
		if i == m.linkCursor {
			sb.WriteString(styleCursor.Render(" ▶ ") + line + "\n")
		} else {
			sb.WriteString("   " + line + "\n")
		}
	}
	return lipgloss.NewStyle().Width(m.content.Width).Height(m.content.Height).MaxHeight(m.content.Height).Render(sb.String())
}
