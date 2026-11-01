package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestListSprintsPaginatesAndSorts(t *testing.T) {
	t.Parallel()
	pages := []string{
		`{"values":[{"id":3,"name":"S3","state":"future"},{"id":2,"name":"S2","state":"active","startDate":"2026-02-01"}],"isLast":false}`,
		`{"values":[{"id":1,"name":"S1","state":"closed","startDate":"2026-01-01"}],"isLast":true}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/agile/1.0/board/7/sprint" || r.URL.Query().Get("state") != "active,closed,future" {
			t.Errorf("request = %s", r.URL)
		}
		start, _ := strconv.Atoi(r.URL.Query().Get("startAt"))
		w.Header().Set("Content-Type", "application/json")
		if start == 0 {
			w.Write([]byte(pages[0]))
		} else {
			w.Write([]byte(pages[1]))
		}
	}))
	defer srv.Close()

	got, err := newTestJiraClient(srv.URL).ListSprints(context.Background(), 7, "active", "closed", "future")
	if err != nil {
		t.Fatalf("ListSprints() error: %v", err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	if diff := cmp.Diff([]string{"S1", "S2", "S3"}, names); diff != "" {
		t.Errorf("order diff (-want +got):\n%s", diff)
	}
}

func TestEpicChildrenJQL(t *testing.T) {
	t.Parallel()
	c := newTestJiraClient("http://x")
	if got := c.EpicChildrenJQL("E-1", ""); got != `"Epic Link" = "E-1" ORDER BY rank ASC` {
		t.Errorf("server default = %s", got)
	}
	c.cloud = true
	if got := c.EpicChildrenJQL("E-1", ""); got != `parent = "E-1" ORDER BY rank ASC` {
		t.Errorf("cloud default = %s", got)
	}
	if got := c.EpicChildrenJQL("E-1", "cf[10014]"); got != `cf[10014] = "E-1" ORDER BY rank ASC` {
		t.Errorf("override = %s", got)
	}
}

func TestLinkedChildren(t *testing.T) {
	t.Parallel()
	issue := &Issue{Fields: IssueFields{IssueLinks: []IssueLink{
		{Type: IssueLinkType{Inward: "is implemented by", Outward: "implements"}, InwardIssue: &Issue{Key: "S-1"}},
		{Type: IssueLinkType{Inward: "gets values from", Outward: "gives values to"}, InwardIssue: &Issue{Key: "S-2"}},
		{Type: IssueLinkType{Inward: "is blocked by", Outward: "blocks"}, OutwardIssue: &Issue{Key: "S-3"}},
		{Type: IssueLinkType{Inward: "is implemented by", Outward: "implements"}, OutwardIssue: &Issue{Key: "F-0"}},
	}}}
	keys, rel := LinkedChildren(issue, []string{"Is Implemented By", "gets values from"})
	if diff := cmp.Diff([]string{"S-1", "S-2"}, keys); diff != "" {
		t.Errorf("keys diff (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(map[string]string{"S-1": "is implemented by", "S-2": "gets values from"}, rel); diff != "" {
		t.Errorf("relations diff (-want +got):\n%s", diff)
	}
	if got := KeysJQL(keys); got != `key in ("S-1", "S-2") ORDER BY key ASC` {
		t.Errorf("KeysJQL = %s", got)
	}
	if KeysJQL(nil) != "" {
		t.Error("no keys should give empty JQL")
	}
}

func TestGetSprint(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/agile/1.0/sprint/42" {
			t.Errorf("request = %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":42,"name":"Sprint 42","state":"active"}`))
	}))
	defer srv.Close()
	got, err := newTestJiraClient(srv.URL).GetSprint(context.Background(), 42)
	if err != nil || got.Name != "Sprint 42" || got.State != "active" {
		t.Errorf("GetSprint() = %+v, %v", got, err)
	}
}
