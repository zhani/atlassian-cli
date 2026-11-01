package mirrorsync

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
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
