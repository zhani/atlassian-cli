package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zhani/atlassian-cli/internal/config"
	"github.com/zhani/atlassian-cli/internal/jira"
)

var jiraIssueListCmd = &cobra.Command{
	Use:   "list",
	Short: "List Jira issues using filter flags",
	Long: `List Jira issues matching filter flags, which are turned into JQL.

Prefix a value with ~ to negate it (-s~Done). For --assignee and --reporter,
"me" means the current user and "x" means nobody. --created and --updated take
a period such as -7d, 2w, -12h, today, week, month or year.

The project defaults to JIRA_DEFAULT_PROJECT (-p "" searches all projects).
--resolution Unresolved lists open issues. Use --show-jql to print the
generated query without running it.`,
	Example: `  atlassian-cli jira issue list -a me -R Unresolved -p ""
  atlassian-cli jira issue list -a me -s~Done
  atlassian-cli jira issue list -p PROJ -t Bug -y High --created week
  atlassian-cli jira issue list -l backend -l~wontfix --order-by updated
  atlassian-cli jira issue list -a x -q "summary ~ crash" --plain --columns key,status,summary`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadJiraConfig()
		if err != nil {
			return err
		}
		opts := filterOptions(cmd, cfg)
		opts.Type, _ = cmd.Flags().GetString("type")
		jql, err := jira.BuildJQL(opts)
		if err != nil {
			return err
		}
		if shown, err := showJQL(cmd, jql); shown || err != nil {
			return err
		}
		return showIssues(cmd, jira.NewClient(cfg), jql)
	},
}

// addFilterFlags registers the issue filter flags shared by issue, feature,
// epic and sprint listings (all but the issue type, which differs).
func addFilterFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringP("project", "p", "", `Project key (default: JIRA_DEFAULT_PROJECT; -p "" for all projects)`)
	f.StringP("status", "s", "", "Status")
	f.StringP("assignee", "a", "", `Assignee ("me", "x" for unassigned, or a user)`)
	f.StringP("reporter", "r", "", `Reporter ("me", "x", or a user)`)
	f.StringP("priority", "y", "", "Priority")
	f.StringP("resolution", "R", "", `Resolution ("Unresolved" for none, "~Unresolved" for any)`)
	f.StringArrayP("label", "l", nil, "Label (repeatable)")
	f.BoolP("watching", "w", false, "Only issues you are watching")
	f.String("created", "", "Created since (-7d, 2w, today, week, month, year)")
	f.String("updated", "", "Updated since (-7d, 2w, today, week, month, year)")
	f.String("order-by", "created", "Field to sort by")
	f.Bool("reverse", false, "Sort ascending instead of descending")
	f.StringP("jql", "q", "", "Additional JQL, ANDed with the filters")
	f.Bool("show-jql", false, "Print the generated JQL and exit")
}

// filterOptions reads the flags registered by addFilterFlags. The project
// defaults to JIRA_DEFAULT_PROJECT.
func filterOptions(cmd *cobra.Command, cfg *config.AtlassianConfig) jira.ListOptions {
	flags := cmd.Flags()
	var o jira.ListOptions
	o.Project, _ = flags.GetString("project")
	if !flags.Changed("project") { // an explicit -p "" means all projects
		o.Project = cfg.DefaultProject
	}
	o.Status, _ = flags.GetString("status")
	o.Assignee, _ = flags.GetString("assignee")
	o.Reporter, _ = flags.GetString("reporter")
	o.Priority, _ = flags.GetString("priority")
	o.Resolution, _ = flags.GetString("resolution")
	o.Labels, _ = flags.GetStringArray("label")
	o.Watching, _ = flags.GetBool("watching")
	o.Created, _ = flags.GetString("created")
	o.Updated, _ = flags.GetString("updated")
	o.OrderBy, _ = flags.GetString("order-by")
	o.Reverse, _ = flags.GetBool("reverse")
	o.JQL, _ = flags.GetString("jql")
	return o
}

// showJQL prints jql and reports true when --show-jql was given.
func showJQL(cmd *cobra.Command, jql string) (bool, error) {
	if show, _ := cmd.Flags().GetBool("show-jql"); !show {
		return false, nil
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), jql)
	return true, err
}

func init() {
	addFilterFlags(jiraIssueListCmd)
	jiraIssueListCmd.Flags().StringP("type", "t", "", "Issue type")
	addIssueListFlags(jiraIssueListCmd)
	jiraIssueCmd.AddCommand(jiraIssueListCmd)
}
