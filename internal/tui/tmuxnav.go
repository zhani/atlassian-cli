package tui

import (
	"os"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
)

// tmuxSelectPane moves tmux's focus to the neighbouring pane ("L" or "R")
// when running inside tmux. vim-tmux-navigator's bindings hand ctrl+h/ctrl+l
// to atlassian-cli (see README), so the key reaches tmux only if we pass it
// on: at the edge of a view the focus then leaves atlassian-cli, as it leaves
// vim. A var so tests can record the calls.
var tmuxSelectPane = func(dir string) tea.Cmd {
	if os.Getenv("TMUX") == "" {
		return nil
	}
	args := []string{"select-pane", "-" + dir}
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		args = []string{"select-pane", "-t", pane, "-" + dir}
	}
	return func() tea.Msg {
		_ = exec.Command("tmux", args...).Run()
		return nil
	}
}
