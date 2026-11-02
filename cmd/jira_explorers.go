package cmd

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zhani/atlassian-cli/internal/bookmarks"
	"github.com/zhani/atlassian-cli/internal/config"
	"github.com/zhani/atlassian-cli/internal/jira"
	"github.com/zhani/atlassian-cli/internal/tui"
)

// explorerFields are fetched for sidebar items; issuelinks gives features
// their children without a query per feature.
var explorerFields = []string{"summary", "status", "issuetype", "assignee", "priority", "updated", "issuelinks"}

// addExplorerFlags registers the output flags of the explorer commands.
func addExplorerFlags(cmd *cobra.Command, items string) {
	cmd.Flags().Int("max-results", defaultMaxResults(), "Maximum number of "+items+" (and of issues per "+strings.TrimSuffix(items, "s")+"; default from JIRA_MAX_RESULTS)")
	cmd.Flags().BoolP("interactive", "i", false, "Launch the interactive explorer")
	cmd.Flags().Bool("plain", false, "Print plain output even when running in a terminal")
	cmd.Flags().Bool("all", false, "Include resolved "+items)
	cmd.MarkFlagsMutuallyExclusive("interactive", "plain")
}

// drillIntoEpics opens the issues of epics in the explorer's issue pane.
func drillIntoEpics(client *jira.Client, cfg *config.AtlassianConfig) func(jira.Issue) (string, bool) {
	return func(is jira.Issue) (string, bool) {
		if is.Fields.IssueType == nil || !strings.EqualFold(is.Fields.IssueType.Name, "Epic") {
			return "", false
		}
		return client.EpicChildrenJQL(is.Key, cfg.EpicLinkField), true
	}
}

func issueItem(client *jira.Client, is jira.Issue) tui.ExplorerItem {
	status := ""
	if is.Fields.Status != nil {
		status = is.Fields.Status.Name
	}
	if is.Fields.Assignee != nil {
		status += " · " + is.Fields.Assignee.DisplayName
	}
	return tui.ExplorerItem{Key: is.Key, Title: is.Fields.Summary, Status: status, URL: client.IssueURL(is.Key)}
}

// --- features ---

var jiraFeatureCmd = &cobra.Command{
	Use:     "feature",
	Aliases: []string{"features"},
	Short:   "Explore features and the issues that implement them",
}

var jiraFeatureListCmd = &cobra.Command{
	Use:   "list [feature-key]",
	Short: "List features with their linked issues",
	Long: `List features (issues of type JIRA_FEATURE_TYPE, default "New Feature") with
the issues linked to them by the link phrases in JIRA_FEATURE_LINK_TYPES
(default "is implemented by,gets values from", comma-separated). --type and
--link-type override them.

In a terminal this opens an explorer: features on the left, their linked
issues on the right with a Link column. tab switches panes; in the issue pane,
l opens the issues of an epic and h goes back. With a feature key, only that
feature's issues are shown.`,
	Example: `  atlassian-cli jira feature list
  atlassian-cli jira feature list -s "In Progress" -a me
  atlassian-cli jira feature list FEAT-12
  atlassian-cli jira feature list --plain -o csv`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadJiraConfig()
		if err != nil {
			return err
		}
		client := jira.NewClient(cfg)
		featureType, _ := cmd.Flags().GetString("type")
		if featureType == "" {
			featureType = cfg.FeatureType
		}
		if featureType == "" {
			featureType = config.DefaultFeatureType
		}
		phrases := featureLinkPhrases(cmd, cfg)
		limit, _ := cmd.Flags().GetInt("max-results")
		if len(args) == 1 {
			return showFeature(cmd, client, args[0], phrases)
		}

		opts := filterOptions(cmd, cfg)
		opts.Type = featureType
		all, _ := cmd.Flags().GetBool("all")
		opts.Unresolved = !all && opts.Resolution == "" // an explicit -R wins
		jql, err := jira.BuildJQL(opts)
		if err != nil {
			return err
		}
		if shown, err := showJQL(cmd, jql); shown || err != nil {
			return err
		}
		features, err := client.SearchIssuesAll(cmd.Context(), jql, explorerFields, limit)
		if err != nil {
			return err
		}

		items := make([]tui.ExplorerItem, len(features))
		children := make([][]string, len(features))
		for i := range features {
			keys, relation := jira.LinkedChildren(&features[i], phrases)
			children[i] = keys
			items[i] = issueItem(client, features[i])
			items[i].JQL, items[i].Annotations = jira.KeysJQL(keys), relation
		}

		if !useInteractive(cmd) {
			return printFeatures(cmd, client, items, children, limit)
		}
		browse := browserOptions(client, limit)
		browse.AnnotationTitle = "Link"
		return tui.RunExplorer(cmd.Context(), tui.ExplorerOptions{
			Title:    "Features",
			Items:    items,
			Issues:   browse,
			DrillJQL: drillIntoEpics(client, cfg),
			Kind:     bookmarks.Feature,
		})
	},
}

