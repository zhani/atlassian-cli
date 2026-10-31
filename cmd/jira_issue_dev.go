package cmd

import (
	"github.com/spf13/cobra"
)

var jiraIssueDevCmd = &cobra.Command{
	Use:     "development <issue-key>",
	Aliases: []string{"dev"},
	Short:   "Show pull requests and branches linked to a Jira issue",
	Long: `Show the pull requests and branches that connected tools (Bitbucket,
GitHub, GitLab, ...) report in the issue's development panel.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		info, err := client.GetDevInfo(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		f := newFormatter(cmd)
		if isJSONOutput(cmd) {
			return f.PrintJSON(info)
		}
		var rows [][]string
		for _, p := range info.PullRequests {
			rows = append(rows, []string{"PR " + p.ID, p.Status, p.Name, p.Source + " → " + p.Destination, p.URL})
		}
		for _, b := range info.Branches {
			rows = append(rows, []string{"branch", "", b.Name, b.Repository, b.URL})
		}
		if len(rows) == 0 {
			return f.PrintMessage("No pull requests or branches linked to " + args[0])
		}
		return f.PrintRecords(info, []string{"Kind", "Status", "Name", "Branches/Repo", "URL"}, rows)
	},
}

func init() {
	jiraIssueCmd.AddCommand(jiraIssueDevCmd)
}
