package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestDefaultMaxResults(t *testing.T) {
	for env, want := range map[string]int{"": 500, "50": 50, " 200 ": 200, "0": 500, "-3": 500, "lots": 500} {
		t.Setenv(maxResultsEnv, env)
		if got := defaultMaxResults(); got != want {
			t.Errorf("JIRA_MAX_RESULTS=%q: got %d, want %d", env, got, want)
		}
	}
}

func TestMaxResultsFlagDefaultAndOverride(t *testing.T) {
	t.Setenv(maxResultsEnv, "80")
	cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
	addIssueListFlags(cmd)
	if n, _ := cmd.Flags().GetInt("max-results"); n != 80 {
		t.Errorf("default with JIRA_MAX_RESULTS=80: got %d", n)
	}
	cmd.SetArgs([]string{"--max-results", "7"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if n, _ := cmd.Flags().GetInt("max-results"); n != 7 {
		t.Errorf("explicit --max-results 7: got %d", n)
	}
}
