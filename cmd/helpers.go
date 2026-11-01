package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/zhani/atlassian-cli/internal/config"
	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/formatter"
	"github.com/zhani/atlassian-cli/internal/jira"
)

func newJiraClient() (*jira.Client, error) {
	cfg, err := config.LoadJiraConfig()
	if err != nil {
		return nil, err
	}
	return jira.NewClient(cfg), nil
}

func newConfluenceClient() (*confluence.Client, error) {
	cfg, err := config.LoadConfluenceConfig()
	if err != nil {
		return nil, err
	}
	return confluence.NewClient(cfg), nil
}

func newFormatter(cmd *cobra.Command) *formatter.Formatter {
	format, _ := cmd.Flags().GetString("output")
	return formatter.New(formatter.ParseFormat(format), os.Stdout)
}

// isJSONOutput reports whether JSON output was requested.
func isJSONOutput(cmd *cobra.Command) bool {
	format, _ := cmd.Flags().GetString("output")
	return formatter.ParseFormat(format) == formatter.JSON
}
