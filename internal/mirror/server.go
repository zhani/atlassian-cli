package mirror

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Server holds a loaded mirror and serves it.
type Server struct {
	Dir      string
	Manifest Manifest

	issues   map[string]map[string]any // key -> raw issue
	order    []string
	issueDir map[string]string // key -> issue directory
	names    map[string]string // field ID -> name
	byName   map[string][]string
	myself   map[string]any
	fields   json.RawMessage
	links    json.RawMessage
	boards   []boardData
	jiraAtt  map[string]string // attachment ID -> file

	pages     map[string]map[string]any
	pageOrder []string
	pageAtts  map[string][]map[string]any // page ID -> attachment list
	pageDir   map[string]string
	confFiles map[string]string // attachment ID -> file
	urlFiles  map[string]string // site path -> file
	spaces    map[string]map[string]any

	jiraMux, confMux   *http.ServeMux
	jiraBase, confBase string // what the original URLs are rewritten to
	now                func() time.Time
}

type boardData struct {
	Board   map[string]any   `json:"board"`
	Sprints []map[string]any `json:"sprints"`
	JQL     string           `json:"jql,omitempty"`
}

// Load reads a mirror directory: the mirrored data first, then local/ on
// top of it.
func Load(dir string) (*Server, error) {
	m, err := ReadManifest(dir)
	if err != nil {
		return nil, err
	}
	s := &Server{
		Dir: dir, Manifest: *m,
		issues: map[string]map[string]any{}, issueDir: map[string]string{},
		names: map[string]string{}, byName: map[string][]string{}, jiraAtt: map[string]string{},
		pages: map[string]map[string]any{}, pageAtts: map[string][]map[string]any{}, pageDir: map[string]string{},
		confFiles: map[string]string{}, urlFiles: map[string]string{}, spaces: map[string]map[string]any{},
		jiraBase: JiraBase, confBase: ConfluenceBase, now: time.Now,
	}
	for _, root := range []string{dir, filepath.Join(dir, "local")} {
		if err := s.loadRoot(root); err != nil {
			return nil, err
		}
	}
	for id, n := range s.names {
		s.byName[strings.ToLower(n)] = append(s.byName[strings.ToLower(n)], id)
	}
	sort.SliceStable(s.order, func(i, j int) bool { return keyLess(s.order[j], s.order[i]) })
	s.routes()
	return s, nil
}

func (s *Server) loadRoot(root string) error {
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	var names map[string]string
	if readJSON(filepath.Join(root, "jira", "names.json"), &names) == nil {
		for k, v := range names {
			s.names[k] = v
		}
	}
	var me map[string]any
	if readJSON(filepath.Join(root, "jira", "myself.json"), &me) == nil {
		s.myself = me
	}
	if b, err := os.ReadFile(filepath.Join(root, "jira", "fields.json")); err == nil {
		s.fields = b
	}
	if b, err := os.ReadFile(filepath.Join(root, "jira", "linktypes.json")); err == nil {
		s.links = b
	}
	boardFiles, _ := filepath.Glob(filepath.Join(root, "jira", "boards", "*.json"))
	for _, f := range boardFiles {
		var b boardData
		if err := readJSON(f, &b); err != nil {
			log.Printf("mirror: skipping %s: %v", f, err)
			continue
		}
		s.boards = append(s.boards, b)
	}
	issueDirs, _ := os.ReadDir(filepath.Join(root, "jira", "issues"))
	for _, e := range issueDirs {
		if !e.IsDir() {
			continue
		}
		d := filepath.Join(root, "jira", "issues", e.Name())
		var is map[string]any
		if err := readJSON(filepath.Join(d, "issue.json"), &is); err != nil {
			continue
		}
		key, _ := is["key"].(string)
		if key == "" {
			continue
		}
		if _, seen := s.issues[key]; !seen {
			s.order = append(s.order, key)
		}
		s.issues[key] = is
		s.issueDir[key] = d
		if n, ok := is["names"].(map[string]any); ok {
			for id, v := range n {
				if name, ok := v.(string); ok && s.names[id] == "" {
					s.names[id] = name
				}
			}
		}
		files, _ := os.ReadDir(filepath.Join(d, "attachments"))
		for _, f := range files {
			s.jiraAtt[f.Name()] = filepath.Join(d, "attachments", f.Name())
		}
	}
	spaceFiles, _ := filepath.Glob(filepath.Join(root, "confluence", "spaces", "*.json"))
	for _, f := range spaceFiles {
		var sp map[string]any
		if readJSON(f, &sp) == nil {
			if k, _ := sp["key"].(string); k != "" {
				s.spaces[k] = sp
			}
		}
	}
	pageDirs, _ := os.ReadDir(filepath.Join(root, "confluence", "pages"))
	for _, e := range pageDirs {
		if !e.IsDir() {
			continue
		}
		d := filepath.Join(root, "confluence", "pages", e.Name())
		var p map[string]any
		if err := readJSON(filepath.Join(d, "page.json"), &p); err != nil {
			continue
		}
		id := str(p["id"])
		if id == "" {
			continue
		}
		if _, seen := s.pages[id]; !seen {
			s.pageOrder = append(s.pageOrder, id)
		}
		s.pages[id] = p
		s.pageDir[id] = d
		var atts []map[string]any
		_ = readJSON(filepath.Join(d, "attachments.json"), &atts)
		s.pageAtts[id] = atts
		files, _ := os.ReadDir(filepath.Join(d, "files"))
		for _, f := range files {
			s.confFiles[f.Name()] = filepath.Join(d, "files", f.Name())
		}
		var urls map[string]string
		if readJSON(filepath.Join(d, "urls.json"), &urls) == nil {
			for path, name := range urls {
				s.urlFiles[path] = filepath.Join(d, "urls", name)
			}
		}
		if sp, ok := p["space"].(map[string]any); ok {
			if k := str(sp["key"]); k != "" && s.spaces[k] == nil {
				s.spaces[k] = sp
			}
		}
	}
	return nil
}

