package mirrorsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zhani/atlassian-cli/internal/bookmarks"
	"github.com/zhani/atlassian-cli/internal/mirror"
)

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// archive builds a mirror archive with the given issues (key -> summary,
// each with one attachment named after the summary).
func archive(t *testing.T, exportedAt, since string, issues map[string]string, bm string) string {
	t.Helper()
	dir := t.TempDir()
	m := mirror.Manifest{Version: 1, ExportedAt: exportedAt, Since: since}
	data, _ := json.Marshal(m)
	put(t, filepath.Join(dir, "mirror.json"), string(data))
	for key, summary := range issues {
		is, _ := json.Marshal(map[string]any{"key": key, "fields": map[string]any{"summary": summary}})
		put(t, filepath.Join(dir, "jira", "issues", key, "issue.json"), string(is))
		put(t, filepath.Join(dir, "jira", "issues", key, "attachments", summary), summary)
	}
	put(t, filepath.Join(dir, "jira", "names.json"), `{"customfield_1":"Epic Link"}`)
	if bm != "" {
		put(t, filepath.Join(dir, "bookmarks.yaml"), bm)
	}
	out := filepath.Join(t.TempDir(), "m.tar.gz")
	if err := writeTarGz(dir, out); err != nil {
		t.Fatal(err)
	}
	return out
}

func attachments(t *testing.T, dir, key string) []string {
	entries, _ := os.ReadDir(filepath.Join(dir, "jira", "issues", key, "attachments"))
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestImportUpsertAndPrune(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mirror")
	store := &bookmarks.Store{Path: filepath.Join(t.TempDir(), "bookmarks.yaml")}
	if err := store.Add(bookmarks.Bookmark{Kind: bookmarks.Issue, Key: "MINE-9", Name: "mine"}); err != nil {
		t.Fatal(err)
	}
	bm := "bookmarks:\n  - kind: issue\n    key: A-1\n  - kind: issue\n    key: MINE-9\n    name: theirs\n  - kind: query\n    jql: project = A\n"

	res, err := Import(archive(t, "2026-10-01T00:00:00Z", "", map[string]string{"A-1": "one", "A-2": "two"}, bm), dir, ImportOptions{Bookmarks: store})
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 2 || res.Updated != 0 || !res.Full || res.Bookmarks != 2 {
		t.Fatalf("first import: %+v", res)
	}
	list, _ := store.List()
	if len(list) != 3 || list[0].Name != "mine" {
		t.Fatalf("bookmarks not union-merged keeping local ones: %+v", list)
	}
	put(t, filepath.Join(dir, "local", "jira", "issues", "L-1", "issue.json"), `{"key":"L-1","fields":{}}`)

	// Incremental: A-1 changed (attachment replaced), A-3 new; A-2 kept.
	res, err = Import(archive(t, "2026-10-05T00:00:00Z", "2026-10-01T00:00:00Z", map[string]string{"A-1": "uno", "A-3": "three"}, ""), dir, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.New != 1 || res.Updated != 1 || res.Kept != 1 || res.Full {
		t.Fatalf("incremental import: %+v", res)
	}
	if got := attachments(t, dir, "A-1"); !slices.Equal(got, []string{"uno"}) {
		t.Fatalf("A-1 attachments = %v, want the replaced set", got)
	}
	if _, err := Import(archive(t, "2026-10-06T00:00:00Z", "2026-10-05T00:00:00Z", nil, ""), dir, ImportOptions{Prune: true}); err == nil {
		t.Fatal("--prune with an incremental archive must fail")
	}

	// An older full snapshot without A-2/A-3, pruned.
	res, err = Import(archive(t, "2026-10-03T00:00:00Z", "", map[string]string{"A-1": "uno"}, ""), dir, ImportOptions{Prune: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Pruned, []string{"jira/issues/A-2", "jira/issues/A-3"}) || res.Updated != 1 {
		t.Fatalf("pruning import: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "local", "jira", "issues", "L-1", "issue.json")); err != nil {
		t.Fatal("local/ must survive a prune")
	}
	m, err := mirror.ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.ExportedAt != "2026-10-05T00:00:00Z" || m.LastFullAt != "2026-10-03T00:00:00Z" || m.Issues != 2 {
		t.Fatalf("manifest: %+v", m)
	}
	entries, _ := os.ReadDir(filepath.Dir(dir))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".mirror-import-") {
			t.Fatalf("staging dir left behind: %s", e.Name())
		}
	}
	entries, _ = os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".trash-") {
			t.Fatalf("trash left behind: %s", e.Name())
		}
	}
}

