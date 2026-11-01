package mirrorsync

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/zhani/atlassian-cli/internal/config"
	"github.com/zhani/atlassian-cli/internal/jira"
)

func TestActiveSprintIDs(t *testing.T) {
	sprints := []map[string]any{
		{"id": 1, "state": "closed"}, {"id": float64(2), "state": "active"},
		{"id": "3", "state": "ACTIVE"}, {"id": 4, "state": "future"},
	}
	if got := activeSprintIDs(sprints); !reflect.DeepEqual(got, []int{2, 3}) {
		t.Errorf("got %v", got)
	}
	if got := activeSprintIDs(nil); got != nil {
		t.Errorf("no sprints: got %v", got)
	}
}

func TestActiveSprintJQL(t *testing.T) {
	for _, c := range []struct {
		ids        []int
		haveBoards bool
		project    string
		want       string
	}{
		{[]int{2, 3}, true, "P", "sprint in (2, 3)"},
		{nil, true, "P", ""}, // boards without an active sprint: nothing
		{nil, false, "PROJ", `project = "PROJ" AND sprint in openSprints() AND sprint not in futureSprints()`},
		{nil, false, "", ""},
	} {
		if got := activeSprintJQL(c.ids, c.haveBoards, c.project); got != c.want {
			t.Errorf("%v %v %q: got %q, want %q", c.ids, c.haveBoards, c.project, got, c.want)
		}
	}
}

func TestMyIssuesJQL(t *testing.T) {
	for _, c := range []struct {
		resolutions []string
		want        string
	}{
		{[]string{"Fixed", "Won't Fix", "Done", "Duplicate"},
			`assignee = currentUser() AND (resolution IS EMPTY OR resolution NOT IN ("Done", "Fixed"))`},
		{[]string{"done", "Duplicate"}, // only names the server has, as it spells them
			`assignee = currentUser() AND (resolution IS EMPTY OR resolution NOT IN ("done"))`},
		{[]string{"Duplicate"}, "assignee = currentUser()"},
	} {
		if got := myIssuesJQL(c.resolutions); got != c.want {
			t.Errorf("%v: got %q, want %q", c.resolutions, got, c.want)
		}
	}
}

