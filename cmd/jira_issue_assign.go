package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var jiraIssueAssignCmd = &cobra.Command{
	Use:   "assign <issue-key> <user>",
	Short: "Assign a Jira issue",
	Long: `Assign a Jira issue. The user is "me", "x" (unassign), "default" (the
project's default assignee), or a username, email or display name; a name
matching several users lists them.`,
	Example: `  atlassian-cli jira issue assign PROJ-1 me
  atlassian-cli jira issue assign PROJ-1 jdoe
  atlassian-cli jira issue assign PROJ-1 "Jane Doe"
  atlassian-cli jira issue assign PROJ-1 x`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		key, ref := args[0], args[1]

		var id, name string
		switch strings.ToLower(ref) {
		case "x":
			name = "nobody"
		case "default":
			id, name = "-1", "the default assignee"
		default:
			user, err := client.ResolveUser(cmd.Context(), ref)
			if err != nil {
				return err
			}
			id, name = client.UserID(user), user.DisplayName
		}
		if err := client.AssignIssue(cmd.Context(), key, id); err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Assigned %s to %s", key, name))
	},
}

var jiraUserCmd = &cobra.Command{
	Use:     "user",
	Aliases: []string{"users"},
	Short:   "Jira user commands",
}

var jiraUserSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search users by name, username or email",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		users, err := client.SearchUsers(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		rows := make([][]string, len(users))
		for i := range users {
			u := &users[i]
			rows[i] = []string{client.UserID(u), u.DisplayName, u.EmailAddress, fmt.Sprint(u.Active)}
		}
		return newFormatter(cmd).PrintRecords(users, []string{"ID", "Name", "Email", "Active"}, rows)
	},
}

func init() {
	jiraIssueCmd.AddCommand(jiraIssueAssignCmd)
	jiraUserCmd.AddCommand(jiraUserSearchCmd)
	jiraCmd.AddCommand(jiraUserCmd)
}
