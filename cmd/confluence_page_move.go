package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var confluencePageCopyCmd = &cobra.Command{
	Use:   "copy <page-id>",
	Short: "Copy a Confluence page",
	Long: `Create a new page with the content of an existing one. By default the copy
is titled "Copy of <title>" and placed next to the original. Attachments,
labels and child pages are not copied.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		title, _ := cmd.Flags().GetString("title")
		space, _ := cmd.Flags().GetString("space")
		parent, _ := cmd.Flags().GetString("parent")
		page, err := client.CopyPage(cmd.Context(), args[0], title, space, parent)
		if err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Copied page %s to %q (%s)", args[0], page.Title, page.ID))
	},
}

var confluencePageMoveCmd = &cobra.Command{
	Use:   "move <page-id> <new-parent-id>",
	Short: "Move a Confluence page under another parent",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		page, err := client.MovePage(cmd.Context(), args[0], args[1])
		if err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Moved %q under page %s", page.Title, args[1]))
	},
}

func init() {
	confluencePageCopyCmd.Flags().String("title", "", `Title of the copy (default: "Copy of <title>")`)
	confluencePageCopyCmd.Flags().String("space", "", "Target space key (default: same space)")
	confluencePageCopyCmd.Flags().String("parent", "", "Parent page ID of the copy (default: the original's parent)")
	confluencePageCmd.AddCommand(confluencePageCopyCmd, confluencePageMoveCmd)
}
