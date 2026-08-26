package session

import (
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
)

func costUsage(tokens int, total float64) ai.Usage {
	return ai.Usage{
		Input: tokens, TotalTokens: tokens,
		Cost: ai.UsageCost{Total: total},
	}
}

func TestCollectStatsSumsAssistantCost(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	if _, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "hi"}); err != nil {
		t.Fatal(err)
	}
	asst := &ai.AssistantMessage{
		Role:    ai.RoleAssistant,
		Content: []*ai.Content{{Type: ai.KindText, Text: "yo"}},
		Usage:   costUsage(10, 1.5),
	}
	if _, err := m.AppendMessage("assistant", asst); err != nil {
		t.Fatal(err)
	}
	got := CollectStats(m, nil, 0)
	if got.Cost != 1.5 {
		t.Fatalf("cost=%v want 1.5", got.Cost)
	}
	if got.Tokens.Input != 10 || got.Tokens.Total != 10 {
		t.Fatalf("tokens=%+v", got.Tokens)
	}
}

func TestCollectStatsIncludesToolResultUsage(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	tr := map[string]any{
		"role":    "toolResult",
		"content": "ok",
		"usage":   costUsage(100, 1),
	}
	if _, err := m.AppendMessage("toolResult", tr); err != nil {
		t.Fatal(err)
	}
	got := CollectStats(m, nil, 0)
	if got.Cost != 1 || got.Tokens.Input != 100 {
		t.Fatalf("got cost=%v tokens=%+v", got.Cost, got.Tokens)
	}
}

func TestCollectStatsIncludesCompactionUsage(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	e, err := m.AppendCompaction("summary")
	if err != nil {
		t.Fatal(err)
	}
	u := costUsage(40, 1)
	e.Usage = &u
	got := CollectStats(m, nil, 0)
	if got.Cost != 1 || got.Tokens.Input != 40 {
		t.Fatalf("got cost=%v tokens=%+v", got.Cost, got.Tokens)
	}
	if got.TotalMessages != 0 {
		t.Fatalf("compaction should not count as a message: %d", got.TotalMessages)
	}
}

func TestCollectStatsIncludesBranchSummaryUsage(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	u := costUsage(40, 1)
	m.entries = append(m.entries, &Entry{Type: "branch_summary", Usage: &u})
	got := CollectStats(m, nil, 0)
	if got.Cost != 1 || got.Tokens.Input != 40 {
		t.Fatalf("got cost=%v tokens=%+v", got.Cost, got.Tokens)
	}
}
