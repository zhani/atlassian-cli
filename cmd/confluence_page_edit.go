package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/htmlconv"
	"github.com/zhani/atlassian-cli/internal/mdconv"
)

var confluencePageEditCmd = &cobra.Command{
	Use:   "edit <page-id>",
	Short: "Edit a Confluence page in $EDITOR",
	Long: `Open a page in $EDITOR as Markdown and save it back as a new version.

Pages using features that Markdown cannot represent (macros other than code,
info, note, warning and tip; page links and mentions; inline styles; merged
table cells, ...) are refused, because saving would drop them. Use --raw to
edit the storage format (XHTML) instead, or --allow-lossy to edit anyway.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !isatty.IsTerminal(os.Stdin.Fd()) {
			return errors.New("edit needs a terminal; use `confluence page update` in scripts")
		}
		client, err := newConfluenceClient()
		if err != nil {
			return err
		}
		page, err := client.GetPage(cmd.Context(), args[0], []string{"body.storage", "version"})
		if err != nil {
			return err
		}
		storage := ""
		if page.Body.Storage != nil {
			storage = page.Body.Storage.Value
		}

		raw, _ := cmd.Flags().GetBool("raw")
		allowLossy, _ := cmd.Flags().GetBool("allow-lossy")
		if !raw && !allowLossy {
			if lost := htmlconv.Unsupported(storage); len(lost) > 0 {
				return fmt.Errorf("page %s uses features that Markdown editing would lose: %s\nedit the storage format with --raw, or pass --allow-lossy to drop them",
					page.ID, strings.Join(lost, ", "))
			}
		}

		initial, ext := storage+"\n", ".html"
		if !raw {
			initial, ext = htmlconv.Convert(storage)+"\n", ".md"
		}
		edited, err := editText(ext, initial)
		if err != nil {
			return err
		}

		title, _ := cmd.Flags().GetString("title")
		if edited == initial && (title == "" || title == page.Title) {
			_, err := fmt.Fprintln(cmd.ErrOrStderr(), "No changes.")
			return err
		}
		if title == "" {
			title = page.Title
		}
		body := edited
		if !raw {
			if body, err = mdconv.ToStorage(edited); err != nil {
				return err
			}
		}
		message, _ := cmd.Flags().GetString("message")
		updated, err := client.UpdatePage(cmd.Context(), page.ID, &confluence.UpdatePageInput{
			Title:   title,
			Body:    body,
			Version: page.Version.Number + 1, // fails if someone saved in the meantime
			Message: message,
		})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Updated %q to version %d\n", updated.Title, updated.Version.Number)
		return err
	},
}

func init() {
	confluencePageEditCmd.Flags().Bool("raw", false, "Edit the storage format (XHTML) instead of Markdown")
	confluencePageEditCmd.Flags().Bool("allow-lossy", false, "Edit as Markdown even if unsupported content would be dropped")
	confluencePageEditCmd.Flags().String("title", "", "New page title")
	confluencePageEditCmd.Flags().StringP("message", "m", "", "Version message")
	confluencePageEditCmd.MarkFlagsMutuallyExclusive("raw", "allow-lossy")
	confluencePageCmd.AddCommand(confluencePageEditCmd)
}
