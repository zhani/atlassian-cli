package formatter

import (
	"bytes"
	"strings"
	"testing"

	"github.com/putcho01/atlassian-cli/internal/jira"
)

func TestParseFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  Format
	}{
		{"lowercase json", "json", JSON},
		{"uppercase JSON", "JSON", JSON},
		{"mixed case Json", "Json", JSON},
		{"lowercase markdown", "markdown", Markdown},
		{"alias md", "md", Markdown},
		{"uppercase MD", "MD", Markdown},
		{"table explicit", "table", Table},
		{"empty defaults to table", "", Table},
		{"unknown defaults to table", "csv", Table},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ParseFormat(tt.input)
			if got != tt.want {
				t.Errorf("ParseFormat(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSafeGetters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"safeName nil", safeName(nil), ""},
		{"safeName non-nil", safeName(&jira.Status{Name: "Done"}), "Done"},
		{"safeIssueType nil", safeIssueType(nil), ""},
		{"safeIssueType non-nil", safeIssueType(&jira.IssueType{Name: "Story"}), "Story"},
		{"safePriority nil", safePriority(nil), ""},
		{"safePriority non-nil", safePriority(&jira.Priority{Name: "High"}), "High"},
		{"safeUser nil", safeUser(nil), "Unassigned"},
		{"safeUser non-nil", safeUser(&jira.User{DisplayName: "Alice"}), "Alice"},
		{"safeUserDisplay nil unknown", safeUserDisplay(nil, "Unknown"), "Unknown"},
		{"safeUserDisplay nil unassigned", safeUserDisplay(nil, "Unassigned"), "Unassigned"},
		{"safeUserDisplay non-nil", safeUserDisplay(&jira.User{DisplayName: "Bob"}, "Unknown"), "Bob"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.want {
				t.Errorf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestPrintComments(t *testing.T) {
	t.Parallel()
	comments := []jira.Comment{
		{ID: "1", Author: &jira.User{DisplayName: "Alice"}, Body: "Hello", Created: "2024-01-01"},
		{ID: "2", Author: nil, Body: "World", Created: "2024-01-02"},
	}

	tests := []struct {
		name     string
		format   Format
		contains []string
	}{
		{
			name:     "table contains headers and IDs",
			format:   Table,
			contains: []string{"ID", "AUTHOR", "CREATED", "1", "Alice", "Hello", "2", "Unknown"},
		},
		{
			name:     "markdown contains author and body",
			format:   Markdown,
			contains: []string{"### Alice", "Hello", "### Unknown", "World"},
		},
		{
			name:     "json contains comment fields",
			format:   JSON,
			contains: []string{`"id"`, `"1"`, `"Alice"`},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			f := New(tt.format, &buf)
			if err := f.PrintComments(comments); err != nil {
				t.Fatalf("PrintComments() error: %v", err)
			}
			out := buf.String()
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output does not contain %q\ngot: %s", want, out)
				}
			}
		})
	}
}

func TestPrintComment(t *testing.T) {
	t.Parallel()
	comment := &jira.Comment{
		ID:      "10",
		Author:  &jira.User{DisplayName: "Alice"},
		Body:    "Single comment",
		Created: "2024-01-01",
	}

	tests := []struct {
		name     string
		format   Format
		contains []string
	}{
		{
			name:     "table contains ID and author",
			format:   Table,
			contains: []string{"10", "Alice", "2024-01-01", "Single comment"},
		},
		{
			name:     "markdown contains heading and body",
			format:   Markdown,
			contains: []string{"### Alice", "Single comment"},
		},
		{
			name:     "json contains id field",
			format:   JSON,
			contains: []string{`"id"`, `"10"`},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			f := New(tt.format, &buf)
			if err := f.PrintComment(comment); err != nil {
				t.Fatalf("PrintComment() error: %v", err)
			}
			out := buf.String()
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output does not contain %q\ngot: %s", want, out)
				}
			}
		})
	}
}

func TestPrintAttachments(t *testing.T) {
	t.Parallel()
	attachments := []jira.Attachment{
		{ID: "10", Filename: "spec.pdf", Size: 2048, MimeType: "application/pdf", Created: "2024-01-01"},
		{ID: "11", Filename: "logo.png", Size: 512, MimeType: "image/png", Created: "2024-01-02"},
	}

	tests := []struct {
		name     string
		format   Format
		contains []string
	}{
		{
			name:     "table contains id filename and size",
			format:   Table,
			contains: []string{"10", "spec.pdf", "2.0 KB", "application/pdf", "logo.png", "512 B"},
		},
		{
			name:     "markdown contains header and rows",
			format:   Markdown,
			contains: []string{"| ID |", "spec.pdf", "logo.png"},
		},
		{
			name:     "json contains fields",
			format:   JSON,
			contains: []string{`"id"`, `"spec.pdf"`, `"filename"`},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			f := New(tt.format, &buf)
			if err := f.PrintAttachments(attachments); err != nil {
				t.Fatalf("PrintAttachments() error: %v", err)
			}
			out := buf.String()
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output does not contain %q\ngot: %s", want, out)
				}
			}
		})
	}
}

func TestHumanSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		size int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{2048, "2.0 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		if got := humanSize(tt.size); got != tt.want {
			t.Errorf("humanSize(%d) = %q, want %q", tt.size, got, tt.want)
		}
	}
}

func TestPrintIssueCustomFields(t *testing.T) {
	t.Parallel()
	issue := &jira.Issue{
		Key: "PROJ-1",
		Fields: jira.IssueFields{
			Summary: "Task",
			Custom: map[string]any{
				"customfield_10001": "sprint-42",
				"customfield_10002": map[string]any{"value": "High"},
			},
		},
	}

	tests := []struct {
		name     string
		format   Format
		contains []string
	}{
		{
			name:     "markdown lists custom field rows",
			format:   Markdown,
			contains: []string{"customfield_10001", "sprint-42", "customfield_10002", `"value":"High"`},
		},
		{
			name:     "json flattens custom fields",
			format:   JSON,
			contains: []string{`"customfield_10001"`, `"sprint-42"`},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := New(tt.format, &buf).PrintIssue(issue); err != nil {
				t.Fatalf("PrintIssue() error: %v", err)
			}
			out := buf.String()
			for _, want := range tt.contains {
				if !strings.Contains(out, want) {
					t.Errorf("output does not contain %q\ngot: %s", want, out)
				}
			}
		})
	}
}
