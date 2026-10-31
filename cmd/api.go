package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// rawRequester is implemented by the Jira and Confluence clients.
type rawRequester interface {
	Raw(ctx context.Context, method, path string, query url.Values, body []byte) (int, []byte, error)
}

// newAPICmd builds an `api` command that sends an authenticated request to
// any REST path of a product.
func newAPICmd(product, example string, client func() (rawRequester, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api <path>",
		Short: "Send an authenticated request to any " + product + " REST endpoint",
		Long: `Send an authenticated request to any ` + product + ` REST endpoint and print the
response. JSON responses are pretty-printed. The command fails (after
printing the response) when the status is 400 or higher.

The method defaults to GET, or POST when --data is given. --data takes a JSON
string, @FILE, or @- for stdin.`,
		Example: example,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if !strings.HasPrefix(path, "/") {
				return errors.New("path must start with / and be relative to the configured URL")
			}

			data, _ := cmd.Flags().GetString("data")
			body, err := requestBody(data)
			if err != nil {
				return err
			}
			method, _ := cmd.Flags().GetString("method")
			if !cmd.Flags().Changed("method") && body != nil {
				method = "POST"
			}
			params, _ := cmd.Flags().GetStringArray("query")
			query := url.Values{}
			for _, p := range params {
				k, v, ok := strings.Cut(p, "=")
				if !ok {
					return fmt.Errorf("--query %q: expected key=value", p)
				}
				query.Add(k, v)
			}

			c, err := client()
			if err != nil {
				return err
			}
			status, resp, err := c.Raw(cmd.Context(), strings.ToUpper(method), path, query, body)
			if err != nil {
				return err
			}
			if err := writeResponse(cmd.OutOrStdout(), resp); err != nil {
				return err
			}
			if status >= 400 {
				return fmt.Errorf("HTTP %d", status)
			}
			return nil
		},
	}
	cmd.Flags().StringP("method", "X", "GET", "HTTP method")
	cmd.Flags().StringP("data", "d", "", "Request body: JSON, @FILE, or @- for stdin")
	cmd.Flags().StringArrayP("query", "q", nil, "Query parameter key=value (repeatable)")
	return cmd
}

func requestBody(data string) ([]byte, error) {
	switch {
	case data == "":
		return nil, nil
	case data == "@-":
		return io.ReadAll(os.Stdin)
	case strings.HasPrefix(data, "@"):
		return os.ReadFile(data[1:])
	default:
		return []byte(data), nil
	}
}

// writeResponse pretty-prints JSON and copies anything else verbatim.
func writeResponse(w io.Writer, resp []byte) error {
	resp = bytes.TrimSpace(resp)
	if len(resp) == 0 {
		return nil
	}
	var out bytes.Buffer
	if json.Indent(&out, resp, "", "  ") == nil {
		out.WriteByte('\n')
		_, err := w.Write(out.Bytes())
		return err
	}
	_, err := w.Write(append(resp, '\n'))
	return err
}

func init() {
	jiraCmd.AddCommand(newAPICmd("Jira", `  atlassian-cli jira api /rest/api/2/myself
  atlassian-cli jira api /rest/api/2/search -q jql="project = PROJ" -q maxResults=5
  atlassian-cli jira api /rest/api/2/issue/PROJ-1 -X PUT -d '{"fields":{"summary":"New"}}'
  atlassian-cli jira api /rest/api/2/issue -d @issue.json`,
		func() (rawRequester, error) { return newJiraClient() }))
	confluenceCmd.AddCommand(newAPICmd("Confluence", `  atlassian-cli confluence api /rest/api/space -q limit=5
  atlassian-cli confluence api /rest/api/content/12345 -q expand=body.storage`,
		func() (rawRequester, error) { return newConfluenceClient() }))
}
