package mirrorsync

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/zhani/atlassian-cli/internal/config"
	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/htmlconv"
	"github.com/zhani/atlassian-cli/internal/jira"
	"github.com/zhani/atlassian-cli/internal/mirror"
	"github.com/zhani/atlassian-cli/internal/xray"
	"github.com/zhani/atlassian-cli/internal/xref"
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
	got := xray.CoverageTests(v)
	sort.Strings(got)
	if want := []string{"T-1", "T-2", "T-3", "T-4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for _, other := range []any{"PROJ-1", map[string]any{"value": "issueKey in (X-1)"}, nil, 42.0} {
		if k := xray.CoverageTests(other); len(k) != 0 {
			t.Errorf("%v: got %v, want nothing", other, k)
		}
	}
}

func TestExportOnlyOneSide(t *testing.T) {
	jsrv, _ := fakeJira(t, `[]`, `[]`)
	confHits := 0
	csrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		confHits++
		http.NotFound(w, nil)
	}))
	t.Cleanup(csrv.Close)
	jcfg := &config.AtlassianConfig{URL: jsrv.URL, Token: "x"}
	ccfg := &config.AtlassianConfig{URL: csrv.URL, Token: "x"}
	newExporter := func() *Exporter {
		return &Exporter{Jira: jira.NewClient(jcfg), JiraCfg: jcfg, Confluence: confluence.NewClient(ccfg), ConfCfg: ccfg}
	}
	out := func() string { return filepath.Join(t.TempDir(), "m.tar.gz") }

	m, err := newExporter().Export(context.Background(), ExportOptions{Out: out(), Log: io.Discard, Boards: []int{7}, Only: mirror.SideJira})
	if err != nil {
		t.Fatal(err)
	}
	// Both URLs are recorded whenever configured (cross-references, link rewriting).
	if !slices.Equal(m.Contents, []string{mirror.SideJira}) || m.ConfluenceURL != csrv.URL || m.JiraURL == "" || confHits != 0 {
		t.Fatalf("--only jira: contents %v, confluence url %q, %d Confluence request(s)", m.Contents, m.ConfluenceURL, confHits)
	}

	m, err = newExporter().Export(context.Background(), ExportOptions{Out: out(), Log: io.Discard, Only: mirror.SideConfluence})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.Contents, []string{mirror.SideConfluence}) || m.JiraURL != jsrv.URL || m.ConfluenceURL == "" || m.Issues != 0 {
		t.Fatalf("--only confluence: %+v", m)
	}

	m, err = newExporter().Export(context.Background(), ExportOptions{Out: out(), Log: io.Discard, Boards: []int{7}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.Contents, mirror.Sides) {
		t.Fatalf("default contents = %v, want both", m.Contents)
	}

	for _, bad := range []ExportOptions{
		{Only: mirror.SideConfluence, Boards: []int{7}},
		{Only: mirror.SideJira, Pages: []string{"1"}},
		{Only: "wiki"},
	} {
		bad.Out, bad.Log = out(), io.Discard
		if _, err := newExporter().Export(context.Background(), bad); err == nil {
			t.Errorf("%+v: want an error", bad)
		}
	}
	if _, err := (&Exporter{Jira: jira.NewClient(jcfg), JiraCfg: jcfg}).Export(context.Background(),
		ExportOptions{Out: out(), Log: io.Discard, Only: mirror.SideConfluence}); err == nil {
		t.Error("--only confluence without Confluence configured: want an error")
	}
}

