package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/zhani/atlassian-cli/internal/xray"
)

var jiraIssueEvidenceCmd = &cobra.Command{
	Use:   "evidence <issue-key> [name...]",
	Short: "Download the evidence and step attachments of an Xray test",
	Long: "Download the files of an Xray test issue: the evidence of its latest test runs\n" +
		"(per step and per run) and the attachments of its manual steps. With names,\n" +
		"only those files. They are saved under --dir as <id>-<name>; --list only lists\n" +
		"them. Works against the server (with Xray) and an offline mirror.",
	Example: `  atlassian-cli jira issue evidence PROJ-42                  # all, into the current directory
  atlassian-cli jira issue evidence PROJ-42 --list
  atlassian-cli jira issue evidence PROJ-42 screenshot.png --dir /tmp/evidence`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newJiraClient()
		if err != nil {
			return err
		}
		ctx := cmd.Context()
		issue, err := client.GetIssue(ctx, args[0])
		if err != nil {
			return err
		}
		runErr := client.LoadXrayRuns(ctx, issue)
		var files []xray.File
		for _, st := range xray.ManualSteps(issue.Fields.Custom) {
			files = append(files, st.Files...)
		}
		for _, run := range issue.XrayRuns {
			files = append(files, xray.Files(run)...)
		}
		if want := args[1:]; len(want) > 0 {
			var picked []xray.File
			for _, f := range files {
				for _, w := range want {
					if f.Name == w || f.ID == w || f.LocalName() == w {
						picked = append(picked, f)
						break
					}
				}
			}
			files = picked
		}
		if len(files) == 0 {
			if runErr != nil {
				return fmt.Errorf("no evidence found on %s (%v)", args[0], runErr)
			}
			return fmt.Errorf("no evidence or step attachments found on %s", args[0])
		}
		out := cmd.OutOrStdout()
		if list, _ := cmd.Flags().GetBool("list"); list {
			for _, f := range files {
				fmt.Fprintln(out, f.LocalName())
			}
			return nil
		}
		dir, _ := cmd.Flags().GetString("dir")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, f := range files {
			path := filepath.Join(dir, f.LocalName())
			if seen[path] {
				continue
			}
			seen[path] = true
			body, err := client.DownloadAttachment(ctx, f.URL)
			if err != nil {
				return fmt.Errorf("%s: %w", f.Name, err)
			}
			dst, err := os.Create(path)
			if err == nil {
				_, err = io.Copy(dst, body)
				if cerr := dst.Close(); err == nil {
					err = cerr
				}
			}
			body.Close()
			if err != nil {
				return fmt.Errorf("%s: %w", f.Name, err)
			}
			fmt.Fprintln(out, path)
		}
		return nil
	},
}

func init() {
	jiraIssueEvidenceCmd.Flags().String("dir", ".", "Directory to save the files in")
	jiraIssueEvidenceCmd.Flags().Bool("list", false, "Only list the files")
	jiraIssueCmd.AddCommand(jiraIssueEvidenceCmd)
}
