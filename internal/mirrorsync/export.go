// Package mirrorsync makes offline mirror archives from the live Jira and
// Confluence (Export) and merges them into a mirror directory (Import). The
// mirror package serves the result.
package mirrorsync

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zhani/atlassian-cli/internal/bookmarks"
	"github.com/zhani/atlassian-cli/internal/config"
	"github.com/zhani/atlassian-cli/internal/confluence"
	"github.com/zhani/atlassian-cli/internal/jira"
	"github.com/zhani/atlassian-cli/internal/mirror"
)

// ExportOptions selects what to export.
type ExportOptions struct {
	Out       string   // .tar.gz to write
	Boards    []int    // agile boards: their filter's issues and their sprints
	JQL       []string // extra queries
	Pages     []string // Confluence pages, exported with all descendants
	Bookmarks bool     // also everything bookmarked, and the bookmark file
	// ActiveSprints also exports the issues of the active sprints of the
	// exported boards (else of the default project), when there are any.
	ActiveSprints bool
	// MyIssues also exports the issues assigned to the exporting user that
	// are unresolved or resolved as anything but Done/Fixed.
	MyIssues bool
	Since    string // only issues/pages changed since (empty: full snapshot)
	Log      io.Writer
}

// Exporter talks to the live servers. Jira or Confluence may be nil when
// not configured.
type Exporter struct {
	Jira       *jira.Client
	Confluence *confluence.Client
	JiraCfg    *config.AtlassianConfig
	ConfCfg    *config.AtlassianConfig

	opts     ExportOptions
	since    time.Time
	stage    string
	done     map[string]bool // exported issue keys
	pageDone map[string]bool
	attSeen  map[string]bool
	names    map[string]string
	warnings int
	stats    struct{ issues, pages, attachments int }
}

func (e *Exporter) logf(format string, args ...any) {
	if e.opts.Log != nil {
		fmt.Fprintf(e.opts.Log, format+"\n", args...)
	}
}

func (e *Exporter) warn(format string, args ...any) {
	e.warnings++
	e.logf("warning: "+format, args...)
}

