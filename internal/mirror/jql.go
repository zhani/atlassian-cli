package mirror

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SearchJQL returns the mirrored issues matching a JQL query, sorted by its
// ORDER BY (default: key, newest first).
func (s *Server) SearchJQL(jql string) ([]map[string]any, error) {
	q, err := parseQuery(jql)
	if err != nil {
		return nil, fmt.Errorf("JQL: %w", err)
	}
	ctx := &jqlContext{s: s, now: s.now()}
	var out []map[string]any
	for _, key := range s.order {
		is := s.issues[key]
		ok, err := eval(q.where, func(c *clause) (bool, error) { return ctx.match(is, c) })
		if err != nil {
			return nil, fmt.Errorf("JQL: %w", err)
		}
		if ok {
			out = append(out, is)
		}
	}
	order := q.order
	if len(order) == 0 {
		order = []sortKey{{field: "key", desc: true}}
	}
	sort.SliceStable(out, func(i, j int) bool {
		for _, k := range order {
			c := ctx.compare(out[i], out[j], k.field)
			if c == 0 {
				continue
			}
			if k.desc {
				return c > 0
			}
			return c < 0
		}
		return false
	})
	return out, nil
}

type jqlContext struct {
	s   *Server
	now time.Time
}

type fieldKind int

const (
	kindPlain fieldKind = iota
	kindText
	kindDate
	kindUser
)

var cfRe = regexp.MustCompile(`^cf\[(\d+)\]$`)

// values returns the comparable values of a field on an issue.
func (c *jqlContext) values(is map[string]any, field string) ([]string, fieldKind, error) {
	f := obj(is, "fields")
	s := c.s
	switch field {
	case "key", "issuekey", "issue":
		return []string{str(is["key"])}, kindPlain, nil
	case "id":
		return []string{str(is["id"])}, kindPlain, nil
	case "project":
		p := obj(f, "project")
		return nonEmpty(str(p["key"]), str(p["name"]), str(p["id"])), kindPlain, nil
	case "status":
		return named(obj(f, "status")), kindPlain, nil
	case "statuscategory":
		sc := obj(obj(f, "status"), "statusCategory")
		return nonEmpty(str(sc["name"]), str(sc["key"]), str(sc["id"])), kindPlain, nil
	case "issuetype", "type":
		return named(obj(f, "issuetype")), kindPlain, nil
	case "priority":
		return named(obj(f, "priority")), kindPlain, nil
	case "resolution":
		return named(obj(f, "resolution")), kindPlain, nil
	case "assignee", "reporter", "creator":
		return userValues(obj(f, field)), kindUser, nil
	case "labels", "label":
		return flatten(f["labels"]), kindPlain, nil
	case "component", "components":
		return flatten(f["components"]), kindPlain, nil
	case "fixversion", "fixversions":
		return flatten(f["fixVersions"]), kindPlain, nil
	case "affectedversion", "affectedversions", "version", "versions":
		return flatten(f["versions"]), kindPlain, nil
	case "summary", "description", "environment":
		return nonEmpty(str(f[field])), kindText, nil
	case "comment":
		return []string{commentText(f)}, kindText, nil
	case "text":
		parts := []string{str(f["summary"]), str(f["description"]), str(f["environment"]), commentText(f)}
		for k, v := range f {
			if strings.HasPrefix(k, "customfield_") {
				if t, ok := v.(string); ok {
					parts = append(parts, t)
				}
			}
		}
		return []string{strings.Join(parts, "\n")}, kindText, nil
	case "created", "createddate":
		return nonEmpty(str(f["created"])), kindDate, nil
	case "updated", "updateddate":
		return nonEmpty(str(f["updated"])), kindDate, nil
	case "resolved", "resolutiondate":
		return nonEmpty(str(f["resolutiondate"])), kindDate, nil
	case "due", "duedate":
		return nonEmpty(str(f["duedate"])), kindDate, nil
	case "parent":
		vals := nonEmpty(str(obj(f, "parent")["key"]), str(obj(f, "parent")["id"]))
		return vals, kindPlain, nil
	case "sprint":
		var vals []string
		for _, sp := range s.sprintsOf(is) {
			vals = append(vals, sp.ID, sp.Name)
		}
		return vals, kindPlain, nil
	case "watcher":
		if b, _ := obj(f, "watches")["isWatching"].(bool); b {
			return userValues(s.myself), kindUser, nil
		}
		return nil, kindUser, nil
	case "rank":
		return s.customValues(f, "Rank"), kindPlain, nil
	case "epic link", "epiclink":
		return s.customValues(f, "Epic Link"), kindPlain, nil
	}
	if m := cfRe.FindStringSubmatch(field); m != nil {
		field = "customfield_" + m[1]
	}
	if v, ok := f[field]; ok {
		return flatten(v), guessKind(field, s.names[field]), nil
	}
	if strings.HasPrefix(field, "customfield_") {
		return nil, kindPlain, nil // a known field ID without a value here
	}
	if ids := s.byName[field]; len(ids) > 0 {
		var vals []string
		for _, id := range ids {
			vals = append(vals, flatten(f[id])...)
		}
		return vals, guessKind(ids[0], field), nil
	}
	for _, other := range s.issues { // a field that exists on some other issue
		if _, ok := obj(other, "fields")[field]; ok {
			return nil, kindPlain, nil
		}
	}
	return nil, 0, fmt.Errorf("field %q does not exist in the offline mirror", field)
}

