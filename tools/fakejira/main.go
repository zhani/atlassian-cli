// Command fakejira is a small stateful stand-in for the Jira Server/DC and
// Cloud REST APIs, for trying out atlassian-cli (especially the TUI) without
// a real instance. It also serves a minimal in-memory Confluence (see
// confluence.go) seeded with demo pages.
//
// It serves issues from a data directory laid out as
//
//	<dir>/<KEY>/<KEY>.json     an issue as printed by `atlassian-cli jira issue get -o json`
//	<dir>/<KEY>/<file>         attachments of that issue (any other file)
//
// or from a single JSON file holding an array of issues (the output of
// `atlassian-cli jira issue search -o json`).
//
// Usage:
//
//	go run ./tools/fakejira -data ./issues
//	JIRA_URL=http://127.0.0.1:18080 JIRA_PERSONAL_TOKEN=x atlassian-cli jira issue search 'project = PROJ'
//
// Supported JQL is deliberately minimal: clauses joined with AND, each of the
// form `field = value`, `field != value` or `field in (a, b)` on key, project,
// status, issuetype, priority, assignee and labels; resolution and anything
// unrecognised is ignored, as is ORDER BY. Assignee clauses are ignored for
// issues whose exported assignee carries no user ID (only a display name).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type store struct {
	mu          sync.Mutex // serializes requests, see serialize
	baseURL     string
	order       []string                  // issue keys in load order
	issues      map[string]map[string]any // key -> raw issue
	attachments map[string]attachment     // attachment id -> file
	statuses    []string                  // every status seen, offered as transitions

	pages     map[string]*page // Confluence pages by ID, see confluence.go
	pageOrder []string
}

type attachment struct {
	ID, Key, Filename, Path, MimeType string
	Size                              int64
}

func main() {
	addr := flag.String("addr", "127.0.0.1:18080", "listen address")
	data := flag.String("data", "", "issue data directory or JSON array file (required)")
	flag.Parse()
	if *data == "" {
		flag.Usage()
		os.Exit(2)
	}

	s := &store{
		baseURL:     "http://" + *addr,
		issues:      map[string]map[string]any{},
		attachments: map[string]attachment{},
	}
	if err := s.load(*data); err != nil {
		log.Fatal(err)
	}
	s.seedConfluence()
	log.Printf("loaded %d issues and %d attachments; listening on %s", len(s.order), len(s.attachments), s.baseURL)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /rest/api/2/myself", s.myself)
	mux.HandleFunc("GET /rest/api/2/search", s.searchOffset)
	mux.HandleFunc("GET /rest/api/2/search/jql", s.searchToken)
	mux.HandleFunc("GET /rest/api/2/issue/{key}", s.getIssue)
	mux.HandleFunc("GET /rest/api/2/issue/{key}/transitions", s.getTransitions)
	mux.HandleFunc("POST /rest/api/2/issue/{key}/transitions", s.doTransition)
	mux.HandleFunc("GET /rest/api/2/issue/{key}/comment", s.getComments)
	mux.HandleFunc("POST /rest/api/2/issue/{key}/comment", s.addComment)
	mux.HandleFunc("POST /rest/api/2/issue", s.createIssue)
	mux.HandleFunc("GET /rest/api/2/attachment/{id}", s.getAttachment)
	mux.HandleFunc("GET /secure/attachment/{id}/{name}", s.downloadAttachment)
	s.registerConfluence(mux)
	log.Fatal(http.ListenAndServe(*addr, s.serialize(mux)))
}

// serialize logs each request and handles one at a time, so handlers can
// read and mutate the store (including while encoding it) without locks.
func (s *store) serialize(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		log.Printf("%s %s", r.Method, r.URL.RequestURI())
		h.ServeHTTP(w, r)
	})
}

// --- loading ---

