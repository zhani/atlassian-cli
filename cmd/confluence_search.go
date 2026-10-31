package cmd

import (
	"errors"
	"fmt"

	"github.com/putcho01/atlassian-cli/internal/confluence"
	"github.com/spf13/cobra"
)

var confluenceSearchCmd = &cobra.Command{
	Use:   "search [cql]",
	Short: "Search Confluence content",
	Long: `Search Confluence content with a CQL query, or build one from --text,
--space and --type.`,
	Example: `  atlassian-cli confluence search --text "release notes" --space DEV
  atlassian-cli confluence search 'space = DEV AND label = "howto"'
  atlassian-cli confluence search --space DEV --type blogpost --limit 10`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		text, _ := cmd.Flags().GetString("text")
		space, _ := cmd.Flags().GetString("space")
		typ, _ := cmd.Flags().GetString("type")
		limit, _ := cmd.Flags().GetInt("limit")

		var cql string
		switch {
		case len(args) == 1:
			if text != "" || space != "" || cmd.Flags().Changed("type") {
				return errors.New("pass either a CQL query or --text/--space/--type, not both")
			}
			cql = args[0]
		case text == "" && space == "":
			return errors.New("a CQL query, --text or --space is required")
		default:
			cql = confluence.BuildCQL(confluence.SearchOptions{Text: text, Space: space, Type: typ})
		}
		if show, _ := cmd.Flags().GetBool("show-cql"); show {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), cql)
			return err
		}

		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		pages, err := client.SearchCQL(cmd.Context(), cql, limit)
		if err != nil {
			return err
		}
		return newFormatter(cmd).PrintPageList(pages)
	},
}

func init() {
	confluenceSearchCmd.Flags().String("text", "", "Full-text search")
	confluenceSearchCmd.Flags().String("space", "", "Space key")
	confluenceSearchCmd.Flags().String("type", "page", "Content type: page, blogpost or all")
	confluenceSearchCmd.Flags().Int("limit", 25, "Maximum number of results")
	confluenceSearchCmd.Flags().Bool("show-cql", false, "Print the CQL query and exit")
	confluenceCmd.AddCommand(confluenceSearchCmd)
}
