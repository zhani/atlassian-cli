package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var jiraIssueDeleteCmd = &cobra.Command{
	Use:   "delete <issue-key>",
	Short: "Delete a Jira issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cascade, _ := cmd.Flags().GetBool("cascade")
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		if err := client.DeleteIssue(cmd.Context(), args[0], cascade); err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Issue %s deleted", args[0]))
	},
}

func init() {
	jiraIssueDeleteCmd.Flags().Bool("cascade", false, "Also delete the issue's subtasks (required when it has any)")
	jiraIssueCmd.AddCommand(jiraIssueDeleteCmd)
}
