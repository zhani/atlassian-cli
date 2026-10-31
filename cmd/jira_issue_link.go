package cmd

import (
	"fmt"

	"github.com/putcho01/atlassian-cli/internal/jira"
	"github.com/spf13/cobra"
)

var jiraIssueLinkCmd = &cobra.Command{
	Use:   "link <issue-key> <other-key> <type>",
	Short: "Link two Jira issues",
	Long: `Link two issues so that the link reads "<issue-key> <type> <other-key>".

The type is a link type name ("Blocks") or one of its phrases ("blocks",
"is blocked by"); see "jira issue link types".`,
	Example: `  atlassian-cli jira issue link PROJ-1 PROJ-2 Blocks             # PROJ-1 blocks PROJ-2
  atlassian-cli jira issue link PROJ-1 PROJ-2 "is blocked by"     # PROJ-1 is blocked by PROJ-2
  atlassian-cli jira issue link FEAT-1 PROJ-3 "is implemented by"`,
	Args: cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		types, err := client.GetLinkTypes(cmd.Context())
		if err != nil {
			return err
		}
		lt, swap, err := jira.ResolveLinkType(types, args[2])
		if err != nil {
			return err
		}
		from, to := args[0], args[1]
		phrase := lt.Outward
		if swap {
			from, to, phrase = to, from, lt.Inward
		}
		if err := client.LinkIssues(cmd.Context(), lt.Name, from, to); err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Linked: %s %s %s", args[0], phrase, args[1]))
	},
}

var jiraIssueUnlinkCmd = &cobra.Command{
	Use:   "unlink <issue-key> <other-key>",
	Short: "Remove the links between two Jira issues",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		typ, _ := cmd.Flags().GetString("type")
		n, err := client.UnlinkIssues(cmd.Context(), args[0], args[1], typ)
		if err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Removed %d link(s) between %s and %s", n, args[0], args[1]))
	},
}

var jiraIssueLinkRemoteCmd = &cobra.Command{
	Use:   "remote <issue-key> <url> [title]",
	Short: "Add a web link to a Jira issue",
	Args:  cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		title := ""
		if len(args) == 3 {
			title = args[2]
		}
		if err := client.AddRemoteLink(cmd.Context(), args[0], args[1], title); err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Added link %s to %s", args[1], args[0]))
	},
}

var jiraIssueLinkTypesCmd = &cobra.Command{
	Use:   "types",
	Short: "List issue link types",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		types, err := client.GetLinkTypes(cmd.Context())
		if err != nil {
			return err
		}
		rows := make([][]string, len(types))
		for i, t := range types {
			rows[i] = []string{t.Name, t.Outward, t.Inward}
		}
		return newFormatter(cmd).PrintRecords(types, []string{"Name", "Outward", "Inward"}, rows)
	},
}

func init() {
	jiraIssueUnlinkCmd.Flags().String("type", "", "Only remove links of this type")
	jiraIssueLinkCmd.AddCommand(jiraIssueLinkRemoteCmd, jiraIssueLinkTypesCmd)
	jiraIssueCmd.AddCommand(jiraIssueLinkCmd, jiraIssueUnlinkCmd)
}
