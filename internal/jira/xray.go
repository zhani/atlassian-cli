package jira

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/zhani/atlassian-cli/internal/xray"
)

// XrayTestRun returns an Xray (Server/DC) test run as Xray serves it: its
// status, steps with their results, comments and evidence.
func (c *Client) XrayTestRun(ctx context.Context, id string) (map[string]any, error) {
	status, body, err := c.Raw(ctx, "GET", xray.TestRunPath(id), nil, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("xray test run %s: HTTP %d", id, status)
	}
	var run map[string]any
	if err := json.Unmarshal(body, &run); err != nil {
		return nil, fmt.Errorf("xray test run %s: %w", id, err)
	}
	return run, nil
}

// LoadXrayRuns fills issue.XrayRuns with the test runs its TestRunStatus
// field names. Best effort: without Xray, or for runs it can't read, the
// issue simply gets fewer runs; the first error is returned for reporting.
func (c *Client) LoadXrayRuns(ctx context.Context, issue *Issue) error {
	var first error
	for _, ref := range xray.RunRefs(issue.Fields.Custom) {
		run, err := c.XrayTestRun(ctx, ref.ID)
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		issue.XrayRuns = append(issue.XrayRuns, run)
	}
	return first
}
