package tui

import (
	"context"
	"fmt"
	"html"
	"sort"
	"strings"
	"sync"

	"github.com/zhani/atlassian-cli/internal/xref"
)

// Issue lists on pages: a Jira issues macro with a query (a filter) shows
// as a table of the issues it finds, with the macro's columns, as in the
// browser. The queries run when the page loads; in a mirror they are
// answered as Jira answered them at export.

// IssueSearcher runs a JQL query, returning up to limit issues as Jira's
// JSON with the given fields and expansions.
type IssueSearcher func(ctx context.Context, jql string, fields []string, limit int, expand ...string) ([]map[string]any, error)

type issueList struct {
	issues []map[string]any
	err    error
}

// maxListIssues caps the rows of one list (the macro's own limit first).
const maxListIssues = 1000

// loadIssueLists runs the queries of a page's issue lists, in parallel,
// keyed by normalized JQL.
func loadIssueLists(ctx context.Context, search IssueSearcher, storage string) map[string]issueList {
	queries := xref.IssueQueries(storage)
	if search == nil || len(queries) == 0 {
		return nil
	}
	out := make(map[string]issueList, len(queries))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, q := range queries {
		wg.Add(1)
		go func(q xref.IssueQuery) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fields := append([]string{"summary"}, q.Columns...)
			var expand []string
			for _, c := range q.Columns {
				if mayBeRich(c) {
					expand = []string{"renderedFields"}
				}
			}
			issues, err := search(ctx, q.JQL, fields, min(q.Max, maxListIssues), expand...)
			mu.Lock()
			out[xref.NormalizeJQL(q.JQL)] = issueList{issues: issues, err: err}
			mu.Unlock()
		}(q)
	}
	wg.Wait()
	return out
}

// expandIssueLists replaces the issue list macros in storage by HTML tables
// of their results (the converter renders those as Markdown tables).
func expandIssueLists(storage string, lists map[string]issueList) string {
	if lists == nil {
		return storage
	}
	return xref.ReplaceIssueQueries(storage, func(q xref.IssueQuery) string {
		l, ok := lists[xref.NormalizeJQL(q.JQL)]
		if !ok {
			return ""
		}
		jql := "<code>" + html.EscapeString(xref.NormalizeJQL(q.JQL)) + "</code>"
		if l.err != nil {
			return "<p><strong>Jira issues</strong> " + jql + ": " + html.EscapeString(oneLine(l.err.Error(), 200)) + "</p>"
		}
		count := fmt.Sprintf("%d issues", len(l.issues))
		switch {
		case len(l.issues) == 1:
			count = "1 issue"
		case len(l.issues) >= min(q.Max, maxListIssues):
			count = fmt.Sprintf("first %d issues", len(l.issues))
		}
		var sb strings.Builder
		sb.WriteString("<p><strong>Jira issues</strong> (" + count + ") " + jql + "</p>")
		if len(l.issues) == 0 {
			return sb.String()
		}
		// Rich text (a description) shows formatted, as Confluence shows it
		// in the cell, but below the table: a terminal table cell holds one
		// line.
		var plain, rich []int
		for i, c := range q.Columns {
			if richColumn(l.issues, c) {
				rich = append(rich, i)
			} else {
				plain = append(plain, i)
			}
		}
		if len(plain) > 0 {
			sb.WriteString("<table><tr>")
			for _, i := range plain {
				sb.WriteString("<th>" + cellText(q.Headers[i]) + "</th>")
			}
			sb.WriteString("</tr>")
			for _, is := range l.issues {
				sb.WriteString("<tr>")
				for _, i := range plain {
					sb.WriteString("<td>" + cellText(columnValue(is, q.Columns[i])) + "</td>")
				}
				sb.WriteString("</tr>")
			}
			sb.WriteString("</table>")
		}
		// One issue's text alone needs no label, as in the browser.
		label := len(plain) > 0 || len(l.issues) > 1 || len(rich) > 1
		for _, is := range l.issues {
			for _, i := range rich {
				text := richValue(is, q.Columns[i])
				if text == "" {
					continue
				}
				if label {
					k, _ := is["key"].(string)
					sb.WriteString("<p><strong>" + html.EscapeString(k) + "</strong> · " + html.EscapeString(q.Headers[i]) + "</p>")
				}
				sb.WriteString("<div>" + text + "</div>")
			}
		}
		return sb.String()
	})
}

