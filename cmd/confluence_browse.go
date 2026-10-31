package cmd

import (
	"errors"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/putcho01/atlassian-cli/internal/browser"
	"github.com/putcho01/atlassian-cli/internal/tui"
	"github.com/spf13/cobra"
)

var confluenceBrowseCmd = &cobra.Command{
	Use:   "browse [space-key]",
	Short: "Browse spaces and page trees interactively",
	Long: `Browse Confluence in the terminal: a page tree on the left (spaces, then
pages, expanded on demand) and the selected page rendered as Markdown on the
right. Without a space key, all spaces are listed.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !isatty.IsTerminal(os.Stdout.Fd()) || !isatty.IsTerminal(os.Stdin.Fd()) {
			return errors.New("browse needs a terminal; use `confluence page list` or `confluence page children` in scripts")
		}
		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		opts := tui.TreeOptions{Source: client, OpenURL: browser.Open, Copy: tui.CopyToClipboard}
		if len(args) == 1 {
			opts.Space = args[0]
		}
		return tui.RunPageTree(cmd.Context(), opts)
	},
}

func init() {
	confluenceCmd.AddCommand(confluenceBrowseCmd)
}
