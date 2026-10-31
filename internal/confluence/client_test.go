package confluence

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

func newTestConfluenceClient(baseURL string) *Client {
	return &Client{http: httpclient.NewFromConfig(baseURL, "test@test.com", "token")}
}

func TestCreatePage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   *CreatePageInput
		want    *Page
		wantErr bool
	}{
		{
			name: "success without parent",
			input: &CreatePageInput{
				SpaceKey: "PROJ",
				Title:    "New Page",
				Body:     "<p>Hello</p>",
			},
			want: &Page{ID: "123", Title: "New Page", Status: "current"},
		},
		{
			name: "success with parent",
			input: &CreatePageInput{
				SpaceKey: "PROJ",
				Title:    "Child Page",
				Body:     "<p>Child</p>",
				ParentID: "99",
			},
			want: &Page{ID: "124", Title: "Child Page", Status: "current"},
		},
		{
			name:    "server error",
			input:   &CreatePageInput{SpaceKey: "ERR", Title: "fail", Body: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("/rest/api/content", func(w http.ResponseWriter, r *http.Request) {
				var req map[string]any
				json.NewDecoder(r.Body).Decode(&req)
				if space, _ := req["space"].(map[string]any); space != nil {
					if space["key"] == "ERR" {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.want)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			got, err := newTestConfluenceClient(srv.URL).CreatePage(context.Background(), tt.input)
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
				t.Errorf("CreatePage() diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUpdatePage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pageID  string
		input   *UpdatePageInput
		want    *Page
		wantErr bool
	}{
		{
			name:   "success",
			pageID: "123",
			input:  &UpdatePageInput{Title: "Updated", Body: "<p>new</p>", Version: 2},
			want:   &Page{ID: "123", Title: "Updated", Status: "current"},
		},
		{
			name:    "server error",
			pageID:  "999",
			input:   &UpdatePageInput{Title: "fail", Body: "", Version: 1},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("/rest/api/content/123", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.want)
			})
			mux.HandleFunc("/rest/api/content/999", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			})
			srv := httptest.NewServer(mux)
			defer srv.Close()

			got, err := newTestConfluenceClient(srv.URL).UpdatePage(context.Background(), tt.pageID, tt.input)
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
				t.Errorf("UpdatePage() diff (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUpdatePageAutoVersion(t *testing.T) {
	t.Parallel()
	current := &Page{ID: "123", Title: "Old Title", Status: "current", Version: Version{Number: 3}}
	updated := &Page{ID: "123", Title: "New Title", Status: "current", Version: Version{Number: 4}}

	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/content/123", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(current)
		} else {
			// verify version was auto-incremented
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)
			ver := req["version"].(map[string]any)
			if int(ver["number"].(float64)) != 4 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(updated)
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := newTestConfluenceClient(srv.URL).UpdatePage(context.Background(), "123", &UpdatePageInput{
		Title:   "New Title",
		Body:    "<p>new</p>",
		Version: 0, // auto-fetch
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Version.Number != 4 {
		t.Errorf("expected version 4, got %d", got.Version.Number)
	}
}

func TestBuildRestrictionBody(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    *RestrictionInput
		expected []map[string]any
	}{
		{
			name: "user type",
			input: &RestrictionInput{
				Operation: "update",
				Type:      "user",
				Name:      "acc-id-123",
			},
			expected: []map[string]any{
				{
					"operation": "update",
					"restrictions": map[string]any{
						"user": map[string]any{
							"type":      "known",
							"accountId": "acc-id-123",
						},
					},
				},
			},
		},
		{
			name: "group type",
			input: &RestrictionInput{
				Operation: "read",
				Type:      "group",
				Name:      "engineering",
			},
			expected: []map[string]any{
				{
					"operation": "read",
					"restrictions": map[string]any{
						"group": map[string]any{
							"type": "group",
							"name": "engineering",
						},
					},
				},
			},
		},
		{
			name: "non-user type falls into group branch",
			input: &RestrictionInput{
				Operation: "update",
				Type:      "role",
				Name:      "admins",
			},
			expected: []map[string]any{
				{
					"operation": "update",
					"restrictions": map[string]any{
						"group": map[string]any{
							"type": "group",
							"name": "admins",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := buildRestrictionBody(tt.input)
			if diff := cmp.Diff(tt.expected, got); diff != "" {
				t.Errorf("buildRestrictionBody() diff (-want +got):\n%s", diff)
			}
		})
	}
}

// pagedHandler serves items with start/limit pagination and a next link,
// capping pages at serverMax like real servers do.
func pagedHandler[T any](t *testing.T, items []T, serverMax int, check func(*http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		start, _ := strconv.Atoi(r.URL.Query().Get("start"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		end := min(len(items), start+min(limit, serverMax))
		resp := pageResponse[T]{Results: items[start:end], Start: start, Limit: limit, Size: end - start}
		if end < len(items) {
			resp.Links.Next = "/next"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func pagesNamed(n int) []Page {
	pages := make([]Page, n)
	for i := range pages {
		pages[i] = Page{ID: strconv.Itoa(i + 1), Title: "Page " + strconv.Itoa(i+1)}
	}
	return pages
}

func pageIDs(pages []Page) []string {
	ids := make([]string, len(pages))
	for i, p := range pages {
		ids[i] = p.ID
	}
	return ids
}

func TestSearchCQLPaginates(t *testing.T) {
	t.Parallel()
	all := pagesNamed(7)
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/content/search", pagedHandler(t, all, 3, func(r *http.Request) {
		if got := r.URL.Query().Get("cql"); got != `space = "DEV"` {
			t.Errorf("cql = %q", got)
		}
	}))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestConfluenceClient(srv.URL)

	got, err := c.SearchCQL(context.Background(), `space = "DEV"`, 0)
	if err != nil {
		t.Fatalf("SearchCQL() error: %v", err)
	}
	if diff := cmp.Diff(pageIDs(all), pageIDs(got)); diff != "" {
		t.Errorf("all results diff (-want +got):\n%s", diff)
	}

	got, err = c.SearchCQL(context.Background(), `space = "DEV"`, 4)
	if err != nil {
		t.Fatalf("SearchCQL() error: %v", err)
	}
	if diff := cmp.Diff([]string{"1", "2", "3", "4"}, pageIDs(got)); diff != "" {
		t.Errorf("limited results diff (-want +got):\n%s", diff)
	}
}

func TestGetVersionsFallsBackToExperimental(t *testing.T) {
	t.Parallel()
	versions := []Version{{Number: 2, Message: "second"}, {Number: 1}}
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/content/42/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"message":"not found"}`)
	})
	mux.HandleFunc("/rest/experimental/content/42/version", pagedHandler(t, versions, 50, nil))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	got, err := newTestConfluenceClient(srv.URL).GetVersions(context.Background(), "42", 0)
	if err != nil {
		t.Fatalf("GetVersions() error: %v", err)
	}
	if diff := cmp.Diff(versions, got); diff != "" {
		t.Errorf("GetVersions() diff (-want +got):\n%s", diff)
	}
}

// pageServer records write requests and serves one page and its attachments.
func pageServer(t *testing.T, writes *[]string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rest/api/content/10", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"10","title":"Src","space":{"key":"DEV"},"ancestors":[{"id":"1"},{"id":"5"}],
			"version":{"number":3},"body":{"storage":{"value":"<p>x</p>"}}}`)
	})
	mux.HandleFunc("GET /rest/api/content/10/child/attachment", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"results":[{"id":"att1","title":"old.png"}]}`)
	})
	record := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			body = nil
		}
		*writes = append(*writes, r.Method+" "+r.URL.Path+" "+string(body))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/data"):
			io.WriteString(w, `{"id":"att1","title":"old.png"}`)
		case strings.HasSuffix(r.URL.Path, "/child/attachment"):
			io.WriteString(w, `{"results":[{"id":"att2","title":"new.png"}]}`)
		default:
			io.WriteString(w, `{"id":"11","title":"t"}`)
		}
	}
	mux.HandleFunc("POST /", record)
	mux.HandleFunc("PUT /", record)
	return httptest.NewServer(mux)
}

func TestCopyAndMovePage(t *testing.T) {
	t.Parallel()
	var writes []string
	srv := pageServer(t, &writes)
	defer srv.Close()
	c := newTestConfluenceClient(srv.URL)
	ctx := context.Background()

	if _, err := c.CopyPage(ctx, "10", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CopyPage(ctx, "10", "Other", "OPS", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.MovePage(ctx, "10", "7"); err != nil {
		t.Fatal(err)
	}

	var bodies []map[string]any
	for _, w := range writes {
		var m map[string]any
		_ = json.Unmarshal([]byte(w[strings.Index(w, " {")+1:]), &m)
		bodies = append(bodies, m)
	}
	// Same space: copied next to the source, under its parent.
	if bodies[0]["title"] != "Copy of Src" || cmp.Diff([]any{map[string]any{"id": "5"}}, bodies[0]["ancestors"]) != "" {
		t.Errorf("copy in place = %v", bodies[0])
	}
	// Other space: no parent from the source space.
	if bodies[1]["title"] != "Other" || bodies[1]["ancestors"] != nil {
		t.Errorf("copy to other space = %v", bodies[1])
	}
	if !strings.HasPrefix(writes[2], "PUT /rest/api/content/10 ") ||
		cmp.Diff([]any{map[string]any{"id": "7"}}, bodies[2]["ancestors"]) != "" ||
		bodies[2]["version"].(map[string]any)["number"] != 4.0 {
		t.Errorf("move = %s", writes[2])
	}
}

func TestUploadAttachmentNewAndExisting(t *testing.T) {
	t.Parallel()
	var writes []string
	srv := pageServer(t, &writes)
	defer srv.Close()
	c := newTestConfluenceClient(srv.URL)

	att, err := c.UploadAttachment(context.Background(), "10", "new.png", strings.NewReader("a"))
	if err != nil || att.ID != "att2" {
		t.Fatalf("new upload: %+v %v", att, err)
	}
	att, err = c.UploadAttachment(context.Background(), "10", "old.png", strings.NewReader("b"))
	if err != nil || att.ID != "att1" {
		t.Fatalf("new version upload: %+v %v", att, err)
	}
	want := []string{"POST /rest/api/content/10/child/attachment ", "POST /rest/api/content/10/child/attachment/att1/data "}
	if diff := cmp.Diff(want, writes); diff != "" {
		t.Errorf("requests diff (-want +got):\n%s", diff)
	}
}
