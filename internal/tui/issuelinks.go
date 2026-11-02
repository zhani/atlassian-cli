package tui

import (
	"regexp"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/zhani/atlassian-cli/internal/jira"
	"github.com/zhani/atlassian-cli/internal/xray"
)

// Following links from the detail view: l lists the issues the shown issue
// points to (parent, epic, issue links, subtasks, Xray tests covering it,
// keys mentioned in its text); enter opens one in the detail view and esc
// walks back the way you came.

var (
	issueLinksKey = key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "follow links"))
	linkOpenKey   = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open"))
)

// linkItem is one issue the shown issue points to.
type linkItem struct {
	rel, key, summary, status string
}

var mentionedKey = regexp.MustCompile(`\b[A-Z][A-Z0-9_]+-\d+\b`)

// issueLinkItems lists what an issue points to, in this order: parent,
// epic, issue links, subtasks, covering tests, then keys mentioned in the
// description and comments. Each key once; never the issue itself. known
// fills in summary and status for keys it can resolve (loaded issues).
func issueLinkItems(issue *jira.Issue, known func(string) *jira.Issue) []linkItem {
	var out []linkItem
	seen := map[string]bool{issue.Key: true}
	add := func(rel string, is *jira.Issue, k string) {
		if is != nil && k == "" {
			k = is.Key
		}
		k = strings.ToUpper(strings.TrimSpace(k))
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		it := linkItem{rel: rel, key: k}
		if is == nil || is.Fields.Summary == "" {
			is = known(k)
		}
		if is != nil {
			it.summary, it.status = is.Fields.Summary, safeName(is.Fields.Status)
		}
		out = append(out, it)
	}
	f := issue.Fields
	if f.Parent != nil {
		add("parent", f.Parent, "")
	}
	for id, name := range issue.Names {
		if strings.EqualFold(name, "Epic Link") {
			if k, ok := f.Custom[id].(string); ok {
				add("epic", nil, k)
			}
		}
	}
	for _, l := range f.IssueLinks {
		if phrase, linked := l.Describe(); linked != nil {
			add(phrase, linked, "")
		}
	}
	for i := range f.Subtasks {
		add("subtask", &f.Subtasks[i], "")
	}
	for _, id := range sortedCustom(f.Custom) {
		for _, k := range xray.CoverageTests(f.Custom[id]) {
			add("covered by test", nil, k)
		}
	}
	// Keys mentioned in the text count only in projects the issue already
	// relates to, so UTF-8, SHA-256 or ISO-9001 aren't taken for issues.
	projects := map[string]bool{projectOf(issue.Key): true}
	for _, it := range out {
		projects[projectOf(it.key)] = true
	}
	text := f.Description
	if f.Comment != nil {
		for _, c := range f.Comment.Comments {
			text += "\n" + c.Body
		}
	}
	for _, k := range mentionedKey.FindAllString(text, -1) {
		if projects[projectOf(k)] {
			add("mentioned", nil, k)
		}
	}
	return out
}

func projectOf(issueKey string) string {
	p, _, _ := strings.Cut(issueKey, "-")
	return p
}

func sortedCustom(m map[string]any) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// knownIssue finds a loaded issue by key: the detail cache, then the list.
func (m model) knownIssue(k string) *jira.Issue {
	if is := m.cache[k]; is != nil {
		return is
	}
	for i := range m.issues {
		if m.issues[i].Key == k {
			return &m.issues[i]
		}
	}
	return nil
}

// openLinks shows the link picker for the detail view's issue.
func (m model) openLinks() (tea.Model, tea.Cmd) {
	issue := m.cache[m.detailKey]
	if issue == nil {
		m.setStatus("Still loading "+m.detailKey, false)
		return m, nil
	}
	m.links = issueLinkItems(issue, m.knownIssue)
	if len(m.links) == 0 {
		m.setStatus("No links on "+m.detailKey, false)
		return m, nil
	}
	m.linkCursor = 0
	m.mode = modeLinks
	m.layout()
	return m, nil
}

func (m model) handleLinksKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.back), key.Matches(msg, keys.quit):
		m.mode = modeDetail
		m.layout()
	case key.Matches(msg, keys.up):
		m.linkCursor = max(0, m.linkCursor-1)
	case key.Matches(msg, keys.down):
		m.linkCursor = min(len(m.links)-1, m.linkCursor+1)
	case key.Matches(msg, keys.top):
		m.linkCursor = 0
	case key.Matches(msg, keys.bottom):
		m.linkCursor = len(m.links) - 1
	case key.Matches(msg, keys.choose):
		target := m.links[m.linkCursor].key
		m.mode = modeDetail
		m.layout()
		return m.followTo(target)
	}
	return m, nil
}

// followTo shows another issue in the detail view, remembering the current
// one for esc.
func (m model) followTo(target string) (tea.Model, tea.Cmd) {
	m.detailTrail = append(append([]string(nil), m.detailTrail...), m.detailKey)
	return m.showInDetail(target)
}

// followBack returns to the issue the detail view came from; false when
// there is none (esc then goes back to the list).
func (m model) followBack() (model, bool) {
	n := len(m.detailTrail)
	if n == 0 {
		return m, false
	}
	prev := m.detailTrail[n-1]
	m.detailTrail = m.detailTrail[:n-1]
	next, _ := m.showInDetail(prev)
	return next.(model), true
}

func (m model) showInDetail(k string) (tea.Model, tea.Cmd) {
	m.detailKey = k
	m.detail.GotoTop()
	m.refreshDetailContent()
	if _, ok := m.cache[k]; ok {
		return m, nil
	}
	return m, tea.Batch(m.startLoading(), m.loadIssue(k))
}

// linksView renders the link picker.
func (m model) linksView() string {
	var sb strings.Builder
	sb.WriteString(styleDim.Render(" Open a linked issue (enter), esc to go back:") + "\n\n")
	relW := 0
	for _, l := range m.links {
		relW = max(relW, lipgloss.Width(l.rel))
	}
	rows := max(1, m.detail.Height-2)
	start := max(0, min(m.linkCursor-rows/2, len(m.links)-rows))
	for i := start; i < min(len(m.links), start+rows); i++ {
		l := m.links[i]
		status := ""
		if l.status != "" {
			status = "  (" + l.status + ")"
		}
		// Shorten the plain summary to fit; styled text can't be cut safely.
		room := m.width - 3 - relW - 2 - lipgloss.Width(l.key) - 2 - lipgloss.Width(status) - 1
		summary := l.summary
		if summary != "" {
			summary = "  " + truncate(summary, max(8, room))
		}
		line := styleDim.Render(padRight(l.rel, relW)) + "  " + styleTitle.Render(l.key) + summary + styleDim.Render(status)
		if i == m.linkCursor {
			sb.WriteString(styleCursor.Render(" ▶ ") + line + "\n")
		} else {
			sb.WriteString("   " + line + "\n")
		}
	}
	return lipgloss.NewStyle().Height(m.detail.Height).Render(sb.String())
}