func TestExtractRejectsEscapes(t *testing.T) {
	src := t.TempDir()
	put(t, filepath.Join(src, "mirror.json"), `{"version":1}`)
	out := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := writeTarGz(src, out); err != nil {
		t.Fatal(err)
	}
	if err := extractTarGz(out, t.TempDir()); err != nil {
		t.Fatalf("clean archive: %v", err)
	}
	if !filepath.IsLocal("a/b") || filepath.IsLocal("../x") {
		t.Fatal("filepath.IsLocal behaves unexpectedly")
	}
}

// sideArchive builds an archive covering the given sides, with issues and
// pages (by key / id).
func sideArchive(t *testing.T, m mirror.Manifest, issues, pages []string) string {
	t.Helper()
	dir := t.TempDir()
	m.Version = 1
	data, _ := json.Marshal(m)
	put(t, filepath.Join(dir, "mirror.json"), string(data))
	for _, key := range issues {
		put(t, filepath.Join(dir, "jira", "issues", key, "issue.json"), `{"key":"`+key+`","fields":{}}`)
	}
	for _, id := range pages {
		put(t, filepath.Join(dir, "confluence", "pages", id, "page.json"), `{"id":"`+id+`"}`)
	}
	out := filepath.Join(t.TempDir(), "m.tar.gz")
	if err := writeTarGz(dir, out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestImportOneSideKeepsTheOther(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mirror")
	const jiraURL, confURL = "https://jira.example", "https://wiki.example"
	both := mirror.Manifest{ExportedAt: "2026-10-01T00:00:00Z", JiraURL: jiraURL, ConfluenceURL: confURL, Boards: []int{7}}
	if _, err := Import(sideArchive(t, both, []string{"A-1", "A-2"}, []string{"10", "11"}), dir, ImportOptions{}); err != nil {
		t.Fatal(err)
	}

	// A full Jira-only snapshot without A-2, pruned: the pages and the
	// Confluence URL stay; only Jira's freshness moves.
	jiraOnly := mirror.Manifest{ExportedAt: "2026-10-08T00:00:00Z", JiraURL: jiraURL, Boards: []int{8}, Contents: []string{mirror.SideJira}}
	res, err := Import(sideArchive(t, jiraOnly, []string{"A-1"}, nil), dir, ImportOptions{Prune: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Pruned, []string{"jira/issues/A-2"}) || !slices.Equal(res.Contents, []string{mirror.SideJira}) {
		t.Fatalf("jira-only prune: %+v", res)
	}
	m, _ := mirror.ReadManifest(dir)
	if m.ConfluenceURL != confURL || m.JiraURL != jiraURL || m.Pages != 2 || m.Issues != 1 {
		t.Fatalf("jira-only import lost the Confluence side: %+v", m)
	}
	if m.Jira.LastFullAt != "2026-10-08T00:00:00Z" || m.Confluence.ExportedAt != "2026-10-01T00:00:00Z" ||
		m.ExportedAt != "2026-10-08T00:00:00Z" || m.LastFullAt != "2026-10-01T00:00:00Z" {
		t.Fatalf("freshness: top %s/%s jira %+v confluence %+v", m.ExportedAt, m.LastFullAt, *m.Jira, *m.Confluence)
	}
	if !slices.Equal(m.Contents, mirror.Sides) || !slices.Equal(m.Boards, []int{8}) {
		t.Fatalf("contents %v boards %v", m.Contents, m.Boards)
	}

	// A Confluence delta: page 12 added, Jira untouched.
	conf := mirror.Manifest{ExportedAt: "2026-10-09T00:00:00Z", Since: "2026-10-01T00:00:00Z", ConfluenceURL: confURL, Contents: []string{mirror.SideConfluence}}
	if _, err := Import(sideArchive(t, conf, nil, []string{"12"}), dir, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	m, _ = mirror.ReadManifest(dir)
	if m.Pages != 3 || m.Issues != 1 || m.Jira.ExportedAt != "2026-10-08T00:00:00Z" ||
		m.Confluence.ExportedAt != "2026-10-09T00:00:00Z" || m.Confluence.LastFullAt != "2026-10-01T00:00:00Z" ||
		m.Confluence.Since != "2026-10-01T00:00:00Z" || m.Jira.Since != "" || !slices.Equal(m.Boards, []int{8}) {
		t.Fatalf("confluence delta: %+v jira %+v confluence %+v", m, *m.Jira, *m.Confluence)
	}
}

func TestSideStateOfLegacyMirror(t *testing.T) {
	old := &mirror.Manifest{Version: 1, ExportedAt: "2026-10-05T00:00:00Z", LastFullAt: "2026-10-01T00:00:00Z", Since: "2026-10-01T00:00:00Z"}
	for _, side := range mirror.Sides {
		if st := old.Side(side); st == nil || st.LastFullAt != "2026-10-01T00:00:00Z" || st.ExportedAt != "2026-10-05T00:00:00Z" {
			t.Fatalf("%s of a legacy manifest: %+v", side, st)
		}
	}
	in := &mirror.Manifest{Version: 1, ExportedAt: "2026-10-06T00:00:00Z", Contents: []string{mirror.SideConfluence}}
	m := mirror.MergeImport(old, in, "now")
	if m.Jira.LastFullAt != "2026-10-01T00:00:00Z" || m.Confluence.LastFullAt != "2026-10-06T00:00:00Z" || m.LastFullAt != "2026-10-01T00:00:00Z" {
		t.Fatalf("merged legacy: jira %+v confluence %+v", *m.Jira, *m.Confluence)
	}
}

// TestPruneKeepsLinkedItems: a side's full snapshot prunes its missing
// items, except those a staying item of the mirror links to (brought in by
// the other side's export through a reference).
func TestPruneKeepsLinkedItems(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mirror")
	const jiraURL, confURL = "https://jira.example", "https://wiki.example"
	both := mirror.Manifest{ExportedAt: "2026-10-01T00:00:00Z", JiraURL: jiraURL, ConfluenceURL: confURL}
	if _, err := Import(sideArchive(t, both, []string{"A-1", "A-2", "A-3"}, []string{"10", "11", "12", "13"}), dir, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	// Page 10 shows A-2; A-1 links to pages 11 (by ID) and 12 (by title).
	put(t, filepath.Join(dir, "confluence", "pages", "10", "page.json"),
		`{"id":"10","body":{"storage":{"value":"<ac:structured-macro ac:name=\"jira\"><ac:parameter ac:name=\"key\">A-2</ac:parameter></ac:structured-macro>"}}}`)
	put(t, filepath.Join(dir, "confluence", "pages", "12", "page.json"), `{"id":"12","title":"Design Notes","space":{"key":"DEV"}}`)

	jiraOnly := mirror.Manifest{ExportedAt: "2026-10-08T00:00:00Z", JiraURL: jiraURL, Contents: []string{mirror.SideJira}}
	res, err := Import(sideArchive(t, jiraOnly, []string{"A-1"}, nil), dir, ImportOptions{Prune: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Pruned, []string{"jira/issues/A-3"}) {
		t.Fatalf("jira prune: pruned %v, want only A-3 (A-2 is on page 10)", res.Pruned)
	}

	put(t, filepath.Join(dir, "jira", "issues", "A-1", "issue.json"),
		`{"key":"A-1","fields":{"description":"see https://wiki.example/pages/viewpage.action?pageId=11"}}`)
	put(t, filepath.Join(dir, "jira", "issues", "A-1", "remotelinks.json"),
		`[{"object":{"url":"https://wiki.example/display/DEV/Design+Notes"}}]`)
	confOnly := mirror.Manifest{ExportedAt: "2026-10-09T00:00:00Z", ConfluenceURL: confURL, Contents: []string{mirror.SideConfluence}}
	res, err = Import(sideArchive(t, confOnly, nil, []string{"10"}), dir, ImportOptions{Prune: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(res.Pruned, []string{"confluence/pages/13"}) {
		t.Fatalf("confluence prune: pruned %v, want only page 13 (11 and 12 are linked from A-1)", res.Pruned)
	}
}
