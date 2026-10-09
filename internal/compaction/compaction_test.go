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

func TestEstimateTokensIncludesThinkingToolArgsAndImages(t *testing.T) {
	thinking := ai.Message{
		Role: ai.RoleAssistant,
		Assistant: &ai.AssistantMessage{
			Content: []*ai.Content{
				{Type: ai.KindThinking, Thinking: "abcd"},
				{Type: ai.KindText, Text: "abcd"},
			},
		},
	}
	if got := EstimateTokens(thinking); got != 2 {
		t.Fatalf("thinking+text = %d, want 2", got)
	}

	wide := strings.Repeat("x", 4000)
	tool := ai.Message{
		Role: ai.RoleAssistant,
		Assistant: &ai.AssistantMessage{
			Content: []*ai.Content{{
				Type:      ai.KindToolCall,
				ToolName:  "write",
				Arguments: map[string]any{"content": wide},
			}},
		},
	}
	if tool.Text() != "" {
		t.Fatalf("Text() = %q, want empty", tool.Text())
	}
	// "write" + {"content":"<4000 x>"} = 5 + 4013 = 4018 chars → 1005 tokens.
	if got := EstimateTokens(tool); got != 1005 {
		t.Fatalf("tool args = %d, want 1005", got)
	}

	img := ai.Message{
		Content: "ab",
		Images:  []ai.ImageContent{{Type: "image", Data: "xx", MimeType: "image/png"}},
	}
	// 2 chars + 4800 image chars → 1201 tokens.
	if got := EstimateTokens(img); got != 1201 {
		t.Fatalf("image = %d, want 1201", got)
	}
	two := ai.Message{Images: []ai.ImageContent{{}, {}}}
	if got := EstimateTokens(two); got != 2400 {
		t.Fatalf("two images = %d, want 2400", got)
	}
}

func TestContextTokensUsesLastUsagePlusTrailing(t *testing.T) {
	msgs := []ai.Message{
		{Role: ai.RoleUser, Content: strings.Repeat("a", 400)},
		usageMessage(ai.Usage{Input: 10, Output: 5, CacheRead: 3, CacheWrite: 2, TotalTokens: 20}, "hi"),
		{Role: ai.RoleToolResult, Content: "abcdefgh"},
	}
	tools := []ai.Tool{{Name: "read", Description: "desc", Parameters: map[string]any{}}}
	if got := ContextTokens(msgs, strings.Repeat("s", 4000), tools); got != 22 {
		t.Fatalf("ContextTokens = %d, want 22 (usage 20 + trailing 2, system/tools excluded)", got)
	}

	msgs[1] = usageMessage(ai.Usage{Input: 10, Output: 5, CacheRead: 3, CacheWrite: 2}, "hi")
	if got := ContextTokens(msgs, "", nil); got != 22 {
		t.Fatalf("summed usage = %d, want 22", got)
	}

	later := append(append([]ai.Message{}, msgs...), usageMessage(ai.Usage{TotalTokens: 30}, "next"))
	if got := ContextTokens(later, "", nil); got != 30 {
		t.Fatalf("later usage = %d, want 30", got)
	}
}

func TestContextTokensNoUsageIncludesPrompt(t *testing.T) {
	msgs := []ai.Message{
		{
			Role: ai.RoleAssistant,
			Assistant: &ai.AssistantMessage{
				Content: []*ai.Content{{Type: ai.KindThinking, Thinking: "abcd"}},
			},
		},
		{
			Role:    ai.RoleUser,
			Content: "ab",
			Images:  []ai.ImageContent{{Type: "image", Data: "xx", MimeType: "image/png"}},
		},
	}
	tools := []ai.Tool{{Name: "read", Description: "desc", Parameters: map[string]any{}}}
	// thinking 1 + image message 1201 + ceil((4 + 54 tool JSON) / 4) = 1217.
	if got := ContextTokens(msgs, "abcd", tools); got != 1217 {
		t.Fatalf("ContextTokens = %d, want 1217", got)
	}
}

func TestContextTokensRejectsInvalidUsage(t *testing.T) {
	text := ai.Message{Role: ai.RoleUser, Content: "abcdefgh"}
	zero := usageMessage(ai.Usage{}, "abcdefgh")
	if got := ContextTokens([]ai.Message{text, zero}, "abcd", nil); got != 5 {
		t.Fatalf("zero usage = %d, want 5", got)
	}
	aborted := usageMessage(ai.Usage{TotalTokens: 99999}, "abcdefgh")
	aborted.Assistant.StopReason = ai.StopAborted
	if got := ContextTokens([]ai.Message{aborted}, "", nil); got != 2 {
		t.Fatalf("aborted usage = %d, want 2", got)
	}
	errored := usageMessage(ai.Usage{TotalTokens: 99999}, "abcdefgh")
	errored.Assistant.StopReason = ai.StopError
	if got := ContextTokens([]ai.Message{errored}, "", nil); got != 2 {
		t.Fatalf("error usage = %d, want 2", got)
	}

	valid := usageMessage(ai.Usage{TotalTokens: 50}, "hi")
	zeroAfter := usageMessage(ai.Usage{}, "abcdefgh")
	trail := ai.Message{Role: ai.RoleToolResult, Content: "abcdefgh"}
	if got := ContextTokens([]ai.Message{valid, trail, zeroAfter}, strings.Repeat("s", 400), nil); got != 54 {
		t.Fatalf("usage then invalid = %d, want 54", got)
	}
}

