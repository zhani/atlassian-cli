// Package xref finds the references between Jira and Confluence in their
// content: the Confluence pages an issue links to (description, comments,
// remote links) and the Jira issues a page shows (Jira issue macro) or
// links to. The export follows them so both ends are mirrored; the TUIs
// follow them so one opens the other.
package xref

import (
	"encoding/base64"
	"encoding/binary"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// PageRef is a reference to a Confluence page: by ID, or by space and
// title (a /display/SPACE/Title link).
type PageRef struct {
	ID    string
	Space string
	Title string
	URL   string // the link it came from; empty for remote links without one
}

// Key identifies the page for de-duplication.
func (r PageRef) Key() string {
	if r.ID != "" {
		return "id:" + r.ID
	}
	return "title:" + r.Space + "/" + r.Title
}

// Label names the page for a list: its title when known, else its ID.
func (r PageRef) Label() string {
	if r.Title != "" {
		return r.Title
	}
	return "page " + r.ID
}

// IssueRef is a reference to a Jira issue.
type IssueRef struct {
	Key  string
	URL  string // the link it came from; empty for a Jira issue macro
	List bool   // found by an issue list (query) on the page, not named on it
}

// urlRe finds absolute URLs in HTML, Jira wiki markup ([text|url]) and
// plain text.
var urlRe = regexp.MustCompile(`https?://[^\s"'<>\[\]|\\{}]+`)

var (
	spacesPageRe = regexp.MustCompile(`^/spaces/([^/]+)/pages/(\d+)(?:/|$)`)
	displayRe    = regexp.MustCompile(`^/display/([^/]+)/([^/]+)$`)
	tinyRe       = regexp.MustCompile(`^/x/([A-Za-z0-9_-]{1,11})$`)
	issueKeyRe   = regexp.MustCompile(`^[A-Z][A-Z0-9_]*-\d+$`)
	browseRe     = regexp.MustCompile(`^/(?:browse|projects/[^/]+/issues)/([A-Z][A-Z0-9_]*-\d+)$`)
	jiraMacroRe  = regexp.MustCompile(`(?s)<ac:structured-macro\b[^>]*\bac:name="jira"[^>]*>(.*?)</ac:structured-macro>`)
	macroKeyRe   = regexp.MustCompile(`<ac:parameter\s+ac:name="key"\s*>\s*([^<\s]+)\s*</ac:parameter>`)
)

// relative returns the part of u under one of bases ("/path?query"), or
// false when u is on none of them.
func relative(u string, bases []string) (string, bool) {
	for _, b := range bases {
		b = strings.TrimRight(b, "/")
		if b == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(u, b); ok && (rest == "" || strings.ContainsAny(rest[:1], "/?#")) {
			return rest, true
		}
	}
	return "", false
}

// links returns the URLs in text, HTML entities decoded, each once.
func links(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, u := range urlRe.FindAllString(text, -1) {
		u = strings.TrimRight(html.UnescapeString(u), ".,;:)!")
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	return out
}

// PageRefs lists the Confluence pages text links to, in order, each once.
// Only links under one of bases (the Confluence base URLs) count; the
// forms are viewpage.action?pageId=N (or ?spaceKey=S&title=T),
// /spaces/S/pages/N/…, /display/S/Title and tiny links (/x/AbC).
func PageRefs(text string, bases ...string) []PageRef {
	seen := map[string]bool{}
	var out []PageRef
	for _, u := range links(text) {
		rel, ok := relative(u, bases)
		if !ok {
			continue
		}
		ref, ok := pageRef(rel)
		if !ok {
			continue
		}
		ref.URL = u
		if !seen[ref.Key()] {
			seen[ref.Key()] = true
			out = append(out, ref)
		}
	}
	return out
}

func pageRef(rel string) (PageRef, bool) {
	u, err := url.Parse(rel)
	if err != nil {
		return PageRef{}, false
	}
	path, q := u.Path, u.Query()
	if strings.HasSuffix(path, ".action") && strings.Contains(path, "/pages/") {
		if id := q.Get("pageId"); isDigits(id) {
			return PageRef{ID: id}, true
		}
		if s, t := q.Get("spaceKey"), q.Get("title"); s != "" && t != "" {
			return PageRef{Space: s, Title: t}, true
		}
		return PageRef{}, false
	}
	if m := spacesPageRe.FindStringSubmatch(path); m != nil {
		return PageRef{ID: m[2]}, true
	}
	// Titles use + for spaces; a literal + is %2B, so decode the raw path.
	if m := displayRe.FindStringSubmatch(u.EscapedPath()); m != nil {
		space, err1 := url.PathUnescape(m[1])
		title, err2 := url.PathUnescape(strings.ReplaceAll(m[2], "+", " "))
		if err1 == nil && err2 == nil && title != "" {
			return PageRef{Space: space, Title: title}, true
		}
	}
	if m := tinyRe.FindStringSubmatch(path); m != nil {
		if id, ok := DecodeTinyLink(m[1]); ok {
			return PageRef{ID: id}, true
		}
	}
	return PageRef{}, false
}

// DecodeTinyLink returns the page ID of a Confluence tiny link code (the
// part after /x/): the page ID as little-endian bytes, base64 with - and _
// for / and +, trailing zero bytes ('A's) and padding dropped.
func DecodeTinyLink(code string) (string, bool) {
	if code == "" || len(code) > 11 {
		return "", false
	}
	s := strings.NewReplacer("-", "/", "_", "+").Replace(code)
	s += strings.Repeat("A", 11-len(s)) + "="
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != 8 {
		return "", false
	}
	id := binary.LittleEndian.Uint64(b)
	if id == 0 {
		return "", false
	}
	return strconv.FormatUint(id, 10), true
}

// IssueRefs lists the Jira issues in a Confluence page's storage format, in
// order, each once: Jira issue macros (single issue) and links to issues
// under one of bases (the Jira base URLs: /browse/KEY,
// /projects/P/issues/KEY).
func IssueRefs(storage string, bases ...string) []IssueRef {
	seen := map[string]bool{}
	var out []IssueRef
	add := func(r IssueRef) {
		r.Key = strings.ToUpper(strings.TrimSpace(r.Key))
		if issueKeyRe.MatchString(r.Key) && !seen[r.Key] {
			seen[r.Key] = true
			out = append(out, r)
		}
	}
	for _, m := range jiraMacroRe.FindAllStringSubmatch(storage, -1) {
		if k := macroKeyRe.FindStringSubmatch(m[1]); k != nil {
			add(IssueRef{Key: html.UnescapeString(k[1])})
		}
	}
	for _, u := range links(storage) {
		rel, ok := relative(u, bases)
		if !ok {
			continue
		}
		p, err := url.Parse(rel)
		if err != nil {
			continue
		}
		if m := browseRe.FindStringSubmatch(p.Path); m != nil {
			add(IssueRef{Key: m[1], URL: u})
		}
	}
	return out
}

// Strings collects every string in a decoded JSON value (an issue, its
// remote links), so references can be found in any field.
func Strings(v any) string {
	var sb strings.Builder
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case string:
			sb.WriteString(t)
			sb.WriteByte('\n')
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(t[k])
			}
		case []any:
			for _, x := range t {
				walk(x)
			}
		}
	}
	walk(v)
	return sb.String()
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// IssueQuery is a Jira issues macro listing the result of a JQL query (a
// filter): the issue table a page shows.
type IssueQuery struct {
	JQL     string   // as written in the macro, whitespace trimmed
	Columns []string // field IDs ("issuekey", "summary", "customfield_123")
	Headers []string // their headings as the macro names them
	Max     int      // maximumIssues; 20 when the macro gives none
	Server  string   // the Jira server's name in Confluence's application links
}