// featureLinkPhrases returns the link phrases from a feature to its issues:
// --link-type, JIRA_FEATURE_LINK_TYPES or the defaults.
func featureLinkPhrases(cmd *cobra.Command, cfg *config.AtlassianConfig) []string {
	phrases, _ := cmd.Flags().GetStringArray("link-type")
	if len(phrases) == 0 {
		phrases = cfg.FeatureLinkTypes
	}
	if len(phrases) == 0 {
		phrases = config.DefaultFeatureLinkTypes
	}
	return phrases
}

// showFeature shows the issues linked to one feature, in the issue browser
// or as feature/issue rows.
func showFeature(cmd *cobra.Command, client *jira.Client, key string, phrases []string) error {
	f, err := client.GetIssue(cmd.Context(), key)
	if err != nil {
		return err
	}
	keys, relation := jira.LinkedChildren(f, phrases)
	it := issueItem(client, *f)
	it.JQL, it.Annotations = jira.KeysJQL(keys), relation
	if !useInteractive(cmd) {
		limit, _ := cmd.Flags().GetInt("max-results")
		return printFeatures(cmd, client, []tui.ExplorerItem{it}, [][]string{keys}, limit)
	}
	if it.JQL == "" {
		return fmt.Errorf("%s has no issues linked as %s", it.Key, strings.Join(phrases, " / "))
	}
	return showIssuesWith(cmd, client, it.JQL, func(o *tui.Options) {
		o.Caption = it.Key + "  " + it.Title
		o.AnnotationTitle, o.Annotations = "Link", it.Annotations
	})
}

