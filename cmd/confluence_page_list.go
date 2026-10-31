package cmd

import (
	"github.com/spf13/cobra"
)

var confluencePageListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the pages of a space",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		space, _ := cmd.Flags().GetString("space")
		limit, _ := cmd.Flags().GetInt("limit")
		pages, err := client.ListPages(cmd.Context(), space, limit)
		if err != nil {
			return err
		}
		return newFormatter(cmd).PrintPageList(pages)
	},
}

var confluencePageChildrenCmd = &cobra.Command{
	Use:   "children <page-id>",
	Short: "List the child pages of a page",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		limit, _ := cmd.Flags().GetInt("limit")
		pages, err := client.GetChildren(cmd.Context(), args[0], limit)
		if err != nil {
			return err
		}
		return newFormatter(cmd).PrintPageList(pages)
	},
}

var confluencePageVersionsCmd = &cobra.Command{
	Use:   "versions <page-id>",
	Short: "Show the version history of a page",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		limit, _ := cmd.Flags().GetInt("limit")
		versions, err := client.GetVersions(cmd.Context(), args[0], limit)
		if err != nil {
			return err
		}
		return newFormatter(cmd).PrintVersions(versions)
	},
}

var confluenceSpaceCmd = &cobra.Command{
	Use:   "space",
	Short: "Confluence space commands",
}

var confluenceSpaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List spaces",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		limit, _ := cmd.Flags().GetInt("limit")
		spaces, err := client.ListSpaces(cmd.Context(), limit)
		if err != nil {
			return err
		}
		return newFormatter(cmd).PrintSpaces(spaces)
	},
}

func init() {
	confluencePageListCmd.Flags().String("space", "", "Space key (required)")
	_ = confluencePageListCmd.MarkFlagRequired("space")
	for _, c := range []*cobra.Command{confluencePageListCmd, confluencePageChildrenCmd, confluencePageVersionsCmd, confluenceSpaceListCmd} {
		c.Flags().Int("limit", 100, "Maximum number of results (0 for all)")
	}
	confluencePageCmd.AddCommand(confluencePageListCmd, confluencePageChildrenCmd, confluencePageVersionsCmd)
	confluenceSpaceCmd.AddCommand(confluenceSpaceListCmd)
	confluenceCmd.AddCommand(confluenceSpaceCmd)
}
