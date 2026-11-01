package cmd

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/putcho01/atlassian-cli/internal/bookmarks"
	"github.com/putcho01/atlassian-cli/internal/browser"
	"github.com/putcho01/atlassian-cli/internal/config"
	"github.com/putcho01/atlassian-cli/internal/jira"
	"github.com/putcho01/atlassian-cli/internal/tui"
	"github.com/spf13/cobra"
)

var bookmarkCmd = &cobra.Command{
	Use:     "bookmark",
	Aliases: []string{"bookmarks", "bm"},
	Short:   "Bookmark issues, queries, features, epics, sprints and pages",
	Long: `Bookmarks give quick access to the work you return to: Jira issues, saved
JQL queries, features, epics, sprints and Confluence pages.

Without a subcommand, a terminal shows the bookmark browser: enter opens a
bookmark in the matching view (issue view, issue list, feature/epic/sprint
issues or the page tree) and quitting that view returns to the bookmarks.
Elsewhere the bookmarks are listed.

Add bookmarks with ` + "`bookmark add`" + `, or press B on an issue, a feature/epic/sprint
in the explorers or a page in ` + "`confluence browse`" + `; S in an issue list
bookmarks its query. They are kept in ATLASSIAN_BOOKMARKS or
~/.config/atlassian-cli/bookmarks.yaml (` + "`bookmark edit`" + ` opens it in $EDITOR).

Bookmarks are referred to by number (as listed), name, key or ID.`,
	Example: `  atlassian-cli bookmark
  atlassian-cli bookmark add PROJ-123
  atlassian-cli bookmark add query "assignee = currentUser() AND resolution IS EMPTY" --name "My open work"
  atlassian-cli bookmark add feature PROJ-7
  atlassian-cli bookmark add page 123456
  atlassian-cli bookmark open "My open work"
  atlassian-cli bookmark open 2 --web`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := bookmarks.Open()
		if err != nil {
			return err
		}
		if !useInteractive(cmd) {
			return printBookmarks(cmd, store, "")
		}
		return browseBookmarks(cmd, store)
	},
}

// browseBookmarks runs the bookmark browser and opens the chosen bookmarks
// until the user quits it.
func browseBookmarks(cmd *cobra.Command, store *bookmarks.Store) error {
	opts := tui.BookmarkOptions{Store: store, OpenURL: browser.Open, Copy: tui.CopyToClipboard}
	for {
		b, err := tui.RunBookmarks(cmd.Context(), opts)
		if err != nil || b == nil {
			return err
		}
		opts.Selected, opts.Message, opts.MessageErr = b.ID(), "", false
		if err := openBookmark(cmd, *b); err != nil {
			opts.Message, opts.MessageErr = b.Label()+": "+err.Error(), true
		}
	}
}

var bookmarkListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List bookmarks",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := bookmarks.Open()
		if err != nil {
			return err
		}
		kind, _ := cmd.Flags().GetString("kind")
		return printBookmarks(cmd, store, kind)
	},
}

func printBookmarks(cmd *cobra.Command, store *bookmarks.Store, kind string) error {
	list, err := store.List()
	if err != nil {
		return err
	}
	var want bookmarks.Kind
	if kind != "" {
		if want, err = bookmarks.ParseKind(kind); err != nil {
			return err
		}
	}
	type row struct {
		N int `json:"n"`
		bookmarks.Bookmark
	}
	var out []row
	var rows [][]string
	for i, b := range list {
		if want != "" && b.Kind != want {
			continue
		}
		out = append(out, row{i + 1, b})
		rows = append(rows, []string{strconv.Itoa(i + 1), string(b.Kind), b.Target(), b.DisplayName()})
	}
	return newFormatter(cmd).PrintRecords(out, []string{"#", "Kind", "Key / query", "Name"}, rows)
}

var issueKeyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*-\d+$`)

var bookmarkAddCmd = &cobra.Command{
	Use:   "add [kind] <key|id|jql>",
	Short: "Bookmark an issue, query, feature, epic, sprint or page",
	Long: `Bookmark an item. kind is issue (the default for an issue key), query
(JQL), feature, epic, sprint (ID) or page (Confluence page ID). The item is
looked up to store its title and link, unless --no-check is given.
Bookmarking an item again replaces its bookmark.`,
	Example: `  atlassian-cli bookmark add PROJ-123
  atlassian-cli bookmark add epic PROJ-50 --name "Payments epic"
  atlassian-cli bookmark add query "project = PROJ AND sprint in openSprints()" --name Sprint
  atlassian-cli bookmark add sprint 42
  atlassian-cli bookmark add page 123456`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var b bookmarks.Bookmark
		if len(args) == 1 {
			if !issueKeyRe.MatchString(args[0]) {
				return fmt.Errorf("%q is not an issue key; give the kind: bookmark add <issue|query|feature|epic|sprint|page> %s", args[0], args[0])
			}
			b = bookmarks.Bookmark{Kind: bookmarks.Issue, Key: args[0]}
		} else {
			kind, err := bookmarks.ParseKind(args[0])
			if err != nil {
				return err
			}
			b = bookmarks.Bookmark{Kind: kind, Key: args[1]}
			if kind == bookmarks.Query {
				b.Key, b.JQL = "", args[1]
			}
		}
		b.Name, _ = cmd.Flags().GetString("name")
		if b.Kind == bookmarks.Query && b.Name == "" {
			b.Name = b.JQL
		}
		if noCheck, _ := cmd.Flags().GetBool("no-check"); !noCheck {
			if err := describeBookmark(cmd, &b); err != nil {
				return err
			}
		}
		store, err := bookmarks.Open()
		if err != nil {
			return err
		}
		if err := store.Add(b); err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Bookmarked %s %s", b.Kind, b.Label()))
	},
}

// describeBookmark looks the item up (see bookmarks.Describe).
func describeBookmark(cmd *cobra.Command, b *bookmarks.Bookmark) error {
	return bookmarks.Describe(cmd.Context(), b,
		func() (bookmarks.JiraLookup, error) { return newJiraClient() },
		func() (bookmarks.PageLookup, error) { return newConfluenceClient() })
}

var bookmarkRemoveCmd = &cobra.Command{
	Use:     "remove <bookmark>...",
	Aliases: []string{"rm", "delete"},
	Short:   "Remove bookmarks",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := bookmarks.Open()
		if err != nil {
			return err
		}
		removed, err := store.Remove(args...)
		if err != nil {
			return err
		}
		names := make([]string, len(removed))
		for i, b := range removed {
			names[i] = b.Label()
		}
		return newFormatter(cmd).PrintMessage("Removed " + strings.Join(names, ", "))
	},
}

var bookmarkRenameCmd = &cobra.Command{
	Use:   "rename <bookmark> <name>",
	Short: "Rename a bookmark",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := bookmarks.Open()
		if err != nil {
			return err
		}
		b, err := store.Rename(args[0], args[1])
		if err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage("Renamed to " + b.Label())
	},
}

var bookmarkEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit the bookmark file in $EDITOR",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := bookmarks.Open()
		if err != nil {
			return err
		}
		text, err := store.Raw()
		if err != nil {
			return err
		}
		edited, err := editText(".yaml", text)
		if err != nil {
			return err
		}
		if err := store.SaveRaw(edited); err != nil {
			return fmt.Errorf("not saved: %w", err)
		}
		return newFormatter(cmd).PrintMessage("Saved " + store.Path)
	},
}

var bookmarkOpenCmd = &cobra.Command{
	Use:   "open <bookmark>",
	Short: "Open a bookmark",
	Long: `Open a bookmark in its view: an issue in the issue view, a query in the
