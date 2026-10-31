package cmd

import (
	"fmt"

	"github.com/putcho01/atlassian-cli/internal/config"
	"github.com/putcho01/atlassian-cli/internal/jira"
	"github.com/spf13/cobra"
)

var jiraIssueListCmd = &cobra.Command{
	Use:   "list",
	Short: "List Jira issues using filter flags",
	Long: `List Jira issues matching filter flags, which are turned into JQL.

Prefix a value with ~ to negate it (-s~Done). For --assignee and --reporter,
"me" means the current user and "x" means nobody. --created and --updated take
a period such as -7d, 2w, -12h, today, week, month or year.

The project defaults to JIRA_DEFAULT_PROJECT. Use --show-jql to print the
generated query without running it.`,
	Example: `  atlassian-cli jira issue list -a me -s~Done
  atlassian-cli jira issue list -p PROJ -t Bug -y High --created week
  atlassian-cli jira issue list -l backend -l~wontfix --order-by updated
  atlassian-cli jira issue list -a x -q "summary ~ crash" --plain --columns key,status,summary`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadJiraConfig()
		if err != nil {
			return err
		}

		flags := cmd.Flags()
		opts := jira.ListOptions{}
		opts.Project, _ = flags.GetString("project")
		if opts.Project == "" {
			opts.Project = cfg.DefaultProject
		}
		opts.Status, _ = flags.GetString("status")
		opts.Assignee, _ = flags.GetString("assignee")
		opts.Reporter, _ = flags.GetString("reporter")
		opts.Type, _ = flags.GetString("type")
		opts.Priority, _ = flags.GetString("priority")
		opts.Labels, _ = flags.GetStringArray("label")
		opts.Watching, _ = flags.GetBool("watching")
		opts.Created, _ = flags.GetString("created")
		opts.Updated, _ = flags.GetString("updated")
		opts.OrderBy, _ = flags.GetString("order-by")
		opts.Reverse, _ = flags.GetBool("reverse")
		opts.JQL, _ = flags.GetString("jql")

		jql, err := jira.BuildJQL(opts)
		if err != nil {
			return err
		}
		if show, _ := flags.GetBool("show-jql"); show {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), jql)
			return err
		}
		return showIssues(cmd, jira.NewClient(cfg), jql)
	},
}

func init() {
	f := jiraIssueListCmd.Flags()
	f.StringP("project", "p", "", "Project key (default: JIRA_DEFAULT_PROJECT)")
	f.StringP("status", "s", "", "Status")
	f.StringP("assignee", "a", "", `Assignee ("me", "x" for unassigned, or a user)`)
	f.StringP("reporter", "r", "", `Reporter ("me", "x", or a user)`)
	f.StringP("type", "t", "", "Issue type")
	f.StringP("priority", "y", "", "Priority")
	f.StringArrayP("label", "l", nil, "Label (repeatable)")
	f.BoolP("watching", "w", false, "Only issues you are watching")
	f.String("created", "", "Created since (-7d, 2w, today, week, month, year)")
	f.String("updated", "", "Updated since (-7d, 2w, today, week, month, year)")
	f.String("order-by", "created", "Field to sort by")
	f.Bool("reverse", false, "Sort ascending instead of descending")
	f.StringP("jql", "q", "", "Additional JQL, ANDed with the filters")
	f.Bool("show-jql", false, "Print the generated JQL and exit")
	addIssueListFlags(jiraIssueListCmd)
	jiraIssueCmd.AddCommand(jiraIssueListCmd)
}
