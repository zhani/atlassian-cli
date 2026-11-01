package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zhani/atlassian-cli/internal/jira"
)

var jiraIssueCloneCmd = &cobra.Command{
	Use:   "clone <issue-key>",
	Short: "Clone a Jira issue",
	Long: `Create a copy of an issue in the same project with its summary, description,
type, priority, labels, components, assignee and (for subtasks) parent.
Custom fields, comments, attachments and links are not copied.`,
	Example: `  atlassian-cli jira issue clone PROJ-1
  atlassian-cli jira issue clone PROJ-1 -s "Same bug on Android" -a me
  atlassian-cli jira issue clone PROJ-1 -H "v1.2:v1.3" -H "iOS:Android"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		var o jira.CloneOptions
		o.Summary, _ = cmd.Flags().GetString("summary")
		o.Priority, _ = cmd.Flags().GetString("priority")
		o.Labels, _ = cmd.Flags().GetStringArray("label")
		replace, _ := cmd.Flags().GetStringArray("replace")
		for _, r := range replace {
			find, repl, ok := strings.Cut(r, ":")
			if !ok || find == "" {
				return fmt.Errorf("--replace %q: expected find:replace", r)
			}
			o.Replace = append(o.Replace, [2]string{find, repl})
		}
		if ref, _ := cmd.Flags().GetString("assignee"); ref != "" {
			user, err := client.ResolveUser(cmd.Context(), ref)
			if err != nil {
				return err
			}
			o.Assignee = client.UserID(user)
		}

		issue, err := client.CloneIssue(cmd.Context(), args[0], o)
		if err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Cloned %s as %s", args[0], issue.Key))
	},
}

var jiraIssueWorklogCmd = &cobra.Command{
	Use:   "worklog",
	Short: "Manage time logged on a Jira issue",
}

var jiraIssueWorklogAddCmd = &cobra.Command{
	Use:   "add <issue-key> <time-spent>",
	Short: "Log time on a Jira issue",
	Long: `Log time on a Jira issue. Time uses Jira's duration format, e.g. "30m",
"2h 30m" or "1d". --started takes a Jira timestamp
(2026-01-31T09:00:00.000+0000); the default is now.`,
	Example: `  atlassian-cli jira issue worklog add PROJ-1 "2h 30m" --comment "Code review"`,
	Args:    cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		comment, _ := cmd.Flags().GetString("comment")
		started, _ := cmd.Flags().GetString("started")
		w, err := client.AddWorklog(cmd.Context(), args[0], args[1], jiraText(cmd, comment), started)
		if err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Logged %s on %s", w.TimeSpent, args[0]))
	},
}

func init() {
	f := jiraIssueCloneCmd.Flags()
	f.StringP("summary", "s", "", "Summary of the clone (default: same as the original)")
	f.StringP("priority", "y", "", "Priority of the clone")
	f.StringP("assignee", "a", "", `Assignee of the clone ("me" or a user)`)
	f.StringArrayP("label", "l", nil, "Labels of the clone (repeatable; replaces the original labels)")
	f.StringArrayP("replace", "H", nil, "Replace text in summary and description, find:replace (repeatable)")
	jiraIssueWorklogAddCmd.Flags().String("comment", "", "Worklog comment (Markdown)")
	jiraIssueWorklogAddCmd.Flags().String("started", "", "Start time as a Jira timestamp (default: now)")
	addMarkdownFlag(jiraIssueWorklogAddCmd)
	jiraIssueWorklogCmd.AddCommand(jiraIssueWorklogAddCmd)
	jiraIssueCmd.AddCommand(jiraIssueCloneCmd, jiraIssueWorklogCmd)
}