// featureRow is one feature/child pair of the plain feature listing.
type featureRow struct {
	Feature  string `json:"feature"`
	Key      string `json:"key"`
	Link     string `json:"link"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Assignee string `json:"assignee"`
	Summary  string `json:"summary"`
}

// printFeatures prints one row per feature/linked issue pair (features
// without linked issues get a single row); children[i] are the linked issue
// keys of items[i]. Linked issues are fetched with one query.
func printFeatures(cmd *cobra.Command, client *jira.Client, items []tui.ExplorerItem, children [][]string, limit int) error {
	var all []string
	for _, keys := range children {
		all = append(all, keys...)
	}
	linked := map[string]jira.Issue{}
	if jql := jira.KeysJQL(all); jql != "" {
		found, err := client.SearchIssuesAll(cmd.Context(), jql, []string{"summary", "status", "issuetype", "assignee"}, 0)
		if err != nil {
			return err
		}
		for _, is := range found {
			linked[is.Key] = is
		}
	}

	var rows []featureRow
	for i, it := range items {
		if len(children[i]) == 0 {
			rows = append(rows, featureRow{Feature: it.Key, Summary: it.Title})
			continue
		}
		for n, k := range children[i] {
			if limit > 0 && n >= limit {
				break
			}
			c := linked[k]
			r := featureRow{Feature: it.Key, Key: k, Link: it.Annotations[k], Summary: c.Fields.Summary}
			if c.Fields.IssueType != nil {
				r.Type = c.Fields.IssueType.Name
			}
			if c.Fields.Status != nil {
				r.Status = c.Fields.Status.Name
			}
			if c.Fields.Assignee != nil {
				r.Assignee = c.Fields.Assignee.DisplayName
			}
			rows = append(rows, r)
		}
	}

	table := make([][]string, len(rows))
	for i, r := range rows {
		table[i] = []string{r.Feature, r.Key, r.Link, r.Type, r.Status, r.Assignee, r.Summary}
	}
	return newFormatter(cmd).PrintRecords(rows, []string{"Feature", "Key", "Link", "Type", "Status", "Assignee", "Summary"}, table)
}

// --- epics ---

var jiraEpicCmd = &cobra.Command{
	Use:     "epic",
	Aliases: []string{"epics"},
	Short:   "Explore epics and manage their issues",
}

var jiraEpicListCmd = &cobra.Command{
	Use:   "list [epic-key]",
	Short: "List epics, or the issues of an epic",
	Long: `List epics. In a terminal this opens an explorer with epics on the left and
their issues on the right. With an epic key, the epic's issues are listed.

Issues belong to an epic through "parent" on Cloud and "Epic Link" on
Server/DC; set JIRA_EPIC_LINK_FIELD to use another JQL field.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadJiraConfig()
		if err != nil {
			return err
		}
		client := jira.NewClient(cfg)
		if len(args) == 1 {
			jql := client.EpicChildrenJQL(args[0], cfg.EpicLinkField)
			if shown, err := showJQL(cmd, jql); shown || err != nil {
				return err
			}
			return showIssuesWith(cmd, client, jql, func(o *tui.Options) { o.Caption = "Issues of " + args[0] })
		}

		opts := filterOptions(cmd, cfg)
		opts.Type = "Epic"
		all, _ := cmd.Flags().GetBool("all")
		opts.Unresolved = !all && opts.Resolution == "" // an explicit -R wins
		jql, err := jira.BuildJQL(opts)
		if err != nil {
			return err
		}
		if shown, err := showJQL(cmd, jql); shown || err != nil {
			return err
		}
		if !useInteractive(cmd) {
			return showIssues(cmd, client, jql)
		}
		limit, _ := cmd.Flags().GetInt("max-results")
		epics, err := client.SearchIssuesAll(cmd.Context(), jql, explorerFields, limit)
		if err != nil {
			return err
		}
		items := make([]tui.ExplorerItem, len(epics))
		for i, e := range epics {
			items[i] = issueItem(client, e)
			items[i].JQL = client.EpicChildrenJQL(e.Key, cfg.EpicLinkField)
		}
		return tui.RunExplorer(cmd.Context(), tui.ExplorerOptions{Title: "Epics", Items: items, Issues: browserOptions(client, limit), Kind: bookmarks.Epic})
	},
}

var jiraEpicAddCmd = &cobra.Command{
	Use:   "add <epic-key> <issue-key>...",
	Short: "Add issues to an epic",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		if err := client.AddToEpic(cmd.Context(), args[0], args[1:]); err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Added %s to %s", strings.Join(args[1:], ", "), args[0]))
	},
}

var jiraEpicRemoveCmd = &cobra.Command{
	Use:   "remove <issue-key>...",
	Short: "Remove issues from their epic",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		if err := client.AddToEpic(cmd.Context(), "", args); err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Removed %s from their epic", strings.Join(args, ", ")))
	},
}

// --- sprints ---

var jiraSprintCmd = &cobra.Command{
	Use:     "sprint",
	Aliases: []string{"sprints"},
	Short:   "Explore sprints and manage their issues",
}

// maxSprints is how many sprints the explorer lists; older closed sprints
// are dropped first.
const maxSprints = 25

