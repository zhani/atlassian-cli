package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/putcho01/atlassian-cli/internal/jira"
	"github.com/spf13/cobra"
)

var jiraFieldCmd = &cobra.Command{
	Use:     "field",
	Aliases: []string{"fields"},
	Short:   "Jira field commands",
}

var jiraFieldListCmd = &cobra.Command{
	Use:   "list [filter]",
	Short: "List fields and their IDs",
	Long: `List system and custom fields with their IDs. The optional filter matches
field names and IDs (case-insensitive). Field names can be used with
"issue get --fields" and "issue list --columns".`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		fields, err := client.ListFields(cmd.Context())
		if err != nil {
			return err
		}
		customOnly, _ := cmd.Flags().GetBool("custom")
		filter := ""
		if len(args) == 1 {
			filter = strings.ToLower(args[0])
		}
		var out []jira.Field
		for _, f := range fields {
			if customOnly && !f.Custom {
				continue
			}
			if filter != "" && !strings.Contains(strings.ToLower(f.Name+" "+f.ID), filter) {
				continue
			}
			out = append(out, f)
		}
		sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })

		rows := make([][]string, len(out))
		for i, f := range out {
			typ := ""
			if f.Schema != nil {
				typ = f.Schema.Type
				if f.Schema.Items != "" {
					typ += "<" + f.Schema.Items + ">"
				}
			}
			rows[i] = []string{f.ID, f.Name, typ, fmt.Sprint(f.Custom)}
		}
		return newFormatter(cmd).PrintRecords(out, []string{"ID", "Name", "Type", "Custom"}, rows)
	},
}

func init() {
	jiraFieldListCmd.Flags().Bool("custom", false, "Only custom fields")
	jiraFieldCmd.AddCommand(jiraFieldListCmd)
	jiraCmd.AddCommand(jiraFieldCmd)
}
