package tui

import (
	"context"
	"io"
	"net/url"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zhani/atlassian-cli/internal/jira"
	"github.com/zhani/atlassian-cli/internal/xref"
)

// Links between Jira and Confluence: the issue browser's l also lists the
// pages an issue links to and opens one in the page browser; the page
// browser's L lists the issues a page shows or links to and opens one in
// the issue browser. The other browser runs in this process, in place of
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

// openIssueLinks opens the picker of the issues the shown page shows (Jira
// issue macro) or links to.
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
	var refs []xref.IssueRef
	if p.Body.Storage != nil {
		refs = xref.IssueRefs(p.Body.Storage.Value, m.opts.IssueBases...)
	}
	if len(refs) == 0 {
		m.setStatus("No Jira issues on "+p.Title, false)
		return m, nil
	}
	m.issueLinks, m.linkCursor = refs, 0
	return m, nil
}

func (m treeModel) handleIssueLinksKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.back), key.Matches(msg, keys.quit), key.Matches(msg, treeKeys.issues):
		m.issueLinks = nil
	case key.Matches(msg, keys.up):
		m.linkCursor = max(0, m.linkCursor-1)
	case key.Matches(msg, keys.down):
		m.linkCursor = min(len(m.issueLinks)-1, m.linkCursor+1)
	case key.Matches(msg, keys.top):
		m.linkCursor = 0
	case key.Matches(msg, keys.bottom):
		m.linkCursor = len(m.issueLinks) - 1
	case key.Matches(msg, keys.choose):
		k, open := m.issueLinks[m.linkCursor].Key, m.opts.OpenIssue
		m.issueLinks = nil
		return m, execCross(m.ctx, k, func(ctx context.Context) error { return open(ctx, k) })
	}
	return m, nil
}

// issueLinksView renders the issue picker in place of the page content.
func (m treeModel) issueLinksView() string {
	var sb strings.Builder
	sb.WriteString(styleDim.Render(" Open an issue of this page (enter), esc to go back:") + "\n\n")
	rows := max(1, m.content.Height-2)
	start := max(0, min(m.linkCursor-rows/2, len(m.issueLinks)-rows))
	for i := start; i < min(len(m.issueLinks), start+rows); i++ {
		r := m.issueLinks[i]
		how := "issue macro"
		if r.URL != "" {
			how = "link"
		}
		line := styleTitle.Render(r.Key) + "  " + styleDim.Render(how)
		if i == m.linkCursor {
			sb.WriteString(styleCursor.Render(" ▶ ") + line + "\n")
		} else {
			sb.WriteString("   " + line + "\n")
		}
	}
	return lipgloss.NewStyle().Width(m.content.Width).Height(m.content.Height).MaxHeight(m.content.Height).Render(sb.String())
}