var jiraSprintListCmd = &cobra.Command{
	Use:   "list [sprint-id]",
	Short: "List sprints, or the issues of a sprint",
	Long: `List the sprints of a board. In a terminal this opens an explorer with
sprints on the left and their issues on the right; elsewhere it prints the
sprints. With a sprint ID, --current, --prev or --next, the issues of that
sprint are listed (filter flags apply to them).

The board is --board, JIRA_DEFAULT_BOARD, or the only board of the project.`,
	Example: `  atlassian-cli jira sprint list
  atlassian-cli jira sprint list --current -a me
  atlassian-cli jira sprint list --prev --plain
  atlassian-cli jira sprint list 42 -s~Done`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadJiraConfig()
		if err != nil {
			return err
		}
		client := jira.NewClient(cfg)
		opts := filterOptions(cmd, cfg)
		if !cmd.Flags().Changed("project") {
			opts.Project = "" // sprints may span projects
		}
		if !cmd.Flags().Changed("order-by") {
			opts.OrderBy, opts.Reverse = "rank", true // board order
		}
		sprintJQL := func(id int) (string, error) {
			o := opts
			o.JQL = fmt.Sprintf("sprint = %d", id)
			if opts.JQL != "" {
				o.JQL += " AND (" + opts.JQL + ")"
			}
			return jira.BuildJQL(o)
		}

		if len(args) == 1 {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("sprint ID must be a number, got %q", args[0])
			}
			return showSprint(cmd, client, sprintJQL, id, "Sprint "+args[0])
		}

		board, err := resolveBoard(cmd, client, cfg)
		if err != nil {
			return err
		}
		pick := ""
		for _, f := range []string{"current", "prev", "next"} {
			if v, _ := cmd.Flags().GetBool(f); v {
				pick = f
			}
		}
		states := []string{"active", "closed", "future"}
		if s, _ := cmd.Flags().GetStringSlice("state"); len(s) > 0 && pick == "" {
			states = s
		}
		sprints, err := client.ListSprints(cmd.Context(), board, states...)
		if err != nil {
			return err
		}

		if pick != "" {
			s, err := pickSprint(sprints, pick)
			if err != nil {
				return err
			}
			return showSprint(cmd, client, sprintJQL, s.ID, s.Name)
		}

		if len(sprints) > maxSprints {
			sprints = sprints[len(sprints)-maxSprints:]
		}
		if !useInteractive(cmd) {
			rows := make([][]string, len(sprints))
			for i, s := range sprints {
				rows[i] = []string{strconv.Itoa(s.ID), s.Name, s.State, shortDate(s.StartDate), shortDate(s.EndDate)}
			}
			return newFormatter(cmd).PrintRecords(sprints, []string{"ID", "Name", "State", "Start", "End"}, rows)
		}

		items := make([]tui.ExplorerItem, len(sprints))
		selected := 0
		for i, s := range sprints {
			jql, err := sprintJQL(s.ID)
			if err != nil {
				return err
			}
			status := s.State
			if s.StartDate != "" {
				status += " · " + shortDate(s.StartDate) + " – " + shortDate(s.EndDate)
			}
			items[i] = tui.ExplorerItem{Key: strconv.Itoa(s.ID), Title: s.Name, Status: status, JQL: jql}
			if s.State == "active" && selected == 0 {
				selected = i // start at the active sprint
			}
		}
		limit, _ := cmd.Flags().GetInt("max-results")
		return tui.RunExplorer(cmd.Context(), tui.ExplorerOptions{
			Title:    "Sprints",
			Items:    items,
			Selected: selected,
			Issues:   browserOptions(client, limit),
			DrillJQL: drillIntoEpics(client, cfg),
			Kind:     bookmarks.Sprint,
		})
	},
}

func showSprint(cmd *cobra.Command, client *jira.Client, sprintJQL func(int) (string, error), id int, name string) error {
	jql, err := sprintJQL(id)
	if err != nil {
		return err
	}
	if shown, err := showJQL(cmd, jql); shown || err != nil {
		return err
	}
	return showIssuesWith(cmd, client, jql, func(o *tui.Options) { o.Caption = name })
}