// --- transport ---

type roundTripper struct{ dir string }

func (rt roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if rt.dir == "" {
		return nil, errors.New("no mirror directory is active")
	}
	s, err := ServerFor(rt.dir)
	if err != nil {
		return nil, err
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	resp := rec.Result()
	resp.Request = req
	return resp, nil
}

// ServeHTTP routes by host: the Jira and Confluence bases are different
// hosts. Other hosts (mirror serve) get Jira, and Confluence under /wiki.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		s.readOnly(w, r)
		return
	}
	host := r.URL.Host
	if host == "" {
		host = r.Host
	}
	switch {
	case "http://"+host == ConfluenceBase:
		s.confMux.ServeHTTP(w, r)
	case strings.HasPrefix(r.URL.Path, "/wiki/"):
		r2 := r.Clone(r.Context())
		r2.URL.Path = strings.TrimPrefix(r.URL.Path, "/wiki")
		s.confMux.ServeHTTP(w, r2)
	default:
		s.jiraMux.ServeHTTP(w, r)
	}
}

func (s *Server) readOnly(w http.ResponseWriter, _ *http.Request) {
	writeErr(w, http.StatusMethodNotAllowed, "read-only mirror (exported "+s.Manifest.ExportedAt+"); changes need the real server")
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"errorMessages": []string{msg}, "message": msg})
}

