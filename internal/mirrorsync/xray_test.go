package mirrorsync

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zhani/atlassian-cli/internal/config"
	"github.com/zhani/atlassian-cli/internal/jira"
	"github.com/zhani/atlassian-cli/internal/xray"
)

// An Xray test (manual steps + TestRunStatus) whose latest run failed step 6
// with evidence: export from a fake server, import, then read it offline.
func TestXrayTestRunThroughMirror(t *testing.T) {
	var srvURL string
	steps := ""
	for i := 1; i <= 6; i++ {
		steps += fmt.Sprintf(`%s{"id":%d,"index":%d,"fields":{"Action":"action %d","Data":"","Expected Result":"expect %d"},"attachments":[]}`,
			map[bool]string{true: "", false: ","}[i == 1], 100+i, i, i, i)
	}
	issue := func() string {
		return `{"key":"T-1","id":"1","fields":{"summary":"radio test","issuetype":{"name":"Test"},
			"updated":"2026-10-30T10:00:00.000+0000",
			"customfield_10806":{"steps":[` + steps + `]},
			"customfield_10812":{"issueId":1,"statuses":[{"group":"T-8","statusResults":[
				{"environment":"Dev","latest":3,"latestFinal":3,"latestFinalId":5001,"latestId":5001}]}]}}}`
	}
	runSteps := ""
	for i := 1; i <= 6; i++ {
		st := `{"index":` + fmt.Sprint(i) + `,"status":"PASS","evidences":[]}`
		if i == 6 {
			st = `{"index":6,"status":"FAIL","comment":{"raw":"no carrier on screen"},"evidences":[
				{"id":901,"fileName":"scope 1.png","fileURL":"` + srvURL + `/plugins/servlet/raven/attachment/901/scope%201.png"}]}`
		}
		runSteps += map[bool]string{true: "", false: ","}[i == 1] + st
	}
	mux := http.NewServeMux()
	ok := func(body func() string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, body()) }
	}
	lit := func(s string) func() string { return func() string { return s } }
	mux.HandleFunc("GET /rest/api/2/myself", ok(lit(`{"name":"me"}`)))
	mux.HandleFunc("GET /rest/api/2/field", ok(lit(`[{"id":"customfield_10806","name":"Manual Test Steps"}]`)))
	mux.HandleFunc("GET /rest/api/2/issueLinkType", ok(lit(`{"issueLinkTypes":[]}`)))
	mux.HandleFunc("GET /rest/api/2/issue/{key}/comment", ok(lit(`{"comments":[],"total":0}`)))
	mux.HandleFunc("GET /rest/api/2/issue/{key}", ok(issue))
	mux.HandleFunc("GET /rest/api/2/search", ok(func() string {
		return `{"startAt":0,"maxResults":100,"total":1,"issues":[` + issue() + `]}`
	}))
	mux.HandleFunc("GET /rest/raven/1.0/api/testrun/5001", ok(func() string {
		return `{"id":5001,"status":"FAIL","testExecKey":"T-9","executedBy":"tester","steps":[` + runSteps + `]}`
	}))
	mux.HandleFunc("GET /plugins/servlet/raven/attachment/901/{name}", ok(lit("PNG-EVIDENCE")))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL
	// runSteps captured srvURL before it was set; rebuild it now.
	runSteps = strings.ReplaceAll(runSteps, `"/plugins/`, `"`+srvURL+`/plugins/`)

	cfg := &config.AtlassianConfig{URL: srv.URL, Token: "x"}
	e := &Exporter{Jira: jira.NewClient(cfg), JiraCfg: cfg}
	archive := filepath.Join(t.TempDir(), "m.tar.gz")
	if _, err := e.Export(context.Background(), ExportOptions{Out: archive, Log: io.Discard, JQL: []string{"key = T-1"}}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "mirror")
	if _, err := Import(archive, dir, ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	// Offline, through the mirror, as `issue get` / the TUI do.
	for _, v := range []string{"JIRA_URL", "CONFLUENCE_URL", "ATLASSIAN_URL", "JIRA_PERSONAL_TOKEN"} {
		t.Setenv(v, "")
	}
	t.Setenv("ATLASSIAN_MIRROR", dir)
	mcfg, err := config.LoadJiraConfig()
	if err != nil {
		t.Fatal(err)
	}
	mc := jira.NewClient(mcfg)
	ctx := context.Background()
	got, err := mc.GetIssue(ctx, "T-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := mc.LoadXrayRuns(ctx, got); err != nil || len(got.XrayRuns) != 1 {
		t.Fatalf("runs through mirror: %d, %v", len(got.XrayRuns), err)
	}
	md := xray.Markdown(xray.ManualSteps(got.Fields.Custom), got.XrayRuns)
	for _, want := range []string{"### Step 1 — PASS", "**Action:** action 6", "### Step 6 — FAIL",
		"**Comment:** no carrier on screen", "**Evidence:** `scope 1.png`"} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	files := xray.Files(got.XrayRuns[0])
	if len(files) != 1 || strings.Contains(files[0].URL, srv.URL) {
		t.Fatalf("evidence URL should point at the mirror: %+v", files)
	}
	body, err := mc.DownloadAttachment(ctx, files[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(body)
	body.Close()
	if string(data) != "PNG-EVIDENCE" {
		t.Errorf("evidence through mirror: %q", data)
	}
}
