package tui

import (
	"strings"
	"unicode"
)

// Fuzzy matching in the spirit of fzf: every space-separated term of the
// pattern must occur in the text as a subsequence. Matching ignores case
// unless the pattern has an upper-case letter (smart case). Scores favour
// consecutive characters and characters at word starts, and penalise gaps.

const (
	scoreMatch       = 16
	bonusBoundary    = 10 // character starts a word
	bonusConsecutive = 8  // character follows the previous match directly
	bonusFirst       = 6  // character is the first of the text
	penaltyGapStart  = 3
	penaltyGap       = 1
)

// fuzzyMatch reports whether pattern matches text, with a score (higher is
// better) and the matched rune positions in text (sorted, unique).
func fuzzyMatch(pattern, text string) (score int, positions []int, ok bool) {
	terms := strings.Fields(pattern)
	if len(terms) == 0 {
		return 0, nil, true
	}
	caseSensitive := strings.IndexFunc(pattern, unicode.IsUpper) >= 0
	runes := []rune(text)
	folded := runes
	if !caseSensitive {
		folded = []rune(strings.ToLower(text))
		if len(folded) != len(runes) { // rare: lower-casing changed the length
			folded = make([]rune, len(runes))
			for i, r := range runes {
				folded[i] = unicode.ToLower(r)
			}
		}
	}
	seen := map[int]bool{}
	for _, term := range terms {
		t := []rune(term)
		if !caseSensitive {
			t = []rune(strings.ToLower(term))
		}
		s, pos, found := matchTerm(t, folded, runes)
		if !found {
			return 0, nil, false
		}
		score += s
		for _, p := range pos {
			if !seen[p] {
				seen[p] = true
				positions = append(positions, p)
			}
		}
	}
	sortInts(positions)
	return score, positions, true
}

// matchTerm finds the best placement of term in text: for each possible
// end of a forward subsequence match it walks back to the shortest window
// (as fzf v1 does) and keeps the highest score.
func matchTerm(term, folded, orig []rune) (best int, bestPos []int, ok bool) {
	n := len(folded)
	for start := 0; start < n; start++ {
		if folded[start] != term[0] {
			continue
		}
		// Forward: first subsequence match beginning at start.
		end, ti := -1, 0
		for i := start; i < n && ti < len(term); i++ {
			if folded[i] == term[ti] {
				ti++
				if ti == len(term) {
					end = i
				}
			}
		}
		if end < 0 {
			break // no later start can match either
		}
		// Backward: tighten the window ending at end.
		pos := make([]int, len(term))
		ti = len(term) - 1
		for i := end; i >= start && ti >= 0; i-- {
			if folded[i] == term[ti] {
				pos[ti] = i
				ti--
			}
		}
		s := scorePositions(pos, orig)
		if !ok || s > best {
			best, bestPos, ok = s, pos, true
		}
	}
	return best, bestPos, ok
}

func scorePositions(pos []int, text []rune) int {
	score := 0
	for i, p := range pos {
		score += scoreMatch
		if p == 0 {
			score += bonusFirst
		}
		if isBoundary(text, p) {
			score += bonusBoundary
		}
		if i > 0 {
			if gap := p - pos[i-1] - 1; gap == 0 {
				score += bonusConsecutive
			} else {
				score -= penaltyGapStart + penaltyGap*(gap-1)
			}
		}
	}
	return score
}

// isBoundary reports whether text[i] starts a word: the first character,
// one after a separator, or an upper-case letter after a lower-case one.
func isBoundary(text []rune, i int) bool {
	if i == 0 {
		return true
	}
	prev, cur := text[i-1], text[i]
	switch {
	case !unicode.IsLetter(prev) && !unicode.IsDigit(prev):
		return unicode.IsLetter(cur) || unicode.IsDigit(cur)
	case unicode.IsLower(prev) && unicode.IsUpper(cur):
		return true
	case unicode.IsLetter(prev) && unicode.IsDigit(cur):
		return true
	}
	return false
}

func sortInts(a []int) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
