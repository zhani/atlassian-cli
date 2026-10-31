package jira

import (
	"fmt"
	"regexp"
	"strings"
)

// ListOptions describes an issue query built from command-line filters.
//
// String filters may start with "~" to negate them. Assignee and Reporter
// also accept "me" (the current user) and "x" (nobody).
type ListOptions struct {
	Project    string
	Status     string
	Assignee   string
	Reporter   string
	Type       string
	Priority   string
	Labels     []string
	Watching   bool
	Unresolved bool // only issues without a resolution

	// Created and Updated select issues created/updated since a point in
	// time: a relative period such as "-7d", "2w" or "-30m", or one of
	// today, week, month and year.
	Created string
	Updated string

	OrderBy string // field to sort by; default "created"
	Reverse bool   // sort ascending instead of descending

	JQL string // extra JQL ANDed with the filters
}

var relativeDate = regexp.MustCompile(`^-?\d+[wdhm]$`)

var namedDates = map[string]string{
	"today": "startOfDay()",
	"week":  "startOfWeek()",
	"month": "startOfMonth()",
	"year":  "startOfYear()",
}

// BuildJQL turns ListOptions into a JQL query.
func BuildJQL(o ListOptions) (string, error) {
	var clauses []string
	add := func(field, value string) {
		if value != "" {
			clauses = append(clauses, equality(field, value))
		}
	}
	addUser := func(field, value string) {
		if value != "" {
			clauses = append(clauses, userClause(field, value))
		}
	}

	add("project", o.Project)
	add("status", o.Status)
	add("issuetype", o.Type)
	add("priority", o.Priority)
	addUser("assignee", o.Assignee)
	addUser("reporter", o.Reporter)
	for _, l := range o.Labels {
		add("labels", l)
	}
	if o.Watching {
		clauses = append(clauses, "watcher = currentUser()")
	}
	if o.Unresolved {
		clauses = append(clauses, "resolution = Unresolved")
	}
	for _, d := range []struct{ field, value string }{{"created", o.Created}, {"updated", o.Updated}} {
		if d.value == "" {
			continue
		}
		since, err := dateValue(d.value)
		if err != nil {
			return "", fmt.Errorf("--%s: %w", d.field, err)
		}
		clauses = append(clauses, d.field+" >= "+since)
	}
	if jql := strings.TrimSpace(o.JQL); jql != "" {
		clauses = append(clauses, "("+jql+")")
	}

	orderBy := o.OrderBy
	if orderBy == "" {
		orderBy = "created"
	}
	direction := "DESC"
	if o.Reverse {
		direction = "ASC"
	}

	query := strings.Join(clauses, " AND ")
	order := "ORDER BY " + orderBy + " " + direction
	if query == "" {
		return order, nil
	}
	return query + " " + order, nil
}

// negated splits a leading "~" (the negation marker) from a filter value.
func negated(value string) (string, bool) {
	if v, ok := strings.CutPrefix(value, "~"); ok {
		return v, true
	}
	return value, false
}

func equality(field, value string) string {
	v, neg := negated(value)
	op := "="
	if neg {
		op = "!="
	}
	return field + " " + op + " " + quote(v)
}

func userClause(field, value string) string {
	v, neg := negated(value)
	switch strings.ToLower(v) {
	case "x":
		if neg {
			return field + " IS NOT EMPTY"
		}
		return field + " IS EMPTY"
	case "me":
		if neg {
			return field + " != currentUser()"
		}
		return field + " = currentUser()"
	}
	return equality(field, value)
}

func dateValue(v string) (string, error) {
	if named, ok := namedDates[strings.ToLower(v)]; ok {
		return named, nil
	}
	if relativeDate.MatchString(v) {
		if !strings.HasPrefix(v, "-") {
			v = "-" + v
		}
		return quote(v), nil
	}
	return "", fmt.Errorf("invalid period %q (use e.g. -7d, 2w, today, week, month, year)", v)
}

// quote returns a JQL string literal.
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
