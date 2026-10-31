package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/putcho01/atlassian-cli/internal/mdconv"
	"github.com/spf13/cobra"
)

// addBodyFlags registers a long-text flag (e.g. --body) together with the
// alternative sources read by readBody: --template and --editor.
func addBodyFlags(cmd *cobra.Command, name, usage string) {
	cmd.Flags().String(name, "", usage)
	cmd.Flags().String("template", "", "Read the "+name+" from a file (- for stdin)")
	cmd.Flags().BoolP("editor", "e", false, "Write the "+name+" in $EDITOR")
	cmd.MarkFlagsMutuallyExclusive(name, "template", "editor")
}

// addMarkdownFlag registers --raw for commands that convert Markdown to Jira
// wiki markup.
func addMarkdownFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("raw", false, "Send text as-is (Jira wiki markup) instead of converting from Markdown")
}

// readBody returns the text for a flag registered with addBodyFlags, taken
// from the first available source:
//
//  1. the flag itself
//  2. --template FILE (- reads stdin)
//  3. $EDITOR, when --editor is given
//  4. for required text only: piped stdin, then $EDITOR on a terminal
//
// Optional text never reads stdin implicitly, so a command run inside a
// `while read` loop cannot swallow the loop's input.
func readBody(cmd *cobra.Command, name string, required bool, editorExt string) (string, error) {
	if cmd.Flags().Changed(name) {
		v, _ := cmd.Flags().GetString(name)
		return v, nil
	}
	if path, _ := cmd.Flags().GetString("template"); path != "" {
		if path == "-" {
			return readAll(os.Stdin)
		}
		data, err := os.ReadFile(path)
		return string(data), err
	}
	if editor, _ := cmd.Flags().GetBool("editor"); editor {
		return editText(editorExt)
	}
	if !required {
		return "", nil
	}
	if stdinPiped() {
		return readAll(os.Stdin)
	}
	if isatty.IsTerminal(os.Stdin.Fd()) {
		return editText(editorExt)
	}
	return "", fmt.Errorf("--%s is required (or use --template, pipe it on stdin, or run in a terminal to use $EDITOR)", name)
}

// jiraText converts Markdown to Jira wiki markup unless --raw was given.
func jiraText(cmd *cobra.Command, text string) string {
	if raw, _ := cmd.Flags().GetBool("raw"); raw || strings.TrimSpace(text) == "" {
		return text
	}
	return mdconv.ToJiraWiki(text)
}

func readAll(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	return string(data), err
}

// stdinPiped reports whether stdin is a pipe or a redirected file, as
// opposed to a terminal or /dev/null.
func stdinPiped() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeNamedPipe != 0 || fi.Mode().IsRegular()
}

// editText opens $VISUAL / $EDITOR (default vi) on an empty temporary file
// and returns what was saved. An empty result aborts the command.
func editText(ext string) (string, error) {
	f, err := os.CreateTemp("", "atlassian-cli-*"+ext)
	if err != nil {
		return "", err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	args := append(strings.Fields(editor), path) // allow e.g. EDITOR="code --wait"
	c := exec.Command(args[0], args[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Talk to the terminal directly so the editor works even when stdout is
	// redirected (e.g. `comment add KEY -o json > out.json`).
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		defer tty.Close()
		c.Stdin, c.Stdout, c.Stderr = tty, tty, tty
	}
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("editor %q: %w", editor, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", errors.New("aborted: empty text")
	}
	return string(data), nil
}