// Export writes the archive and returns its manifest.
func (e *Exporter) Export(ctx context.Context, opts ExportOptions) (*mirror.Manifest, error) {
	started := time.Now().UTC()
	e.opts = opts
	e.done, e.pageDone, e.attSeen, e.names = map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]string{}
	if opts.Since != "" {
		t, err := mirror.ParseSince(opts.Since, time.Now())
		if err != nil {
			return nil, fmt.Errorf("--since: %w", err)
		}
		e.since = t
	}
	out, err := filepath.Abs(opts.Out)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return nil, err
	}
	e.stage, err = os.MkdirTemp(filepath.Dir(out), ".mirror-export-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(e.stage)

	var marks []bookmarks.Bookmark
	var bookmarkFile string
	if opts.Bookmarks {
		store, err := bookmarks.Open()
		if err != nil {
			return nil, err
		}
		if marks, err = store.List(); err != nil {
			return nil, err
		}
		bookmarkFile = store.Path
	}

	m := &mirror.Manifest{Version: 1, ExportedAt: started.Format(time.RFC3339), Since: ""}
	if !e.since.IsZero() {
		m.Since = e.since.UTC().Format(time.RFC3339)
	}

	if e.Jira != nil {
		m.JiraURL, m.Cloud = e.Jira.BaseURL(), e.Jira.IsCloud()
		m.Boards = opts.Boards
		if len(m.Boards) == 0 && e.JiraCfg.DefaultBoard != "" {
			if id, err := strconv.Atoi(e.JiraCfg.DefaultBoard); err == nil {
				m.Boards = []int{id}
			}
		}
		m.Settings = mirror.Settings{
			DefaultProject: e.JiraCfg.DefaultProject, DefaultBoard: e.JiraCfg.DefaultBoard,
			FeatureType: e.JiraCfg.FeatureType, FeatureLinkTypes: e.JiraCfg.FeatureLinkTypes,
			EpicLinkField: e.JiraCfg.EpicLinkField, DetailFields: e.JiraCfg.DetailFields,
		}
		if err := e.exportJira(ctx, m.Boards, marks); err != nil {
			return nil, err
		}
	} else if len(opts.Boards) > 0 || len(opts.JQL) > 0 {
		return nil, fmt.Errorf("--board/--jql need Jira (set JIRA_URL and a token)")
	}

	pages := append([]string{}, opts.Pages...)
	for _, b := range marks {
		if b.Kind == bookmarks.Page {
			pages = append(pages, b.Key)
		}
	}
	if len(pages) > 0 {
		if e.Confluence == nil {
			if len(opts.Pages) > 0 {
				return nil, fmt.Errorf("--page needs Confluence (set CONFLUENCE_URL and a token)")
			}
			e.warn("Confluence is not configured: skipping %d bookmarked page(s)", len(pages))
		} else {
			m.ConfluenceURL = e.Confluence.BaseURL()
			for _, id := range pages {
				if err := e.exportPageTree(ctx, id); err != nil {
					e.warn("page %s: %v", id, err)
				}
			}
		}
	}

	if bookmarkFile != "" {
		if data, err := os.ReadFile(bookmarkFile); err == nil {
			if err := os.WriteFile(filepath.Join(e.stage, "bookmarks.yaml"), data, 0o644); err != nil {
				return nil, err
			}
		}
	}

	m.Issues, m.Pages, m.Attachments = e.stats.issues, e.stats.pages, e.stats.attachments
	m.Bytes = dirSize(e.stage)
	if err := mirror.WriteManifest(e.stage, m); err != nil {
		return nil, err
	}
	if err := writeTarGz(e.stage, out); err != nil {
		return nil, err
	}
	kind := "full snapshot"
	if m.Since != "" {
		kind = "changes since " + m.Since
	}
	e.logf("exported %d issues, %d pages, %d attachments (%s, %s) → %s", m.Issues, m.Pages, m.Attachments,
		humanBytes(m.Bytes), kind, out)
	if e.warnings > 0 {
		e.logf("%d warning(s), see above", e.warnings)
	}
	return m, nil
}

// --- Jira ---

