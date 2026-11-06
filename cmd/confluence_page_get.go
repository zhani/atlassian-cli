package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/htmlconv"
)

var confluencePageGetCmd = &cobra.Command{
	Use:   "get <page-id>",
	Short: "Get a Confluence page",
	Example: `  atlassian-cli confluence page get 12345
  atlassian-cli confluence page get 12345 --content-only > page.md        # body as Markdown
  atlassian-cli confluence page get 12345 --content-only --raw > page.xml # body as storage XHTML`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		page, err := client.GetPage(cmd.Context(), args[0], nil)
		if err != nil {
			return err
		}

		contentOnly, _ := cmd.Flags().GetBool("content-only")
		raw, _ := cmd.Flags().GetBool("raw")
		if !contentOnly && !raw {
			return newFormatter(cmd).PrintPage(page)
		}
		storage := ""
		if page.Body.Storage != nil {
			storage = page.Body.Storage.Value
		}
		if raw {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), storage)
		} else {
			users := confluence.ResolveMentions(cmd.Context(), client, storage)
			_, err = fmt.Fprintln(cmd.OutOrStdout(), htmlconv.ConvertWith(storage, users.Options()))
		}
		return err
	},
}

func init() {
	confluencePageGetCmd.Flags().Bool("content-only", false, "Print only the page body, as Markdown")
	confluencePageGetCmd.Flags().Bool("raw", false, "Print only the page body, as storage format (XHTML)")
	confluencePageCmd.AddCommand(confluencePageGetCmd)
}