// pickSprint selects the active ("current"), last closed ("prev") or first
// future ("next") sprint; sprints are ordered closed, active, future.
func pickSprint(sprints []jira.Sprint, which string) (jira.Sprint, error) {
	var found []jira.Sprint
	state := map[string]string{"current": "active", "prev": "closed", "next": "future"}[which]
	for _, s := range sprints {
		if s.State == state {
			found = append(found, s)
		}
	}
	switch {
	case len(found) == 0:
		return jira.Sprint{}, fmt.Errorf("no %s sprint on this board", state)
	case which == "prev":
		return found[len(found)-1], nil
	default:
		return found[0], nil
	}
}

// resolveBoard picks the board from --board, JIRA_DEFAULT_BOARD, or the
// project's only Scrum board.
func resolveBoard(cmd *cobra.Command, client *jira.Client, cfg *config.AtlassianConfig) (int, error) {
	ref, _ := cmd.Flags().GetString("board")
	if ref == "" {
		ref = cfg.DefaultBoard
	}
	if ref != "" {
		id, err := strconv.Atoi(ref)
		if err != nil {
			return 0, fmt.Errorf("board must be a numeric ID, got %q", ref)
		}
		return id, nil
	}
	project, _ := cmd.Flags().GetString("project")
	if project == "" {
		project = cfg.DefaultProject
	}
	boards, err := client.ListBoards(cmd.Context(), project)
	if err != nil {
		return 0, err
	}
	var scrum []jira.Board
	for _, b := range boards {
		if b.Type == "scrum" {
			scrum = append(scrum, b)
		}
	}
	switch len(scrum) {
	case 1:
		return scrum[0].ID, nil
	case 0:
		return 0, errors.New("no Scrum board found; pass --board or set JIRA_DEFAULT_BOARD")
	}
	names := make([]string, len(scrum))
	for i, b := range scrum {
		names[i] = fmt.Sprintf("%d (%s)", b.ID, b.Name)
	}
	return 0, fmt.Errorf("several boards found, pass --board or set JIRA_DEFAULT_BOARD: %s", strings.Join(names, ", "))
}

func shortDate(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

var jiraSprintAddCmd = &cobra.Command{
	Use:   "add <sprint-id> <issue-key>...",
	Short: "Move issues into a sprint",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("sprint ID must be a number, got %q", args[0])
		}
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		if err := client.AddToSprint(cmd.Context(), id, args[1:]); err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Moved %s to sprint %d", strings.Join(args[1:], ", "), id))
	},
}

func init() {
	addFilterFlags(jiraFeatureListCmd)
	jiraFeatureListCmd.Flags().StringP("type", "t", "", `Feature issue type (default: JIRA_FEATURE_TYPE or "New Feature")`)
	jiraFeatureListCmd.Flags().StringArray("link-type", nil, "Link phrase from a feature to its issues (repeatable; default: JIRA_FEATURE_LINK_TYPES)")
	addExplorerFlags(jiraFeatureListCmd, "features")
	jiraFeatureCmd.AddCommand(jiraFeatureListCmd)

	addFilterFlags(jiraEpicListCmd)
	addExplorerFlags(jiraEpicListCmd, "epics")
	addColumnFlags(jiraEpicListCmd)
	jiraEpicCmd.AddCommand(jiraEpicListCmd, jiraEpicAddCmd, jiraEpicRemoveCmd)

	addFilterFlags(jiraSprintListCmd)
	addExplorerFlags(jiraSprintListCmd, "sprints")
	addColumnFlags(jiraSprintListCmd)
	f := jiraSprintListCmd.Flags()
	f.String("board", "", "Board ID (default: JIRA_DEFAULT_BOARD or the project's only Scrum board)")
	f.Bool("current", false, "Issues of the active sprint")
	f.Bool("prev", false, "Issues of the last closed sprint")
	f.Bool("next", false, "Issues of the next planned sprint")
	f.StringSlice("state", nil, "Sprint states to list: active, closed, future (default: all)")
	jiraSprintListCmd.MarkFlagsMutuallyExclusive("current", "prev", "next")
	jiraSprintCmd.AddCommand(jiraSprintListCmd, jiraSprintAddCmd)

	jiraCmd.AddCommand(jiraFeatureCmd, jiraEpicCmd, jiraSprintCmd)
}
