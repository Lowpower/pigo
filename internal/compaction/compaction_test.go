package compaction

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
)

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens(ai.Message{Content: "abcdefgh"}); got != 2 { // ceil(8/4)
		t.Errorf("EstimateTokens(8 chars) = %d, want 2", got)
	}
	if got := EstimateTokens(ai.Message{Content: "abcde"}); got != 2 { // ceil(5/4)
		t.Errorf("EstimateTokens(5 chars) = %d, want 2", got)
	}
	if got := EstimateTokens(ai.Message{Content: ""}); got != 0 {
		t.Errorf("EstimateTokens(empty) = %d, want 0", got)
	}
}

func TestShouldCompact(t *testing.T) {
	s := DefaultSettings() // reserve 16384
	if !ShouldCompact(90000, 100000, s) {
		t.Error("90000 tokens in a 100000 window should trigger compaction")
	}
	if ShouldCompact(50000, 100000, s) {
		t.Error("50000 tokens in a 100000 window should not trigger compaction")
	}
}

func TestFindCutIndex(t *testing.T) {
	// 30 messages of 4000 chars each = 1000 tokens each.
	msgs := makeMessages(30, 4000)
	// keepRecent 20000 -> the last 20 messages reach the threshold, cut at index 10.
	if got := FindCutIndex(msgs, 20000); got != 10 {
		t.Errorf("FindCutIndex = %d, want 10", got)
	}
	// Whole conversation fits within keepRecent -> nothing to summarize.
	if got := FindCutIndex(msgs, 1_000_000); got != 0 {
		t.Errorf("FindCutIndex (large keep) = %d, want 0", got)
	}
}

func TestCompactReplacesOldWithSummary(t *testing.T) {
	msgs := makeMessages(30, 4000) // 30000 tokens total
	summarizer := ai.ScriptedStreamFn("## Goal\nFinish pigo. Done.", 0)

	before := EstimateContextTokens(msgs)
	compacted, summary, err := Compact(context.Background(), summarizer, "test", msgs, DefaultSettings())
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}

	if summary == "" || !strings.Contains(summary, "Goal") {
		t.Errorf("summary = %q, want the scripted summary", summary)
	}
	// summary message + last 20 kept = 21 messages.
	if len(compacted) != 21 {
		t.Fatalf("compacted len = %d, want 21", len(compacted))
	}
	if !strings.HasPrefix(compacted[0].Content, SummaryMarker) || !strings.Contains(compacted[0].Content, "Goal") {
		t.Errorf("compacted[0] = %q, want summary marker + summary", compacted[0].Content)
	}
	after := EstimateContextTokens(compacted)
	if after >= before {
		t.Errorf("compacted context (%d) not smaller than original (%d)", after, before)
	}
}

