package xref

import (
	"reflect"
	"testing"
)

const conf = "https://wiki.example.com"

func TestPageRefs(t *testing.T) {
	text := `See [design|https://wiki.example.com/pages/viewpage.action?pageId=123] and
<a href="https://wiki.example.com/pages/viewpage.action?spaceKey=DEV&amp;title=Release+Notes">notes</a>,
https://wiki.example.com/spaces/DEV/pages/456/Some+Page#Heading, https://wiki.example.com/display/DEV/C%2B%2B+Style
and https://wiki.example.com/x/ewAAAAAAAAA plus https://wiki.example.com/x/ew.
Not these: https://other.example.com/pages/viewpage.action?pageId=9 https://wiki.example.com/download/attachments/5/a.png
https://wiki.example.com/display/DEV https://wiki.example.com/pages/viewpage.action?pageId=123`
	got := PageRefs(text, conf+"/")
	want := []PageRef{
		{ID: "123", URL: conf + "/pages/viewpage.action?pageId=123"},
		{Space: "DEV", Title: "Release Notes", URL: conf + "/pages/viewpage.action?spaceKey=DEV&title=Release+Notes"},
		{ID: "456", URL: conf + "/spaces/DEV/pages/456/Some+Page#Heading"},
		{Space: "DEV", Title: "C++ Style", URL: conf + "/display/DEV/C%2B%2B+Style"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PageRefs:\n got %+v\nwant %+v (and the tiny link 123 de-duplicated)", got, want)
	}
}

func TestPageRefsCloudBase(t *testing.T) {
	// Cloud: Confluence lives under /wiki on the Jira host; Jira links are
	// not pages.
	text := "https://acme.atlassian.net/wiki/spaces/ENG/pages/77 https://acme.atlassian.net/browse/ENG-1"
	got := PageRefs(text, "https://acme.atlassian.net/wiki")
	if len(got) != 1 || got[0].ID != "77" {
		t.Fatalf("got %+v", got)
	}
	if PageRefs(text) != nil {
		t.Fatal("no bases must find nothing")
	}
}

func TestDecodeTinyLink(t *testing.T) {
	for code, want := range map[string]string{"ew": "123", "ewAAAAAAAAA": "123", "AAAB": "65536", "QIGHAQ": "25657664"} {
		if got, ok := DecodeTinyLink(code); !ok || got != want {
			t.Errorf("DecodeTinyLink(%q) = %q, %v; want %q", code, got, ok, want)
		}
	}
	for _, bad := range []string{"", "AAAA", "ewAAAAAAAAAAA", "!!"} {
		if got, ok := DecodeTinyLink(bad); ok {
			t.Errorf("DecodeTinyLink(%q) = %q, want failure", bad, got)
		}
	}
}

func TestIssueRefs(t *testing.T) {
	storage := `<p>Tracked in <ac:structured-macro ac:name="jira" ac:schema-version="1" ac:macro-id="x">` +
		`<ac:parameter ac:name="server">Jira</ac:parameter><ac:parameter ac:name="key">dev-12</ac:parameter>` +
		`</ac:structured-macro> and <a href="https://jira.example.com/browse/DEV-7">DEV-7</a>, ` +
		`<a href="https://jira.example.com/projects/OPS/issues/OPS-3?filter=all">x</a>, ` +
		`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="jqlQuery">project = DEV</ac:parameter></ac:structured-macro>` +
		` <a href="https://other.example.com/browse/X-1">x</a> https://jira.example.com/browse/DEV-7 UTF-8</p>`
	got := IssueRefs(storage, "https://jira.example.com")
	want := []IssueRef{
		{Key: "DEV-12"},
		{Key: "DEV-7", URL: "https://jira.example.com/browse/DEV-7"},
		{Key: "OPS-3", URL: "https://jira.example.com/projects/OPS/issues/OPS-3?filter=all"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IssueRefs:\n got %+v\nwant %+v", got, want)
	}
}

func TestStrings(t *testing.T) {
	v := map[string]any{"b": []any{"x", map[string]any{"c": "y"}}, "a": "z", "n": 1.0}
	if got := Strings(v); got != "z\nx\ny\n" {
		t.Fatalf("Strings = %q", got)
	}
}

func TestIssueQueries(t *testing.T) {
	storage := `<ac:structured-macro ac:name="jira" ac:schema-version="1"><ac:parameter ac:name="server">PSS Jira</ac:parameter>` +
		`<ac:parameter ac:name="columnIds">issuekey,summary,customfield_11519</ac:parameter>` +
		`<ac:parameter ac:name="columns">key,summary,Test Execution Status</ac:parameter>` +
		`<ac:parameter ac:name="maximumIssues">1000</ac:parameter>` +
		`<ac:parameter ac:name="jqlQuery">issue in linkedIssues(&quot;A-1&quot;)   AND type = Story   </ac:parameter></ac:structured-macro>` +
		`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="key">A-2</ac:parameter></ac:structured-macro>` +
		`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="jqlQuery">project = A</ac:parameter></ac:structured-macro>` +
		`<ac:structured-macro ac:name="jira"><ac:parameter ac:name="jqlQuery">issue in linkedIssues("A-1") AND type = Story</ac:parameter></ac:structured-macro>`
	got := IssueQueries(storage)
	if len(got) != 2 {
		t.Fatalf("got %d queries: %+v", len(got), got)
	}
	want := IssueQuery{JQL: `issue in linkedIssues("A-1")   AND type = Story`, Server: "PSS Jira", Max: 1000,
		Columns: []string{"issuekey", "summary", "customfield_11519"}, Headers: []string{"key", "summary", "Test Execution Status"}}
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("query:\n got %+v\nwant %+v", got[0], want)
	}
	if got[1].Max != 20 || got[1].Columns[0] != "issuekey" || got[1].Headers[2] != "type" {
		t.Fatalf("defaults: %+v", got[1])
	}
	out := ReplaceIssueQueries(storage, func(q IssueQuery) string { return "[" + NormalizeJQL(q.JQL) + "]" })
	if want := `[issue in linkedIssues("A-1") AND type = Story]<ac:structured-macro ac:name="jira"><ac:parameter ac:name="key">A-2</ac:parameter></ac:structured-macro>[project = A][issue in linkedIssues("A-1") AND type = Story]`; out != want {
		t.Fatalf("ReplaceIssueQueries:\n got %s\nwant %s", out, want)
	}
}
