// Package xray reads Xray (Server/DC) test data that plain Jira fields only
// hint at: a Test's manual steps (the "Manual Test Steps" field), the IDs of
// its latest test runs (the "TestRunStatus" field) and the runs themselves
// (GET /rest/raven/1.0/api/testrun/{id}: per-step status, comment, actual
// result and evidence files). Field IDs differ per server, so the fields are
// recognised by their shape, not by ID or name.
package xray

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// TestRunPath is where Xray Server/DC serves a test run.
func TestRunPath(id string) string { return "/rest/raven/1.0/api/testrun/" + url.PathEscape(id) }

// Step is one manual test step: its labelled texts in display order
// (Action, Data, Expected Result, then any custom step fields).
type Step struct {
	Index  int
	Fields [][2]string
	Files  []File
}

// File is an evidence or step attachment: its name and download URL.
type File struct {
	ID, Name, URL string
}

var stepOrder = map[string]int{"action": 0, "step": 0, "data": 1, "expected result": 2, "result": 2}

// ManualSteps returns the steps of the first custom field shaped like Xray's
// Manual Test Steps ({"steps": [{"index", "fields": {...}} …]}), or nil.
func ManualSteps(custom map[string]any) []Step {
	for _, id := range sortedKeys(custom) {
		m, ok := custom[id].(map[string]any)
		if !ok {
			continue
		}
		list, ok := m["steps"].([]any)
		if !ok || len(list) == 0 {
			continue
		}
		var steps []Step
		for i, it := range list {
			sm, ok := it.(map[string]any)
			if !ok {
				return nil
			}
			steps = append(steps, Step{Index: intOr(sm["index"], i+1), Fields: stepFields(sm), Files: Files(sm["attachments"])})
		}
		if len(steps) > 0 && len(steps[0].Fields) > 0 {
			return steps
		}
	}
	return nil
}

