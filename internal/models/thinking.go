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