func (s *store) load(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		var issues []map[string]any
		if err := readJSON(path, &issues); err != nil {
			return err
		}
		for _, is := range issues {
			s.add(is, "")
		}
		return s.finish()
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(path, e.Name())
		var is map[string]any
		if err := readJSON(filepath.Join(dir, e.Name()+".json"), &is); err != nil {
			log.Printf("skipping %s: %v", dir, err)
			continue
		}
		s.add(is, dir)
	}
	return s.finish()
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// add registers an issue and, when dir is set, the files beside its JSON as
// attachments (replacing whatever attachment metadata the JSON had, since its
// content URLs point at the original server).
func (s *store) add(is map[string]any, dir string) {
	key, _ := is["key"].(string)
	if key == "" {
		return
	}
	fields := mapOf(is, "fields")
	if fields == nil {
		fields = map[string]any{}
		is["fields"] = fields
	}
	s.issues[key] = is
	s.order = append(s.order, key)
	if dir == "" {
		return
	}

	var atts []any
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		if f.IsDir() || f.Name() == key+".json" {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		a := attachment{
			ID:       strconv.Itoa(10000 + len(s.attachments)),
			Key:      key,
			Filename: f.Name(),
			Path:     filepath.Join(dir, f.Name()),
			Size:     info.Size(),
			MimeType: mime.TypeByExtension(filepath.Ext(f.Name())),
		}
		if a.MimeType == "" {
			a.MimeType = "application/octet-stream"
		}
		s.attachments[a.ID] = a
		atts = append(atts, s.attachmentJSON(a))
	}
	fields["attachment"] = atts
}

func (s *store) finish() error {
	if len(s.order) == 0 {
		return fmt.Errorf("no issues found")
	}
	seen := map[string]bool{}
	for _, key := range s.order {
		if name := nameOf(mapOf(s.issues[key], "fields"), "status"); name != "" && !seen[name] {
			seen[name] = true
			s.statuses = append(s.statuses, name)
		}
	}
	sort.Strings(s.statuses)
	return nil
}

func (s *store) attachmentJSON(a attachment) map[string]any {
	return map[string]any{
		"id":       a.ID,
		"filename": a.Filename,
		"size":     a.Size,
		"mimeType": a.MimeType,
		"created":  "2026-01-01T00:00:00.000+0000",
		"author":   map[string]any{"displayName": "fakejira"},
		"content":  s.baseURL + "/secure/attachment/" + a.ID + "/" + a.Filename,
	}
}

// --- handlers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter, what string) {
	writeJSON(w, http.StatusNotFound, map[string]any{"errorMessages": []string{what + " does not exist"}})
}

func (s *store) myself(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"name": "fake", "displayName": "Fake User", "active": true})
}

// searchOffset implements the Server/DC startAt/maxResults pagination.
func (s *store) searchOffset(w http.ResponseWriter, r *http.Request) {
	matches := s.search(r.URL.Query().Get("jql"))
	start := intParam(r, "startAt", 0)
	end := min(len(matches), start+intParam(r, "maxResults", 50))
	start = min(start, end)
	writeJSON(w, http.StatusOK, map[string]any{
		"startAt": start, "maxResults": end - start, "total": len(matches), "issues": matches[start:end],
	})
}

// searchToken implements the Cloud nextPageToken pagination; the token is
// simply the offset of the next page.
func (s *store) searchToken(w http.ResponseWriter, r *http.Request) {
	matches := s.search(r.URL.Query().Get("jql"))
	start, _ := strconv.Atoi(r.URL.Query().Get("nextPageToken"))
	end := min(len(matches), start+intParam(r, "maxResults", 50))
	start = min(start, end)
	resp := map[string]any{"issues": matches[start:end], "isLast": end >= len(matches)}
	if end < len(matches) {
		resp["nextPageToken"] = strconv.Itoa(end)
	}
	writeJSON(w, http.StatusOK, resp)
}

func intParam(r *http.Request, name string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil && v >= 0 {
		return v
	}
	return def
}

func (s *store) search(jql string) []map[string]any {
	clauses := parseJQL(jql)
	var out []map[string]any
	for _, key := range s.order {
		is := s.issues[key]
		if matchesAll(is, clauses) {
			out = append(out, is)
		}
	}
	return out
}

