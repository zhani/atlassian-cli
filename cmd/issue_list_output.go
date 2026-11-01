package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/putcho01/atlassian-cli/internal/browser"
	"github.com/putcho01/atlassian-cli/internal/config"
	"github.com/putcho01/atlassian-cli/internal/formatter"
	"github.com/putcho01/atlassian-cli/internal/jira"
	"github.com/putcho01/atlassian-cli/internal/tui"
	"github.com/spf13/cobra"
)

// addIssueListFlags registers the output flags shared by commands that list
// issues (search, list).
func addIssueListFlags(cmd *cobra.Command) {
	cmd.Flags().Int("max-results", 50, "Maximum number of results (fetched across pages)")
	cmd.Flags().BoolP("interactive", "i", false, "Launch the interactive issue browser")
	cmd.Flags().Bool("plain", false, "Print plain output even when running in a terminal")
	cmd.MarkFlagsMutuallyExclusive("interactive", "plain")
	addColumnFlags(cmd)
}

// addColumnFlags registers --columns and --no-headers for issue tables.
func addColumnFlags(cmd *cobra.Command) {
	cmd.Flags().StringSlice("columns", nil, "Columns for table/csv/markdown output: "+strings.Join(formatter.IssueColumnNames(), ", ")+", or a field name")
	cmd.Flags().Bool("no-headers", false, "Omit the header row from table/csv output")
}

// showIssues runs jql and shows the result in the interactive browser or
// prints it with the selected output format.
func showIssues(cmd *cobra.Command, client *jira.Client, jql string) error {
	return showIssuesWith(cmd, client, jql, nil)
}

// showIssuesWith is showIssues with a hook to adjust the browser options
// (caption, annotation column).
func showIssuesWith(cmd *cobra.Command, client *jira.Client, jql string, adjust func(*tui.Options)) error {
	maxResults, _ := cmd.Flags().GetInt("max-results")
	issues, err := client.SearchIssuesAll(cmd.Context(), jql, nil, maxResults)
	if err != nil {
		return err
	}

	if useInteractive(cmd) {
		opts := browserOptions(client, maxResults)
		opts.JQL, opts.Issues = jql, issues
		if adjust != nil {
			adjust(&opts)
		}
		return tui.RunIssueList(cmd.Context(), opts)
	}

	f := newFormatter(cmd)
	columns, _ := cmd.Flags().GetStringSlice("columns")
	columns, err = resolveColumns(cmd, client, f, columns)
	if err != nil {
		return err
	}
	if err := f.SetIssueColumns(columns); err != nil {
		return err
	}
	noHeaders, _ := cmd.Flags().GetBool("no-headers")
	f.SetNoHeaders(noHeaders)
	return f.PrintIssueList(issues)
}

// browserOptions returns the issue browser options shared by all views.
func browserOptions(client *jira.Client, limit int) tui.Options {
	opts := tui.Options{Source: client, Limit: limit, OpenURL: browser.Open, Copy: tui.CopyToClipboard}
	if cfg, err := config.LoadJiraConfig(); err == nil {
		opts.DetailFields = cfg.DetailFields
	}
	return opts
}

// useInteractive decides whether to launch the TUI: always with -i, never
// with --plain, an explicit --output or output-shaping flags, otherwise only
// on a terminal.
func useInteractive(cmd *cobra.Command) bool {
	if interactive, _ := cmd.Flags().GetBool("interactive"); interactive {
		return true
	}
	for _, f := range []string{"plain", "output", "columns", "no-headers"} {
		if cmd.Flags().Changed(f) {
			return false
		}
	}
	return isatty.IsTerminal(os.Stdout.Fd()) && isatty.IsTerminal(os.Stdin.Fd())
}

// resolveColumns replaces field display names in columns ("Story Points")
// by their IDs and gives those columns the names as titles. The field list
// is only fetched when a column is not a built-in name or an ID.
func resolveColumns(cmd *cobra.Command, client *jira.Client, f *formatter.Formatter, columns []string) ([]string, error) {
	var names []string
	for _, c := range columns {
		if !formatter.IsIssueColumn(c) {
			names = append(names, c)
		}
	}
	if len(names) == 0 {
		return columns, nil
	}
	fields, err := client.ListFields(cmd.Context())
	if err != nil {
		return nil, err
	}
	ids, err := jira.ResolveFieldIDs(fields, names)
	if err != nil {
		return nil, err
	}
	titles := map[string]string{}
	out := make([]string, 0, len(columns))
	for _, c := range columns {
		if formatter.IsIssueColumn(c) {
			out = append(out, c)
			continue
		}
		id := ids[0]
		ids = ids[1:]
		if !formatter.IsIssueColumn(id) {
			return nil, fmt.Errorf("field %q (%s) cannot be a column: use a built-in column or a custom field", c, id)
		}
		titles[id] = strings.TrimSpace(c)
		out = append(out, id)
	}
	f.SetFieldTitles(titles)
	return out, nil
}
