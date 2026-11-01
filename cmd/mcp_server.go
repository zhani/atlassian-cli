package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/zhani/atlassian-cli/internal/mcptools"
)

var mcpServerCmd = &cobra.Command{
	Use:   "mcp-server",
	Short: "Start MCP server (stdio JSON-RPC)",
	Long:  "Start an MCP server that communicates via stdio using JSON-RPC. This allows AI assistants to interact with Jira and Confluence.",
	RunE: func(cmd *cobra.Command, args []string) error {
		toolGroups, _ := cmd.Flags().GetStringSlice("tools")
		return mcptools.Run(cmd.Context(), version, toolGroups)
	},
}

func init() {
	mcpServerCmd.Flags().StringSlice("tools", nil, "Tool groups to enable (default: all except "+strings.Join(mcptools.OptInGroups(), ", ")+"). Available: "+availableGroups())
	rootCmd.AddCommand(mcpServerCmd)
}

func availableGroups() string {
	return strings.Join(mcptools.AllGroups(), ", ")
}