// TestExportCrossRefs: issues bring in the pages they link to (description,
// remote links, /display/ links resolved by title) and exported pages bring
// in the issues they show or link to, one step only.
func TestExportCrossRefs(t *testing.T) {
	var jsrv, csrv *httptest.Server
	issues := map[string]func() string{
		"P-1": func() string {
			return `{"key":"P-1","fields":{"summary":"root","description":"spec: [x|` + csrv.URL + `/pages/viewpage.action?pageId=10]"}}`
		},
		// On page 20; its own page and parent must not follow.
		"P-30": func() string {
			return `{"key":"P-30","fields":{"parent":{"key":"P-32"},"description":"` + csrv.URL + `/pages/viewpage.action?pageId=12"}}`
		},
	}
	remote := map[string]func() string{
		"P-1": func() string {
			return `[{"id":1,"object":{"url":"` + csrv.URL + `/display/DEV/The+Spec","title":"The Spec"}}]`
		},
	}
	ok := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, body) }
	}
	jm := http.NewServeMux()
	jm.HandleFunc("GET /rest/api/2/myself", ok(`{"name":"me"}`))
	jm.HandleFunc("GET /rest/api/2/field", ok(`[]`))
	jm.HandleFunc("GET /rest/api/2/issueLinkType", ok(`{"issueLinkTypes":[]}`))
	jm.HandleFunc("GET /rest/api/2/issue/{key}/comment", ok(`{"comments":[],"total":0}`))
	jm.HandleFunc("GET /rest/api/2/issue/{key}/remotelink", func(w http.ResponseWriter, r *http.Request) {
		body := `[]`
		if f, found := remote[r.PathValue("key")]; found {
			body = f()
		}
		io.WriteString(w, body)
	})
	jm.HandleFunc("GET /rest/api/2/issue/{key}", func(w http.ResponseWriter, r *http.Request) {
		key := r.PathValue("key")
		body := `{"key":"` + key + `","fields":{}}`
		if f, found := issues[key]; found {
			body = f()
		}
		io.WriteString(w, body)
	})
	var listMax string
	jm.HandleFunc("GET /rest/api/2/search", func(w http.ResponseWriter, r *http.Request) {
		switch jql := r.URL.Query().Get("jql"); {
		case strings.Contains(jql, "linkedIssues"): // an issue list on page 20
			listMax = r.URL.Query().Get("maxResults")
			io.WriteString(w, `{"startAt":0,"total":5,"issues":[{"key":"P-41","fields":{}},{"key":"P-40","fields":{}}]}`)
		case strings.Contains(jql, "OTHER"):
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"errorMessages":["The value 'OTHER' does not exist for the field 'project'."]}`)
		default:
			io.WriteString(w, `{"startAt":0,"maxResults":100,"total":1,"issues":[`+issues["P-1"]()+`]}`)
		}
	})
	jsrv = httptest.NewServer(jm)
	t.Cleanup(jsrv.Close)

	page := func(id, body string) string {
		return `{"id":"` + id + `","type":"page","title":"T` + id + `","space":{"key":"DEV"},"version":{"number":1,"when":"2026-10-30T10:00:00.000Z"},"body":{"storage":{"value":"` + body + `"}}}`
	}
	cm := http.NewServeMux()
	cm.HandleFunc("GET /rest/api/content/search", ok(`{"results":[],"_links":{}}`))
	cm.HandleFunc("GET /rest/api/content/{id}/child/attachment", ok(`{"results":[],"_links":{}}`))
	cm.HandleFunc("GET /rest/api/content", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("spaceKey") == "DEV" && q.Get("title") == "The Spec" {
			io.WriteString(w, `{"results":[{"id":"11"}]}`)
			return
		}
		io.WriteString(w, `{"results":[]}`)
	})
	cm.HandleFunc("GET /rest/api/content/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		body := ""
		if id == "20" {
			body = `<ac:structured-macro ac:name=\"jira\"><ac:parameter ac:name=\"key\">P-30</ac:parameter></ac:structured-macro>` +
				`<a href=\"` + jsrv.URL + `/browse/P-31\">P-31</a>` +
				`<ac:structured-macro ac:name=\"jira\"><ac:parameter ac:name=\"maximumIssues\">2</ac:parameter>` +
				`<ac:parameter ac:name=\"jqlQuery\">issue in linkedIssues(&quot;P-1&quot;)  </ac:parameter></ac:structured-macro>` +
				`<ac:structured-macro ac:name=\"jira\"><ac:parameter ac:name=\"jqlQuery\">project = OTHER</ac:parameter></ac:structured-macro>`
		}
		io.WriteString(w, page(id, body))
	})
	csrv = httptest.NewServer(cm)
	t.Cleanup(csrv.Close)

	jcfg := &config.AtlassianConfig{URL: jsrv.URL, Token: "x"}
	ccfg := &config.AtlassianConfig{URL: csrv.URL, Token: "x"}
	e := &Exporter{Jira: jira.NewClient(jcfg), JiraCfg: jcfg, Confluence: confluence.NewClient(ccfg), ConfCfg: ccfg}
	out := filepath.Join(t.TempDir(), "m.tar.gz")
	m, err := e.Export(context.Background(), ExportOptions{
		Out: out, Log: io.Discard, JQL: []string{"key = P-1"}, Pages: []string{"20"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"10", "11", "20"} {
		if !e.pageDone[id] {
			t.Errorf("page %s not exported", id)
		}
	}
	if e.pageDone["12"] {
		t.Error("page 12 is linked from an issue that came in through a page: must not be exported")
	}
	for _, k := range []string{"P-1", "P-30", "P-31", "P-40", "P-41"} {
		if !e.done[k] {
			t.Errorf("issue %s not exported", k)
		}
	}
	if e.done["P-32"] {
		t.Error("P-32 is a relative of an issue on a page: must not be exported")
	}
	if m.Issues != 5 || m.Pages != 3 {
		t.Errorf("manifest counts: %d issues, %d pages", m.Issues, m.Pages)
	}
	if listMax != "2" {
		t.Errorf("issue list fetched %s issues, want the macro's maximumIssues 2", listMax)
	}
	// The page's issue lists as Jira answered them, failures included.
	dir := t.TempDir()
	if _, err := Import(out, dir, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	var lists []xref.QueryResult
	if err := readJSONFile(filepath.Join(dir, "confluence", "pages", "20", "jira-queries.json"), &lists); err != nil {
		t.Fatal(err)
	}
	if len(lists) != 2 || lists[0].JQL != `issue in linkedIssues("P-1")` || strings.Join(lists[0].Keys, ",") != "P-41,P-40" ||
		lists[0].Total != 5 || lists[0].Error != "" || lists[1].Error == "" || len(lists[1].Keys) != 0 {
		t.Errorf("jira-queries.json: %+v", lists)
	}
}

func TestMentionedUsers(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/rest/api/user" || r.URL.Query().Get("key") != "k1" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"userKey":"k1","username":"jdoe","displayName":"Doe, Jane","email":"jane@example.com"}`)
	}))
	t.Cleanup(srv.Close)
	cfg := &config.AtlassianConfig{URL: srv.URL, Token: "x"}
	e := &Exporter{Confluence: confluence.NewClient(cfg), ConfCfg: cfg, opts: ExportOptions{Log: io.Discard}, users: map[htmlconv.UserRef]map[string]any{}}
	storage := `<ac:link><ri:user ri:userkey="k1" /></ac:link> <ac:link><ri:user ri:userkey="gone" /></ac:link>`
	for range 2 { // the second page reuses the lookups
		users := e.mentionedUsers(context.Background(), "1", storage)
		if len(users) != 1 || users[0]["email"] != "jane@example.com" {
			t.Fatalf("users: %v", users)
		}
	}
	if calls != 2 {
		t.Errorf("%d lookups, want 2", calls)
	}
}
