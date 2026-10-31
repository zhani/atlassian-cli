package jira

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var testLinkTypes = []IssueLinkType{
	{Name: "Blocks", Outward: "blocks", Inward: "is blocked by"},
	{Name: "Implements", Outward: "implements", Inward: "is implemented by"},
}

func TestResolveLinkType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ref      string
		wantName string
		wantSwap bool
		wantErr  bool
	}{
		{ref: "Blocks", wantName: "Blocks"},
		{ref: "blocks", wantName: "Blocks"},
		{ref: "is blocked by", wantName: "Blocks", wantSwap: true},
		{ref: "IS IMPLEMENTED BY", wantName: "Implements", wantSwap: true},
		{ref: "relates", wantErr: true},
	}
	for _, tt := range tests {
		lt, swap, err := ResolveLinkType(testLinkTypes, tt.ref)
		if (err != nil) != tt.wantErr {
			t.Fatalf("%q: err = %v", tt.ref, err)
		}
		if lt.Name != tt.wantName || swap != tt.wantSwap {
			t.Errorf("%q: got %s swap=%v, want %s swap=%v", tt.ref, lt.Name, swap, tt.wantName, tt.wantSwap)
		}
	}
}

func TestResolveFieldIDs(t *testing.T) {
	t.Parallel()
	fields := []Field{{ID: "summary", Name: "Summary"}, {ID: "customfield_10010", Name: "Story Points", Custom: true}}
	got, err := ResolveFieldIDs(fields, []string{"story points", "SUMMARY", "customfield_10010"})
	if err != nil {
		t.Fatalf("ResolveFieldIDs() error: %v", err)
	}
	if diff := cmp.Diff([]string{"customfield_10010", "summary", "customfield_10010"}, got); diff != "" {
		t.Errorf("diff (-want +got):\n%s", diff)
	}
	if _, err := ResolveFieldIDs(fields, []string{"Nope"}); err == nil {
		t.Error("unknown field should fail")
	}
}

func TestCloneFields(t *testing.T) {
	t.Parallel()
	src := func() map[string]any {
		return map[string]any{
			"project":     map[string]any{"key": "P", "id": "1", "self": "x"},
			"issuetype":   map[string]any{"id": "10", "name": "Bug"},
			"priority":    map[string]any{"name": "High"},
			"summary":     "Crash in v1",
			"description": "Seen in v1",
			"labels":      []any{"a"},
			"components":  []any{map[string]any{"id": "7", "name": "UI"}},
			"assignee":    map[string]any{"name": "jdoe", "accountId": "acc"},
			"parent":      nil,
		}
	}

	got := cloneFields(src(), CloneOptions{Replace: [][2]string{{"v1", "v2"}}}, false)
	want := map[string]any{
		"project":     map[string]string{"key": "P"},
		"issuetype":   map[string]string{"id": "10"},
		"priority":    map[string]string{"name": "High"},
		"summary":     "Crash in v2",
		"description": "Seen in v2",
		"labels":      []any{"a"},
		"components":  []map[string]string{{"id": "7"}},
		"assignee":    map[string]any{"name": "jdoe"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("cloneFields() diff (-want +got):\n%s", diff)
	}

	got = cloneFields(src(), CloneOptions{Summary: "New", Priority: "Low", Assignee: "acc2", Labels: []string{"b"}}, true)
	for k, v := range map[string]any{
		"summary":  "New",
		"priority": map[string]string{"name": "Low"},
		"assignee": map[string]any{"accountId": "acc2"},
		"labels":   []string{"b"},
	} {
		if diff := cmp.Diff(v, got[k]); diff != "" {
			t.Errorf("override %s diff (-want +got):\n%s", k, diff)
		}
	}
}

// recorder serves canned JSON per "METHOD path" and records request bodies.
type recorder struct {
	mu       sync.Mutex
	routes   map[string]string
	requests []string
}

func (rec *recorder) server(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.requests = append(rec.requests, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		resp, ok := rec.routes[r.Method+" "+r.URL.Path]
		rec.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, resp)
	}))
}

