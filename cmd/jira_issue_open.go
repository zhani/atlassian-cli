package cmd

import (
	"github.com/putcho01/atlassian-cli/internal/browser"
	"github.com/spf13/cobra"
)

var jiraIssueOpenCmd = &cobra.Command{
	Use:   "open <issue-key>",
	Short: "Open a Jira issue in the browser",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		return browser.Open(client.IssueURL(args[0]))
	},
}

func init() {
	jiraIssueCmd.AddCommand(jiraIssueOpenCmd)
}
