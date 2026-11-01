package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/zhani/atlassian-cli/internal/jira"
)

var jiraIssueAttachmentCmd = &cobra.Command{
	Use:   "attachment",
	Short: "Manage attachments on a Jira issue",
}

var jiraIssueAttachmentListCmd = &cobra.Command{
	Use:   "list <issue-key>",
	Short: "List attachments on a Jira issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		attachments, err := client.GetAttachments(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return newFormatter(cmd).PrintAttachments(attachments)
	},
}

var jiraIssueAttachmentGetCmd = &cobra.Command{
	Use:   "get <attachment-id>",
	Short: "Show metadata for a single Jira attachment",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		attachment, err := client.GetAttachment(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return newFormatter(cmd).PrintAttachment(attachment)
	},
}

var jiraIssueAttachmentDownloadCmd = &cobra.Command{
	Use:   "download <issue-key> <filename-or-id>",
	Short: "Download an attachment from a Jira issue",
	Long: "Download an attachment from a Jira issue. The second argument matches an\n" +
		"attachment by filename or ID. Use --file to set the output path (default:\n" +
		"the attachment's filename), or --file - to write to stdout.",
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		issueKey, ref := args[0], args[1]
		outFile, _ := cmd.Flags().GetString("file")

		client, err := newJiraClient()
		if err != nil {
			return err
		}
		attachments, err := client.GetAttachments(cmd.Context(), issueKey)
		if err != nil {
			return err
		}

		var match *jira.Attachment
		for i := range attachments {
			if attachments[i].ID == ref || attachments[i].Filename == ref {
				match = &attachments[i]
				break
			}
		}
		if match == nil {
			return fmt.Errorf("no attachment matching %q found on %s", ref, issueKey)
		}

		body, err := client.DownloadAttachment(cmd.Context(), match.Content)
		if err != nil {
			return err
		}
		defer body.Close()

		if outFile == "" {
			outFile = match.Filename
		}

		var dst io.Writer
		if outFile == "-" {
			dst = cmd.OutOrStdout()
		} else {
			f, err := os.Create(outFile)
			if err != nil {
				return err
			}
			defer f.Close()
			dst = f
		}

		if _, err := io.Copy(dst, body); err != nil {
			return err
		}
		if outFile != "-" {
			return newFormatter(cmd).PrintMessage(fmt.Sprintf("Downloaded %s -> %s", match.Filename, outFile))
		}
		return nil
	},
}

func init() {
	jiraIssueAttachmentDownloadCmd.Flags().String("file", "", "Output file path (default: attachment filename, - for stdout)")
	jiraIssueAttachmentCmd.AddCommand(jiraIssueAttachmentListCmd)
	jiraIssueAttachmentCmd.AddCommand(jiraIssueAttachmentGetCmd)
	jiraIssueAttachmentCmd.AddCommand(jiraIssueAttachmentDownloadCmd)
	jiraIssueCmd.AddCommand(jiraIssueAttachmentCmd)
}