// writeJSON writes v with the original server URLs replaced by the
// mirror's, so links (attachments, images) resolve against the mirror.
func (s *Server) writeJSON(w http.ResponseWriter, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	data = s.rewrite(data)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (s *Server) rewrite(data []byte) []byte {
	for _, p := range [][2]string{{s.Manifest.ConfluenceURL, s.confBase}, {s.Manifest.JiraURL, s.jiraBase}} {
		orig := strings.TrimRight(p[0], "/")
		if orig == "" {
			continue
		}
		data = bytes.ReplaceAll(data, []byte(orig), []byte(p[1]))
		// HTML embedded in JSON escapes / in some exporters.
		data = bytes.ReplaceAll(data, []byte(strings.ReplaceAll(orig, "/", `\/`)), []byte(strings.ReplaceAll(p[1], "/", `\/`)))
	}
	return data
}

func serveFile(w http.ResponseWriter, r *http.Request, path, name string) {
	ct := mime.TypeByExtension(filepath.Ext(name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	http.ServeFile(w, r, path)
}

func notFound(w http.ResponseWriter, what string) {
	writeErr(w, http.StatusNotFound, what+" is not in the offline mirror")
}

// --- helpers on raw JSON ---

func str(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	}
	return ""
}

func obj(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

func intParam(r *http.Request, name string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil && v >= 0 {
		return v
	}
	return def
}

var keyRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_]*)-(\d+)$`)

// keyLess orders issue keys by project, then number.
func keyLess(a, b string) bool {
	ma, mb := keyRe.FindStringSubmatch(a), keyRe.FindStringSubmatch(b)
	if ma == nil || mb == nil || ma[1] != mb[1] {
		return a < b
	}
	na, _ := strconv.Atoi(ma[2])
	nb, _ := strconv.Atoi(mb[2])
	return na < nb
}

func (s *Server) routes() {
	j := http.NewServeMux()
	j.HandleFunc("GET /rest/api/2/myself", s.getMyself)
	j.HandleFunc("GET /rest/api/2/serverInfo", func(w http.ResponseWriter, _ *http.Request) {
		s.writeJSON(w, map[string]any{"baseUrl": s.jiraBase, "serverTitle": "offline mirror (exported " + s.Manifest.ExportedAt + ")"})
	})
	j.HandleFunc("GET /rest/api/2/search", s.searchOffset)
	j.HandleFunc("GET /rest/api/2/search/jql", s.searchToken)
	j.HandleFunc("GET /rest/api/2/issue/{key}", s.getIssue)
	j.HandleFunc("GET /rest/api/2/issue/{key}/comment", s.getComments)
	j.HandleFunc("GET /rest/api/2/issue/{key}/transitions", s.readOnly)
	j.HandleFunc("GET /rest/api/2/issue/{key}/remotelink", func(w http.ResponseWriter, _ *http.Request) { s.writeJSON(w, []any{}) })
	j.HandleFunc("GET /rest/api/2/attachment/{id}", s.getJiraAttachment)
	j.HandleFunc("GET /rest/api/2/field", s.getFields)
	j.HandleFunc("GET /rest/api/2/issueLinkType", func(w http.ResponseWriter, _ *http.Request) {
		if s.links == nil {
			s.writeJSON(w, map[string]any{"issueLinkTypes": []any{}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(s.links)
	})
	j.HandleFunc("GET /rest/api/2/user/search", s.userSearch)
	j.HandleFunc("GET /rest/agile/1.0/board", s.listBoards)
	j.HandleFunc("GET /rest/agile/1.0/board/{id}", s.getBoard)
	j.HandleFunc("GET /rest/agile/1.0/board/{id}/sprint", s.listSprints)
	j.HandleFunc("GET /rest/agile/1.0/sprint/{id}", s.getSprint)
	j.HandleFunc("GET /rest/dev-status/latest/issue/summary", func(w http.ResponseWriter, _ *http.Request) {
		s.writeJSON(w, map[string]any{"summary": map[string]any{}})
	})
	j.HandleFunc("GET /rest/dev-status/latest/issue/detail", func(w http.ResponseWriter, _ *http.Request) {
		s.writeJSON(w, map[string]any{"detail": []any{}})
	})
	j.HandleFunc("GET /", s.jiraFallback)
	s.jiraMux = j

	c := http.NewServeMux()
	c.HandleFunc("GET /rest/api/space", s.listSpaces)
	c.HandleFunc("GET /rest/api/space/{key}", s.getSpace)
	c.HandleFunc("GET /rest/api/space/{key}/content/page", s.rootPages)
	c.HandleFunc("GET /rest/api/content", s.listContent)
	c.HandleFunc("GET /rest/api/content/search", s.searchContent)
	c.HandleFunc("GET /rest/api/content/{id}", s.getPage)
	c.HandleFunc("GET /rest/api/content/{id}/child/page", s.childPages)
	c.HandleFunc("GET /rest/api/content/{id}/child/attachment", s.pageAttachments)
	c.HandleFunc("GET /rest/api/content/{id}/descendant/page", s.descendantPages)
	c.HandleFunc("GET /rest/api/content/{id}/version", s.pageVersions)
	c.HandleFunc("GET /rest/experimental/content/{id}/version", s.pageVersions)
	c.HandleFunc("GET /rest/api/content/{id}/label", s.pageLabels)
	c.HandleFunc("GET /rest/api/content/{id}/restriction", func(w http.ResponseWriter, _ *http.Request) {
		s.writeJSON(w, map[string]any{"results": []any{}, "size": 0})
	})
	c.HandleFunc("GET /", s.confFallback)
	s.confMux = c
}

// --- Jira ---

func (s *Server) getMyself(w http.ResponseWriter, _ *http.Request) {
	if s.myself == nil {
		s.writeJSON(w, map[string]any{"name": "mirror", "displayName": "Offline mirror", "active": true})
		return
	}
	s.writeJSON(w, s.myself)
}

var attachmentPathRe = regexp.MustCompile(`/secure/(?:attachment|thumbnail)/(\d+)/([^/]*)$`)

// jiraFallback serves attachment content (/secure/attachment/<id>/<name>,
// also thumbnails and under a context path).
func (s *Server) jiraFallback(w http.ResponseWriter, r *http.Request) {
	if !s.serveJiraFile(w, r) && !s.serveConfFile(w, r) {
		notFound(w, r.URL.Path)
	}
}

func (s *Server) serveJiraFile(w http.ResponseWriter, r *http.Request) bool {
	m := attachmentPathRe.FindStringSubmatch(r.URL.Path)
	if m == nil {
		return false
	}
	path, ok := s.jiraAtt[m[1]]
	if !ok {
		return false
	}
	name, _ := url.PathUnescape(m[2])
	if a := s.jiraAttachmentMeta(m[1]); a != nil {
		name = str(a["filename"])
	}
	serveFile(w, r, path, name)
	return true
}

func (s *Server) jiraAttachmentMeta(id string) map[string]any {
	for _, key := range s.order {
		atts, _ := obj(s.issues[key], "fields")["attachment"].([]any)
		for _, a := range atts {
			if am, ok := a.(map[string]any); ok && str(am["id"]) == id {
				return am
			}
		}
	}
	return nil
}

func (s *Server) getJiraAttachment(w http.ResponseWriter, r *http.Request) {
	a := s.jiraAttachmentMeta(r.PathValue("id"))
	if a == nil {
		notFound(w, "Attachment "+r.PathValue("id"))
		return
	}
	s.writeJSON(w, a)
}

// issueView returns a copy of an issue limited to the requested fields and
// expansions.
func (s *Server) issueView(is map[string]any, fieldsParam, expand string) map[string]any {
	out := map[string]any{}
	for k, v := range is {
		out[k] = v
	}
	if !strings.Contains(expand, "renderedFields") {
		delete(out, "renderedFields")
	}
	if strings.Contains(expand, "names") {
		names := map[string]string{}
		for id := range obj(is, "fields") {
			if n := s.names[id]; n != "" {
				names[id] = n
			} else {
				names[id] = id
			}
		}
		out["names"] = names
	} else {
		delete(out, "names")
	}
	if fieldsParam == "" || strings.Contains(fieldsParam, "*all") || strings.Contains(fieldsParam, "*navigable") {
		return out
	}
	want := map[string]bool{}
	for _, f := range strings.Split(fieldsParam, ",") {
		f = strings.TrimSpace(f)
		want[f] = true
		for _, id := range s.byName[strings.ToLower(f)] {
			want[id] = true
		}
	}
	fields := map[string]any{}
	for k, v := range obj(is, "fields") {
		if want[k] {
			fields[k] = v
		}
	}
	out["fields"] = fields
	if rf := obj(out, "renderedFields"); rf != nil {
		sub := map[string]any{}
		for k, v := range rf {
			if want[k] {
				sub[k] = v
			}
		}
		out["renderedFields"] = sub
	}
	return out
}

func (s *Server) getIssue(w http.ResponseWriter, r *http.Request) {
	key := strings.ToUpper(r.PathValue("key"))
	is, ok := s.issues[key]
	if !ok {
		for _, k := range s.order { // by numeric ID
			if str(s.issues[k]["id"]) == r.PathValue("key") {
				is, ok = s.issues[k], true
				break
			}
		}
	}
	if !ok {
		notFound(w, "Issue "+r.PathValue("key"))
		return
	}
	q := r.URL.Query()
	s.writeJSON(w, s.issueView(is, q.Get("fields"), q.Get("expand")))
}

func (s *Server) getComments(w http.ResponseWriter, r *http.Request) {
	is, ok := s.issues[strings.ToUpper(r.PathValue("key"))]
	if !ok {
		notFound(w, "Issue "+r.PathValue("key"))
		return
	}
	page := obj(obj(is, "fields"), "comment")
	comments, _ := page["comments"].([]any)
	rendered, _ := obj(obj(is, "renderedFields"), "comment")["comments"].([]any)
	var out []any
	for i, c := range comments {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		cp := map[string]any{}
		for k, v := range cm {
			cp[k] = v
		}
		if i < len(rendered) {
			if rm, ok := rendered[i].(map[string]any); ok {
				cp["renderedBody"] = rm["body"]
			}
		}
		out = append(out, cp)
	}
	if out == nil {
		out = []any{}
	}
	s.writeJSON(w, map[string]any{"comments": out, "total": len(out), "startAt": 0, "maxResults": len(out)})
}

func (s *Server) getFields(w http.ResponseWriter, _ *http.Request) {
	if s.fields != nil {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(s.fields)
		return
	}
	var out []any
	for id, n := range s.names {
		out = append(out, map[string]any{"id": id, "name": n, "custom": strings.HasPrefix(id, "customfield_")})
	}
	s.writeJSON(w, out)
}

// userSearch finds users among the people on mirrored issues.
func (s *Server) userSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(r.URL.Query().Get("query") + r.URL.Query().Get("username"))
	seen := map[string]bool{}
	var out []any
	for _, key := range s.order {
		f := obj(s.issues[key], "fields")
		for _, role := range []string{"assignee", "reporter", "creator"} {
			u := obj(f, role)
			if u == nil {
				continue
			}
			id := str(u["accountId"]) + str(u["name"])
			if seen[id] {
				continue
			}
			hay := strings.ToLower(str(u["displayName"]) + " " + str(u["name"]) + " " + str(u["emailAddress"]))
			if q == "" || strings.Contains(hay, q) {
				seen[id] = true
				out = append(out, u)
			}
		}
	}
	if out == nil {
		out = []any{}
	}
	s.writeJSON(w, out)
}

func (s *Server) runJQL(w http.ResponseWriter, jql string) ([]map[string]any, bool) {
	res, err := s.SearchJQL(jql)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	return res, true
}

func (s *Server) searchOffset(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	matches, ok := s.runJQL(w, q.Get("jql"))
	if !ok {
		return
	}
	start := intParam(r, "startAt", 0)
	end := min(len(matches), start+intParam(r, "maxResults", 50))
	start = min(start, end)
	page := make([]any, 0, end-start)
	for _, is := range matches[start:end] {
		page = append(page, s.issueView(is, q.Get("fields"), q.Get("expand")))
	}
	s.writeJSON(w, map[string]any{"startAt": start, "maxResults": end - start, "total": len(matches), "issues": page})
}

func (s *Server) searchToken(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	matches, ok := s.runJQL(w, q.Get("jql"))
	if !ok {
		return
	}
	start, _ := strconv.Atoi(q.Get("nextPageToken"))
	end := min(len(matches), start+intParam(r, "maxResults", 50))
	start = min(start, end)
	page := make([]any, 0, end-start)
	for _, is := range matches[start:end] {
		page = append(page, s.issueView(is, q.Get("fields"), q.Get("expand")))
	}
	resp := map[string]any{"issues": page, "isLast": end >= len(matches)}
	if end < len(matches) {
		resp["nextPageToken"] = strconv.Itoa(end)
	}
	s.writeJSON(w, resp)
}

func agileValues(w http.ResponseWriter, r *http.Request, s *Server, all []any) {
	start := intParam(r, "startAt", 0)
	end := min(len(all), start+intParam(r, "maxResults", 50))
	start = min(start, end)
	s.writeJSON(w, map[string]any{"values": all[start:end], "startAt": start, "isLast": end >= len(all), "total": len(all)})
}

func (s *Server) listBoards(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("projectKeyOrId")
	var out []any
	for _, b := range s.boards {
		if b.Board == nil {
			continue // sprints without their board
		}
		if project != "" {
			loc := obj(b.Board, "location")
			if !strings.EqualFold(str(loc["projectKey"]), project) && str(loc["projectId"]) != project {
				continue
			}
		}
		out = append(out, b.Board)
	}
	agileValues(w, r, s, out)
}

func (s *Server) board(id string) *boardData {
	for i := range s.boards {
		if s.boards[i].Board != nil && str(s.boards[i].Board["id"]) == id {
			return &s.boards[i]
		}
	}
	return nil
}

func (s *Server) getBoard(w http.ResponseWriter, r *http.Request) {
	b := s.board(r.PathValue("id"))
	if b == nil {
		notFound(w, "Board "+r.PathValue("id"))
		return
	}
	s.writeJSON(w, b.Board)
}

func (s *Server) listSprints(w http.ResponseWriter, r *http.Request) {
	b := s.board(r.PathValue("id"))
	if b == nil {
		notFound(w, "Board "+r.PathValue("id"))
		return
	}
	states := strings.ToLower(r.URL.Query().Get("state"))
	var out []any
	for _, sp := range b.Sprints {
		if states == "" || strings.Contains(states, strings.ToLower(str(sp["state"]))) {
			out = append(out, sp)
		}
	}
	agileValues(w, r, s, out)
}

// allSprints lists the sprints of the mirrored boards, then those only
// known from issues' sprint fields.
func (s *Server) allSprints() []sprintRef {
	seen := map[string]bool{}
	var out []sprintRef
	for _, b := range s.boards {
		for _, sp := range b.Sprints {
			ref := sprintRef{ID: str(sp["id"]), Name: str(sp["name"]), State: strings.ToLower(str(sp["state"])), raw: sp}
			if !seen[ref.ID] {
				seen[ref.ID] = true
				out = append(out, ref)
			}
		}
	}
	for _, key := range s.order {
		for _, ref := range s.sprintsOf(s.issues[key]) {
			if !seen[ref.ID] {
				seen[ref.ID] = true
				out = append(out, ref)
			}
		}
	}
	return out
}

func (s *Server) getSprint(w http.ResponseWriter, r *http.Request) {
	for _, sp := range s.allSprints() {
		if sp.ID == r.PathValue("id") {
			if sp.raw != nil {
				s.writeJSON(w, sp.raw)
			} else {
				id, _ := strconv.Atoi(sp.ID)
				s.writeJSON(w, map[string]any{"id": id, "name": sp.Name, "state": sp.State})
			}
			return
		}
	}
	notFound(w, "Sprint "+r.PathValue("id"))
}

// --- Confluence ---

// paged writes start/limit paginated results with a next link.
func (s *Server) paged(w http.ResponseWriter, r *http.Request, items []any) {
	start := intParam(r, "start", 0)
	end := min(len(items), start+intParam(r, "limit", 25))
	start = min(start, end)
	resp := map[string]any{"results": items[start:end], "start": start, "limit": end - start, "size": end - start, "_links": map[string]any{}}
	if end < len(items) {
		resp["_links"] = map[string]any{"next": "/next"}
	}
	s.writeJSON(w, resp)
}

// parentID is the nearest ancestor of a page, "" for a space root.
func parentID(p map[string]any) string {
	anc, _ := p["ancestors"].([]any)
	if len(anc) == 0 {
		return ""
	}
	a, _ := anc[len(anc)-1].(map[string]any)
	return str(a["id"])
}

func pageSpace(p map[string]any) string { return str(obj(p, "space")["key"]) }

// pageView returns a page for the response: ancestors limited to mirrored
// pages (so the tree hangs a mirrored subtree off its space), and the body
// only when asked for.
func (s *Server) pageView(p map[string]any, expand string) map[string]any {
	out := map[string]any{}
	for k, v := range p {
		out[k] = v
	}
	if anc, ok := p["ancestors"].([]any); ok {
		kept := []any{}
		for _, a := range anc {
			if am, ok := a.(map[string]any); ok && s.pages[str(am["id"])] != nil {
				kept = append(kept, a)
			}
		}
		out["ancestors"] = kept
	}
	if expand != "" && !strings.Contains(expand, "body") {
		delete(out, "body")
	}
	return out
}

// mirroredParent is the nearest ancestor that is in the mirror.
func (s *Server) mirroredParent(p map[string]any) string {
	anc, _ := p["ancestors"].([]any)
	for i := len(anc) - 1; i >= 0; i-- {
		if a, ok := anc[i].(map[string]any); ok && s.pages[str(a["id"])] != nil {
			return str(a["id"])
		}
	}
	return ""
}

func (s *Server) pageList(r *http.Request, keep func(p map[string]any) bool) []any {
	expand := r.URL.Query().Get("expand")
	out := []any{}
	for _, id := range s.pageOrder {
		if p := s.pages[id]; keep(p) {
			out = append(out, s.pageView(p, expand))
		}
	}
	return out
}

func (s *Server) listSpaces(w http.ResponseWriter, r *http.Request) {
	keys := make([]string, 0, len(s.spaces))
	for k := range s.spaces {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]any, len(keys))
	for i, k := range keys {
		out[i] = s.spaces[k]
	}
	s.paged(w, r, out)
}

func (s *Server) getSpace(w http.ResponseWriter, r *http.Request) {
	sp, ok := s.spaces[r.PathValue("key")]
	if !ok {
		notFound(w, "Space "+r.PathValue("key"))
		return
	}
	s.writeJSON(w, sp)
}

func (s *Server) rootPages(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	s.paged(w, r, s.pageList(r, func(p map[string]any) bool {
		return pageSpace(p) == key && s.mirroredParent(p) == ""
	}))
}

func (s *Server) listContent(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	space, title := q.Get("spaceKey"), q.Get("title")
	if t := q.Get("type"); t != "" && t != "page" {
		s.paged(w, r, []any{})
		return
	}
	s.paged(w, r, s.pageList(r, func(p map[string]any) bool {
		return (space == "" || pageSpace(p) == space) && (title == "" || strings.EqualFold(str(p["title"]), title))
	}))
}

func (s *Server) getPage(w http.ResponseWriter, r *http.Request) {
	p, ok := s.pages[r.PathValue("id")]
	if !ok {
		notFound(w, "Page "+r.PathValue("id"))
		return
	}
	s.writeJSON(w, s.pageView(p, ""))
}

func (s *Server) childPages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.paged(w, r, s.pageList(r, func(p map[string]any) bool { return s.mirroredParent(p) == id }))
}

func (s *Server) isDescendant(p map[string]any, id string) bool {
	anc, _ := p["ancestors"].([]any)
	for _, a := range anc {
		if am, ok := a.(map[string]any); ok && str(am["id"]) == id {
			return true
		}
	}
	return false
}

func (s *Server) descendantPages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.paged(w, r, s.pageList(r, func(p map[string]any) bool { return s.isDescendant(p, id) }))
}

func (s *Server) pageAttachments(w http.ResponseWriter, r *http.Request) {
	atts := s.pageAtts[r.PathValue("id")]
	out := make([]any, 0, len(atts))
	for _, a := range atts {
		out = append(out, a)
	}
	s.writeJSON(w, map[string]any{"results": out, "size": len(out), "start": 0, "limit": len(out), "_links": map[string]any{}})
}

func (s *Server) pageVersions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.pages[r.PathValue("id")]
	if !ok {
		notFound(w, "Page "+r.PathValue("id"))
		return
	}
	var out []any
	if v := obj(p, "version"); v != nil {
		out = append(out, v)
	}
	s.paged(w, r, out)
}

func (s *Server) pageLabels(w http.ResponseWriter, r *http.Request) {
	p := s.pages[r.PathValue("id")]
	labels, _ := obj(obj(p, "metadata"), "labels")["results"].([]any)
	if labels == nil {
		labels = []any{}
	}
	s.writeJSON(w, map[string]any{"results": labels, "size": len(labels)})
}

var downloadRe = regexp.MustCompile(`/download/(?:attachments|thumbnails)/(\d+)/([^/?]+)`)

// confFallback serves attachment downloads and mirrored linked images.
// When Jira and Confluence share a host, either one's links may arrive at
// the other's base, so each falls back to the other's files.
func (s *Server) confFallback(w http.ResponseWriter, r *http.Request) {
	if !s.serveConfFile(w, r) && !s.serveJiraFile(w, r) {
		notFound(w, r.URL.Path)
	}
}

func (s *Server) serveConfFile(w http.ResponseWriter, r *http.Request) bool {
	for _, k := range []string{r.URL.RequestURI(), r.URL.Path} {
		if f, ok := s.urlFiles[k]; ok {
			serveFile(w, r, f, r.URL.Path)
			return true
		}
	}
	m := downloadRe.FindStringSubmatch(r.URL.Path)
	if m == nil {
		return false
	}
	name, _ := url.PathUnescape(m[2])
	for _, a := range s.pageAtts[m[1]] {
		if str(a["title"]) == name {
			if f, ok := s.confFiles[str(a["id"])]; ok {
				serveFile(w, r, f, name)
				return true
			}
		}
	}
	return false
}

// ServeAt makes the server rewrite links for being reached over HTTP at
// base (Jira) and base+"/wiki" (Confluence), for `mirror serve`.
func (s *Server) ServeAt(base string) {
	base = strings.TrimRight(base, "/")
	s.jiraBase, s.confBase = base, base+"/wiki"
}

// String is used by `mirror status`.
func (s *Server) String() string {
	return fmt.Sprintf("%d issues, %d pages", len(s.issues), len(s.pages))
}