// fakeJira serves what Export calls for a board (no filter configured, so the
// board's own issue list is used) and records the search queries.
func fakeJira(t *testing.T, sprints string, resolutions string) (*httptest.Server, *[]string) {
	t.Helper()
	var queries []string
	issue := `{"key":"P-1","id":"1","fields":{"summary":"s","updated":"2026-10-30T10:00:00.000+0000"}}`
	mux := http.NewServeMux()
	ok := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, body) }
	}
	mux.HandleFunc("GET /rest/api/2/myself", ok(`{"name":"me"}`))
	mux.HandleFunc("GET /rest/api/2/field", ok(`[]`))
	mux.HandleFunc("GET /rest/api/2/issueLinkType", ok(`{"issueLinkTypes":[]}`))
	mux.HandleFunc("GET /rest/api/2/resolution", ok(resolutions))
	mux.HandleFunc("GET /rest/agile/1.0/board/7", ok(`{"id":7,"name":"B","type":"scrum"}`))
	mux.HandleFunc("GET /rest/agile/1.0/board/7/sprint", ok(`{"values":`+sprints+`,"isLast":true}`))
	mux.HandleFunc("GET /rest/agile/1.0/board/7/issue", ok(`{"issues":[{"key":"P-1"}],"total":1}`))
	mux.HandleFunc("GET /rest/api/2/issue/{key}", ok(issue))
	mux.HandleFunc("GET /rest/api/2/issue/{key}/comment", ok(`{"comments":[],"total":0}`))
	mux.HandleFunc("GET /rest/api/2/search", func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query().Get("jql"))
		io.WriteString(w, `{"startAt":0,"maxResults":100,"total":1,"issues":[`+issue+`]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &queries
}

func export(t *testing.T, srv *httptest.Server, opts ExportOptions) {
	t.Helper()
	cfg := &config.AtlassianConfig{URL: srv.URL, Token: "x"}
	e := &Exporter{Jira: jira.NewClient(cfg), JiraCfg: cfg}
	opts.Out, opts.Log = filepath.Join(t.TempDir(), "m.tar.gz"), io.Discard
	opts.Boards = []int{7}
	if _, err := e.Export(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
}

func hasPrefix(qs []string, prefix string) bool {
	for _, q := range qs {
		if strings.HasPrefix(q, prefix) {
			return true
		}
	}
	return false
}

func TestExportActiveSprintAndMyIssues(t *testing.T) {
	srv, queries := fakeJira(t, `[{"id":3,"state":"closed"},{"id":5,"state":"active"}]`,
		`[{"name":"Fixed"},{"name":"Done"},{"name":"Won't Fix"}]`)
	export(t, srv, ExportOptions{ActiveSprints: true, MyIssues: true})
	if !hasPrefix(*queries, "(sprint in (5))") {
		t.Errorf("no active sprint query in %q", *queries)
	}
	if !hasPrefix(*queries, `(assignee = currentUser() AND (resolution IS EMPTY OR resolution NOT IN ("Done", "Fixed")))`) {
		t.Errorf("no my-issues query in %q", *queries)
	}
}

func TestExportWithoutActiveSprintOrOptOut(t *testing.T) {
	srv, queries := fakeJira(t, `[{"id":3,"state":"closed"}]`, `[{"name":"Done"}]`)
	export(t, srv, ExportOptions{ActiveSprints: true, MyIssues: false})
	if hasPrefix(*queries, "(sprint") || hasPrefix(*queries, "(assignee") {
		t.Errorf("unexpected queries %q", *queries)
	}
}

func TestExportFollowsFirstLevelLinksOnly(t *testing.T) {
	issues := map[string]string{
		"P-1": `{"key":"P-1","fields":{"summary":"root","customfield_10008":"P-9",
			"parent":{"key":"P-5"},"subtasks":[{"key":"P-6"}],
			"issuelinks":[{"type":{"outward":"blocks"},"outwardIssue":{"key":"P-2"}},
			              {"type":{"inward":"relates to"},"inwardIssue":{"key":"p-3"}}],
			"customfield_10825":[{"issueKey":"P-1","status":"NOTRUN","okJql":"issueKey in ()",
			    "notRunJql":"issueKey in (P-7, P-8)","nok":0}]}}`,
		"P-2": `{"key":"P-2","fields":{"issuelinks":[{"outwardIssue":{"key":"P-4"}}]}}`,
	}
	var fetched []string
	mux := http.NewServeMux()
	ok := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, body) }
	}
	mux.HandleFunc("GET /rest/api/2/myself", ok(`{"name":"me"}`))
	mux.HandleFunc("GET /rest/api/2/field", ok(`[{"id":"customfield_10008","name":"Epic Link"}]`))
	mux.HandleFunc("GET /rest/api/2/issueLinkType", ok(`{"issueLinkTypes":[]}`))
	mux.HandleFunc("GET /rest/api/2/issue/{key}/comment", ok(`{"comments":[],"total":0}`))
	mux.HandleFunc("GET /rest/api/2/issue/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		fetched = append(fetched, key)
		body, found := issues[key]
		if !found {
			body = `{"key":"` + key + `","fields":{}}`
		}
		io.WriteString(w, body)
	})
	mux.HandleFunc("GET /rest/api/2/search", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"startAt":0,"maxResults":100,"total":1,"issues":[`+issues["P-1"]+`]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := &config.AtlassianConfig{URL: srv.URL, Token: "x"}
	e := &Exporter{Jira: jira.NewClient(cfg), JiraCfg: cfg}
	_, err := e.Export(context.Background(), ExportOptions{
		Out: filepath.Join(t.TempDir(), "m.tar.gz"), Log: io.Discard, JQL: []string{"key = P-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"P-2", "P-3", "P-5", "P-6", "P-7", "P-8", "P-9"} {
		if !e.done[k] {
			t.Errorf("%s (first-level relative of P-1) not exported; fetched %v", k, fetched)
		}
	}
	if e.done["P-4"] {
		t.Errorf("P-4 is a link of a link and must not be exported (not recursive)")
	}
}

func TestCoverageKeys(t *testing.T) {
	v := []any{
		map[string]any{"issueKey": "R-1", "okJql": "issueKey in (T-1,T-2)", "nokJql": "issueKey in ()",
			"notRunJql": "issueKey IN (T-3)", "status": "NOTRUN"},
		map[string]any{"issueKey": "R-2", "unknownJql": "key in (T-4) AND project = X"},
	}
	got := coverageKeys(v)
	sort.Strings(got)
	if want := []string{"T-1", "T-2", "T-3", "T-4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for _, other := range []any{"PROJ-1", map[string]any{"value": "issueKey in (X-1)"}, nil, 42.0} {
		if k := coverageKeys(other); len(k) != 0 {
			t.Errorf("%v: got %v, want nothing", other, k)
		}
	}
}
