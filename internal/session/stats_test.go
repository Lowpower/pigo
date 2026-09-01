package session

import (
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
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
		Tokens:       TokenTotals{Input: 10, Output: 5, CacheRead: 2, CacheWrite: 1, Total: 18},
		Cost:         1.25,
		ContextUsage: &ContextUsage{Tokens: &tok, ContextWindow: 200, Percent: &pct},
	}
	got := FormatInfo(s, "demo")
	for _, want := range []string{"Name: demo", "File: /tmp/s.jsonl", "ID: abc", "Total: 2", "Input: 13", "Output: 5", "$1.250", "12 / 200"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestCollectStatsCostBreakdownByModel(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	if _, err := m.AppendMessage("assistant", &ai.AssistantMessage{
		Role:     ai.RoleAssistant,
		Provider: "anthropic",
		Model:    "claude-sonnet-4",
		Usage:    costUsage(10, 1.5),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AppendMessage("assistant", &ai.AssistantMessage{
		Role:     ai.RoleAssistant,
		Provider: "openai",
		Model:    "gpt-4o",
		Usage:    costUsage(4, 0.5),
	}); err != nil {
		t.Fatal(err)
	}
	u := costUsage(40, 1)
	if _, err := m.AppendCompaction("sum", "", 0, CompactionMeta{Usage: &u}); err != nil {
		t.Fatal(err)
	}
	got := CollectStats(m, nil, 0)
	if len(got.CostBreakdown) != 3 {
		t.Fatalf("breakdown=%+v", got.CostBreakdown)
	}
	if got.CostBreakdown[0].Key != "anthropic/claude-sonnet-4" || got.CostBreakdown[0].Cost != 1.5 {
		t.Fatalf("first=%+v", got.CostBreakdown[0])
	}
	keys := map[string]CostBreakdown{}
	for _, row := range got.CostBreakdown {
		keys[row.Key] = row
	}
	if keys["Tools/summaries"].Tokens != 40 || keys["openai/gpt-4o"].Cost != 0.5 {
		t.Fatalf("keys=%+v", keys)
	}
}

func TestCollectStatsCacheWasteOnFullMiss(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	write := func(cacheWrite int, cacheWriteCost float64) error {
		_, err := m.AppendMessage("assistant", &ai.AssistantMessage{
			Role:     ai.RoleAssistant,
			Provider: "anthropic",
			Model:    "claude-sonnet-4",
			Usage: ai.Usage{
				CacheWrite: cacheWrite,
				Cost:       ai.UsageCost{CacheWrite: cacheWriteCost, Total: cacheWriteCost},
			},
		})
		return err
	}
	if err := write(100_000, 0.375); err != nil {
		t.Fatal(err)
	}
	if err := write(110_000, 0.4125); err != nil {
		t.Fatal(err)
	}
	got := CollectStats(m, nil, 0)
	if got.CacheWaste == nil || got.CacheWaste.MissCount != 1 || got.CacheWaste.MissedTokens != 100_000 {
		t.Fatalf("waste=%+v", got.CacheWaste)
	}
}

func TestCollectStatsCacheWasteUsesCatalogPrice(t *testing.T) {
	models.ClearOverlays()
	t.Cleanup(models.ClearOverlays)
	models.SetUserOverlay("priced", []models.Model{{
		Provider: "priced", ID: "m1", Cost: &models.Cost{CacheRead: 0.30},
	}})
	m := New(t.TempDir(), t.TempDir())
	write := func(cacheWrite int, inputCost float64) error {
		_, err := m.AppendMessage("assistant", &ai.AssistantMessage{
			Role:     ai.RoleAssistant,
			Provider: "priced",
			Model:    "m1",
			Usage: ai.Usage{
				CacheWrite: cacheWrite,
				Cost:       ai.UsageCost{Input: inputCost, Total: inputCost},
			},
		})
		return err
	}
	if err := write(100_000, 0.375); err != nil {
		t.Fatal(err)
	}
	if err := write(110_000, 0.4125); err != nil {
		t.Fatal(err)
	}
	got := CollectStats(m, nil, 0)
	if got.CacheWaste == nil || got.CacheWaste.MissedCost <= 0 {
		t.Fatalf("catalog fallback should price the miss: %+v", got.CacheWaste)
	}
}

func TestFormatInfoPerModelAndCacheRebilled(t *testing.T) {
	s := Stats{
		Cost: 2,
		CostBreakdown: []CostBreakdown{
			{Key: "anthropic/claude-sonnet-4", Cost: 1.5, Tokens: 10},
			{Key: "Tools/summaries", Cost: 0.5, Tokens: 40},
		},
		CacheWaste: &CacheWaste{MissedTokens: 100000, MissedCost: 0.362, MissCount: 1},
	}
	got := FormatInfo(s, "")
	for _, want := range []string{
		"anthropic/claude-sonnet-4: $1.500 (10 tokens)",
		"Tools/summaries: $0.500 (40 tokens)",
		"Cache Re-billed: $0.362 (100000 tokens, 1 miss)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
