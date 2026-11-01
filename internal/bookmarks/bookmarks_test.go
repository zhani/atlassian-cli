package bookmarks

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Path: filepath.Join(t.TempDir(), "sub", "bookmarks.yaml")}
}

func TestAddListAndReplace(t *testing.T) {
	s := newStore(t)
	if list, err := s.List(); err != nil || len(list) != 0 {
		t.Fatalf("empty store: %v %v", list, err)
	}
	for _, b := range []Bookmark{
		{Kind: Issue, Key: "proj-1", Title: "First"},
		{Kind: Query, Name: "My bugs", JQL: "assignee = currentUser() AND type = Bug"},
		{Kind: Page, Key: "12345", Title: "Runbook", Space: "OPS"},
	} {
		if err := s.Add(b); err != nil {
			t.Fatal(err)
		}
	}
	// Re-adding the same issue replaces it in place.
	if err := s.Add(Bookmark{Kind: Issue, Key: "PROJ-1", Name: "Renamed"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Name != "Renamed" || list[0].Key != "PROJ-1" || list[0].Added == "" {
		t.Fatalf("list = %+v", list)
	}
	data, _ := os.ReadFile(s.Path)
	if !strings.HasPrefix(string(data), "# atlassian-cli bookmarks") || !strings.Contains(string(data), "jql: assignee = currentUser() AND type = Bug") {
		t.Errorf("file:\n%s", data)
	}
	if info, _ := os.Stat(filepath.Dir(s.Path)); info.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v", info.Mode().Perm())
	}
}

func TestValidate(t *testing.T) {
	for _, b := range []Bookmark{
		{Kind: Query, JQL: "x = 1"}, // no name
		{Kind: Query, Name: "n"},    // no JQL
		{Kind: Issue},               // no key
		{Kind: Sprint, Key: "abc"},  // not a number
		{Kind: "board", Key: "1"},   // unknown kind
	} {
		if err := b.Validate(); err == nil {
			t.Errorf("%+v accepted", b)
		}
	}
}

func TestFindAndRemove(t *testing.T) {
	s := newStore(t)
	for _, b := range []Bookmark{
		{Kind: Issue, Key: "A-1"},
		{Kind: Sprint, Key: "2", Title: "Sprint 2"},
		{Kind: Query, Name: "Mine", JQL: "assignee = currentUser()"},
		{Kind: Epic, Key: "A-9", Name: "mine"},
	} {
		if err := s.Add(b); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		ref, want string
		err       bool
	}{
		{"a-1", "issue:A-1", false},
		{"1", "issue:A-1", false}, // position
		{"2", "sprint:2", false},  // sprint ID wins over position 2
		{"3", "query:assignee = currentUser()", false},
		{"assignee  =   currentUser()", "query:assignee = currentUser()", false},
		{"MINE", "", true}, // two names match
		{"nope", "", true},
	}
	for _, tt := range tests {
		b, _, err := s.Find(tt.ref)
		if tt.err {
			if err == nil {
				t.Errorf("Find(%q) = %v, want error", tt.ref, b.ID())
			}
			continue
		}
		if err != nil || b.ID() != tt.want {
			t.Errorf("Find(%q) = %v, %v; want %s", tt.ref, b.ID(), err, tt.want)
		}
	}

	removed, err := s.Remove("A-1", "A-9")
	if err != nil || len(removed) != 2 {
		t.Fatalf("Remove: %v %v", removed, err)
	}
	list, _ := s.List()
	if len(list) != 2 || list[0].Kind != Sprint {
		t.Errorf("after remove: %+v", list)
	}
}

func TestToggleAndRename(t *testing.T) {
	s := newStore(t)
	b := Bookmark{Kind: Feature, Key: "F-1", Title: "Login"}
	if added, err := s.Toggle(b); err != nil || !added {
		t.Fatalf("toggle on: %v %v", added, err)
	}
	if has, _ := s.Has(b); !has {
		t.Error("not bookmarked")
	}
	if got, err := s.Rename("f-1", "Login feature"); err != nil || got.Name != "Login feature" {
		t.Errorf("rename: %+v %v", got, err)
	}
	if added, err := s.Toggle(b); err != nil || added {
		t.Fatalf("toggle off: %v %v", added, err)
	}
	if list, _ := s.List(); len(list) != 0 {
		t.Errorf("left: %+v", list)
	}
}

func TestParseKindAndLabel(t *testing.T) {
	for in, want := range map[string]Kind{"issues": Issue, "JQL": Query, "page": Page, "Sprints": Sprint} {
		if k, err := ParseKind(in); err != nil || k != want {
			t.Errorf("ParseKind(%q) = %v, %v", in, k, err)
		}
	}
	if l := (Bookmark{Kind: Issue, Key: "A-1", Title: "Fix"}).Label(); l != "A-1 Fix" {
		t.Errorf("label %q", l)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Setenv("ATLASSIAN_BOOKMARKS", "/tmp/x.yaml")
	if p, err := DefaultPath(); err != nil || p != "/tmp/x.yaml" {
		t.Errorf("%q %v", p, err)
	}
	if runtime.GOOS != "linux" {
		return // os.UserConfigDir only follows XDG_CONFIG_HOME on Unix-likes
	}
	t.Setenv("ATLASSIAN_BOOKMARKS", "")
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	if p, _ := DefaultPath(); p != "/cfg/atlassian-cli/bookmarks.yaml" {
		t.Errorf("default path %q", p)
	}
}

func TestBrokenFile(t *testing.T) {
	s := newStore(t)
	os.MkdirAll(filepath.Dir(s.Path), 0o700)
	os.WriteFile(s.Path, []byte("bookmarks: [oops"), 0o600)
	if _, err := s.List(); err == nil || !strings.Contains(err.Error(), s.Path) {
		t.Errorf("want parse error naming the file, got %v", err)
	}
}

func TestDeleteAndSetName(t *testing.T) {
	s := newStore(t)
	page := Bookmark{Kind: Page, Key: "1", Title: "P"}
	issue := Bookmark{Kind: Issue, Key: "A-1"}
	for _, b := range []Bookmark{issue, page} {
		if err := s.Add(b); err != nil {
			t.Fatal(err)
		}
	}
	// Page "1" is the second bookmark: by item, not by position.
	if err := s.SetName(page, "Runbook"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(page); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List()
	if len(list) != 1 || list[0].ID() != issue.ID() {
		t.Errorf("left: %+v", list)
	}
	if err := s.Delete(page); err == nil {
		t.Error("deleting a missing bookmark succeeded")
	}
	if err := s.SetName(Bookmark{Kind: Query, JQL: "x"}, ""); err == nil {
		t.Error("empty query name accepted")
	}
}

func TestRawRoundTrip(t *testing.T) {
	s := newStore(t)
	text, err := s.Raw()
	if err != nil || !strings.Contains(text, "bookmarks: []") {
		t.Fatalf("template: %q %v", text, err)
	}
	edited := "# mine\nbookmarks:\n  - name: Bugs\n    kind: query\n    jql: type = Bug\n"
	if err := s.SaveRaw(edited); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Raw(); got != edited {
		t.Errorf("comments not kept: %q", got)
	}
	if err := s.SaveRaw("bookmarks:\n  - kind: sprint\n    key: abc\n"); err == nil {
		t.Error("invalid bookmark accepted")
	}
}