func guessKind(id, name string) fieldKind {
	n := strings.ToLower(name + " " + id)
	if strings.Contains(n, "date") || strings.HasSuffix(n, "time") {
		return kindDate
	}
	return kindPlain
}

func (s *Server) customValues(f map[string]any, name string) []string {
	var vals []string
	for _, id := range s.byName[strings.ToLower(name)] {
		vals = append(vals, flatten(f[id])...)
	}
	return vals
}

func nonEmpty(vs ...string) []string {
	var out []string
	for _, v := range vs {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func named(m map[string]any) []string {
	if m == nil {
		return nil
	}
	return nonEmpty(str(m["name"]), str(m["id"]))
}

func userValues(u map[string]any) []string {
	if u == nil {
		return nil
	}
	return nonEmpty(str(u["name"]), str(u["key"]), str(u["accountId"]), str(u["emailAddress"]), str(u["displayName"]))
}

// flatten turns a field value into strings: objects give their value, name,
// key and ID, arrays all of their elements.
func flatten(v any) []string {
	switch v := v.(type) {
	case nil:
		return nil
	case []any:
		var out []string
		for _, e := range v {
			out = append(out, flatten(e)...)
		}
		return out
	case map[string]any:
		out := nonEmpty(str(v["value"]), str(v["name"]), str(v["key"]), str(v["id"]), str(v["accountId"]), str(v["displayName"]))
		if child := obj(v, "child"); child != nil {
			out = append(out, flatten(child)...)
		}
		return out
	}
	if s := str(v); s != "" {
		return []string{s}
	}
	return nil
}

func commentText(f map[string]any) string {
	cs, _ := obj(f, "comment")["comments"].([]any)
	var sb strings.Builder
	for _, c := range cs {
		if cm, ok := c.(map[string]any); ok {
			sb.WriteString(str(cm["body"]))
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// sprintRef is a sprint an issue is (or was) in.
type sprintRef struct {
	ID, Name, State string
	raw             map[string]any
}

var sprintStringRe = regexp.MustCompile(`(\w+)=([^,\]]*)`)

// sprintsOf parses the issue's Sprint field: objects on Cloud and newer
// Server/DC, "...Sprint@x[id=1,state=ACTIVE,name=Sprint 1,...]" strings on
// older Server/DC.
func (s *Server) sprintsOf(is map[string]any) []sprintRef {
	f := obj(is, "fields")
	ids := s.byName["sprint"]
	if len(ids) == 0 {
		ids = []string{"sprint"}
	}
	var out []sprintRef
	for _, id := range ids {
		list, _ := f[id].([]any)
		for _, e := range list {
			switch e := e.(type) {
			case map[string]any:
				out = append(out, sprintRef{ID: str(e["id"]), Name: str(e["name"]), State: strings.ToLower(str(e["state"]))})
			case string:
				ref := sprintRef{}
				for _, m := range sprintStringRe.FindAllStringSubmatch(e, -1) {
					switch m[1] {
					case "id":
						ref.ID = m[2]
					case "name":
						ref.Name = m[2]
					case "state":
						ref.State = strings.ToLower(m[2])
					}
				}
				if ref.ID != "" {
					out = append(out, ref)
				}
			}
		}
	}
	return out
}

// operandValues resolves a clause value to the strings it stands for.
func (c *jqlContext) operandValues(o operand) ([]string, error) {
	switch o.fn {
	case "":
		return []string{o.text}, nil
	case "currentuser":
		return userValues(c.s.myself), nil
	case "opensprints", "closedsprints", "futuresprints":
		want := map[string][]string{"opensprints": {"active", "future"}, "closedsprints": {"closed"}, "futuresprints": {"future"}}[o.fn]
		var ids []string
		for _, sp := range c.s.allSprints() {
			for _, w := range want {
				if sp.State == w {
					ids = append(ids, sp.ID)
				}
			}
		}
		if len(ids) == 0 {
			ids = []string{"\x00none"}
		}
		return ids, nil
	case "linkedissues":
		if len(o.args) == 0 {
			return nil, fmt.Errorf("linkedIssues() needs an issue key")
		}
		return c.linked(strings.ToUpper(o.args[0]), o.args[1:]), nil
	case "watchedissues":
		var keys []string
		for _, k := range c.s.order {
			if b, _ := obj(obj(c.s.issues[k], "fields"), "watches")["isWatching"].(bool); b {
				keys = append(keys, k)
			}
		}
		return keys, nil
	case "now", "startofday", "startofweek", "startofmonth", "startofyear", "endofday", "endofweek", "endofmonth", "endofyear":
		t, err := c.dateFunc(o)
		if err != nil {
			return nil, err
		}
		return []string{t.Format(time.RFC3339)}, nil
	}
	return nil, fmt.Errorf("function %s() is not supported by the offline mirror", o.text)
}

// linked returns the keys linked to key, optionally only by link phrases.
func (c *jqlContext) linked(key string, phrases []string) []string {
	var out []string
	add := func(other map[string]any, phrase string) {
		if other == nil {
			return
		}
		if len(phrases) > 0 {
			ok := false
			for _, p := range phrases {
				if strings.EqualFold(p, phrase) {
					ok = true
				}
			}
			if !ok {
				return
			}
		}
		out = append(out, str(other["key"]))
	}
	for _, l := range linksOf(c.s.issues[key]) {
		t := obj(l, "type")
		add(obj(l, "outwardIssue"), str(t["outward"]))
		add(obj(l, "inwardIssue"), str(t["inward"]))
	}
	// Links are also stored on the other side; a partial mirror may only
	// have that one.
	for _, k := range c.s.order {
		for _, l := range linksOf(c.s.issues[k]) {
			t := obj(l, "type")
			if strings.EqualFold(str(obj(l, "outwardIssue")["key"]), key) {
				add(map[string]any{"key": k}, str(t["inward"]))
			}
			if strings.EqualFold(str(obj(l, "inwardIssue")["key"]), key) {
				add(map[string]any{"key": k}, str(t["outward"]))
			}
		}
	}
	if len(out) == 0 {
		return []string{"\x00none"}
	}
	return out
}

func linksOf(is map[string]any) []map[string]any {
	ls, _ := obj(is, "fields")["issuelinks"].([]any)
	var out []map[string]any
	for _, l := range ls {
		if m, ok := l.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func (c *jqlContext) match(is map[string]any, cl *clause) (bool, error) {
	got, kind, err := c.values(is, cl.field)
	if err != nil {
		return false, err
	}
	switch cl.op {
	case "is":
		return len(got) == 0, nil
	case "is not":
		return len(got) > 0, nil
	}
	var want []string
	for _, o := range cl.values {
		vs, err := c.operandValues(o)
		if err != nil {
			return false, err
		}
		want = append(want, vs...)
	}
	switch cl.op {
	case "=", "in":
		return c.anyEqual(got, want, kind, cl.field), nil
	case "!=", "not in":
		// Like Jira, an empty field matches neither = nor !=; but
		// resolution != X includes unresolved issues only when X is.
		if len(got) == 0 {
			return cl.field == "resolution" && !containsFold(want, "unresolved"), nil
		}
		return !c.anyEqual(got, want, kind, cl.field), nil
	case "~", "!~":
		hit := false
		for _, g := range got {
			for _, w := range want {
				if textMatches(g, w) {
					hit = true
				}
			}
		}
		return hit == (cl.op == "~"), nil
	case ">", ">=", "<", "<=":
		if len(got) == 0 || len(want) == 0 {
			return false, nil
		}
		for _, g := range got {
			cmp, err := c.compareValues(g, want[0], kind)
			if err != nil {
				return false, err
			}
			ok := map[string]bool{">": cmp > 0, ">=": cmp >= 0, "<": cmp < 0, "<=": cmp <= 0}[cl.op]
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	return false, fmt.Errorf("operator %s is not supported", cl.op)
}

func containsFold(list []string, v string) bool {
	for _, l := range list {
		if strings.EqualFold(l, v) {
			return true
		}
	}
	return false
}

func (c *jqlContext) anyEqual(got, want []string, kind fieldKind, field string) bool {
	if field == "resolution" && len(got) == 0 && containsFold(want, "unresolved") {
		return true
	}
	if len(got) == 0 && field == "resolution" {
		return false
	}
	for _, w := range want {
		for _, g := range got {
			if strings.EqualFold(g, w) {
				return true
			}
			if kind == kindDate {
				if cmp, err := c.compareValues(g, w, kind); err == nil && cmp == 0 {
					return true
				}
			}
		}
	}
	return false
}

func (c *jqlContext) compareValues(got, want string, kind fieldKind) (int, error) {
	if kind == kindDate {
		g, err := parseDate(got, c.now)
		if err != nil {
			return 0, err
		}
		w, err := parseDate(want, c.now)
		if err != nil {
			return 0, fmt.Errorf("invalid date %q", want)
		}
		return g.Compare(w), nil
	}
	gf, err1 := strconv.ParseFloat(got, 64)
	wf, err2 := strconv.ParseFloat(want, 64)
	if err1 == nil && err2 == nil {
		switch {
		case gf < wf:
			return -1, nil
		case gf > wf:
			return 1, nil
		}
		return 0, nil
	}
	return strings.Compare(strings.ToLower(got), strings.ToLower(want)), nil
}

var relRe = regexp.MustCompile(`^([+-]?)(\d+)([yMwdhm])$`)

var dateLayouts = []string{
	"2006-01-02T15:04:05.000-0700", "2006-01-02T15:04:05-0700", time.RFC3339Nano, time.RFC3339,
	"2006-01-02 15:04", "2006/01/02 15:04", "2006-01-02", "2006/01/02",
}

// parseDate understands Jira's stored timestamps, JQL dates and relative
// periods (-7d, 2w, -12h, -30m).
func parseDate(v string, now time.Time) (time.Time, error) {
	v = strings.TrimSpace(v)
	if m := relRe.FindStringSubmatch(v); m != nil {
		return shift(now, m), nil
	}
	for _, l := range dateLayouts {
		if t, err := time.ParseInLocation(l, v, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date %q", v)
}

func shift(t time.Time, m []string) time.Time {
	n, _ := strconv.Atoi(m[2])
	if m[1] == "-" {
		n = -n
	}
	switch m[3] {
	case "y":
		return t.AddDate(n, 0, 0)
	case "M":
		return t.AddDate(0, n, 0)
	case "w":
		return t.AddDate(0, 0, 7*n)
	case "d":
		return t.AddDate(0, 0, n)
	case "h":
		return t.Add(time.Duration(n) * time.Hour)
	}
	return t.Add(time.Duration(n) * time.Minute)
}

func (c *jqlContext) dateFunc(o operand) (time.Time, error) {
	now := c.now
	y, mo, d := now.Date()
	day := time.Date(y, mo, d, 0, 0, 0, 0, now.Location())
	var t time.Time
	switch o.fn {
	case "now":
		t = now
	case "startofday":
		t = day
	case "endofday":
		t = day.AddDate(0, 0, 1).Add(-time.Second)
	case "startofweek", "endofweek":
		wd := (int(now.Weekday()) + 6) % 7 // Monday first
		t = day.AddDate(0, 0, -wd)
		if o.fn == "endofweek" {
			t = t.AddDate(0, 0, 7).Add(-time.Second)
		}
	case "startofmonth", "endofmonth":
		t = time.Date(y, mo, 1, 0, 0, 0, 0, now.Location())
		if o.fn == "endofmonth" {
			t = t.AddDate(0, 1, 0).Add(-time.Second)
		}
	case "startofyear", "endofyear":
		t = time.Date(y, 1, 1, 0, 0, 0, 0, now.Location())
		if o.fn == "endofyear" {
			t = t.AddDate(1, 0, 0).Add(-time.Second)
		}
	}
	if len(o.args) > 0 {
		m := relRe.FindStringSubmatch(strings.TrimSpace(o.args[0]))
		if m == nil {
			return t, fmt.Errorf("invalid increment %q in %s()", o.args[0], o.text)
		}
		t = shift(t, m)
	}
	return t, nil
}

var priorityRank = map[string]int{"blocker": 1, "highest": 1, "critical": 2, "high": 2, "major": 3, "medium": 3, "minor": 4, "low": 4, "trivial": 5, "lowest": 5}

// compare orders two issues by a sort field (-1, 0, 1, ascending).
func (c *jqlContext) compare(a, b map[string]any, field string) int {
	switch field {
	case "key", "issuekey", "id":
		ka, kb := str(a["key"]), str(b["key"])
		switch {
		case ka == kb:
			return 0
		case keyLess(ka, kb):
			return -1
		}
		return 1
	case "priority":
		// Ascending = lowest priority first, as in Jira.
		return -cmpInt(priorityOrder(a), priorityOrder(b))
	}
	va, kind, _ := c.values(a, field)
	vb, _, _ := c.values(b, field)
	switch {
	case len(va) == 0 && len(vb) == 0:
		return 0
	case len(va) == 0:
		return 1 // empty values last
	case len(vb) == 0:
		return -1
	}
	if kind == kindText || kind == kindUser {
		kind = kindPlain
	}
	if field == "assignee" || field == "reporter" || field == "creator" {
		da, db := str(obj(obj(a, "fields"), field)["displayName"]), str(obj(obj(b, "fields"), field)["displayName"])
		return strings.Compare(strings.ToLower(da), strings.ToLower(db))
	}
	cmp, err := c.compareValues(va[0], vb[0], kind)
	if err != nil {
		return strings.Compare(va[0], vb[0])
	}
	return cmp
}

func priorityOrder(is map[string]any) int {
	p := obj(obj(is, "fields"), "priority")
	if r, ok := priorityRank[strings.ToLower(str(p["name"]))]; ok {
		return r
	}
	if n, err := strconv.Atoi(str(p["id"])); err == nil {
		return n
	}
	return 99
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// ParseSince parses an export's --since: RFC 3339, a date (2006-01-02,
// optionally with 15:04) or a period back from now (-7d, 2w, 12h, 30m).
func ParseSince(v string, now time.Time) (time.Time, error) {
	v = strings.TrimSpace(v)
	if m := relRe.FindStringSubmatch(v); m != nil {
		if m[1] != "-" {
			m = []string{m[0], "-", m[2], m[3]}
		}
		return shift(now, m), nil
	}
	return parseDate(v, now)
}
