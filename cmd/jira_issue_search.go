package cmd

import (
	"os"

	"github.com/mattn/go-isatty"
	"github.com/putcho01/atlassian-cli/internal/browser"
	"github.com/putcho01/atlassian-cli/internal/tui"
	"github.com/spf13/cobra"
)

var jiraIssueSearchCmd = &cobra.Command{
	Use:   "search <jql>",
	Short: "Search Jira issues using JQL",
	Long: `Search Jira issues using JQL.

When run in a terminal without an explicit --output, results open in the
interactive browser (same as -i). Use --plain or -o to get plain output.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		maxResults, _ := cmd.Flags().GetInt("max-results")
		issues, err := client.SearchIssuesAll(cmd.Context(), args[0], nil, maxResults)
		if err != nil {
			return err
		}

		if !useInteractive(cmd) {
			return newFormatter(cmd).PrintIssueList(issues)
		}
		return tui.RunIssueList(cmd.Context(), tui.Options{
			Source:  client,
			JQL:     args[0],
			Limit:   maxResults,
			Issues:  issues,
			OpenURL: browser.Open,
			Copy:    tui.CopyToClipboard,
		})
	},
}

// useInteractive decides whether to launch the TUI: always with -i, never
// with --plain or an explicit --output, otherwise only on a terminal.
func useInteractive(cmd *cobra.Command) bool {
	if interactive, _ := cmd.Flags().GetBool("interactive"); interactive {
		return true
	}
	if plain, _ := cmd.Flags().GetBool("plain"); plain || cmd.Flags().Changed("output") {
		return false
	}
	return isatty.IsTerminal(os.Stdout.Fd()) && isatty.IsTerminal(os.Stdin.Fd())
}

func init() {
	jiraIssueSearchCmd.Flags().Int("max-results", 50, "Maximum number of results (fetched across pages)")
	jiraIssueSearchCmd.Flags().BoolP("interactive", "i", false, "Launch the interactive issue browser")
	jiraIssueSearchCmd.Flags().Bool("plain", false, "Print plain output even when running in a terminal")
	jiraIssueSearchCmd.MarkFlagsMutuallyExclusive("interactive", "plain")
	jiraIssueCmd.AddCommand(jiraIssueSearchCmd)
}
