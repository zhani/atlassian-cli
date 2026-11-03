package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// The Confluence side is an in-memory page store seeded with a demo page.
// Point CONFLUENCE_URL at the same address as JIRA_URL.

type page struct {
	ID, Title, Space, Parent, Body string
	Versions                       []version // oldest first
}

type version struct {
	Number  int    `json:"number"`
	When    string `json:"when"`
	Message string `json:"message"`
}

const demoStorage = `<h1>Welcome</h1><p>This page is served by <strong>devserver</strong>.</p>` +
	`<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[func main() {
	fmt.Println("hi")
}]]></ac:plain-text-body></ac:structured-macro>` +
	`<ac:structured-macro ac:name="note"><ac:rich-text-body><p>Edit me with <code>confluence page edit</code>.</p></ac:rich-text-body></ac:structured-macro>` +
	`<table><tbody><tr><th>Key</th><th>Value</th></tr><tr><td>a</td><td>1</td></tr></tbody></table>`

const lossyStorage = `<p>See <ac:link><ri:page ri:content-title="Welcome" /></ac:link>.</p>` +
	`<ac:structured-macro ac:name="toc" />`

func (s *store) seedConfluence() {
	s.pages = map[string]*page{}
	s.addPage("Welcome", "DEMO", "", demoStorage)
	s.addPage("Uses macros", "DEMO", "1", lossyStorage)
	diagrams := s.addPage("Diagrams", "DEMO", "1", "")
	links := s.attachDemoImages(diagrams.ID)
	diagrams.Body = `<p>The system at a glance:</p>` +
		`<ac:image ac:alt="Architecture"><ri:attachment ri:filename="architecture.png" /></ac:image>` +
		`<h2>Throughput</h2><p>Requests per day last week.</p>` +
		`<ac:image><ri:attachment ri:filename="chart.jpg" /></ac:image>` +
		`<h2>Linked image</h2><p>An image by URL rather than as an attachment:</p>` +
		`<ac:image><ri:url ri:value="` + s.baseURL + links["gradient.png"] + `" /></ac:image>` +
		`<table><tbody><tr><th>Name</th><th>Preview</th></tr>` +
		`<tr><td>chart</td><td><ac:image ac:height="40"><ri:attachment ri:filename="chart.jpg" /></ac:image></td></tr></tbody></table>` +
		`<h2>Screenshot</h2><p>A large image (over 1 MiB as PNG):</p>` +
		`<ac:image><ri:attachment ri:filename="screenshot.png" /></ac:image>` +
		`<p>A missing attachment:</p><ac:image><ri:attachment ri:filename="missing.png" /></ac:image>`
}

func (s *store) addPage(title, space, parent, body string) *page {
	p := &page{
		ID: strconv.Itoa(len(s.pageOrder) + 1), Title: title, Space: space, Parent: parent, Body: body,
		Versions: []version{{Number: 1, When: "2026-01-01T00:00:00.000Z"}},
	}
	s.pages[p.ID] = p
	s.pageOrder = append(s.pageOrder, p.ID)
	return p
}

func (s *store) registerConfluence(mux *http.ServeMux) {
	mux.HandleFunc("GET /rest/api/space", s.listSpaces)
	mux.HandleFunc("GET /rest/api/content", s.listContent)
	mux.HandleFunc("POST /rest/api/content", s.createPage)
	mux.HandleFunc("GET /rest/api/content/search", s.searchContent)
	mux.HandleFunc("GET /rest/api/content/{id}", s.getPage)
	mux.HandleFunc("PUT /rest/api/content/{id}", s.updatePage)
	mux.HandleFunc("GET /rest/api/content/{id}/child/page", s.childPages)
	// Like Server/DC: version history only under /rest/experimental.
	mux.HandleFunc("GET /rest/experimental/content/{id}/version", s.pageVersions)
}

func (s *store) pageJSON(p *page, withBody bool) map[string]any {
	cur := p.Versions[len(p.Versions)-1]
	var ancestors []any
	for parent := p.Parent; parent != ""; {
		pp, ok := s.pages[parent]
		if !ok {
			break
		}
		ancestors = append([]any{map[string]any{"id": pp.ID, "title": pp.Title}}, ancestors...)
		parent = pp.Parent
	}
	out := map[string]any{
		"id": p.ID, "type": "page", "status": "current", "title": p.Title, "ancestors": ancestors,
		"space":   map[string]any{"key": p.Space, "name": p.Space + " space"},
		"version": cur,
		"_links":  map[string]any{"webui": "/pages/viewpage.action?pageId=" + p.ID},
	}
	if withBody {
		out["body"] = map[string]any{"storage": map[string]any{"value": p.Body, "representation": "storage"}}
	}
	return out
}

