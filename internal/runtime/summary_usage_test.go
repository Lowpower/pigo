package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/compaction"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/session"
)

func usageReply(text string, u ai.Usage) ai.StreamFn {
	return func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
		return ai.EmitMessage(ctx, &ai.AssistantMessage{
			Role:       ai.RoleAssistant,
			StopReason: ai.StopStop,
			Usage:      u,
			Content:    []*ai.Content{{Type: ai.KindText, Text: text}},
		}), nil
	}
}

func TestRunCompactionRecordsSummarizerUsage(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	appendUserText(t, sess, "older")
	appendUserText(t, sess, strings.Repeat("z", 80))
	want := ai.Usage{Input: 12, Output: 5, TotalTokens: 17, Cost: ai.UsageCost{Total: 0.42}}
	e := compactionEngine(sess, usageReply("## Goal\nDone.", want))
	msgs := session.ModelMessages(session.ContextEntries(sess))
	if _, err := e.runCompaction(context.Background(), "manual", msgs, compaction.Settings{KeepRecentTokens: 20}, false); err != nil {
		t.Fatal(err)
	}
	entry := lastCompaction(t, sess)
	if entry.Usage == nil || *entry.Usage != want {
		t.Fatalf("usage = %+v", entry.Usage)
	}
	stats := session.CollectStats(sess, nil, 0, "", nil)
	if stats.Cost != 0.42 || stats.Tokens.Input != 12 || stats.Tokens.Output != 5 || stats.Tokens.Total != 17 {
		t.Fatalf("stats cost=%v tokens=%+v", stats.Cost, stats.Tokens)
	}
	var summaries *session.CostBreakdown
	for i := range stats.CostBreakdown {
		if stats.CostBreakdown[i].Key == "Tools/summaries" {
			summaries = &stats.CostBreakdown[i]
		}
	}
	if summaries == nil || summaries.Cost != 0.42 || summaries.Tokens != 17 {
		t.Fatalf("breakdown = %+v", stats.CostBreakdown)
	}
}

func TestNavigateTreeRecordsBranchSummaryUsage(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "base"}); err != nil {
		t.Fatal(err)
	}
	stay, err := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "stay"})
	if err != nil {
		t.Fatal(err)
	}
	appendUserText(t, sess, "side")
	want := ai.Usage{Input: 15, Output: 3, TotalTokens: 18, Cost: ai.UsageCost{Total: 0.5}}
	e := compactionEngine(sess, usageReply("## Goal\nExplore.", want))
	if _, err := e.NavigateTree(context.Background(), stay.ID, session.NavigateOpts{Summarize: true}); err != nil {
		t.Fatal(err)
	}
	entry := lastBranchSummary(t, sess)
	if entry.FromHook || entry.Usage == nil || *entry.Usage != want {
		t.Fatalf("entry usage=%v fromHook=%v summary=%q", entry.Usage, entry.FromHook, entry.Summary)
	}
	stats := session.CollectStats(sess, nil, 0, "", nil)
	if stats.Cost != 0.5 || stats.Tokens.Input != 15 || stats.Tokens.Output != 3 {
		t.Fatalf("stats cost=%v tokens=%+v", stats.Cost, stats.Tokens)
	}
}

