package formatter

import (
	"encoding/csv"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/putcho01/atlassian-cli/internal/jira"
)

// issueColumn is a selectable column of issue list output.
type issueColumn struct {
	title string
	value func(jira.Issue) string
}

var issueColumns = map[string]issueColumn{
	"key":      {"Key", func(i jira.Issue) string { return i.Key }},
	"summary":  {"Summary", func(i jira.Issue) string { return i.Fields.Summary }},
	"status":   {"Status", func(i jira.Issue) string { return safeName(i.Fields.Status) }},
	"assignee": {"Assignee", func(i jira.Issue) string { return safeUser(i.Fields.Assignee) }},
	"reporter": {"Reporter", func(i jira.Issue) string { return safeUser(i.Fields.Reporter) }},
	"priority": {"Priority", func(i jira.Issue) string { return safePriority(i.Fields.Priority) }},
	"type":     {"Type", func(i jira.Issue) string { return safeIssueType(i.Fields.IssueType) }},
	"labels":   {"Labels", func(i jira.Issue) string { return strings.Join(i.Fields.Labels, ",") }},
	"created":  {"Created", func(i jira.Issue) string { return i.Fields.Created }},
	"updated":  {"Updated", func(i jira.Issue) string { return i.Fields.Updated }},
	"project": {"Project", func(i jira.Issue) string {
		if i.Fields.Project == nil {
			return ""
		}
		return i.Fields.Project.Key
	}},
}

// DefaultIssueColumns are the columns printed when none are selected.
var DefaultIssueColumns = []string{"key", "summary", "status", "assignee", "priority"}

// IssueColumnNames lists the accepted column names, for help texts.
func IssueColumnNames() []string {
	names := make([]string, 0, len(issueColumns)+1)
	for n := range issueColumns {
		names = append(names, n)
	}
	sort.Strings(names)
	return append(names, "customfield_<id>")
}

// SetIssueColumns selects the columns of issue table, CSV and markdown list
// output. Names are case-insensitive; customfield_<id> selects a custom field.
func (f *Formatter) SetIssueColumns(names []string) error {
	cols := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == "" {
			continue
		}
		if _, ok := issueColumns[n]; !ok && !strings.HasPrefix(n, "customfield_") {
			return fmt.Errorf("unknown column %q (available: %s)", n, strings.Join(IssueColumnNames(), ", "))
		}
		cols = append(cols, n)
	}
	f.columns = cols
	return nil
}

// SetNoHeaders omits the header row from table and CSV output.
func (f *Formatter) SetNoHeaders(v bool) { f.noHeaders = v }

func (f *Formatter) issueColumns() []issueColumn {
	names := f.columns
	if len(names) == 0 {
		names = DefaultIssueColumns
	}
	cols := make([]issueColumn, len(names))
	for i, n := range names {
		if c, ok := issueColumns[n]; ok {
			cols[i] = c
			continue
		}
		field := n
		cols[i] = issueColumn{field, func(is jira.Issue) string { return customValue(is.Fields.Custom[field]) }}
	}
	return cols
}

// printIssueRows renders issues as a table, CSV or markdown table using the
// selected columns.
func (f *Formatter) printIssueRows(issues []jira.Issue) error {
	cols := f.issueColumns()
	row := func(is jira.Issue) []string {
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = c.value(is)
		}
		return cells
	}
	titles := make([]string, len(cols))
	for i, c := range cols {
		titles[i] = c.title
	}

	switch f.format {
	case CSV:
		w := csv.NewWriter(f.writer)
		if !f.noHeaders {
			_ = w.Write(titles)
		}
		for _, is := range issues {
			_ = w.Write(row(is))
		}
		w.Flush()
		return w.Error()
	case Markdown:
		fmt.Fprintf(f.writer, "| %s |\n", strings.Join(titles, " | "))
		fmt.Fprintf(f.writer, "|%s\n", strings.Repeat("---|", len(cols)))
		for _, is := range issues {
			cells := row(is)
			for i, c := range cells {
				cells[i] = escapePipes(c)
			}
			fmt.Fprintf(f.writer, "| %s |\n", strings.Join(cells, " | "))
		}
		return nil
	default:
		tw := tabwriter.NewWriter(f.writer, 0, 4, 2, ' ', 0)
		if !f.noHeaders {
			for i, t := range titles {
				titles[i] = strings.ToUpper(t)
			}
			fmt.Fprintln(tw, strings.Join(titles, "\t"))
		}
		for _, is := range issues {
			fmt.Fprintln(tw, strings.Join(row(is), "\t"))
		}
		return tw.Flush()
	}
}

func escapePipes(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}
