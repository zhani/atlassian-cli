package jira

import "testing"

func TestBuildJQL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		opts    ListOptions
		want    string
		wantErr bool
	}{
		{
			name: "no filters",
			want: "ORDER BY created DESC",
		},
		{
			name: "simple filters",
			opts: ListOptions{Project: "PROJ", Status: "In Progress", Type: "Bug", Priority: "High"},
			want: `project = "PROJ" AND status = "In Progress" AND issuetype = "Bug" AND priority = "High" ORDER BY created DESC`,
		},
		{
			name: "negation",
			opts: ListOptions{Status: "~Done", Labels: []string{"backend", "~wontfix"}},
			want: `status != "Done" AND labels = "backend" AND labels != "wontfix" ORDER BY created DESC`,
		},
		{
			name: "users",
			opts: ListOptions{Assignee: "me", Reporter: "x"},
			want: `assignee = currentUser() AND reporter IS EMPTY ORDER BY created DESC`,
		},
		{
			name: "negated users",
			opts: ListOptions{Assignee: "~x", Reporter: "~ME"},
			want: `assignee IS NOT EMPTY AND reporter != currentUser() ORDER BY created DESC`,
		},
		{
			name: "named user",
			opts: ListOptions{Assignee: "jdoe"},
			want: `assignee = "jdoe" ORDER BY created DESC`,
		},
		{
			name: "dates",
			opts: ListOptions{Created: "-7d", Updated: "week"},
			want: `created >= "-7d" AND updated >= startOfWeek() ORDER BY created DESC`,
		},
		{
			name: "date without minus",
			opts: ListOptions{Created: "2w"},
			want: `created >= "-2w" ORDER BY created DESC`,
		},
		{
			name:    "invalid date",
			opts:    ListOptions{Created: "yesterday"},
			wantErr: true,
		},
		{
			name: "watching, extra jql and ordering",
			opts: ListOptions{Watching: true, JQL: "summary ~ cli OR key = A-1", OrderBy: "rank", Reverse: true},
			want: `watcher = currentUser() AND (summary ~ cli OR key = A-1) ORDER BY rank ASC`,
		},
		{
			name: "unresolved",
			opts: ListOptions{Type: "Epic", Unresolved: true},
			want: `issuetype = "Epic" AND resolution = Unresolved ORDER BY created DESC`,
		},
		{
			name: "resolution",
			opts: ListOptions{Resolution: "unresolved"},
			want: `resolution IS EMPTY ORDER BY created DESC`,
		},
		{
			name: "resolved",
			opts: ListOptions{Resolution: "~Unresolved"},
			want: `resolution IS NOT EMPTY ORDER BY created DESC`,
		},
		{
			name: "named resolution",
			opts: ListOptions{Resolution: "~Won't Do"},
			want: `resolution != "Won't Do" ORDER BY created DESC`,
		},
		{
			name: "quoting",
			opts: ListOptions{Status: `Say "hi" \o/`},
			want: `status = "Say \"hi\" \\o/" ORDER BY created DESC`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := BuildJQL(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("BuildJQL() =\n  %s\nwant\n  %s", got, tt.want)
			}
		})
	}
}
