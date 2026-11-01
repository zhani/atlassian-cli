package mirror

import (
	"fmt"
	"net/http"
	"sort"
	"time"
)

// SearchCQL returns the mirrored pages matching a CQL query. Supported
// fields: type, space, title, text, id, ancestor, parent, label,
// lastmodified, created, creator; ORDER BY title, created or lastmodified.
func (s *Server) SearchCQL(cql string) ([]map[string]any, error) {
	q, err := parseQuery(cql)
	if err != nil {
		return nil, fmt.Errorf("CQL: %w", err)
	}
	now := s.now()
	var out []map[string]any
	for _, id := range s.pageOrder {
		p := s.pages[id]
		ok, err := eval(q.where, func(c *clause) (bool, error) { return s.cqlMatch(p, c, now) })
		if err != nil {
			return nil, fmt.Errorf("CQL: %w", err)
		}
		if ok {
			out = append(out, p)
		}
	}
	if len(q.order) > 0 {
		k := q.order[0]
		sort.SliceStable(out, func(i, j int) bool {
			a, b := cqlValues(out[i], k.field), cqlValues(out[j], k.field)
			if len(a) == 0 || len(b) == 0 {
				return len(a) > len(b)
			}
			if k.desc {
				return a[0] > b[0]
			}
			return a[0] < b[0]
		})
	}
	return out, nil
}

func cqlValues(p map[string]any, field string) []string {
	switch field {
	case "type":
		return nonEmpty(str(p["type"]))
	case "space":
		return nonEmpty(pageSpace(p), str(obj(p, "space")["name"]))
	case "title":
		return nonEmpty(str(p["title"]))
	case "text":
		return []string{str(p["title"]) + "\n" + str(obj(obj(p, "body"), "storage")["value"])}
	case "id", "content":
		return nonEmpty(str(p["id"]))
	case "parent":
		return nonEmpty(parentID(p))
	case "ancestor":
		var out []string
		anc, _ := p["ancestors"].([]any)
		for _, a := range anc {
			if am, ok := a.(map[string]any); ok {
				out = append(out, str(am["id"]))
			}
		}
		return out
	case "label":
		ls, _ := obj(obj(p, "metadata"), "labels")["results"].([]any)
		var out []string
		for _, l := range ls {
			if lm, ok := l.(map[string]any); ok {
				out = append(out, str(lm["name"]))
			}
		}
		return out
	case "lastmodified":
		return nonEmpty(str(obj(p, "version")["when"]))
	case "created":
		return nonEmpty(str(obj(p, "history")["createdDate"]))
	case "creator":
		return userValues(obj(obj(p, "history"), "createdBy"))
	}
	return nil
}

var cqlFields = map[string]bool{"type": true, "space": true, "title": true, "text": true, "id": true, "content": true,
	"parent": true, "ancestor": true, "label": true, "lastmodified": true, "created": true, "creator": true}

func (s *Server) cqlMatch(p map[string]any, c *clause, now time.Time) (bool, error) {
	if !cqlFields[c.field] {
		return false, fmt.Errorf("field %q is not supported by the offline mirror", c.raw)
	}
	got := cqlValues(p, c.field)
	switch c.op {
	case "is":
		return len(got) == 0, nil
	case "is not":
		return len(got) > 0, nil
	}
	var want []string
	for _, o := range c.values {
		switch o.fn {
		case "":
			want = append(want, o.text)
		case "currentuser":
			want = append(want, userValues(s.myself)...)
		case "now":
			t := now
			if len(o.args) > 0 {
				if m := relRe.FindStringSubmatch(o.args[0]); m != nil {
					t = shift(now, m)
				}
			}
			want = append(want, t.Format(time.RFC3339))
		default:
			return false, fmt.Errorf("function %s() is not supported by the offline mirror", o.text)
		}
	}
	ctx := &jqlContext{s: s, now: now}
	kind := kindPlain
	if c.field == "lastmodified" || c.field == "created" {
		kind = kindDate
	}
	switch c.op {
	case "=", "in":
		return ctx.anyEqual(got, want, kind, c.field), nil
	case "!=", "not in":
		return !ctx.anyEqual(got, want, kind, c.field), nil
	case "~", "!~":
		hit := false
		for _, g := range got {
			for _, w := range want {
				if textMatches(g, w) {
					hit = true
				}
			}
		}
		return hit == (c.op == "~"), nil
	default:
		if len(got) == 0 || len(want) == 0 {
			return false, nil
		}
		cmp, err := ctx.compareValues(got[0], want[0], kind)
		if err != nil {
			return false, err
		}
		return map[string]bool{">": cmp > 0, ">=": cmp >= 0, "<": cmp < 0, "<=": cmp <= 0}[c.op], nil
	}
}

func (s *Server) searchContent(w http.ResponseWriter, r *http.Request) {
	pages, err := s.SearchCQL(r.URL.Query().Get("cql"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	expand := r.URL.Query().Get("expand")
	out := make([]any, len(pages))
	for i, p := range pages {
		out[i] = s.pageView(p, expand)
	}
	s.paged(w, r, out)
}
