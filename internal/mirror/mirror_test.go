package mirror_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zhani/atlassian-cli/internal/config"
	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/jira"
	"github.com/zhani/atlassian-cli/internal/mirror"
)

const origJira = "https://jira.example.com/jira"
const origConf = "https://wiki.example.com"

func write(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var data []byte
	switch v := v.(type) {
	case string:
		data = []byte(v)
	default:
		var err error
		if data, err = json.Marshal(v); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func issue(key, summary, status, typ, prio, assignee, created string, extra map[string]any) map[string]any {
	f := map[string]any{
		"summary": summary, "project": map[string]any{"key": strings.Split(key, "-")[0], "name": "Project"},
		"status": map[string]any{"name": status}, "issuetype": map[string]any{"name": typ},
		"created": created, "updated": created,
	}
	if prio != "" {
		f["priority"] = map[string]any{"name": prio}
	}
	if assignee != "" {
		f["assignee"] = map[string]any{"name": assignee, "displayName": strings.ToUpper(assignee)}
	}
	for k, v := range extra {
		f[k] = v
	}
	return map[string]any{"id": "1" + strings.Split(key, "-")[1], "key": key, "self": origJira + "/rest/api/2/issue/" + key, "fields": f}
}

// fixture builds a mirror directory and returns it.
func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	now := time.Now().UTC()
	// "Recent" must stay within this week for startOfWeek() queries: two days
	// ago fell into last week on Mondays and Tuesdays.
	recent := now.Add(-time.Second).Format("2006-01-02T15:04:05.000-0700")
	old := now.AddDate(0, -3, 0).Format("2006-01-02T15:04:05.000-0700")
	write(t, filepath.Join(dir, "mirror.json"), mirror.Manifest{Version: 1, ExportedAt: now.Format(time.RFC3339), JiraURL: origJira, ConfluenceURL: origConf})
	write(t, filepath.Join(dir, "jira", "myself.json"), map[string]any{"name": "me", "displayName": "Me"})
	write(t, filepath.Join(dir, "jira", "names.json"), map[string]string{"customfield_1": "Epic Link", "customfield_2": "Sprint", "customfield_3": "Rank", "customfield_4": "Steps to Reproduce"})
	write(t, filepath.Join(dir, "jira", "boards", "7.json"), map[string]any{
		"board":   map[string]any{"id": 7, "name": "Team", "type": "scrum", "location": map[string]any{"projectKey": "PROJ"}},
		"sprints": []any{map[string]any{"id": 41, "name": "S41", "state": "closed"}, map[string]any{"id": 42, "name": "S42", "state": "active"}},
	})
	issues := []map[string]any{
		issue("PROJ-1", "Login crashes on empty password", "In Progress", "Bug", "High", "me", recent, map[string]any{
			"description":   "Crash in AuthService when the password is empty.",
			"labels":        []any{"backend", "auth"},
			"customfield_1": "PROJ-3",
			"customfield_2": []any{"com.atlassian.greenhopper.service.sprint.Sprint@1[id=42,rapidViewId=7,state=ACTIVE,name=S42,startDate=x]"},
			"customfield_3": "0|i0001",
			"customfield_4": "1. open login",
			"comment":       map[string]any{"comments": []any{map[string]any{"id": "9", "body": "Reproduced on staging"}}, "total": 1},
			"attachment":    []any{map[string]any{"id": "500", "filename": "shot.png", "content": origJira + "/secure/attachment/500/shot.png"}},
			"issuelinks": []any{map[string]any{"type": map[string]any{"name": "Blocks", "inward": "is blocked by", "outward": "blocks"},
				"outwardIssue": map[string]any{"key": "PROJ-2"}}},
		}),
		issue("PROJ-2", "Add dark mode", "Open", "Story", "Low", "", old, map[string]any{
			"labels": []any{"ui"}, "customfield_1": "PROJ-3", "customfield_3": "0|i0000",
			"customfield_2": []any{map[string]any{"id": 41, "name": "S41", "state": "closed"}},
		}),
		issue("PROJ-3", "Auth epic", "Done", "Epic", "", "bob", old, map[string]any{"resolution": map[string]any{"name": "Done"}}),
		issue("PROJ-10", "Feature: SSO", "Open", "New Feature", "Medium", "", recent, map[string]any{
			"issuelinks": []any{map[string]any{"type": map[string]any{"name": "Impl", "inward": "implements", "outward": "is implemented by"},
				"outwardIssue": map[string]any{"key": "PROJ-1"}}},
		}),
	}
	for _, is := range issues {
		write(t, filepath.Join(dir, "jira", "issues", is["key"].(string), "issue.json"), is)
	}
	write(t, filepath.Join(dir, "jira", "issues", "PROJ-1", "attachments", "500"), "PNGBYTES")

	space := map[string]any{"key": "DOC", "name": "Docs"}
	page := func(id, title, body string, ancestors ...string) map[string]any {
		var anc []any
		for _, a := range ancestors {
			anc = append(anc, map[string]any{"id": a})
		}
		return map[string]any{"id": id, "type": "page", "title": title, "space": space, "ancestors": anc,
			"version": map[string]any{"number": 2, "when": recent},
			"body":    map[string]any{"storage": map[string]any{"value": body}}}
	}
	write(t, filepath.Join(dir, "confluence", "pages", "100", "page.json"), page("100", "Architecture", `<p>See <ac:image><ri:url ri:value="`+origConf+`/images/logo.png" /></ac:image></p>`, "1"))
	write(t, filepath.Join(dir, "confluence", "pages", "101", "page.json"), page("101", "Login flow", "<p>The password check happens in AuthService.</p>", "1", "100"))
	write(t, filepath.Join(dir, "confluence", "pages", "100", "attachments.json"), []any{map[string]any{"id": "att1", "title": "diagram.png",
		"_links": map[string]any{"download": "/download/attachments/100/diagram.png?version=1"}}})
	write(t, filepath.Join(dir, "confluence", "pages", "100", "files", "att1"), "DIAGRAM")
	write(t, filepath.Join(dir, "confluence", "pages", "100", "urls.json"), map[string]string{"/images/logo.png": "logo"})
	write(t, filepath.Join(dir, "confluence", "pages", "100", "urls", "logo"), "LOGO")
	// A local addition overrides nothing but adds an issue.
	write(t, filepath.Join(dir, "local", "jira", "issues", "MINE-1", "issue.json"), issue("MINE-1", "My notes", "Open", "Task", "", "", recent, nil))
	return dir
}

func keys(issues []map[string]any) []string {
	var out []string
	for _, is := range issues {
		out = append(out, is["key"].(string))
	}
	return out
}

func TestSearchJQL(t *testing.T) {
	s, err := mirror.Load(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	emitted := func(o jira.ListOptions) string {
		q, err := jira.BuildJQL(o)
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	tests := []struct {
		jql  string
		want []string // nil: just must not fail
	}{
		{"project = PROJ ORDER BY key ASC", []string{"PROJ-1", "PROJ-2", "PROJ-3", "PROJ-10"}},
		{"", []string{"PROJ-10", "PROJ-3", "PROJ-2", "PROJ-1", "MINE-1"}},
		{`text ~ "authservice"`, []string{"PROJ-1"}},
		{`comment ~ staging`, []string{"PROJ-1"}},
		{`summary ~ "crash*"`, []string{"PROJ-1"}},
		{`summary !~ crash AND project = PROJ ORDER BY key`, []string{"PROJ-2", "PROJ-3", "PROJ-10"}},
		{`labels in (ui, auth) ORDER BY key`, []string{"PROJ-1", "PROJ-2"}},
		{`labels not in (ui) ORDER BY key`, []string{"PROJ-1"}},
		{`(status = Open OR status = "In Progress") AND NOT issuetype = Story ORDER BY key`, []string{"MINE-1", "PROJ-1", "PROJ-10"}},
		{`assignee = currentUser()`, []string{"PROJ-1"}},
		{`assignee IS EMPTY AND project = PROJ ORDER BY key`, []string{"PROJ-2", "PROJ-10"}},
		{`resolution = Unresolved AND project = PROJ ORDER BY key`, []string{"PROJ-1", "PROJ-2", "PROJ-10"}},
		{`resolution IS NOT EMPTY`, []string{"PROJ-3"}},
		{`created >= -7d AND project = PROJ ORDER BY key`, []string{"PROJ-1", "PROJ-10"}},
		{`created < startOfMonth(-1M) ORDER BY key`, []string{"PROJ-2", "PROJ-3"}},
		{`"Epic Link" = PROJ-3 ORDER BY rank ASC`, []string{"PROJ-2", "PROJ-1"}},
		{`cf[1] = "PROJ-3" ORDER BY key`, []string{"PROJ-1", "PROJ-2"}},
		{`"Steps to Reproduce" ~ login`, []string{"PROJ-1"}},
		{`sprint = 42`, []string{"PROJ-1"}},
		{`sprint in openSprints()`, []string{"PROJ-1"}},
		{`sprint in closedSprints()`, []string{"PROJ-2"}},
		{`sprint = S41`, []string{"PROJ-2"}},
		{`issue in linkedIssues(PROJ-1) ORDER BY key`, []string{"PROJ-2", "PROJ-10"}},
		{`issue in linkedIssues(PROJ-10, "is implemented by")`, []string{"PROJ-1"}},
		{`key in ("PROJ-2", "PROJ-1") ORDER BY key ASC`, []string{"PROJ-1", "PROJ-2"}},
		{`project = PROJ ORDER BY priority DESC, key ASC`, []string{"PROJ-1", "PROJ-10", "PROJ-2", "PROJ-3"}},
		{emitted(jira.ListOptions{Project: "PROJ", Status: "~Done", Assignee: "me", Labels: []string{"backend", "~ui"}, Created: "week", Unresolved: true}), []string{"PROJ-1"}},
		{emitted(jira.ListOptions{Assignee: "x", Reporter: "~me", Type: "~Epic", Priority: "Low", Resolution: "~Unresolved", Updated: "-2w", Watching: true, JQL: "text ~ x", OrderBy: "updated", Reverse: true}), nil},
		{(&jira.Client{}).EpicChildrenJQL("PROJ-3", ""), []string{"PROJ-2", "PROJ-1"}},
		{jira.KeysJQL([]string{"PROJ-2", "PROJ-10"}), []string{"PROJ-2", "PROJ-10"}},
		{"sprint = 42 ORDER BY rank ASC", []string{"PROJ-1"}},
	}
	for _, tt := range tests {
		got, err := s.SearchJQL(tt.jql)
		if err != nil {
			t.Errorf("%q: %v", tt.jql, err)
			continue
		}
		if tt.want != nil && !slices.Equal(keys(got), tt.want) {
			t.Errorf("%q = %v, want %v", tt.jql, keys(got), tt.want)
		}
	}
	for _, bad := range []string{"nosuchfield = 1", "status was Open", "assignee in membersOf(x)", "status = ", `summary ~ "open`} {
		if _, err := s.SearchJQL(bad); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestSearchCQL(t *testing.T) {
	s, err := mirror.Load(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for cql, want := range map[string][]string{
		`type = page AND space = "DOC" AND text ~ "authservice"`: {"101"},
		`title ~ arch`:                            {"100"},
		`ancestor = 100`:                          {"101"},
		`type = page ORDER BY title DESC`:         {"101", "100"},
		`lastmodified >= now("-7d") AND id = 100`: {"100"},
	} {
		got, err := s.SearchCQL(cql)
		if err != nil {
			t.Errorf("%q: %v", cql, err)
			continue
		}
		var ids []string
		for _, p := range got {
			ids = append(ids, p["id"].(string))
		}
		if !slices.Equal(ids, want) {
			t.Errorf("%q = %v, want %v", cql, ids, want)
		}
	}
}

// TestClientsThroughMirror drives the real clients through the in-process
// transport, configured only by ATLASSIAN_MIRROR.
func TestClientsThroughMirror(t *testing.T) {
	dir := fixture(t)
	for _, v := range []string{"JIRA_URL", "CONFLUENCE_URL", "ATLASSIAN_URL", "JIRA_PERSONAL_TOKEN"} {
		t.Setenv(v, "")
	}
	t.Setenv("ATLASSIAN_MIRROR", dir)
	ctx := context.Background()

	jcfg, err := config.LoadJiraConfig()
	if err != nil {
		t.Fatal(err)
	}
	jc := jira.NewClient(jcfg)
	found, err := jc.SearchIssuesAll(ctx, "project = PROJ ORDER BY key ASC", nil, 0)
	if err != nil || len(found) != 4 {
		t.Fatalf("search: %d issues, %v", len(found), err)
	}
	is, err := jc.GetIssueRendered(ctx, "PROJ-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(is.Fields.Attachment) != 1 || !strings.HasPrefix(is.Fields.Attachment[0].Content, mirror.JiraBase+"/secure/attachment/500/") {
		t.Fatalf("attachment not rewritten to the mirror: %+v", is.Fields.Attachment)
	}
	body, err := jc.DownloadAttachment(ctx, is.Fields.Attachment[0].Content)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(body)
	body.Close()
	if string(data) != "PNGBYTES" {
		t.Fatalf("attachment = %q", data)
	}
	comments, err := jc.ListComments(ctx, "PROJ-1")
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments: %v %v", comments, err)
	}
	if _, err := jc.GetIssue(ctx, "MINE-1"); err != nil {
		t.Fatalf("local/ issue: %v", err)
	}
	boards, err := jc.ListBoards(ctx, "PROJ")
	if err != nil || len(boards) != 1 {
		t.Fatalf("boards: %v %v", boards, err)
	}
	if err := jc.DoTransition(ctx, "PROJ-1", "1"); err == nil || !strings.Contains(err.Error(), "read-only mirror") {
		t.Fatalf("transition: want read-only error, got %v", err)
	}

	ccfg, err := config.LoadConfluenceConfig()
	if err != nil {
		t.Fatal(err)
	}
	cc := confluence.NewClient(ccfg)
	roots, err := cc.RootPages(ctx, "DOC", 0)
	if err != nil || len(roots) != 1 || roots[0].ID != "100" {
		t.Fatalf("roots: %+v %v", roots, err)
	}
	kids, err := cc.GetChildren(ctx, "100", 0)
	if err != nil || len(kids) != 1 {
		t.Fatalf("children: %+v %v", kids, err)
	}
	p, err := cc.GetPage(ctx, "100", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Body.Storage.Value, mirror.ConfluenceBase+"/images/logo.png") {
		t.Fatalf("image URL not rewritten: %s", p.Body.Storage.Value)
	}
	for path, want := range map[string]string{"/images/logo.png": "LOGO", "/download/attachments/100/diagram.png?version=1": "DIAGRAM"} {
		rc, err := cc.DownloadAttachment(ctx, path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		if string(data) != want {
			t.Fatalf("%s = %q", path, data)
		}
	}
	atts, err := cc.GetAttachments(ctx, "100")
	if err != nil || len(atts) != 1 {
		t.Fatalf("attachments: %v %v", atts, err)
	}
}