func TestResolveUser(t *testing.T) {
	t.Parallel()
	rec := &recorder{routes: map[string]string{
		"GET /rest/api/2/user/search": `[{"name":"jdoe","displayName":"John Doe"},{"name":"jdoe2","displayName":"John Doe Jr"}]`,
		"GET /rest/api/2/myself":      `{"name":"me","displayName":"Me"}`,
	}}
	srv := rec.server(t)
	defer srv.Close()
	c := newTestJiraClient(srv.URL) // Server/DC

	u, err := c.ResolveUser(context.Background(), "JDOE")
	if err != nil || u.Name != "jdoe" {
		t.Errorf("exact username match: %+v, %v", u, err)
	}
	if _, err := c.ResolveUser(context.Background(), "john"); !errors.Is(err, ErrAmbiguousUser) {
		t.Errorf("ambiguous match: err = %v", err)
	}
	if u, err := c.ResolveUser(context.Background(), "me"); err != nil || u.Name != "me" {
		t.Errorf("me: %+v, %v", u, err)
	}
	if got := rec.requests[0]; got != "GET /rest/api/2/user/search?username=JDOE " {
		t.Errorf("Server/DC search request = %q", got)
	}
}

func TestLinkAndUnlink(t *testing.T) {
	t.Parallel()
	rec := &recorder{routes: map[string]string{
		"GET /rest/api/2/issue/A-1": `{"fields":{"issuelinks":[
			{"id":"100","type":{"name":"Blocks"},"outwardIssue":{"key":"A-2"}},
			{"id":"101","type":{"name":"Relates"},"inwardIssue":{"key":"A-2"}},
			{"id":"102","type":{"name":"Blocks"},"outwardIssue":{"key":"A-3"}}]}}`,
	}}
	srv := rec.server(t)
	defer srv.Close()
	c := newTestJiraClient(srv.URL)
	ctx := context.Background()

	if err := c.LinkIssues(ctx, "Blocks", "A-1", "A-2"); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(rec.requests[0][len("POST /rest/api/2/issueLink "):]), &body)
	want := map[string]any{
		"type":         map[string]any{"name": "Blocks"},
		"inwardIssue":  map[string]any{"key": "A-1"},
		"outwardIssue": map[string]any{"key": "A-2"},
	}
	if diff := cmp.Diff(want, body); diff != "" {
		t.Errorf("link body diff (-want +got):\n%s", diff)
	}

	n, err := c.UnlinkIssues(ctx, "A-1", "a-2", "")
	if err != nil || n != 2 {
		t.Errorf("unlink all: n=%d err=%v", n, err)
	}
	n, err = c.UnlinkIssues(ctx, "A-1", "A-2", "relates")
	if err != nil || n != 1 {
		t.Errorf("unlink by type: n=%d err=%v", n, err)
	}
	if _, err := c.UnlinkIssues(ctx, "A-1", "A-9", ""); err == nil {
		t.Error("unlinking unrelated issues should fail")
	}
}

func TestGetDevInfo(t *testing.T) {
	t.Parallel()
	rec := &recorder{routes: map[string]string{
		"GET /rest/api/2/issue/A-1": `{"id":"5001","key":"A-1"}`,
		"GET /rest/dev-status/latest/issue/summary": `{"summary":{
			"pullrequest":{"byInstanceType":{"stash":{"count":1}}},
			"branch":{"byInstanceType":{"stash":{"count":1},"github":{"count":0}}}}}`,
		"GET /rest/dev-status/latest/issue/detail": `{"detail":[{
			"pullRequests":[{"id":"#7","name":"Fix","status":"OPEN","url":"http://pr/7","author":{"name":"Ann"},
				"source":{"branch":"feature/A-1"},"destination":{"branch":"main"},"repositoryName":"app"}],
			"branches":[{"name":"feature/A-1","url":"http://br","repository":{"name":"app"}}]}]}`,
	}}
	srv := rec.server(t)
	defer srv.Close()

	got, err := newTestJiraClient(srv.URL).GetDevInfo(context.Background(), "A-1")
	if err != nil {
		t.Fatalf("GetDevInfo() error: %v", err)
	}
	want := &DevInfo{
		PullRequests: []PullRequest{{ID: "#7", Name: "Fix", Status: "OPEN", URL: "http://pr/7", Author: "Ann", Source: "feature/A-1", Destination: "main", Repository: "app"}},
		Branches:     []Branch{{Name: "feature/A-1", URL: "http://br", Repository: "app"}},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("GetDevInfo() diff (-want +got):\n%s", diff)
	}
	last := rec.requests[len(rec.requests)-1]
	if want := "GET /rest/dev-status/latest/issue/detail?applicationType=stash&dataType=branch&issueId=5001 "; last != want {
		t.Errorf("detail request = %q, want %q", last, want)
	}
}