func (s *store) getIssue(w http.ResponseWriter, r *http.Request) {
	is, ok := s.issues[r.PathValue("key")]
	if !ok {
		notFound(w, "Issue "+r.PathValue("key"))
		return
	}
	if !strings.Contains(r.URL.Query().Get("expand"), "renderedFields") {
		writeJSON(w, http.StatusOK, is)
		return
	}
	// Jira renders wiki markup to HTML; approximate it with escaped
	// paragraphs so the rendered code path is exercised.
	out := map[string]any{}
	for k, v := range is {
		out[k] = v
	}
	desc, _ := mapOf(is, "fields")["description"].(string)
	out["renderedFields"] = map[string]any{"description": renderParagraphs(desc)}
	writeJSON(w, http.StatusOK, out)
}

func renderParagraphs(text string) string {
	var sb strings.Builder
	for _, p := range regexp.MustCompile(`\n\s*\n`).Split(strings.TrimSpace(text), -1) {
		if p = strings.TrimSpace(p); p != "" {
			sb.WriteString("<p>" + strings.ReplaceAll(html.EscapeString(p), "\n", "<br/>") + "</p>")
		}
	}
	return sb.String()
}

func (s *store) getTransitions(w http.ResponseWriter, r *http.Request) {
	is, ok := s.issues[r.PathValue("key")]
	if !ok {
		notFound(w, "Issue "+r.PathValue("key"))
		return
	}
	current := nameOf(mapOf(is, "fields"), "status")
	var ts []any
	for i, name := range s.statuses {
		if name == current {
			continue
		}
		ts = append(ts, map[string]any{"id": strconv.Itoa(i + 1), "name": name, "to": map[string]any{"name": name}})
	}
	writeJSON(w, http.StatusOK, map[string]any{"transitions": ts})
}

func (s *store) doTransition(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Transition struct {
			ID string `json:"id"`
		} `json:"transition"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{err.Error()}})
		return
	}
	is, ok := s.issues[r.PathValue("key")]
	if !ok {
		notFound(w, "Issue "+r.PathValue("key"))
		return
	}
	i, err := strconv.Atoi(body.Transition.ID)
	if err != nil || i < 1 || i > len(s.statuses) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{"invalid transition " + body.Transition.ID}})
		return
	}
	fields := mapOf(is, "fields")
	status := mapOf(fields, "status")
	if status == nil {
		status = map[string]any{}
		fields["status"] = status
	}
	status["name"] = s.statuses[i-1]
	w.WriteHeader(http.StatusNoContent)
}

func (s *store) getComments(w http.ResponseWriter, r *http.Request) {
	is, ok := s.issues[r.PathValue("key")]
	if !ok {
		notFound(w, "Issue "+r.PathValue("key"))
		return
	}
	comments := mapOf(mapOf(is, "fields"), "comment")
	if comments == nil {
		comments = map[string]any{"comments": []any{}, "total": 0}
	}
	writeJSON(w, http.StatusOK, comments)
}

func (s *store) addComment(w http.ResponseWriter, r *http.Request) {
	is, ok := s.issues[r.PathValue("key")]
	if !ok {
		notFound(w, "Issue "+r.PathValue("key"))
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{err.Error()}})
		return
	}
	log.Printf("comment on %s:\n%s", r.PathValue("key"), body.Body)

	fields := mapOf(is, "fields")
	page := mapOf(fields, "comment")
	if page == nil {
		page = map[string]any{"comments": []any{}, "total": 0}
		fields["comment"] = page
	}
	comments, _ := page["comments"].([]any)
	c := map[string]any{
		"id":      strconv.Itoa(len(comments) + 1),
		"body":    body.Body,
		"author":  map[string]any{"displayName": "Fake User"},
		"created": "2026-01-01T00:00:00.000+0000",
	}
	page["comments"] = append(comments, c)
	page["total"] = len(comments) + 1
	writeJSON(w, http.StatusCreated, c)
}

// createIssue stores a new issue under the next free key of its project.
func (s *store) createIssue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Fields map[string]any `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Fields == nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errorMessages": []string{"invalid issue"}})
		return
	}
	project, _ := mapOf(body.Fields, "project")["key"].(string)
	if project == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"errors": map[string]string{"project": "project is required"}})
		return
	}
	n := 1
	for _, k := range s.order {
		if p, num, ok := strings.Cut(k, "-"); ok && p == project {
			if v, err := strconv.Atoi(num); err == nil && v >= n {
				n = v + 1
			}
		}
	}
	key := project + "-" + strconv.Itoa(n)
	log.Printf("create %s: %v\ndescription:\n%v", key, body.Fields["summary"], body.Fields["description"])

	body.Fields["status"] = map[string]any{"name": "Open"}
	body.Fields["created"] = "2026-01-01T00:00:00.000+0000"
	is := map[string]any{"id": strconv.Itoa(100000 + len(s.order)), "key": key, "fields": body.Fields}
	s.issues[key] = is
	s.order = append(s.order, key)
	writeJSON(w, http.StatusCreated, map[string]any{"id": is["id"], "key": key, "self": s.baseURL + "/rest/api/2/issue/" + key})
}