// Confluence's own defaults for a Jira issues macro without columns or a
// limit.
var (
	defaultQueryColumns = []string{"issuekey", "summary", "issuetype", "created", "updated", "duedate", "assignee", "reporter", "priority", "status", "resolution"}
	defaultQueryHeaders = []string{"key", "summary", "type", "created", "updated", "due", "assignee", "reporter", "priority", "status", "resolution"}
)

const defaultQueryMax = 20

var macroParamRe = regexp.MustCompile(`(?s)<ac:parameter\s+ac:name="([^"]+)"\s*>(.*?)</ac:parameter>`)

// issueQuery reads a Jira macro's body; false for a single-issue macro.
func issueQuery(body string) (IssueQuery, bool) {
	params := map[string]string{}
	for _, p := range macroParamRe.FindAllStringSubmatch(body, -1) {
		params[p[1]] = strings.TrimSpace(html.UnescapeString(p[2]))
	}
	q := IssueQuery{JQL: params["jqlQuery"], Server: params["server"], Max: defaultQueryMax}
	if q.JQL == "" {
		return q, false
	}
	if n, err := strconv.Atoi(params["maximumIssues"]); err == nil && n > 0 {
		q.Max = n
	}
	ids, names := splitList(params["columnIds"]), splitList(params["columns"])
	if len(ids) == 0 {
		ids = names // older macros name the columns only
	}
	if len(ids) == 0 {
		ids, names = defaultQueryColumns, defaultQueryHeaders
	}
	for i, id := range ids {
		switch strings.ToLower(id) {
		case "key":
			id = "issuekey"
		case "type":
			id = "issuetype"
		case "due":
			id = "duedate"
		}
		name := id
		if i < len(names) {
			name = names[i]
		}
		q.Columns, q.Headers = append(q.Columns, id), append(q.Headers, name)
	}
	return q, true
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// IssueQueries lists the Jira issues macros of a page's storage format that
// show a query's result, in order, each query once.
func IssueQueries(storage string) []IssueQuery {
	seen := map[string]bool{}
	var out []IssueQuery
	for _, m := range jiraMacroRe.FindAllStringSubmatch(storage, -1) {
		if q, ok := issueQuery(m[1]); ok && !seen[NormalizeJQL(q.JQL)] {
			seen[NormalizeJQL(q.JQL)] = true
			out = append(out, q)
		}
	}
	return out
}

// ReplaceIssueQueries replaces each query macro in storage by what render
// returns for it; single-issue macros stay.
func ReplaceIssueQueries(storage string, render func(IssueQuery) string) string {
	return jiraMacroRe.ReplaceAllStringFunc(storage, func(macro string) string {
		m := jiraMacroRe.FindStringSubmatch(macro)
		if q, ok := issueQuery(m[1]); ok {
			return render(q)
		}
		return macro
	})
}

// NormalizeJQL is the form queries are matched by: whitespace collapsed.
func NormalizeJQL(jql string) string {
	return strings.Join(strings.Fields(jql), " ")
}

// QueryResult is a query's result as the export stores it beside a page
// (jira-queries.json): the issue keys in Jira's order, or why it failed.
type QueryResult struct {
	JQL   string   `json:"jql"`
	Keys  []string `json:"keys"`
	Total int      `json:"total"`
	Error string   `json:"error,omitempty"`
}
