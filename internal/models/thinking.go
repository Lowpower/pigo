package models

import (
	"strings"
	"sync"
)

var (
	budgetMu sync.Mutex
	budgets  map[string]int
)

var defaultBudgets = map[string]int{
	"minimal": 1024,
	"low":     2048,
	"medium":  5120,
	"high":    10000,
	"xhigh":   31999,
	"max":     31999,
}

// SetThinkingBudgets replaces the per-level token table (settings.thinkingBudgets).
func SetThinkingBudgets(m map[string]int) {
	budgetMu.Lock()
	defer budgetMu.Unlock()
	if m == nil {
		budgets = nil
		return
	}
	budgets = make(map[string]int, len(m))
	for k, v := range m {
		budgets[k] = v
	}
}

// BudgetTokens returns the token budget for a thinking level.
func BudgetTokens(level string) int {
	budgetMu.Lock()
	defer budgetMu.Unlock()
	if budgets != nil {
		if v, ok := budgets[level]; ok {
			return v
		}
	}
	return defaultBudgets[level]
}

// SupportsReasoning reports whether the catalog marks this model as a
// reasoning model. A missing field keeps thinking available so builtins and
// overlays without the flag do not regress.
func (m Model) SupportsReasoning() bool {
	if m.Reasoning == nil {
		return true
	}
	return *m.Reasoning
}

// SupportsImage reports whether the model accepts image input. A missing
// input list is treated as image-capable.
func (m Model) SupportsImage() bool {
	if len(m.Input) == 0 {
		return true
	}
	for _, in := range m.Input {
		if strings.EqualFold(in, "image") {
			return true
		}
	}
	return false
}

// ThinkingLevelsFor returns the thinking levels this model accepts.
func (m Model) ThinkingLevelsFor() []string {
	if !m.SupportsReasoning() {
		return []string{"off"}
	}
	var out []string
	for _, level := range ThinkingLevels {
		mapped, ok := m.ThinkingLevelMap[level]
		if ok && mapped == nil {
			continue
		}
		if level == "xhigh" || level == "max" {
			if !ok {
				continue
			}
		}
		out = append(out, level)
	}
	if len(out) == 0 {
		return []string{"off"}
	}
	return out
}

// RequestThinkingLevel is the canonical level for one request.
// off and an empty level stay put, including when the map marks off
// unsupported, so callers omit the reasoning field instead of enabling
// a nearby level. Any other level is ClampThinking.
func (m Model) RequestThinkingLevel(level string) string {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "" || level == "off" {
		return level
	}
	return ClampThinking(level, m)
}

// ProviderThinkingValue is the effort string to send for a canonical level.
// An explicit map entry wins. A null or blank entry is unsupported and is
// not sent. A missing entry uses the level itself, except off and empty,
// which are omitted.
func (m Model) ProviderThinkingValue(level string) (string, bool) {
	level = strings.ToLower(strings.TrimSpace(level))
	if mapped, ok := m.ThinkingLevelMap[level]; ok {
		if mapped == nil {
			return "", false
		}
		v := strings.TrimSpace(*mapped)
		if v == "" {
			return "", false
		}
		return v, true
	}
	if level == "" || level == "off" {
		return "", false
	}
	return level, true
}

// ClampThinking maps level onto the nearest supported value for m.
func ClampThinking(level string, m Model) string {
	level = strings.ToLower(strings.TrimSpace(level))
	available := m.ThinkingLevelsFor()
	for _, l := range available {
		if l == level {
			return l
		}
	}
	requested := -1
	for i, l := range ThinkingLevels {
		if l == level {
			requested = i
			break
		}
	}
	if requested == -1 {
		return available[0]
	}
	for i := requested; i < len(ThinkingLevels); i++ {
		for _, l := range available {
			if l == ThinkingLevels[i] {
				return l
			}
		}
	}
	for i := requested - 1; i >= 0; i-- {
		for _, l := range available {
			if l == ThinkingLevels[i] {
				return l
			}
		}
	}
	return available[0]
}

// fillOpus55ThinkingLevels supplies the effort table pi uses when models.dev
// lists Opus 5.5 before effort metadata is complete. A map that already has
// every fallback key is left unchanged, including null entries.
func fillOpus55ThinkingLevels(m Model) Model {
	if !opus55EffortModel(m) {
		return m
	}
	fallback := opus55EffortFallback()
	if thinkingMapHasKeys(m.ThinkingLevelMap, fallback) {
		return m
	}
	out := make(map[string]*string, len(fallback)+len(m.ThinkingLevelMap))
	for k, v := range m.ThinkingLevelMap {
		out[k] = v
	}
	for k, v := range fallback {
		if _, ok := out[k]; ok {
			continue
		}
		out[k] = v
	}
	m.ThinkingLevelMap = out
	return m
}

func opus55EffortModel(m Model) bool {
	return (m.Provider == "anthropic" && m.ID == "claude-opus-5-5") ||
		(m.Provider == "github-copilot" && m.ID == "claude-opus-5.5")
}

func opus55EffortFallback() map[string]*string {
	out := map[string]*string{
		"off":     nil,
		"minimal": nil,
	}
	for _, level := range []string{"low", "medium", "high", "xhigh", "max"} {
		s := level
		out[level] = &s
	}
	return out
}

func thinkingMapHasKeys(got, keys map[string]*string) bool {
	for k := range keys {
		if _, ok := got[k]; !ok {
			return false
		}
	}
	return true
}

// NextThinkingLevelIn cycles current within levels.
func NextThinkingLevelIn(current string, levels []string) string {
	if len(levels) == 0 {
		return "off"
	}
	current = strings.ToLower(strings.TrimSpace(current))
	idx := 0
	for i, l := range levels {
		if l == current {
			idx = i
			break
		}
	}
	return levels[(idx+1)%len(levels)]
}
