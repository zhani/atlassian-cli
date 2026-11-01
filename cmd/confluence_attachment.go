package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/zhani/atlassian-cli/internal/confluence"
)

var confluenceAttachmentCmd = &cobra.Command{
	Use:   "attachment",
	Short: "Manage attachments on a Confluence page",
}

var confluenceAttachmentListCmd = &cobra.Command{
	Use:   "list <page-id>",
	Short: "List attachments on a Confluence page",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		attachments, err := client.GetAttachments(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		return newFormatter(cmd).PrintConfluenceAttachments(attachments)
	},
}

var confluenceAttachmentDownloadCmd = &cobra.Command{
	Use:   "download <page-id> <title-or-id>",
	Short: "Download an attachment from a Confluence page",
	Long: "Download an attachment from a Confluence page. The second argument matches an\n" +
		"attachment by title or ID. Use --file to set the output path (default: the\n" +
		"attachment's title), or --file - to write to stdout.",
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		pageID, ref := args[0], args[1]
		outFile, _ := cmd.Flags().GetString("file")

		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		attachments, err := client.GetAttachments(cmd.Context(), pageID)
		if err != nil {
			return err
		}

		var match *confluence.Attachment
		for i := range attachments {
			if attachments[i].ID == ref || attachments[i].Title == ref {
				match = &attachments[i]
				break
			}
		}
		if match == nil {
			return fmt.Errorf("no attachment matching %q found on page %s", ref, pageID)
		}

		downloadPath := match.Links["download"]
		if downloadPath == "" {
			return fmt.Errorf("attachment %q has no download link", match.Title)
		}
		body, err := client.DownloadAttachment(cmd.Context(), downloadPath)
		if err != nil {
			return err
		}
		defer body.Close()

		if outFile == "" {
			outFile = match.Title
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
			return newFormatter(cmd).PrintMessage(fmt.Sprintf("Downloaded %s -> %s", match.Title, outFile))
		}
		return nil
	},
}

func init() {
	confluenceAttachmentDownloadCmd.Flags().String("file", "", "Output file path (default: attachment title, - for stdout)")
	confluenceAttachmentCmd.AddCommand(confluenceAttachmentListCmd)
	confluenceAttachmentCmd.AddCommand(confluenceAttachmentDownloadCmd)
	confluenceCmd.AddCommand(confluenceAttachmentCmd)
}
