package models

import "sync"

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
