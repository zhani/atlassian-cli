package cmd

import (
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
		return showIssues(cmd, client, args[0])
	},
}

func init() {
	addIssueListFlags(jiraIssueSearchCmd)
	jiraIssueCmd.AddCommand(jiraIssueSearchCmd)
}