func (e *Exporter) get(ctx context.Context, path string, q url.Values, v any) error {
	status, body, err := e.Jira.Raw(ctx, "GET", path, q, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("GET %s: HTTP %d: %s", path, status, truncate(string(body), 200))
	}
	return json.Unmarshal(body, v)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func (e *Exporter) writeFile(rel string, v any) error {
	return writeJSONFile(filepath.Join(e.stage, rel), v)
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

var orderByRe = regexp.MustCompile(`(?is)\s*\border\s+by\b.*$`)

func (e *Exporter) exportJira(ctx context.Context, boards []int, marks []bookmarks.Bookmark) error {
	var me map[string]any
	if err := e.get(ctx, "/rest/api/2/myself", nil, &me); err != nil {
		return fmt.Errorf("jira: %w", err)
	}
	_ = e.writeFile("jira/myself.json", me)
	var fields []map[string]any
	if err := e.get(ctx, "/rest/api/2/field", nil, &fields); err == nil {
		_ = e.writeFile("jira/fields.json", fields)
		for _, f := range fields {
			if id, name := fmt.Sprint(f["id"]), fmt.Sprint(f["name"]); id != "" {
				e.names[id] = name
			}
		}
	} else {
		e.warn("fields: %v", err)
	}
	var lt any
	if err := e.get(ctx, "/rest/api/2/issueLinkType", nil, &lt); err == nil {
		_ = e.writeFile("jira/linktypes.json", lt)
	}

	queries := append([]string{}, e.opts.JQL...)
	var active []int
	for _, id := range boards {
		jql, sprints, err := e.exportBoard(ctx, id)
		if err != nil {
			e.warn("board %d: %v", id, err)
			continue
		}
		if jql != "" {
			queries = append(queries, jql)
		}
		active = append(active, activeSprintIDs(sprints)...)
	}
	if e.opts.ActiveSprints {
		if q := activeSprintJQL(active, len(boards) > 0, e.JiraCfg.DefaultProject); q != "" {
			e.logf("active sprints: %s", q)
			queries = append(queries, q)
		} else {
			e.logf("active sprints: none")
		}
	}
	if e.opts.MyIssues {
		q := myIssuesJQL(e.resolutionNames(ctx))
		e.logf("my issues: %s", q)
		queries = append(queries, q)
	}

	var keys []string
	var sprintIDs []int
	phrases := e.JiraCfg.FeatureLinkTypes
	if len(phrases) == 0 {
		phrases = config.DefaultFeatureLinkTypes
	}
	for _, b := range marks {
		switch b.Kind {
		case bookmarks.Issue:
			keys = append(keys, b.Key)
		case bookmarks.Query:
			queries = append(queries, b.JQL)
		case bookmarks.Epic:
			keys = append(keys, b.Key)
			queries = append(queries, e.Jira.EpicChildrenJQL(b.Key, e.JiraCfg.EpicLinkField))
		case bookmarks.Sprint:
			if id, err := strconv.Atoi(b.Key); err == nil {
				sprintIDs = append(sprintIDs, id)
				queries = append(queries, fmt.Sprintf("sprint = %d", id))
			}
		case bookmarks.Feature:
			keys = append(keys, b.Key)
			f, err := e.Jira.GetIssue(ctx, b.Key)
			if err != nil {
				e.warn("feature %s: %v", b.Key, err)
				continue
			}
			linked, _ := jira.LinkedChildren(f, phrases)
			keys = append(keys, linked...)
		}
	}
	if len(sprintIDs) > 0 {
		var sprints []map[string]any
		for _, id := range sprintIDs {
			var sp map[string]any
			if err := e.get(ctx, "/rest/agile/1.0/sprint/"+strconv.Itoa(id), nil, &sp); err != nil {
				e.warn("sprint %d: %v", id, err)
				continue
			}
			sprints = append(sprints, sp)
		}
		_ = e.writeFile("jira/boards/bookmarked-sprints.json", map[string]any{"sprints": sprints})
	}

	for _, q := range queries {
		if err := e.exportQuery(ctx, q); err != nil {
			e.warn("query %q: %v", q, err)
		}
	}
	for _, k := range keys {
		if err := e.exportKey(ctx, k); err != nil {
			e.warn("issue %s: %v", k, err)
		}
	}
	// One level of relatives, so subtasks and parents open offline too.
	related := e.relatedKeys()
	for _, k := range related {
		if err := e.exportKey(ctx, k); err != nil {
			e.warn("issue %s: %v", k, err)
		}
	}
	return e.writeFile("jira/names.json", e.names)
}

// activeSprintIDs picks the sprints in state "active" (there may be none).
func activeSprintIDs(sprints []map[string]any) []int {
	var ids []int
	for _, sp := range sprints {
		if st, _ := sp["state"].(string); strings.EqualFold(st, "active") {
			if id, err := strconv.Atoi(fmt.Sprint(sp["id"])); err == nil {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// activeSprintJQL selects the active sprints' issues: those of the exported
// boards, or, without boards, the default project's open (not future) ones.
// Empty when there is nothing to export.
func activeSprintJQL(ids []int, haveBoards bool, project string) string {
	if len(ids) > 0 {
		parts := make([]string, len(ids))
		for i, id := range ids {
			parts[i] = strconv.Itoa(id)
		}
		return "sprint in (" + strings.Join(parts, ", ") + ")"
	}
	if !haveBoards && project != "" {
		return fmt.Sprintf("project = %q AND sprint in openSprints() AND sprint not in futureSprints()", project)
	}
	return ""
}

// myIssuesJQL selects the exporting user's issues that are not resolved as
// Done or Fixed. JQL's NOT IN skips empty values, so unresolved issues need
// their own clause; only resolutions the server has may be named (Jira
// rejects the query for an unknown value).
func myIssuesJQL(resolutions []string) string {
	var exclude []string
	for _, want := range []string{"Done", "Fixed"} {
		for _, r := range resolutions {
			if strings.EqualFold(r, want) {
				exclude = append(exclude, strconv.Quote(r))
				break
			}
		}
	}
	if len(exclude) == 0 {
		return "assignee = currentUser()"
	}
	return "assignee = currentUser() AND (resolution IS EMPTY OR resolution NOT IN (" +
		strings.Join(exclude, ", ") + "))"
}

// resolutionNames lists the server's resolutions; on failure it assumes
// Done and Fixed exist.
func (e *Exporter) resolutionNames(ctx context.Context) []string {
	var rs []struct {
		Name string `json:"name"`
	}
	if err := e.get(ctx, "/rest/api/2/resolution", nil, &rs); err != nil {
		e.warn("resolutions: %v", err)
		return []string{"Done", "Fixed"}
	}
	names := make([]string, len(rs))
	for i, r := range rs {
		names[i] = r.Name
	}
	return names
}

func (e *Exporter) exportBoard(ctx context.Context, id int) (string, []map[string]any, error) {
	sid := strconv.Itoa(id)
	var board map[string]any
	if err := e.get(ctx, "/rest/agile/1.0/board/"+sid, nil, &board); err != nil {
		return "", nil, err
	}
	var sprints []map[string]any
	if t, _ := board["type"].(string); t == "scrum" {
		for start := 0; ; {
			var page struct {
				Values []map[string]any `json:"values"`
				IsLast bool             `json:"isLast"`
			}
			q := url.Values{"startAt": {strconv.Itoa(start)}, "maxResults": {"50"}}
			if err := e.get(ctx, "/rest/agile/1.0/board/"+sid+"/sprint", q, &page); err != nil {
				e.warn("board %d sprints: %v", id, err)
				break
			}
			sprints = append(sprints, page.Values...)
			start += len(page.Values)
			if page.IsLast || len(page.Values) == 0 {
				break
			}
		}
	}
	jql := ""
	var conf struct {
		Filter struct {
			ID string `json:"id"`
		} `json:"filter"`
	}
	if err := e.get(ctx, "/rest/agile/1.0/board/"+sid+"/configuration", nil, &conf); err == nil && conf.Filter.ID != "" {
		var filter struct {
			JQL string `json:"jql"`
		}
		if err := e.get(ctx, "/rest/api/2/filter/"+url.PathEscape(conf.Filter.ID), nil, &filter); err == nil {
			jql = filter.JQL
		} else {
			e.warn("board %d filter: %v", id, err)
		}
	}
	if jql == "" { // fall back to the board's own issue listing
		keys, err := e.boardIssueKeys(ctx, sid)
		if err != nil {
			return "", nil, err
		}
		for _, k := range keys {
			if err := e.exportKey(ctx, k); err != nil {
				e.warn("issue %s: %v", k, err)
			}
		}
	}
	e.logf("board %d %q: %d sprints", id, board["name"], len(sprints))
	return jql, sprints, e.writeFile("jira/boards/"+sid+".json", map[string]any{"board": board, "sprints": sprints, "jql": jql})
}

func (e *Exporter) boardIssueKeys(ctx context.Context, sid string) ([]string, error) {
	var keys []string
	for start := 0; ; {
		var page struct {
			Issues []struct {
				Key string `json:"key"`
			} `json:"issues"`
			Total int `json:"total"`
		}
		q := url.Values{"startAt": {strconv.Itoa(start)}, "maxResults": {"100"}, "fields": {"key"}}
		if err := e.get(ctx, "/rest/agile/1.0/board/"+sid+"/issue", q, &page); err != nil {
			return keys, err
		}
		for _, is := range page.Issues {
			keys = append(keys, is.Key)
		}
		start += len(page.Issues)
		if len(page.Issues) == 0 || start >= page.Total {
			return keys, nil
		}
	}
}

// sinceClause limits a query to issues updated since the --since time, as
// a relative period so that it does not depend on the user's time zone.
func (e *Exporter) sinceClause() string {
	if e.since.IsZero() {
		return ""
	}
	mins := int(time.Since(e.since).Minutes()) + 1
	return fmt.Sprintf(` AND updated >= "-%dm"`, mins)
}

func (e *Exporter) exportQuery(ctx context.Context, jql string) error {
	base := strings.TrimSpace(orderByRe.ReplaceAllString(jql, ""))
	if base == "" {
		return nil
	}
	full := "(" + base + ")" + e.sinceClause() + " ORDER BY key ASC"
	q := url.Values{"jql": {full}, "fields": {"*all"}, "expand": {"renderedFields,names"}, "maxResults": {"100"}}
	n := 0
	for start := 0; ; {
		var page struct {
			Issues        []map[string]any `json:"issues"`
			Total         int              `json:"total"`
			IsLast        bool             `json:"isLast"`
			NextPageToken string           `json:"nextPageToken"`
		}
		path := "/rest/api/2/search"
		if e.Jira.IsCloud() {
			path = "/rest/api/2/search/jql"
		} else {
			q.Set("startAt", strconv.Itoa(start))
		}
		if err := e.get(ctx, path, q, &page); err != nil {
			return err
		}
		for _, is := range page.Issues {
			if err := e.storeIssue(ctx, is); err != nil {
				e.warn("issue %v: %v", is["key"], err)
			}
		}
		n += len(page.Issues)
		start += len(page.Issues)
		if len(page.Issues) == 0 {
			break
		}
		if e.Jira.IsCloud() {
			if page.IsLast || page.NextPageToken == "" {
				break
			}
			q.Set("nextPageToken", page.NextPageToken)
		} else if start >= page.Total {
			break
		}
	}
	e.logf("query %q: %d issues", truncate(base, 80), n)
	return nil
}

func (e *Exporter) exportKey(ctx context.Context, key string) error {
	key = strings.ToUpper(strings.TrimSpace(key))
	if key == "" || e.done[key] {
		return nil
	}
	var is map[string]any
	q := url.Values{"expand": {"renderedFields,names"}}
	if err := e.get(ctx, "/rest/api/2/issue/"+url.PathEscape(key), q, &is); err != nil {
		return err
	}
	if !e.since.IsZero() {
		if t, err := mirror.ParseSince(fmt.Sprint(obj(is, "fields")["updated"]), time.Now()); err == nil && t.Before(e.since) {
			return nil // unchanged since the last export
		}
	}
	return e.storeIssue(ctx, is)
}

func obj(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

func (e *Exporter) relatedKeys() []string {
	var out []string
	add := func(k any) {
		if s, ok := k.(string); ok && s != "" && !e.done[s] {
			out = append(out, s)
		}
	}
	for key := range e.done {
		var is map[string]any
		if err := readJSONFile(filepath.Join(e.stage, "jira", "issues", key, "issue.json"), &is); err != nil {
			continue
		}
		f := obj(is, "fields")
		add(obj(f, "parent")["key"])
		subs, _ := f["subtasks"].([]any)
		for _, s := range subs {
			if sm, ok := s.(map[string]any); ok {
				add(sm["key"])
			}
		}
	}
	return out
}

func readJSONFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// storeIssue writes an issue with its complete comments and attachments.
func (e *Exporter) storeIssue(ctx context.Context, is map[string]any) error {
	key, _ := is["key"].(string)
	if key == "" || e.done[key] {
		return nil
	}
	if n, ok := is["names"].(map[string]any); ok {
		for id, v := range n {
			if s, ok := v.(string); ok {
				e.names[id] = s
			}
		}
		delete(is, "names")
	}
	f := obj(is, "fields")
	if f == nil {
		return fmt.Errorf("no fields")
	}
	// Search results may carry only the first comments.
	if c := obj(f, "comment"); c != nil {
		list, _ := c["comments"].([]any)
		total, _ := c["total"].(float64)
		if int(total) > len(list) {
			e.fullComments(ctx, key, is)
		}
	}
	dir := filepath.Join(e.stage, "jira", "issues", key)
	atts, _ := f["attachment"].([]any)
	for _, a := range atts {
		am, ok := a.(map[string]any)
		if !ok {
			continue
		}
		id, content := fmt.Sprint(am["id"]), fmt.Sprint(am["content"])
		if err := e.download(ctx, func() (io.ReadCloser, error) { return e.Jira.DownloadAttachment(ctx, content) },
			filepath.Join(dir, "attachments", id)); err != nil {
			e.warn("%s attachment %v: %v", key, am["filename"], err)
			continue
		}
		e.stats.attachments++
	}
	if err := writeJSONFile(filepath.Join(dir, "issue.json"), is); err != nil {
		return err
	}
	e.done[key] = true
	e.stats.issues++
	return nil
}

func (e *Exporter) fullComments(ctx context.Context, key string, is map[string]any) {
	var page map[string]any
	q := url.Values{"maxResults": {"5000"}, "expand": {"renderedBody"}}
	if err := e.get(ctx, "/rest/api/2/issue/"+url.PathEscape(key)+"/comment", q, &page); err != nil {
		e.warn("%s comments: %v", key, err)
		return
	}
	list, _ := page["comments"].([]any)
	var rendered []any
	for _, c := range list {
		cm, _ := c.(map[string]any)
		rendered = append(rendered, map[string]any{"id": cm["id"], "body": cm["renderedBody"], "author": cm["author"], "created": cm["created"]})
		delete(cm, "renderedBody")
	}
	obj(is, "fields")["comment"] = map[string]any{"comments": list, "total": len(list), "maxResults": len(list), "startAt": 0}
	if rf := obj(is, "renderedFields"); rf != nil {
		rf["comment"] = map[string]any{"comments": rendered, "total": len(rendered), "maxResults": len(rendered), "startAt": 0}
	}
}

func (e *Exporter) download(ctx context.Context, open func() (io.ReadCloser, error), path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	body, err := open()
	if err != nil {
		return err
	}
	defer body.Close()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// --- Confluence ---

func (e *Exporter) cget(ctx context.Context, path string, q url.Values, v any) error {
	status, body, err := e.Confluence.Raw(ctx, "GET", path, q, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("GET %s: HTTP %d: %s", path, status, truncate(string(body), 200))
	}
	return json.Unmarshal(body, v)
}

// exportPageTree exports a page and all its descendants (with --since, only
// those modified since).
func (e *Exporter) exportPageTree(ctx context.Context, id string) error {
	ids := []string{id}
	q := url.Values{"cql": {"ancestor = " + id}, "expand": {"version"}, "limit": {"100"}}
	for start := 0; ; {
		q.Set("start", strconv.Itoa(start))
		var page struct {
			Results []map[string]any `json:"results"`
			Links   struct {
				Next string `json:"next"`
			} `json:"_links"`
		}
		if err := e.cget(ctx, "/rest/api/content/search", q, &page); err != nil {
			e.warn("descendants of page %s: %v", id, err)
			break
		}
		for _, p := range page.Results {
			if t, _ := p["type"].(string); t != "" && t != "page" {
				continue
			}
			if !e.since.IsZero() {
				when, _ := obj(p, "version")["when"].(string)
				if t, err := mirror.ParseSince(when, time.Now()); err == nil && t.Before(e.since) {
					continue
				}
			}
			ids = append(ids, fmt.Sprint(p["id"]))
		}
		start += len(page.Results)
		if page.Links.Next == "" || len(page.Results) == 0 {
			break
		}
	}
	n := 0
	for i, pid := range ids {
		stored, err := e.exportPage(ctx, pid, i == 0)
		if err != nil {
			e.warn("page %s: %v", pid, err)
		}
		if stored {
			n++
		}
	}
	e.logf("page tree %s: %d pages", id, n)
	return nil
}

var riURLRe = regexp.MustCompile(`ri:url\s+ri:value="([^"]+)"`)

func (e *Exporter) exportPage(ctx context.Context, id string, checkSince bool) (bool, error) {
	if e.pageDone[id] {
		return false, nil
	}
	e.pageDone[id] = true
	var p map[string]any
	q := url.Values{"expand": {"body.storage,version,space,ancestors,history,metadata.labels"}}
	if err := e.cget(ctx, "/rest/api/content/"+url.PathEscape(id), q, &p); err != nil {
		return false, err
	}
	if checkSince && !e.since.IsZero() {
		when, _ := obj(p, "version")["when"].(string)
		if t, err := mirror.ParseSince(when, time.Now()); err == nil && t.Before(e.since) {
			return false, nil
		}
	}
	dir := filepath.Join(e.stage, "confluence", "pages", id)
	var atts []map[string]any
	aq := url.Values{"expand": {"version"}, "limit": {"200"}}
	for start := 0; ; {
		aq.Set("start", strconv.Itoa(start))
		var page struct {
			Results []map[string]any `json:"results"`
			Links   struct {
				Next string `json:"next"`
			} `json:"_links"`
		}
		if err := e.cget(ctx, "/rest/api/content/"+url.PathEscape(id)+"/child/attachment", aq, &page); err != nil {
			e.warn("page %s attachments: %v", id, err)
			break
		}
		atts = append(atts, page.Results...)
		start += len(page.Results)
		if page.Links.Next == "" || len(page.Results) == 0 {
			break
		}
	}
	for _, a := range atts {
		aid := fmt.Sprint(a["id"])
		links, _ := a["_links"].(map[string]any)
		dl, _ := links["download"].(string)
		if dl == "" {
			continue
		}
		if err := e.download(ctx, func() (io.ReadCloser, error) { return e.Confluence.DownloadAttachment(ctx, dl) },
			filepath.Join(dir, "files", aid)); err != nil {
			e.warn("page %s attachment %v: %v", id, a["title"], err)
			continue
		}
		e.stats.attachments++
	}
	if err := writeJSONFile(filepath.Join(dir, "attachments.json"), atts); err != nil {
		return false, err
	}

	// Images linked by URL on the same site.
	body, _ := obj(obj(p, "body"), "storage")["value"].(string)
	base := strings.TrimRight(e.Confluence.BaseURL(), "/")
	urls := map[string]string{}
	for _, m := range riURLRe.FindAllStringSubmatch(body, -1) {
		u := html.UnescapeString(m[1])
		if !strings.HasPrefix(u, base+"/") {
			continue
		}
		rel := strings.TrimPrefix(u, base)
		sum := sha1.Sum([]byte(rel))
		name := hex.EncodeToString(sum[:8])
		if err := e.download(ctx, func() (io.ReadCloser, error) { return e.Confluence.DownloadAttachment(ctx, rel) },
			filepath.Join(dir, "urls", name)); err != nil {
			e.warn("page %s image %s: %v", id, u, err)
			continue
		}
		urls[rel] = name
	}
	if len(urls) > 0 {
		if err := writeJSONFile(filepath.Join(dir, "urls.json"), urls); err != nil {
			return false, err
		}
	}
	if err := writeJSONFile(filepath.Join(dir, "page.json"), p); err != nil {
		return false, err
	}
	if sp := obj(p, "space"); sp != nil {
		if key, _ := sp["key"].(string); key != "" {
			path := filepath.Join(e.stage, "confluence", "spaces", key+".json")
			if _, err := os.Stat(path); err != nil {
				_ = writeJSONFile(path, sp)
			}
		}
	}
	e.stats.pages++
	return true, nil
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n += info.Size()
		}
		return nil
	})
	return n
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
