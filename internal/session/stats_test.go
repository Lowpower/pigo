package session

import (
	"strings"
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
	e, err := m.AppendCompaction("summary", "", 0)
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

func TestFormatInfoIncludesTotals(t *testing.T) {
	tok := 12
	pct := 6.0
	s := Stats{
		SessionFile: "/tmp/s.jsonl", SessionID: "abc",
		UserMessages: 1, AssistantMessages: 1, ToolCalls: 2, ToolResults: 2, TotalMessages: 2,
		Tokens: TokenTotals{Input: 10, Output: 5, CacheRead: 2, CacheWrite: 1, Total: 18},
		Cost:   1.25,
		ContextUsage: &ContextUsage{Tokens: &tok, ContextWindow: 200, Percent: &pct},
	}
	got := FormatInfo(s, "demo")
	for _, want := range []string{"Name: demo", "File: /tmp/s.jsonl", "ID: abc", "Total: 2", "Input: 13", "Output: 5", "$1.250", "12 / 200"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
