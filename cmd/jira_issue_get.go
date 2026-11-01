package cmd

import (
	"github.com/spf13/cobra"
	"github.com/zhani/atlassian-cli/internal/jira"
)

var jiraIssueGetCmd = &cobra.Command{
	Use:   "get <issue-key>",
	Short: "Get a Jira issue",
	Example: `  atlassian-cli jira issue get PROJ-1
  atlassian-cli jira issue get PROJ-1 --fields summary,status,"Story Points"`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		refs, _ := cmd.Flags().GetStringSlice("fields")
		if len(refs) == 0 {
			issue, err := client.GetIssue(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return newFormatter(cmd).PrintIssue(issue)
		}

		// Selected fields, by ID or display name, in the order given.
		fields, err := client.ListFields(cmd.Context())
		if err != nil {
			return err
		}
		ids, err := jira.ResolveFieldIDs(fields, refs)
		if err != nil {
			return err
		}
		values, err := client.GetIssueFields(cmd.Context(), args[0], ids)
		if err != nil {
			return err
		}
		names := map[string]string{}
		for _, f := range fields {
			names[f.ID] = f.Name
		}
		byName := make(map[string]any, len(ids))
		rows := make([][]string, len(ids))
		for i, id := range ids {
			byName[names[id]] = values[id]
			rows[i] = []string{names[id], jira.DisplayValue(values[id])}
		}
		return newFormatter(cmd).PrintRecords(byName, []string{"Field", "Value"}, rows)
	},
}

func init() {
	jiraIssueGetCmd.Flags().StringSlice("fields", nil, `Only show these fields, by ID or name (see "jira field list")`)
	jiraIssueCmd.AddCommand(jiraIssueGetCmd)
}
