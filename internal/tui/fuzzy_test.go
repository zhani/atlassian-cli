package tui

import (
	"reflect"
	"testing"
)

func TestFuzzyMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pattern, text string
		ok            bool
		pos           []int
	}{
		{"", "anything", true, nil},
		{"rb", "Runbook", true, []int{0, 3}},
		{"rnbk", "Release notes backlog", true, []int{0, 8, 14, 17}},
		{"book run", "Runbook", true, []int{0, 1, 2, 3, 4, 5, 6}}, // terms in any order
		{"xyz", "Runbook", false, nil},
		{"run x", "Runbook", false, nil}, // every term must match
		{"Run", "runbook", false, nil},   // smart case: upper case is exact
		{"Run", "Runbook", true, []int{0, 1, 2}},
		{"api", "REST API guide", true, []int{5, 6, 7}}, // prefers the word start
	}
	for _, tt := range tests {
		_, pos, ok := fuzzyMatch(tt.pattern, tt.text)
		if ok != tt.ok || (ok && !reflect.DeepEqual(pos, tt.pos)) {
			t.Errorf("fuzzyMatch(%q, %q) = %v %v, want %v %v", tt.pattern, tt.text, pos, ok, tt.pos, tt.ok)
		}
	}
}

func TestFuzzyScoreOrder(t *testing.T) {
	t.Parallel()
	score := func(p, s string) int {
		sc, _, ok := fuzzyMatch(p, s)
		if !ok {
			t.Fatalf("%q does not match %q", p, s)
		}
		return sc
	}
	// Consecutive and word-start matches rank above scattered ones.
	if a, b := score("arch", "Architecture"), score("arch", "a research chapter"); a <= b {
		t.Errorf("consecutive %d <= scattered %d", a, b)
	}
	if a, b := score("dg", "Design Guide"), score("dg", "badger"); a <= b {
		t.Errorf("word starts %d <= inside %d", a, b)
	}
	if a, b := score("rel", "Release notes"), score("rel", "Bugs in the parallel worker"); a <= b {
		t.Errorf("prefix %d <= middle %d", a, b)
	}
}
