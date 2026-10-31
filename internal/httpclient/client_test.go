package httpclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNewFromConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		baseURL        string
		email          string
		token          string
		wantAuthHeader string
		wantBaseURL    string
	}{
		{
			name:           "cloud: email set uses Basic auth",
			baseURL:        "https://example.atlassian.net",
			email:          "user@example.com",
			token:          "mytoken",
			wantAuthHeader: "Basic " + base64.StdEncoding.EncodeToString([]byte("user@example.com:mytoken")),
			wantBaseURL:    "https://example.atlassian.net",
		},
		{
			name:           "server/DC: no email uses Bearer auth",
			baseURL:        "https://jira.company.internal",
			email:          "",
			token:          "personaltoken",
			wantAuthHeader: "Bearer personaltoken",
			wantBaseURL:    "https://jira.company.internal",
		},
		{
			name:           "trailing slash in baseURL is removed",
			baseURL:        "https://example.com/",
			email:          "a@b.com",
			token:          "tok",
			wantAuthHeader: "Basic " + base64.StdEncoding.EncodeToString([]byte("a@b.com:tok")),
			wantBaseURL:    "https://example.com",
		},
		{
			name:           "multiple trailing slashes all removed",
			baseURL:        "https://example.com///",
			email:          "",
			token:          "tok",
			wantAuthHeader: "Bearer tok",
			wantBaseURL:    "https://example.com",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewFromConfig(tt.baseURL, tt.email, tt.token)
			if c.authHeader != tt.wantAuthHeader {
				t.Errorf("authHeader = %q, want %q", c.authHeader, tt.wantAuthHeader)
			}
			if c.baseURL != tt.wantBaseURL {
				t.Errorf("baseURL = %q, want %q", c.baseURL, tt.wantBaseURL)
			}
		})
	}
}

func TestAPIError_Error(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		apiErr     *APIError
		wantPrefix string
		wantAll    []string
	}{
		{
			name: "only messages",
			apiErr: &APIError{
				StatusCode: 400,
				Messages:   []string{"Issue not found"},
			},
			wantPrefix: "API error (HTTP 400):",
			wantAll:    []string{"Issue not found"},
		},
		{
			name: "only errors map",
			apiErr: &APIError{
				StatusCode: 422,
				Errors:     map[string]string{"summary": "Field required"},
			},
			wantPrefix: "API error (HTTP 422):",
			wantAll:    []string{"summary: Field required"},
		},
		{
			name: "both empty returns generic message",
			apiErr: &APIError{
				StatusCode: 500,
			},
			wantPrefix: "API error: HTTP 500",
			wantAll:    nil,
		},
		{
			name: "messages and errors both present",
			apiErr: &APIError{
				StatusCode: 400,
				Messages:   []string{"Bad request"},
				Errors:     map[string]string{"field": "invalid"},
			},
			wantPrefix: "API error (HTTP 400):",
			wantAll:    []string{"Bad request", "field: invalid"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.apiErr.Error()
			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("Error() = %q, want prefix %q", got, tt.wantPrefix)
			}
			for _, s := range tt.wantAll {
				if !strings.Contains(got, s) {
					t.Errorf("Error() = %q, want it to contain %q", got, s)
				}
			}
		})
	}
}

func TestRaw(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if len(body) > 0 && r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusTeapot)
		fmt.Fprintf(w, "%s %s?%s %s", r.Method, r.URL.Path, r.URL.RawQuery, body)
	}))
	defer srv.Close()

	c := NewFromConfig(srv.URL, "", "tok")
	status, body, err := c.Raw(context.Background(), "PUT", "/rest/x", url.Values{"a": {"1"}}, []byte(`{"k":1}`))
	if err != nil {
		t.Fatalf("Raw() error: %v", err)
	}
	if status != http.StatusTeapot {
		t.Errorf("status = %d, want 418 (errors are returned, not raised)", status)
	}
	if got, want := string(body), `PUT /rest/x?a=1 {"k":1}`; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestUpload(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Atlassian-Token") != "no-check" {
			t.Errorf("missing XSRF header")
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("FormFile: %v", err)
		}
		data, _ := io.ReadAll(f)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":%q,"content":%q}`, hdr.Filename, data)
	}))
	defer srv.Close()

	var got struct{ Name, Content string }
	err := NewFromConfig(srv.URL, "", "tok").Upload(context.Background(), "/up", "a.txt", strings.NewReader("hello"), &got)
	if err != nil {
		t.Fatalf("Upload() error: %v", err)
	}
	if got.Name != "a.txt" || got.Content != "hello" {
		t.Errorf("got %+v", got)
	}
}
