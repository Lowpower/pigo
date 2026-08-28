package models

import (
	"strings"

	"github.com/gobwas/glob"
)

// ThinkingLevels is the ordered set of thinking levels.
var ThinkingLevels = []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}

// IsThinkingLevel reports whether s is a known thinking level.
func IsThinkingLevel(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, l := range ThinkingLevels {
		if l == s {
			return true
		}
	}
	return false
}

// NextThinkingLevel cycles thinking levels.
func NextThinkingLevel(current string) string {
	current = strings.ToLower(strings.TrimSpace(current))
	idx := 0
	for i, l := range ThinkingLevels {
		if l == current {
			idx = i
			break
		}
	}
	return ThinkingLevels[(idx+1)%len(ThinkingLevels)]
}

// Model is a known catalog entry used by --list-models and /model.
type Model struct {
	Provider string `json:"provider,omitempty"`
	ID       string `json:"id"`
	API      string `json:"api,omitempty"`
	BaseURL  string `json:"baseUrl,omitempty"`
}

// Spec is a model plus optional thinking override (--models sonnet:high).
type Spec struct {
	Model
	Thinking string
}

// ParseSpec parses "provider/id", "id", and optional ":thinking" (--model).
func ParseSpec(s string) (provider, id, thinking string) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, ":"); i >= 0 && IsThinkingLevel(s[i+1:]) {
		thinking = strings.ToLower(s[i+1:])
		s = s[:i]
	}
	if prov, rest, ok := strings.Cut(s, "/"); ok && prov != "" && rest != "" {
		return prov, rest, thinking
	}
	return "", s, thinking
}

func (m Model) String() string { return m.Provider + "/" + m.ID }

// ResolvePatterns maps --models patterns onto the catalog (globs + substring).
func ResolvePatterns(patterns []string) []Spec {
	return ResolvePatternsIn(patterns, Catalog())
}

// ResolvePatternsIn maps patterns onto list (same matching as ResolvePatterns).
func ResolvePatternsIn(patterns []string, list []Model) []Spec {
	if len(patterns) == 0 {
		return nil
	}
	var out []Spec
	seen := map[string]bool{}
	for _, raw := range patterns {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		_, _, thinking := ParseSpec(raw)
		pattern := raw
		if thinking != "" {
			if i := strings.LastIndex(raw, ":"); i >= 0 {
				pattern = raw[:i]
			}
		}
		g, gerr := glob.Compile(strings.ToLower(pattern))
		for _, m := range list {
			hay := strings.ToLower(m.Provider + "/" + m.ID)
			match := strings.Contains(hay, strings.ToLower(pattern)) ||
				strings.Contains(strings.ToLower(m.ID), strings.ToLower(pattern))
			if gerr == nil && g.Match(hay) {
				match = true
			}
			if !match {
				continue
			}
			key := m.Provider + "/" + m.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Spec{Model: m, Thinking: thinking})
		}
	}
	return out
}

// UnmatchedPatterns returns patterns that resolve to no models in list.
func UnmatchedPatterns(patterns []string, list []Model) []string {
	var out []string
	for _, raw := range patterns {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if len(ResolvePatternsIn([]string{raw}, list)) == 0 {
			out = append(out, raw)
		}
	}
	return out
}

// Cycle returns the next/previous catalog (or scoped) model.
func Cycle(currentProvider, currentID string, scoped []Spec, backward bool) (Spec, bool) {
	list := scoped
	if len(list) == 0 {
		for _, m := range Catalog() {
			list = append(list, Spec{Model: m})
		}
	}
	if len(list) <= 1 {
		return Spec{}, false
	}
	idx := 0
	for i, s := range list {
		if s.Provider == currentProvider && s.ID == currentID {
			idx = i
			break
		}
	}
	next := idx + 1
	if backward {
		next = idx - 1
	}
	n := len(list)
	next = (next%n + n) % n
	return list[next], true
}