func TestCompactPassesCustomInstructions(t *testing.T) {
	msgs := makeMessages(30, 4000)
	var prompt string
	sf := func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		if len(req.Messages) > 0 {
			prompt = req.Messages[len(req.Messages)-1].Content
		}
		return ai.ScriptedStreamFn("## Goal\nFocus.", 0)(ctx, req, opts)
	}
	s := DefaultSettings()
	s.CustomInstructions = "Focus on code changes"
	if _, _, err := Compact(context.Background(), sf, "test", msgs, s); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Additional focus: Focus on code changes") {
		t.Fatalf("summarization prompt missing custom instructions:\n%s", prompt)
	}
}
func TestCompactNoopWhenSmall(t *testing.T) {
	msgs := makeMessages(3, 100)
	compacted, summary, err := Compact(context.Background(), ai.ScriptedStreamFn("x", 0), "test", msgs, DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	if summary != "" || len(compacted) != len(msgs) {
		t.Errorf("small conversation should be unchanged; summary=%q len=%d", summary, len(compacted))
	}
}

func makeMessages(n, chars int) []ai.Message {
	msgs := make([]ai.Message, n)
	for i := range msgs {
		role := ai.RoleUser
		if i%2 == 1 {
			role = ai.RoleAssistant
		}
		msgs[i] = ai.Message{Role: role, Content: strings.Repeat("x", chars)}
	}
	return msgs
}

func TestGenerateBranchSummaryPrependsPreamble(t *testing.T) {
	msgs := []ai.Message{
		{Role: ai.RoleUser, Content: "try something"},
		{Role: ai.RoleAssistant, Content: "did it"},
	}
	got, err := GenerateBranchSummary(context.Background(), ai.ScriptedStreamFn("## Goal\nExplore.", 0), "test", msgs, BranchSummaryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, BranchSummaryPreamble) || !strings.Contains(got, "Goal") {
		t.Fatalf("summary = %q", got)
	}
}

func TestGenerateBranchSummaryPromptBoundary(t *testing.T) {
	msgs := []ai.Message{
		{Role: ai.RoleUser, Content: "try something"},
		{Role: ai.RoleAssistant, Content: "did it"},
	}
	prompt := captureBranchSummaryPrompt(t, msgs, BranchSummaryOpts{})

	const boundary = "# Conversation\nuser: try something\nassistant: did it\n\n# Instructions\n"
	if !strings.HasPrefix(prompt, boundary) {
		t.Fatalf("prompt = %q, want prefix %q", prompt, boundary)
	}
	if strings.Contains(prompt, "<conversation>") || strings.Contains(prompt, "</conversation>") {
		t.Fatalf("prompt still wraps the conversation in XML:\n%s", prompt)
	}
	instr := strings.TrimPrefix(prompt, boundary)
	for _, sentence := range []string{
		"The messages above are earlier context from an ongoing conversation. Later messages are stored separately and do not need to be reconstructed.",
		"Only summarize information explicitly present above. Do not infer or recreate later messages.",
		"## Goal",
	} {
		if !strings.Contains(instr, sentence) {
			t.Fatalf("instructions missing %q:\n%s", sentence, instr)
		}
	}
	if strings.Index(instr, "Later messages are stored separately") > strings.Index(instr, "## Goal") {
		t.Fatalf("stored-separately wording should introduce the instructions:\n%s", instr)
	}
	if strings.Index(instr, "## Next Steps") > strings.Index(instr, "Do not infer or recreate later messages.") {
		t.Fatalf("do-not-recreate wording should close the instructions:\n%s", instr)
	}
}

func TestGenerateBranchSummaryCustomInstructions(t *testing.T) {
	msgs := []ai.Message{{Role: ai.RoleUser, Content: "try something"}}
	prompt := captureBranchSummaryPrompt(t, msgs, BranchSummaryOpts{CustomInstructions: "Focus on file paths"})
	if !strings.Contains(prompt, "# Instructions\n") || !strings.Contains(prompt, "## Goal") {
		t.Fatalf("custom focus should keep the default instructions:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Additional focus: Focus on file paths") {
		t.Fatalf("prompt missing custom instructions:\n%s", prompt)
	}
	if strings.Contains(prompt, "<conversation>") {
		t.Fatalf("prompt still wraps the conversation in XML:\n%s", prompt)
	}
}

func TestGenerateBranchSummaryReplaceInstructions(t *testing.T) {
	msgs := []ai.Message{{Role: ai.RoleUser, Content: "try something"}}
	prompt := captureBranchSummaryPrompt(t, msgs, BranchSummaryOpts{
		CustomInstructions:  "Just the decisions.",
		ReplaceInstructions: true,
	})
	const boundary = "# Conversation\nuser: try something\n\n# Instructions\n"
	if !strings.HasPrefix(prompt, boundary) {
		t.Fatalf("prompt = %q, want prefix %q", prompt, boundary)
	}
	instr := strings.TrimPrefix(prompt, boundary)
	if instr != "Just the decisions." {
		t.Fatalf("instructions = %q, want custom text only", instr)
	}
	if strings.Contains(prompt, "## Goal") || strings.Contains(prompt, "<conversation>") {
		t.Fatalf("replaced prompt kept default sections or XML:\n%s", prompt)
	}
}

func captureBranchSummaryPrompt(t *testing.T, msgs []ai.Message, opts BranchSummaryOpts) string {
	t.Helper()
	var prompt string
	sf := func(ctx context.Context, req ai.Context, streamOpts ai.Options) (*ai.EventStream, error) {
		if len(req.Messages) != 1 {
			t.Fatalf("messages = %d, want 1", len(req.Messages))
		}
		prompt = req.Messages[0].Content
		return ai.ScriptedStreamFn("## Goal\nExplore.", 0)(ctx, req, streamOpts)
	}
	if _, err := GenerateBranchSummary(context.Background(), sf, "test", msgs, opts); err != nil {
		t.Fatal(err)
	}
	if prompt == "" {
		t.Fatal("summarizer was not called")
	}
	return prompt
}

func TestGenerateBranchSummaryAbort(t *testing.T) {
	sf := func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
		return ai.EmitMessage(ctx, &ai.AssistantMessage{Role: ai.RoleAssistant, StopReason: ai.StopAborted}), nil
	}
	_, err := GenerateBranchSummary(context.Background(), sf, "test", []ai.Message{{Role: ai.RoleUser, Content: "x"}}, BranchSummaryOpts{})
	if !errors.Is(err, ErrSummaryAborted) {
		t.Fatalf("err = %v", err)
	}
}
