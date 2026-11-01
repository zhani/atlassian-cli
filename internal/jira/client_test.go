package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/putcho01/atlassian-cli/internal/httpclient"
)

func newTestJiraClient(baseURL string) *Client {
	return &Client{http: httpclient.NewFromConfig(baseURL, "test@test.com", "token")}
}

func TestBuildCreateFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    *CreateIssueInput
		expected map[string]any
	}{
		{
			name: "minimal required fields only",
			input: &CreateIssueInput{
				ProjectKey: "PROJ",
				Summary:    "Fix bug",
				IssueType:  "Bug",
			},
			expected: map[string]any{
				"project":   map[string]string{"key": "PROJ"},
				"summary":   "Fix bug",
				"issuetype": map[string]string{"name": "Bug"},
			},
		},
		{
			name: "all optional fields populated",
			input: &CreateIssueInput{
				ProjectKey:  "PROJ",
				Summary:     "New Feature",
				IssueType:   "Story",
				Description: "As a user...",
				Assignee:    "acc123",
				Priority:    "High",
				Labels:      []string{"backend", "v2"},
				CustomFields: map[string]any{
					"customfield_10001": "value1",
				},
			},
			expected: map[string]any{
				"project":           map[string]string{"key": "PROJ"},
				"summary":           "New Feature",
				"issuetype":         map[string]string{"name": "Story"},
				"description":       "As a user...",
				"assignee":          map[string]any{"accountId": "acc123"},
				"priority":          map[string]string{"name": "High"},
				"labels":            []string{"backend", "v2"},
				"customfield_10001": "value1",
			},
		},
		{
			name: "empty description not added",
			input: &CreateIssueInput{
				ProjectKey:  "PROJ",
				Summary:     "Task",
				IssueType:   "Task",
				Description: "",
			},
			expected: map[string]any{
				"project":   map[string]string{"key": "PROJ"},
				"summary":   "Task",
				"issuetype": map[string]string{"name": "Task"},
			},
		},
		{
			name: "empty assignee not added",
			input: &CreateIssueInput{
				ProjectKey: "PROJ",
				Summary:    "Task",
				IssueType:  "Task",
				Assignee:   "",
			},
			expected: map[string]any{
				"project":   map[string]string{"key": "PROJ"},
				"summary":   "Task",
				"issuetype": map[string]string{"name": "Task"},
			},
		},
		{
			name: "empty labels not added",
			input: &CreateIssueInput{
				ProjectKey: "PROJ",
				Summary:    "Task",
				IssueType:  "Task",
				Labels:     []string{},
			},
			expected: map[string]any{
				"project":   map[string]string{"key": "PROJ"},
				"summary":   "Task",
				"issuetype": map[string]string{"name": "Task"},
			},
		},
		{
			name: "custom fields merged into result",
			input: &CreateIssueInput{
				ProjectKey: "PROJ",
				Summary:    "Task",
				IssueType:  "Task",
				CustomFields: map[string]any{
					"customfield_10000": 42,
				},
			},
			expected: map[string]any{
				"project":           map[string]string{"key": "PROJ"},
				"summary":           "Task",
				"issuetype":         map[string]string{"name": "Task"},
				"customfield_10000": 42,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildCreateFields(tt.input, true)
			if diff := cmp.Diff(tt.expected, got); diff != "" {
				t.Errorf("buildCreateFields() diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestListComments(t *testing.T) {
	t.Parallel()
	comments := []Comment{
		{ID: "1", Author: &User{DisplayName: "Alice"}, Body: "Hello", Created: "2024-01-01"},
		{ID: "2", Author: &User{DisplayName: "Bob"}, Body: "World", Created: "2024-01-02"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/2/issue/TEST-1/comment", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CommentPage{Comments: comments, Total: 2})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := newTestJiraClient(srv.URL).ListComments(context.Background(), "TEST-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff := cmp.Diff(comments, got); diff != "" {
		t.Errorf("ListComments() diff (-want +got):\n%s", diff)
	}
}

func TestAddComment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		want    *Comment
		wantErr bool
	}{
		{
			name: "success",
			body: "This is a comment",
			want: &Comment{ID: "10", Author: &User{DisplayName: "Alice"}, Body: "This is a comment", Created: "2024-01-01"},
		},
		{
			name:    "server error",
			body:    "error",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("/rest/api/2/issue/TEST-1/comment", func(w http.ResponseWriter, r *http.Request) {
				var req map[string]any
				json.NewDecoder(r.Body).Decode(&req)
				if req["body"] == "error" {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.want)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			got, err := newTestJiraClient(srv.URL).AddComment(context.Background(), "TEST-1", tt.body)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("AddComment() diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetAttachments(t *testing.T) {
	t.Parallel()
	attachments := []Attachment{
		{ID: "10", Filename: "spec.pdf", Size: 2048, MimeType: "application/pdf", Content: "http://x/rest/api/2/attachment/content/10"},
		{ID: "11", Filename: "logo.png", Size: 512, MimeType: "image/png", Content: "http://x/rest/api/2/attachment/content/11"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/2/issue/TEST-1", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("fields"); got != "attachment" {
			t.Errorf("expected fields=attachment, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Issue{Fields: IssueFields{Attachment: attachments}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := newTestJiraClient(srv.URL).GetAttachments(context.Background(), "TEST-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff := cmp.Diff(attachments, got); diff != "" {
		t.Errorf("GetAttachments() diff (-want +got):\n%s", diff)
	}
}

func TestGetAttachment(t *testing.T) {
	t.Parallel()
	want := &Attachment{ID: "10", Filename: "spec.pdf", Size: 2048, MimeType: "application/pdf"}

	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/2/attachment/10", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := newTestJiraClient(srv.URL).GetAttachment(context.Background(), "10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("GetAttachment() diff (-want +got):\n%s", diff)
	}
}

func TestDownloadAttachment(t *testing.T) {
	t.Parallel()
	const payload = "file-bytes"

	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/2/attachment/content/10", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(payload))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Absolute content URL (as returned by Jira) must be handled directly.
	body, err := newTestJiraClient(srv.URL).DownloadAttachment(context.Background(), srv.URL+"/rest/api/2/attachment/content/10")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(data) != payload {
		t.Errorf("DownloadAttachment() = %q, want %q", data, payload)
	}
}

func TestDownloadAttachmentError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/2/attachment/content/404", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := newTestJiraClient(srv.URL).DownloadAttachment(context.Background(), srv.URL+"/rest/api/2/attachment/content/404")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

func TestTransitionIssue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		statusName     string
		transitions    []Transition
		wantErr        bool
		wantErrContain string
	}{
		{
			name:       "success by transition name",
			statusName: "In Progress",
			transitions: []Transition{
				{ID: "11", Name: "In Progress", To: &Status{Name: "In Progress"}},
				{ID: "21", Name: "Done", To: &Status{Name: "Done"}},
			},
			wantErr: false,
		},
		{
			name:       "success by To.Name match",
			statusName: "Done",
			transitions: []Transition{
				{ID: "21", Name: "Close Issue", To: &Status{Name: "Done"}},
			},
			wantErr: false,
		},
		{
			name:       "case insensitive match",
			statusName: "in progress",
			transitions: []Transition{
				{ID: "11", Name: "In Progress", To: &Status{Name: "In Progress"}},
			},
			wantErr: false,
		},
		{
			name:       "transition not found",
			statusName: "Nonexistent",
			transitions: []Transition{
				{ID: "11", Name: "In Progress", To: &Status{Name: "In Progress"}},
			},
			wantErr:        true,
			wantErrContain: "not found",
		},
		{
			name:           "empty transitions list",
			statusName:     "Done",
			transitions:    []Transition{},
			wantErr:        true,
			wantErrContain: "not found",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("/rest/api/2/issue/TEST-1/transitions", func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(transitionsResponse{Transitions: tt.transitions})
				case http.MethodPost:
					w.WriteHeader(http.StatusNoContent)
				default:
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			err := newTestJiraClient(srv.URL).TransitionIssue(context.Background(), "TEST-1", tt.statusName)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				if tt.wantErrContain != "" && !strings.Contains(err.Error(), tt.wantErrContain) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.wantErrContain)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func keysOf(issues []Issue) []string {
	keys := make([]string, len(issues))
	for i, is := range issues {
		keys[i] = is.Key
	}
	return keys
}

func issuesNamed(keys ...string) []Issue {
	issues := make([]Issue, len(keys))
	for i, k := range keys {
		issues[i] = Issue{Key: k}
	}
	return issues
}

func TestSearchIssuesAllCloud(t *testing.T) {
	t.Parallel()
	pages := map[string]searchJQLResponse{
		"":   {Issues: issuesNamed("A-1", "A-2"), NextPageToken: "p2"},
		"p2": {Issues: issuesNamed("A-3", "A-4"), NextPageToken: "p3"},
		"p3": {Issues: issuesNamed("A-5"), IsLast: true},
	}

	tests := []struct {
		name  string
		limit int
		want  []string
	}{
		{name: "all pages", limit: 0, want: []string{"A-1", "A-2", "A-3", "A-4", "A-5"}},
		{name: "stops at limit", limit: 3, want: []string{"A-1", "A-2", "A-3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("/rest/api/2/search/jql", func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Query().Get("fields"); got != "*navigable" {
					t.Errorf("fields = %q, want *navigable", got)
				}
				w.Header().Set("Content-Type", "application/json")
				// Deliberately ignore maxResults: the client must still honour the limit.
				json.NewEncoder(w).Encode(pages[r.URL.Query().Get("nextPageToken")])
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			c := newTestJiraClient(srv.URL)
			c.cloud = true
			got, err := c.SearchIssuesAll(context.Background(), "project = A", nil, tt.limit)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if diff := cmp.Diff(tt.want, keysOf(got)); diff != "" {
				t.Errorf("SearchIssuesAll() diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSearchIssuesAllServer(t *testing.T) {
	t.Parallel()
	all := issuesNamed("B-1", "B-2", "B-3", "B-4", "B-5")
	const serverPageSize = 2 // the server caps pages below what we ask for

	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/2/search", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("fields"); got != "key,summary" {
			t.Errorf("fields = %q, want key,summary", got)
		}
		start, _ := strconv.Atoi(r.URL.Query().Get("startAt"))
		end := min(start+serverPageSize, len(all))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SearchResult{StartAt: start, Total: len(all), Issues: all[start:end]})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := newTestJiraClient(srv.URL).SearchIssuesAll(context.Background(), "project = B", []string{"key", "summary"}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff := cmp.Diff(keysOf(all), keysOf(got)); diff != "" {
		t.Errorf("SearchIssuesAll() diff (-want +got):\n%s", diff)
	}
}

func TestGetIssueRendered(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/2/issue/TEST-1", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("expand"); got != "renderedFields,names" {
			t.Errorf("expand = %q, want renderedFields,names", got)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"key": "TEST-1",
			"fields": {
				"summary": "Rendered",
				"description": "*bold*",
				"issuelinks": [{"type": {"name": "Blocks", "inward": "is blocked by", "outward": "blocks"}, "inwardIssue": {"key": "TEST-2"}}],
				"comment": {"comments": [{"id": "1", "body": "_hi_"}], "total": 1}
			},
			"renderedFields": {
				"description": "<p><b>bold</b></p>",
				"comment": {"comments": [{"id": "1", "body": "<p><em>hi</em></p>"}], "total": 1}
			}
		}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := newTestJiraClient(srv.URL).GetIssueRendered(context.Background(), "TEST-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RenderedFields == nil || got.RenderedFields.Description != "<p><b>bold</b></p>" {
		t.Errorf("RenderedFields = %+v, want rendered description", got.RenderedFields)
	}
	if got.RenderedFields.Comment == nil || got.RenderedFields.Comment.Comments[0].Body != "<p><em>hi</em></p>" {
		t.Errorf("rendered comment = %+v", got.RenderedFields.Comment)
	}
	if len(got.Fields.IssueLinks) != 1 {
		t.Fatalf("IssueLinks = %d, want 1", len(got.Fields.IssueLinks))
	}
	phrase, linked := got.Fields.IssueLinks[0].Describe()
	if phrase != "is blocked by" || linked.Key != "TEST-2" {
		t.Errorf("Describe() = %q, %s; want \"is blocked by\", TEST-2", phrase, linked.Key)
	}
}

func TestBuildCreateFieldsServerAssignee(t *testing.T) {
	t.Parallel()
	got := buildCreateFields(&CreateIssueInput{ProjectKey: "P", Summary: "s", IssueType: "Task", Assignee: "jdoe"}, false)
	if diff := cmp.Diff(map[string]any{"name": "jdoe"}, got["assignee"]); diff != "" {
		t.Errorf("Server/DC assignee diff (-want +got):\n%s", diff)
	}
}