// stepFields reads a step's texts from either Xray layout: "fields":
// {"Action": …, "Data": …, "Expected Result": …} or "step"/"data"/"result"
// values that are strings or {"raw", "rendered"}.
func stepFields(sm map[string]any) [][2]string {
	var out [][2]string
	if f, ok := sm["fields"].(map[string]any); ok {
		for k, v := range f {
			if t := text(v); t != "" {
				out = append(out, [2]string{k, t})
			}
		}
	} else {
		for k, label := range map[string]string{"step": "Action", "data": "Data", "result": "Expected Result"} {
			if t := text(sm[k]); t != "" {
				out = append(out, [2]string{label, t})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		oi, iok := stepOrder[strings.ToLower(out[i][0])]
		oj, jok := stepOrder[strings.ToLower(out[j][0])]
		switch {
		case iok && jok:
			return oi < oj
		case iok != jok:
			return iok
		}
		return out[i][0] < out[j][0]
	})
	return out
}

// RunRef points at a test run named by a TestRunStatus field.
type RunRef struct {
	ID, Group, Environment string
}

// RunRefs returns the latest (and latest final) test runs named by custom
// fields shaped like Xray's TestRunStatus ({"statuses": [{"group",
// "statusResults": [{"latestId", "latestFinalId", "environment"}]}]}), each
// run once.
func RunRefs(custom map[string]any) []RunRef {
	var out []RunRef
	seen := map[string]bool{}
	for _, id := range sortedKeys(custom) {
		m, ok := custom[id].(map[string]any)
		if !ok {
			continue
		}
		statuses, _ := m["statuses"].([]any)
		for _, s := range statuses {
			sm, _ := s.(map[string]any)
			results, _ := sm["statusResults"].([]any)
			for _, r := range results {
				rm, _ := r.(map[string]any)
				for _, k := range []string{"latestId", "latestFinalId"} {
					rid := idString(rm[k])
					if rid == "" || seen[rid] {
						continue
					}
					seen[rid] = true
					out = append(out, RunRef{ID: rid, Group: text(sm["group"]), Environment: text(rm["environment"])})
				}
			}
		}
	}
	return out
}

// Files collects every file reference ({"fileName", "fileURL"}, also
// "filename"/"fileUrl") anywhere inside v: a run's and its steps'
// evidences, step attachments.
func Files(v any) []File {
	var out []File
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			name := firstText(x, "fileName", "filename", "name")
			u := firstText(x, "fileURL", "fileUrl", "downloadUrl", "url")
			if name != "" && u != "" {
				out = append(out, File{ID: idString(x["id"]), Name: name, URL: u})
				return
			}
			for _, k := range sortedKeys(x) {
				walk(x[k])
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(v)
	return out
}

// LocalName is a file's name in the mirror: ID-prefixed so evidences with
// the same name don't collide, and without path separators.
func (f File) LocalName() string {
	name := strings.NewReplacer("/", "_", "\\", "_").Replace(f.Name)
	if f.ID != "" {
		return f.ID + "-" + name
	}
	return name
}

// Markdown renders the Test Details section: each step with its texts, and
// for every run its status, comment, actual result and evidence. Empty when
// the issue has neither steps nor runs.
func Markdown(steps []Step, runs []map[string]any) string {
	return MarkdownEmbed(steps, runs, nil)
}

// MarkdownEmbed is Markdown with embed, when set, adding markdown for each
// evidence or step attachment file after the line naming it (the TUI embeds
// images there to draw them in place); embed returns "" to skip a file.
func MarkdownEmbed(steps []Step, runs []map[string]any, embed func(File) string) string {
	embedAll := func(sb *strings.Builder, fs []File) {
		if embed == nil {
			return
		}
		for _, f := range fs {
			if e := embed(f); e != "" {
				sb.WriteString(e + "\n\n")
			}
		}
	}
	if len(steps) == 0 && len(runs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Test Details\n")
	for _, run := range runs {
		fmt.Fprintf(&sb, "\n%s\n", runSummary(run))
		if fs := Files(run["evidences"]); len(fs) > 0 {
			fmt.Fprintf(&sb, "\nRun evidence: %s\n\n", fileList(fs))
			embedAll(&sb, fs)
		}
	}
	results := map[int]map[string]any{} // step index -> latest run's step result
	if len(runs) > 0 {
		rsteps, _ := runs[0]["steps"].([]any)
		for i, s := range rsteps {
			if sm, ok := s.(map[string]any); ok {
				results[intOr(sm["index"], i+1)] = sm
			}
		}
	}
	if len(steps) == 0 { // no definition in the issue: take the texts from the run
		for idx := 1; idx <= len(results); idx++ {
			if r := results[idx]; r != nil {
				steps = append(steps, Step{Index: idx, Fields: stepFields(r)})
			}
		}
	}
	for _, st := range steps {
		r := results[st.Index]
		status := ""
		if r != nil {
			status = " — " + strings.ToUpper(text(r["status"]))
		}
		fmt.Fprintf(&sb, "\n### Step %d%s\n\n", st.Index, strings.TrimSuffix(status, " — "))
		for _, f := range st.Fields {
			fmt.Fprintf(&sb, "**%s:** %s\n\n", f[0], strings.TrimSpace(f[1]))
		}
		if len(st.Files) > 0 {
			fmt.Fprintf(&sb, "**Attachments:** %s\n\n", fileList(st.Files))
			embedAll(&sb, st.Files)
		}
		if r == nil {
			continue
		}
		if t := text(r["actualResult"]); t != "" {
			fmt.Fprintf(&sb, "**Actual result:** %s\n\n", t)
		}
		if t := text(r["comment"]); t != "" {
			fmt.Fprintf(&sb, "**Comment:** %s\n\n", t)
		}
		if fs := Files(r["evidences"]); len(fs) > 0 {
			fmt.Fprintf(&sb, "**Evidence:** %s\n\n", fileList(fs))
			embedAll(&sb, fs)
		}
		if ds := defects(r["defects"]); ds != "" {
			fmt.Fprintf(&sb, "**Defects:** %s\n\n", ds)
		}
	}
	if len(runs) > 1 {
		sb.WriteString("_Step results are from the first run listed._\n")
	}
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

func runSummary(run map[string]any) string {
	parts := []string{"**Test run " + idString(run["id"]) + ":** " + strings.ToUpper(text(run["status"]))}
	if k := text(run["testExecKey"]); k != "" {
		parts = append(parts, "execution "+k)
	}
	if envs, ok := run["testEnvironments"].([]any); ok && len(envs) > 0 {
		var es []string
		for _, e := range envs {
			es = append(es, text(e))
		}
		parts = append(parts, "environment "+strings.Join(es, ", "))
	}
	if who := text(run["executedBy"]); who != "" {
		parts = append(parts, "by "+who)
	}
	if t := firstText(run, "finishedOn", "startedOn"); t != "" {
		parts = append(parts, t)
	}
	s := strings.Join(parts, " · ")
	if c := text(run["comment"]); c != "" {
		s += "\n\n" + c
	}
	return s
}

func fileList(fs []File) string {
	names := make([]string, len(fs))
	for i, f := range fs {
		names[i] = "`" + f.Name + "`"
	}
	return strings.Join(names, ", ")
}

func defects(v any) string {
	list, _ := v.([]any)
	var out []string
	for _, d := range list {
		if t := text(d); t != "" {
			out = append(out, t)
		} else if m, ok := d.(map[string]any); ok {
			if k := firstText(m, "key", "issueKey"); k != "" {
				out = append(out, k)
			}
		}
	}
	return strings.Join(out, ", ")
}

// text reads a displayable string: a string, a number, or an object's
// "raw"/"rendered"/"name"/"value"/"displayName".
func text(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case map[string]any:
		return firstText(x, "raw", "rendered", "displayName", "name", "value")
	}
	return ""
}

func firstText(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func idString(v any) string {
	switch x := v.(type) {
	case float64:
		if x > 0 {
			return strconv.FormatInt(int64(x), 10)
		}
	case string:
		return x
	}
	return ""
}

func intOr(v any, def int) int {
	if f, ok := v.(float64); ok && f > 0 {
		return int(f)
	}
	return def
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