func TestShouldCompactCountsToolResultAfterUsage(t *testing.T) {
	s := Settings{ReserveTokens: 16384}
	msgs := []ai.Message{
		usageMessage(ai.Usage{TotalTokens: 1000}, "ok"),
		{Role: ai.RoleToolResult, Content: strings.Repeat("x", 40000)},
	}
	tokens := ContextTokens(msgs, "", nil)
	if !ShouldCompact(tokens, 20000, s) {
		t.Fatalf("tokens %d should compact in a 20000 window", tokens)
	}
	if ShouldCompact(ContextTokens(msgs[:1], "", nil), 20000, s) {
		t.Fatal("usage alone should stay under the threshold")
	}
}

func usageMessage(usage ai.Usage, text string) ai.Message {
	return ai.Message{
		Role:    ai.RoleAssistant,
		Content: text,
		Assistant: &ai.AssistantMessage{
			Role:       ai.RoleAssistant,
			StopReason: ai.StopStop,
			Usage:      usage,
			Content:    []*ai.Content{{Type: ai.KindText, Text: text}},
		},
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
func TestSummarizeUpdatePromptIncludesPreviousSummary(t *testing.T) {
	msgs := []ai.Message{{Role: ai.RoleUser, Content: "new work"}}
	var prompt string
	var prior []ai.Message
	sf := func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		if len(req.Messages) == 0 {
			t.Fatal("no messages")
		}
		prompt = req.Messages[len(req.Messages)-1].Content
		prior = append([]ai.Message(nil), req.Messages[:len(req.Messages)-1]...)
		return ai.ScriptedStreamFn("## Goal\nUpdated.", 0)(ctx, req, opts)
	}
	summary, err := Summarize(context.Background(), sf, "test", msgs, "Keep the API", "prov", "OLD GOAL")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "Goal") || !strings.Contains(summary, "Updated") {
		t.Fatalf("summary = %q", summary)
	}
	if len(prior) != 1 || prior[0].Content != "new work" {
		t.Fatalf("conversation = %+v", prior)
	}
	if !strings.Contains(prompt, "<previous-summary>\nOLD GOAL\n</previous-summary>") {
		t.Fatalf("prompt missing previous summary:\n%s", prompt)
	}
	if !strings.Contains(prompt, "PRESERVE all existing information from the previous summary") {
		t.Fatalf("prompt missing update instructions:\n%s", prompt)
	}
	if strings.Contains(prompt, "The messages above are a conversation to summarize.") {
		t.Fatalf("update request used the initial prompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Additional focus: Keep the API") {
		t.Fatalf("prompt missing custom instructions:\n%s", prompt)
	}
	if strings.Index(prompt, "</previous-summary>") > strings.Index(prompt, "PRESERVE all existing information") {
		t.Fatalf("previous summary should precede the update prompt:\n%s", prompt)
	}
}

func TestSummarizeWithoutPreviousUsesInitialPrompt(t *testing.T) {
	var prompt string
	sf := func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		prompt = req.Messages[len(req.Messages)-1].Content
		return ai.ScriptedStreamFn("## Goal\nFirst.", 0)(ctx, req, opts)
	}
	msgs := []ai.Message{{Role: ai.RoleUser, Content: "hello"}}
	if _, err := Summarize(context.Background(), sf, "test", msgs, "", "", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "<previous-summary>") || strings.Contains(prompt, "PRESERVE all existing information") {
		t.Fatalf("initial prompt included an update:\n%s", prompt)
	}
	if !strings.Contains(prompt, "The messages above are a conversation to summarize.") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestCollectFileLists(t *testing.T) {
	msgs := []ai.Message{assistantTools(
		toolCall("read", "a.go"),
		toolCall("read", "b.go"),
		toolCall("edit", "b.go"),
		toolCall("write", "c.go"),
		toolCall("grep", "d.go"),
		toolCall("bash", "e.go"),
		toolCall("read", ""),
		toolCall("read", 12),
	)}
	got := CollectFileLists(msgs, []string{"z.go", "c.go"}, []string{"m.go"}, true)
	if strings.Join(got.ReadFiles, ",") != "a.go,z.go" {
		t.Fatalf("readFiles = %v", got.ReadFiles)
	}
	if strings.Join(got.ModifiedFiles, ",") != "b.go,c.go,m.go" {
		t.Fatalf("modifiedFiles = %v", got.ModifiedFiles)
	}

	skipped := CollectFileLists(msgs, []string{"secret.go"}, []string{"hidden.go"}, false)
	if strings.Join(skipped.ReadFiles, ",") != "a.go" {
		t.Fatalf("readFiles without prior = %v", skipped.ReadFiles)
	}
	if strings.Join(skipped.ModifiedFiles, ",") != "b.go,c.go" {
		t.Fatalf("modifiedFiles without prior = %v", skipped.ModifiedFiles)
	}
}

func TestAppendFileSectionsOmitsEmpty(t *testing.T) {
	if got := AppendFileSections("summary", FileLists{}); got != "summary" {
		t.Fatalf("empty lists = %q", got)
	}
	got := AppendFileSections("summary", FileLists{ReadFiles: []string{"a.go"}, ModifiedFiles: []string{"b.go"}})
	want := "summary\n\n<read-files>\na.go\n</read-files>\n\n<modified-files>\nb.go\n</modified-files>"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestCompactAppendsFileSections(t *testing.T) {
	msgs := []ai.Message{
		assistantTools(toolCall("read", "only.go")),
		{Role: ai.RoleUser, Content: strings.Repeat("z", 80)},
	}
	s := Settings{KeepRecentTokens: 20, PreviousSummary: "OLD"}
	compacted, summary, err := Compact(context.Background(), ai.ScriptedStreamFn("## Goal\nNext.", 0), "test", msgs, s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "Goal") || !strings.Contains(summary, "<read-files>\nonly.go\n</read-files>") {
		t.Fatalf("summary = %q", summary)
	}
	if !strings.Contains(compacted[0].Content, "<read-files>\nonly.go\n</read-files>") {
		t.Fatalf("context summary = %q", compacted[0].Content)
	}
}

func assistantTools(calls ...*ai.Content) ai.Message {
	blocks := append([]*ai.Content{{Type: ai.KindText, Text: "did"}}, calls...)
	return ai.Message{
		Role:    ai.RoleAssistant,
		Content: "did",
		Assistant: &ai.AssistantMessage{
			Role:    ai.RoleAssistant,
			Content: blocks,
		},
	}
}

func toolCall(name string, path any) *ai.Content {
	args := map[string]any{}
	if path != nil {
		args["path"] = path
	}
	return &ai.Content{Type: ai.KindToolCall, ToolID: name, ToolName: name, Arguments: args}
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
	got, _, err := GenerateBranchSummary(context.Background(), ai.ScriptedStreamFn("## Goal\nExplore.", 0), "test", msgs, BranchSummaryOpts{})
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
	if _, _, err := GenerateBranchSummary(context.Background(), sf, "test", msgs, opts); err != nil {
		t.Fatal(err)
	}
	if prompt == "" {
		t.Fatal("summarizer was not called")
	}
	return prompt
}

func TestGenerateBranchSummaryFiles(t *testing.T) {
	msgs := []ai.Message{
		assistantTools(toolCall("read", "too-big.go")),
		{Role: ai.RoleUser, Content: "new"},
	}
	msgs[0].Content = strings.Repeat("q", 400)
	msgs[0].Assistant.Content[0].Text = msgs[0].Content
	opts := BranchSummaryOpts{
		ContextWindow: 20,
		ReserveTokens: 10,
		PriorRead:     []string{"kept.go"},
		PriorModified: []string{"old.go"},
	}
	got, files, err := GenerateBranchSummary(context.Background(), ai.ScriptedStreamFn("## Goal\nExplore.", 0), "test", msgs, opts)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(files.ReadFiles, ",") != "kept.go" {
		t.Fatalf("readFiles = %v", files.ReadFiles)
	}
	if strings.Join(files.ModifiedFiles, ",") != "old.go" {
		t.Fatalf("modifiedFiles = %v", files.ModifiedFiles)
	}
	if !strings.HasPrefix(got, BranchSummaryPreamble) || !strings.Contains(got, "<read-files>\nkept.go\n</read-files>") {
		t.Fatalf("summary = %q", got)
	}
	if strings.Contains(got, "too-big.go") {
		t.Fatalf("summary included a file from a message outside the budget: %q", got)
	}
}

func TestGenerateBranchSummaryAbort(t *testing.T) {
	sf := func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
		return ai.EmitMessage(ctx, &ai.AssistantMessage{Role: ai.RoleAssistant, StopReason: ai.StopAborted}), nil
	}
	_, _, err := GenerateBranchSummary(context.Background(), sf, "test", []ai.Message{{Role: ai.RoleUser, Content: "x"}}, BranchSummaryOpts{})
	if !errors.Is(err, ErrSummaryAborted) {
		t.Fatalf("err = %v", err)
	}
}
