package jira

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// sprintName extracts the name from Jira Server's legacy sprint strings
// ("com.atlassian.greenhopper.service.sprint.Sprint@1a2b[id=1,...,name=Sprint 5,...]").
var sprintName = regexp.MustCompile(`^com\.atlassian\.greenhopper\..*\[.*\bname=([^,\]]*)`)

// SimplifyValue reduces a custom field value to something readable: option
// values, user and object names, plain scalars and lists of those. Values
// that are empty, or objects without an obvious name, become nil and are
// left out (they remain available with full output).
func SimplifyValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		if x == "" {
			return nil
		}
		if m := sprintName.FindStringSubmatch(x); m != nil {
			return m[1]
		}
		return x
	case float64, bool:
		return x
	case map[string]any:
		for _, k := range []string{"value", "displayName", "name", "key"} {
			s, ok := x[k].(string)
			if !ok || s == "" {
				continue
			}
			if child, ok := x["child"].(map[string]any); ok { // cascading select
				if cv, ok := child["value"].(string); ok && cv != "" {
					return s + " / " + cv
				}
			}
			return s
		}
		return nil
	case []any:
		var out []any
		for _, e := range x {
			if sv := SimplifyValue(e); sv != nil {
				out = append(out, sv)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
}

// DisplayValue renders a field value for a table cell: simplified values
// as text (lists comma-separated), anything else as compact JSON.
func DisplayValue(v any) string {
	if v == nil {
		return ""
	}
	switch sv := SimplifyValue(v).(type) {
	case nil:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	case []any:
		parts := make([]string, len(sv))
		for i, e := range sv {
			parts[i] = fmt.Sprint(e)
		}
		return strings.Join(parts, ", ")
	case float64:
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%f", sv), "0"), ".")
	default:
		return fmt.Sprint(sv)
	}
}
