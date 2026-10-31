package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var jiraIssueAttachmentUploadCmd = &cobra.Command{
	Use:   "upload <issue-key> <file>...",
	Short: "Attach files to a Jira issue",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		for _, path := range args[1:] {
			if err := withFile(path, func(f *os.File) error {
				_, err := client.UploadAttachment(cmd.Context(), args[0], filepath.Base(path), f)
				return err
			}); err != nil {
				return fmt.Errorf("upload %s: %w", path, err)
			}
			if err := newFormatter(cmd).PrintMessage(fmt.Sprintf("Attached %s to %s", filepath.Base(path), args[0])); err != nil {
				return err
			}
		}
		return nil
	},
}

var jiraIssueAttachmentDeleteCmd = &cobra.Command{
	Use:   "delete <issue-key> <filename-or-id>",
	Short: "Delete an attachment from a Jira issue",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		att, err := client.FindAttachment(cmd.Context(), args[0], args[1])
		if err != nil {
			return err
		}
		if err := client.DeleteAttachment(cmd.Context(), att.ID); err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Deleted %s (%s) from %s", att.Filename, att.ID, args[0]))
	},
}

var confluenceAttachmentUploadCmd = &cobra.Command{
	Use:   "upload <page-id> <file>...",
	Short: "Attach files to a Confluence page",
	Long:  "Attach files to a Confluence page. A file with the name of an existing\nattachment is uploaded as a new version of it.",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		for _, path := range args[1:] {
			if err := withFile(path, func(f *os.File) error {
				_, err := client.UploadAttachment(cmd.Context(), args[0], filepath.Base(path), f)
				return err
			}); err != nil {
				return fmt.Errorf("upload %s: %w", path, err)
			}
			if err := newFormatter(cmd).PrintMessage(fmt.Sprintf("Attached %s to page %s", filepath.Base(path), args[0])); err != nil {
				return err
			}
		}
		return nil
	},
}

var confluenceAttachmentDeleteCmd = &cobra.Command{
	Use:   "delete <page-id> <title-or-id>",
	Short: "Delete an attachment from a Confluence page",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		att, err := client.FindAttachment(cmd.Context(), args[0], args[1])
		if err != nil {
			return err
		}
		if err := client.DeleteAttachment(cmd.Context(), att.ID); err != nil {
			return err
		}
		return newFormatter(cmd).PrintMessage(fmt.Sprintf("Deleted %s (%s) from page %s", att.Title, att.ID, args[0]))
	},
}

func withFile(path string, fn func(*os.File) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return fn(f)
}

func init() {
	jiraIssueAttachmentCmd.AddCommand(jiraIssueAttachmentUploadCmd, jiraIssueAttachmentDeleteCmd)
	confluenceAttachmentCmd.AddCommand(confluenceAttachmentUploadCmd, confluenceAttachmentDeleteCmd)
}