// paged writes items with start/limit pagination and a next link.
func paged[T any](w http.ResponseWriter, r *http.Request, items []T) {
	start := intParam(r, "start", 0)
	end := min(len(items), start+intParam(r, "limit", 25))
	start = min(start, end)
	resp := map[string]any{"results": items[start:end], "start": start, "limit": end - start, "size": end - start, "_links": map[string]any{}}
	if end < len(items) {
		resp["_links"] = map[string]any{"next": "/next"}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *store) listSpaces(w http.ResponseWriter, r *http.Request) {
	seen := map[string]bool{}
	var spaces []any
	for _, id := range s.pageOrder {
		if key := s.pages[id].Space; !seen[key] {
			seen[key] = true
			spaces = append(spaces, map[string]any{"key": key, "name": key + " space", "type": "global"})
		}
	}
	paged(w, r, spaces)
}

func (s *store) listContent(w http.ResponseWriter, r *http.Request) {
	space := r.URL.Query().Get("spaceKey")
	var out []any
	for _, id := range s.pageOrder {
		if p := s.pages[id]; space == "" || p.Space == space {
			out = append(out, s.pageJSON(p, false))
		}
	}
	paged(w, r, out)
}

var (
	cqlSpace = regexp.MustCompile(`space\s*=\s*"?([^"\s]+)"?`)
	cqlText  = regexp.MustCompile(`(?:text|title)\s*~\s*"([^"]*)"`)
)

// searchContent understands `space = KEY` and `text ~ "words"` (substring
// match on title and body); other CQL is ignored.
func (s *store) searchContent(w http.ResponseWriter, r *http.Request) {
	cql := r.URL.Query().Get("cql")
	var space, text string
	if m := cqlSpace.FindStringSubmatch(cql); m != nil {
		space = m[1]
	}
	if m := cqlText.FindStringSubmatch(cql); m != nil {
		text = strings.ToLower(m[1])
	}
	var out []any
	for _, id := range s.pageOrder {
		p := s.pages[id]
		if space != "" && p.Space != space {
			continue
		}
		if text != "" && !strings.Contains(strings.ToLower(p.Title+" "+p.Body), text) {
			continue
		}
		out = append(out, s.pageJSON(p, false))
	}
	paged(w, r, out)
}

func (s *store) getPage(w http.ResponseWriter, r *http.Request) {
	p, ok := s.pages[r.PathValue("id")]
	if !ok {
		notFound(w, "Page "+r.PathValue("id"))
		return
	}
	writeJSON(w, http.StatusOK, s.pageJSON(p, true))
}

type pageWrite struct {
	Title string `json:"title"`
	Space struct {
		Key string `json:"key"`
	} `json:"space"`
	Ancestors []struct {
		ID string `json:"id"`
	} `json:"ancestors"`
	Body struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
	Version version `json:"version"`
}

func (s *store) createPage(w http.ResponseWriter, r *http.Request) {
	var in pageWrite
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Title == "" || in.Space.Key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "title and space are required"})
		return
	}
	parent := ""
	if len(in.Ancestors) > 0 {
		parent = in.Ancestors[0].ID
	}
	p := s.addPage(in.Title, in.Space.Key, parent, in.Body.Storage.Value)
	writeJSON(w, http.StatusOK, s.pageJSON(p, true))
}

func (s *store) updatePage(w http.ResponseWriter, r *http.Request) {
	p, ok := s.pages[r.PathValue("id")]
	if !ok {
		notFound(w, "Page "+r.PathValue("id"))
		return
	}
	var in pageWrite
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": err.Error()})
		return
	}
	if want := p.Versions[len(p.Versions)-1].Number + 1; in.Version.Number != want {
		writeJSON(w, http.StatusConflict, map[string]any{"message": "Version must be incremented on update. Current version is: " + strconv.Itoa(want-1)})
		return
	}
	if in.Title != "" {
		p.Title = in.Title
	}
	if len(in.Ancestors) > 0 {
		p.Parent = in.Ancestors[0].ID
	}
	p.Body = in.Body.Storage.Value
	in.Version.When = "2026-01-01T00:00:00.000Z"
	p.Versions = append(p.Versions, in.Version)
	writeJSON(w, http.StatusOK, s.pageJSON(p, true))
}

func (s *store) childPages(w http.ResponseWriter, r *http.Request) {
	var out []any
	for _, id := range s.pageOrder {
		if p := s.pages[id]; p.Parent == r.PathValue("id") {
			out = append(out, s.pageJSON(p, false))
		}
	}
	paged(w, r, out)
}

func (s *store) pageVersions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.pages[r.PathValue("id")]
	if !ok {
		notFound(w, "Page "+r.PathValue("id"))
		return
	}
	out := make([]version, 0, len(p.Versions))
	for i := len(p.Versions) - 1; i >= 0; i-- {
		out = append(out, p.Versions[i])
	}
	paged(w, r, out)
}
