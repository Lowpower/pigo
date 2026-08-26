package tui

import (
	"sort"
	"strings"
	"unicode"
)

// fuzzyFilter returns items whose getText matches query (subsequence), best first.
func fuzzyFilter[T any](items []T, query string, getText func(T) string) []T {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return append([]T(nil), items...)
	}
	type scored struct {
		item  T
		score int
	}
	var hit []scored
	for _, it := range items {
		ok, score := fuzzyMatch(q, strings.ToLower(getText(it)))
		if ok {
			hit = append(hit, scored{it, score})
		}
	}
	sort.SliceStable(hit, func(i, j int) bool { return hit[i].score < hit[j].score })
	out := make([]T, len(hit))
	for i, h := range hit {
		out[i] = h.item
	}
	return out
}

func fuzzyMatch(query, text string) (bool, int) {
	if query == "" {
		return true, 0
	}
	if len(query) > len(text) {
		return false, 0
	}
	qi := 0
	score := 0
	last := -1
	consec := 0
	for i := 0; i < len(text) && qi < len(query); i++ {
		if text[i] != query[qi] {
			continue
		}
		boundary := i == 0 || isWordBoundary(rune(text[i-1]))
		if last == i-1 {
			consec++
			score -= consec * 5
		} else {
			consec = 0
			if last >= 0 {
				score += (i - last - 1) * 2
			}
		}
		if boundary {
			score -= 10
		}
		score += i / 10
		last = i
		qi++
	}
	if qi < len(query) {
		return false, 0
	}
	if query == text {
		score -= 100
	}
	return true, score
}

func isWordBoundary(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("\\-_/.:", r)
}