// mayBeRich tells whether a column may hold rich text (rendered by Jira).
func mayBeRich(col string) bool {
	return col == "description" || col == "environment" || strings.HasPrefix(col, "customfield_")
}

// richColumn tells whether a column holds rich text: the description, the
// environment, or a custom field Jira renders to block HTML (a text area).
func richColumn(issues []map[string]any, col string) bool {
	if col == "description" || col == "environment" {
		return true
	}
	if !strings.HasPrefix(col, "customfield_") {
		return false
	}
	for _, is := range issues {
		rf, _ := is["renderedFields"].(map[string]any)
		if v, ok := rf[col].(string); ok && strings.Contains(v, "<p>") {
			return true
		}
	}
	return false
}

// richValue is a rich text column as HTML: Jira's rendering, else the raw
// text with its line breaks.
func richValue(is map[string]any, col string) string {
	rf, _ := is["renderedFields"].(map[string]any)
	if v, ok := rf[col].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	f, _ := is["fields"].(map[string]any)
	v, _ := f[col].(string)
	if strings.TrimSpace(v) == "" {
		return ""
	}
	return "<p>" + strings.ReplaceAll(html.EscapeString(v), "\n", "<br/>") + "</p>"
}

// cellText makes a value safe for one Markdown table cell.
func cellText(s string) string {
	return html.EscapeString(strings.ReplaceAll(oneLine(s, 100), "|", "/"))
}

// oneLine collapses whitespace and cuts s to n runes.
func oneLine(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// columnValue is the text of one column of an issue list row.
func columnValue(is map[string]any, col string) string {
	if col == "issuekey" {
		k, _ := is["key"].(string)
		return k
	}
	f, _ := is["fields"].(map[string]any)
	v := f[col]
	if col == "issuelinks" {
		var keys []string
		list, _ := v.([]any)
		for _, l := range list {
			lm, _ := l.(map[string]any)
			for _, side := range []string{"outwardIssue", "inwardIssue"} {
				if o, ok := lm[side].(map[string]any); ok {
					if k, _ := o["key"].(string); k != "" {
						keys = append(keys, k)
					}
				}
			}
		}
		return strings.Join(keys, ", ")
	}
	s := fieldText(v)
	switch col {
	case "created", "updated", "duedate", "resolutiondate":
		if len(s) > 10 {
			s = s[:10]
		}
	}
	return s
}

// fieldText renders a field value as Jira shows it in a list: names of
// objects, lists joined.
func fieldText(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return fmt.Sprint(t)
	case bool:
		return fmt.Sprint(t)
	case []any:
		var parts []string
		for _, x := range t {
			if s := fieldText(x); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		for _, k := range []string{"displayName", "name", "value", "key"} {
			if s, ok := t[k].(string); ok && s != "" {
				if child, ok := t["child"].(map[string]any); ok && k == "value" {
					return s + " - " + fieldText(child)
				}
				return s
			}
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			if s, ok := t[k].(string); ok && s != "" && !strings.HasPrefix(s, "http") {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	}
	return fmt.Sprint(v)
}

// listedIssues lists the keys in a page's issue lists, for the L picker.
func listedIssues(lists map[string]issueList) []string {
	var keys []string
	seen := map[string]bool{}
	jqls := make([]string, 0, len(lists))
	for j := range lists {
		jqls = append(jqls, j)
	}
	sort.Strings(jqls)
	for _, j := range jqls {
		for _, is := range lists[j].issues {
			if k, _ := is["key"].(string); k != "" && !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	return keys
}