func (s *store) getAttachment(w http.ResponseWriter, r *http.Request) {
	a, ok := s.attachments[r.PathValue("id")]
	if !ok {
		notFound(w, "Attachment "+r.PathValue("id"))
		return
	}
	writeJSON(w, http.StatusOK, s.attachmentJSON(a))
}

func (s *store) downloadAttachment(w http.ResponseWriter, r *http.Request) {
	a, ok := s.attachments[r.PathValue("id")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", a.MimeType)
	http.ServeFile(w, r, a.Path)
}

// --- minimal JQL ---

type clause struct {
	field  string
	negate bool
	values []string
}

var clauseRe = regexp.MustCompile(`(?i)^\s*("?[\w ]+"?)\s*(!=|=|not\s+in|in)\s*(.+?)\s*$`)

func parseJQL(jql string) []clause {
	if i := strings.Index(strings.ToLower(jql), "order by"); i >= 0 {
		jql = jql[:i]
	}
	var out []clause
	for _, part := range regexp.MustCompile(`(?i)\s+and\s+`).Split(jql, -1) {
		m := clauseRe.FindStringSubmatch(part)
		if m == nil {
			continue
		}
		field := strings.ToLower(strings.Trim(m[1], `" `))
		op := strings.ToLower(strings.Join(strings.Fields(m[2]), " "))
		raw := strings.Trim(m[3], "() ")
		var values []string
		for _, v := range strings.Split(raw, ",") {
			values = append(values, strings.ToLower(strings.Trim(strings.TrimSpace(v), `"'`)))
		}
		out = append(out, clause{field: field, negate: op == "!=" || op == "not in", values: values})
	}
	return out
}

func matchesAll(is map[string]any, clauses []clause) bool {
	for _, c := range clauses {
		got, ok := fieldValues(is, c.field)
		if !ok {
			continue // unsupported field: ignore the clause
		}
		hit := false
		for _, v := range c.values {
			for _, g := range got {
				if strings.EqualFold(g, v) {
					hit = true
				}
			}
		}
		if hit == c.negate {
			return false
		}
	}
	return true
}

// fieldValues returns the comparable values of a JQL field on an issue.
func fieldValues(is map[string]any, field string) ([]string, bool) {
	f := mapOf(is, "fields")
	switch field {
	case "key", "issuekey":
		k, _ := is["key"].(string)
		return []string{k}, true
	case "project":
		p := mapOf(f, "project")
		k, _ := p["key"].(string)
		n, _ := p["name"].(string)
		return []string{k, n}, true
	case "status":
		return []string{nameOf(f, "status")}, true
	case "issuetype", "type":
		return []string{nameOf(f, "issuetype")}, true
	case "priority":
		return []string{nameOf(f, "priority")}, true
	case "assignee":
		a := mapOf(f, "assignee")
		var vals []string
		for _, k := range []string{"name", "key", "accountId", "emailAddress"} {
			if v, ok := a[k].(string); ok && v != "" {
				vals = append(vals, v)
			}
		}
		if a != nil && len(vals) == 0 {
			return nil, false // exported data without user IDs: can't tell, ignore
		}
		if dn, ok := a["displayName"].(string); ok {
			vals = append(vals, dn)
		}
		return vals, true
	case "labels":
		var vals []string
		if ls, ok := f["labels"].([]any); ok {
			for _, l := range ls {
				if s, ok := l.(string); ok {
					vals = append(vals, s)
				}
			}
		}
		return vals, true
	}
	return nil, false
}

func mapOf(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

func nameOf(m map[string]any, key string) string {
	n, _ := mapOf(m, key)["name"].(string)
	return n
}