issue browser, a feature, epic or sprint as its issues, a page in the page
tree. --web opens it in the web browser instead. Without a terminal (or with
-o/--plain), the issue, issues or page are printed.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := bookmarks.Open()
		if err != nil {
			return err
		}
		b, _, err := store.Find(args[0])
		if err != nil {
			return err
		}
		if web, _ := cmd.Flags().GetBool("web"); web {
			if b.URL == "" {
				if err := describeBookmark(cmd, &b); err != nil {
					return err
				}
			}
			if b.URL == "" {
				return fmt.Errorf("%s has no web page", b.Label())
			}
			return browser.Open(b.URL)
		}
		return openBookmark(cmd, b)
	},
}

// openBookmark shows a bookmark in its view (or prints it without a
// terminal).
func openBookmark(cmd *cobra.Command, b bookmarks.Bookmark) error {
	ctx := cmd.Context()
	if b.Kind == bookmarks.Page {
		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		if !useInteractive(cmd) {
			p, err := client.GetPage(ctx, b.Key, nil)
			if err != nil {
				return err
			}
			return newFormatter(cmd).PrintPage(p)
		}
		opts := tui.TreeOptions{Source: client, PageID: b.Key, Space: b.Space, OpenURL: browser.Open, Copy: tui.CopyToClipboard}
		if store, err := bookmarks.Open(); err == nil {
			opts.Bookmarks = store
		}
		return tui.RunPageTree(ctx, opts)
	}

	cfg, err := config.LoadJiraConfig()
	if err != nil {
		return err
	}
	client := jira.NewClient(cfg)
	caption := b.Label()
	switch b.Kind {
	case bookmarks.Issue:
		if !useInteractive(cmd) {
			is, err := client.GetIssue(ctx, b.Key)
			if err != nil {
				return err
			}
			return newFormatter(cmd).PrintIssue(is)
		}
		return showIssuesWith(cmd, client, fmt.Sprintf("key = %q", b.Key), func(o *tui.Options) {
			o.Caption, o.OpenDetail = caption, b.Key
		})
	case bookmarks.Query:
		return showIssuesWith(cmd, client, b.JQL, func(o *tui.Options) { o.Caption = caption })
	case bookmarks.Feature:
		return showFeature(cmd, client, b.Key, featureLinkPhrases(cmd, cfg))
	case bookmarks.Epic:
		jql := client.EpicChildrenJQL(b.Key, cfg.EpicLinkField)
		return showIssuesWith(cmd, client, jql, func(o *tui.Options) { o.Caption = "Issues of " + caption })
	case bookmarks.Sprint:
		id, err := strconv.Atoi(b.Key)
		if err != nil {
			return fmt.Errorf("sprint ID must be a number, got %q", b.Key)
		}
		jql := fmt.Sprintf("sprint = %d ORDER BY rank ASC", id)
		return showIssuesWith(cmd, client, jql, func(o *tui.Options) { o.Caption = caption })
	}
	return errors.New("unknown bookmark kind " + string(b.Kind))
}

var bookmarkPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the path of the bookmark file",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := bookmarks.DefaultPath()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), p)
		return err
	},
}

func init() {
	addIssueListFlags(bookmarkCmd)
	bookmarkListCmd.Flags().String("kind", "", "Only bookmarks of this kind (issue, query, feature, epic, sprint, page)")
	bookmarkAddCmd.Flags().String("name", "", "Name shown in lists (required for queries; default: key and title)")
	bookmarkAddCmd.Flags().Bool("no-check", false, "Do not look the item up (no title or link is stored)")
	addIssueListFlags(bookmarkOpenCmd)
	bookmarkOpenCmd.Flags().Bool("web", false, "Open in the web browser")
	bookmarkCmd.AddCommand(bookmarkListCmd, bookmarkAddCmd, bookmarkRemoveCmd, bookmarkRenameCmd, bookmarkEditCmd, bookmarkOpenCmd, bookmarkPathCmd)
	rootCmd.AddCommand(bookmarkCmd)
}
