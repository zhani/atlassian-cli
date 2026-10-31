package cmd

import (
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/putcho01/atlassian-cli/internal/browser"
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
	cmd.Flags().StringSlice("columns", nil, "Columns for table/csv/markdown output: "+strings.Join(formatter.IssueColumnNames(), ", "))
	cmd.Flags().Bool("no-headers", false, "Omit the header row from table/csv output")
	cmd.MarkFlagsMutuallyExclusive("interactive", "plain")
}

// showIssues runs jql and shows the result in the interactive browser or
// prints it with the selected output format.
func showIssues(cmd *cobra.Command, client *jira.Client, jql string) error {
	maxResults, _ := cmd.Flags().GetInt("max-results")
	issues, err := client.SearchIssuesAll(cmd.Context(), jql, nil, maxResults)
	if err != nil {
		return err
	}

	if useInteractive(cmd) {
		return tui.RunIssueList(cmd.Context(), tui.Options{
			Source:  client,
			JQL:     jql,
			Limit:   maxResults,
			Issues:  issues,
			OpenURL: browser.Open,
			Copy:    tui.CopyToClipboard,
		})
	}

	f := newFormatter(cmd)
	columns, _ := cmd.Flags().GetStringSlice("columns")
	if err := f.SetIssueColumns(columns); err != nil {
		return err
	}
	noHeaders, _ := cmd.Flags().GetBool("no-headers")
	f.SetNoHeaders(noHeaders)
	return f.PrintIssueList(issues)
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
