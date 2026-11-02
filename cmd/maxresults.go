package cmd

import (
	"os"
	"strconv"
	"strings"
)

// maxResultsEnv overrides the default of every --max-results flag (issue
// list/search and the explorers); an explicit --max-results still wins.
const maxResultsEnv = "JIRA_MAX_RESULTS"

// builtinMaxResults is the default without JIRA_MAX_RESULTS: enough for a
// whole board or mirror in one browser, which loads its issues up front.
const builtinMaxResults = 500

// defaultMaxResults is JIRA_MAX_RESULTS when it is a positive number, else
// builtinMaxResults.
func defaultMaxResults() int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(maxResultsEnv))); err == nil && n > 0 {
		return n
	}
	return builtinMaxResults
}
