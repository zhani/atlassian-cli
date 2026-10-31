package confluence

import "strings"

// SearchOptions describes a content search built from command-line filters.
type SearchOptions struct {
	Text  string // full-text query
	Space string // space key
	Type  string // content type: page (default), blogpost, or "all"
}

// BuildCQL turns SearchOptions into a CQL query. Text searches keep
// Confluence's relevance order; others list the most recently modified first.
func BuildCQL(o SearchOptions) string {
	var clauses []string
	switch t := strings.ToLower(o.Type); t {
	case "all":
	case "":
		clauses = append(clauses, "type = page")
	default:
		clauses = append(clauses, "type = "+quoteCQL(t))
	}
	if o.Space != "" {
		clauses = append(clauses, "space = "+quoteCQL(o.Space))
	}
	if o.Text != "" {
		clauses = append(clauses, "text ~ "+quoteCQL(o.Text))
	}
	cql := strings.Join(clauses, " AND ")
	if o.Text == "" {
		cql += " ORDER BY lastmodified DESC"
	}
	return strings.TrimSpace(cql)
}

func quoteCQL(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