func TestNavigateTreeSkipsUsageWhenBranchHasNoContent(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "base"}); err != nil {
		t.Fatal(err)
	}
	stay, err := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "stay"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sess.AppendMessage("toolResult", map[string]any{
		"role": "toolResult", "toolCallId": "c1", "content": "only a tool result",
	}); err != nil {
		t.Fatal(err)
	}
	called := false
	e := compactionEngine(sess, func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		called = true
		return textReply("unused")(ctx, req, opts)
	})
	if _, err := e.NavigateTree(context.Background(), stay.ID, session.NavigateOpts{Summarize: true}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("summarizer ran with no model-visible messages")
	}
	entry := lastBranchSummary(t, sess)
	if entry.Summary != compaction.NoContentSummary || entry.Usage != nil {
		t.Fatalf("entry summary=%q usage=%v", entry.Summary, entry.Usage)
	}
	stats := session.CollectStats(sess, nil, 0, "", nil)
	if stats.Cost != 0 || stats.Tokens.Total != 0 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestBeforeTreeUsageIsCounted(t *testing.T) {
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "base"}); err != nil {
		t.Fatal(err)
	}
	stay, err := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "stay"})
	if err != nil {
		t.Fatal(err)
	}
	appendUserText(t, sess, "leaf")
	want := ai.Usage{Input: 3, Output: 1, TotalTokens: 4, Cost: ai.UsageCost{Total: 0.2}}
	e := compactionEngine(sess, textReply("unused"))
	e.BeforeTree = func(session.TreePrep) session.TreeHookResult {
		return session.TreeHookResult{Summary: "ext branch", Usage: &want}
	}
	if _, err := e.NavigateTree(context.Background(), stay.ID, session.NavigateOpts{Summarize: true}); err != nil {
		t.Fatal(err)
	}
	entry := lastBranchSummary(t, sess)
	if !entry.FromHook || entry.Usage == nil || *entry.Usage != want {
		t.Fatalf("entry = %+v", entry)
	}
	stats := session.CollectStats(sess, nil, 0, "", nil)
	if stats.Cost != 0.2 || stats.Tokens.Input != 3 {
		t.Fatalf("stats cost=%v tokens=%+v", stats.Cost, stats.Tokens)
	}
}

func TestExtensionCompactionUsageIsCounted(t *testing.T) {
	h := spawnRuntimeExt(t, "compact-usage", nil)
	sess := session.New(t.TempDir(), t.TempDir())
	appendUserText(t, sess, "hi")
	e := compactionEngine(sess, textReply("unused"))
	e.Hosts = []*ext.Host{h}
	msgs := session.ModelMessages(session.ContextEntries(sess))
	if _, err := e.runCompaction(context.Background(), "manual", msgs, compaction.Settings{KeepRecentTokens: 1}, false); err != nil {
		t.Fatal(err)
	}
	entry := lastCompaction(t, sess)
	if !entry.FromHook || entry.Summary != "HOOK SUMMARY" || entry.Usage == nil {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.Usage.Input != 11 || entry.Usage.Output != 4 || entry.Usage.Cost.Total != 0.3 {
		t.Fatalf("usage = %+v", entry.Usage)
	}
	stats := session.CollectStats(sess, nil, 0, "", nil)
	if stats.Cost != 0.3 || stats.Tokens.Input != 11 || stats.Tokens.Output != 4 {
		t.Fatalf("stats cost=%v tokens=%+v", stats.Cost, stats.Tokens)
	}
}

func TestExtensionBranchSummaryUsageIsCounted(t *testing.T) {
	h := spawnRuntimeExt(t, "tree-usage", nil)
	sess := session.New(t.TempDir(), t.TempDir())
	if _, err := sess.AppendMessage("user", map[string]any{"role": "user", "content": "base"}); err != nil {
		t.Fatal(err)
	}
	stay, err := sess.AppendMessage("assistant", map[string]any{"role": "assistant", "content": "stay"})
	if err != nil {
		t.Fatal(err)
	}
	appendUserText(t, sess, "leaf")
	e := compactionEngine(sess, textReply("unused"))
	e.Hosts = []*ext.Host{h}
	if _, err := e.NavigateTree(context.Background(), stay.ID, session.NavigateOpts{Summarize: true}); err != nil {
		t.Fatal(err)
	}
	entry := lastBranchSummary(t, sess)
	if !entry.FromHook || entry.Summary != "EXT BRANCH" || entry.Usage == nil {
		t.Fatalf("entry = %+v", entry)
	}
	if entry.Usage.Input != 8 || entry.Usage.Output != 2 || entry.Usage.Cost.Total != 0.4 {
		t.Fatalf("usage = %+v", entry.Usage)
	}
	stats := session.CollectStats(sess, nil, 0, "", nil)
	if stats.Cost != 0.4 || stats.Tokens.Input != 8 || stats.Tokens.Output != 2 {
		t.Fatalf("stats cost=%v tokens=%+v", stats.Cost, stats.Tokens)
	}
}
