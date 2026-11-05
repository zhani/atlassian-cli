// Package cmd implements the atlassian-cli command-line interface.
//
// The command tree is organised as follows:
//
//	atlassian-cli
//	├── jira
//	│   ├── issue
//	│   │   ├── get        – fetch a single issue by key
//	│   │   ├── list       – list issues using filter flags
//	│   │   ├── search     – search issues with JQL
//	│   │   ├── create     – create a new issue
//	│   │   ├── update     – update issue fields
//	│   │   ├── delete     – delete an issue
//	│   │   ├── transition – move an issue to a new status
//	│   │   ├── subtasks   – list subtasks of an issue
//	│   │   ├── open       – open an issue in the browser
//	│   │   ├── assign     – assign an issue
//	│   │   ├── clone      – clone an issue
//	│   │   ├── link       – link issues (remote, types)
//	│   │   ├── unlink     – remove links between issues
//	│   │   ├── worklog    – log time
//	│   │   ├── development – linked pull requests and branches
//	│   │   ├── attachment – list, get, download, upload, delete
//	│   │   └── comment
//	│   │       ├── list   – list comments on an issue
//	│   │       └── add    – add a comment to an issue
//	│   ├── feature list   – explore features and their linked issues
//	│   ├── epic           – list (explore), add, remove
//	│   ├── sprint         – list (explore), add
//	│   ├── user search    – search users
//	│   ├── field list     – list fields and their IDs
//	│   ├── api            – raw REST request
//	│   └── myself         – show the authenticated user
//	├── confluence (alias: conf)
//	│   ├── search         – search content (CQL or --text/--space)
//	│   ├── browse         – interactive page tree
//	│   ├── api            – raw REST request
//	│   ├── space list     – list spaces
//	│   ├── page
//	│   │   ├── get        – fetch a page by ID
//	│   │   ├── create     – create a new page (Markdown)
//	│   │   ├── update     – update an existing page (Markdown)
//	│   │   ├── edit       – edit a page as Markdown in $EDITOR
//	│   │   ├── list       – list the pages of a space
//	│   │   ├── children   – list child pages
//	│   │   ├── versions   – show version history
//	│   │   ├── copy       – copy a page
//	│   │   └── move       – move a page under another parent
//	│   ├── attachment     – list, download, upload, delete
//	│   ├── label
//	│   │   ├── list       – list labels on a page
//	│   │   ├── add        – add labels to a page
//	│   │   └── remove     – remove a label from a page
//	│   └── restriction
//	│       ├── list       – list restrictions on a page
//	│       ├── add        – add a read/update restriction
//	│       └── remove     – remove a restriction
//	├── mirror             – export, import, status, serve (offline mirror)
//	├── mcp-server         – start an MCP server over stdio
//	└── version            – print version information
package cmd

import (
	"github.com/spf13/cobra"
)

var version = "0.4.5"

var rootCmd = &cobra.Command{
	Use:   "atlassian-cli",
	Short: "A CLI and MCP server for Atlassian Jira and Confluence",
	Long:  "A lightweight, native Go CLI for self-hosted Atlassian Jira and Confluence Server/Data Center instances.",
	// Runs after flags and arguments were validated: from here on, errors
	// come from the request, and printing the usage would only hide them.
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		cmd.SilenceUsage = true
	},
}

func init() {
	rootCmd.PersistentFlags().StringP("output", "o", "table", "Output format: table, json, markdown, csv (csv: list outputs only)")
}

func Execute() error {
	return rootCmd.Execute()
}
