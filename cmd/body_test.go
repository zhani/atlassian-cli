package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func newBodyCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "test"}
	addBodyFlags(c, "body", "body")
	addMarkdownFlag(c)
	if err := c.ParseFlags(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return c
}

func TestReadBody(t *testing.T) {
	t.Parallel()
	tmpl := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(tmpl, []byte("from file"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		args     []string
		required bool
		want     string
	}{
		{name: "flag value", args: []string{"--body", "inline"}, want: "inline"},
		{name: "explicit empty flag", args: []string{"--body", ""}, required: true, want: ""},
		{name: "template file", args: []string{"--template", tmpl}, want: "from file"},
		{name: "optional without input reads nothing", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := readBody(newBodyCmd(t, tt.args...), "body", tt.required, ".md")
			if err != nil {
				t.Fatalf("readBody() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("readBody() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJiraText(t *testing.T) {
	t.Parallel()
	if got := jiraText(newBodyCmd(t), "**bold**"); got != "*bold*\n" {
		t.Errorf("converted = %q", got)
	}
	if got := jiraText(newBodyCmd(t, "--raw"), "**bold**"); got != "**bold**" {
		t.Errorf("raw = %q", got)
	}
	if got := jiraText(newBodyCmd(t), "  "); got != "  " {
		t.Errorf("blank text should pass through, got %q", got)
	}
}
