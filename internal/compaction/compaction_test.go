package compaction

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
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
func TestSummarizeUpdatePromptIncludesPreviousSummary(t *testing.T) {
	msgs := []ai.Message{{Role: ai.RoleUser, Content: "new work"}}
	var prompt string
	var prior []ai.Message
	sf := func(ctx context.Context, req ai.Context, opts ai.Options) (*ai.EventStream, error) {
		if len(req.Messages) != 1 {
			t.Fatalf("messages = %d, want 1", len(req.Messages))
		}
		if len(req.Tools) != 0 {
			t.Fatalf("tools = %d, want 0", len(req.Tools))
		}
		if req.System != SummarizationSystemPrompt {
			t.Fatalf("system = %q", req.System)
		}
		prompt = req.Messages[0].Content
		prior = append([]ai.Message(nil), req.Messages...)
		return ai.ScriptedStreamFn("## Goal\nUpdated.", 0)(ctx, req, opts)
	}
	summary, err := Summarize(context.Background(), sf, "test", msgs, "Keep the API", "prov", "OLD GOAL", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(summary, "Goal") || !strings.Contains(summary, "Updated") {
		t.Fatalf("summary = %q", summary)
	}
	if len(prior) != 1 || !strings.Contains(prior[0].Content, "<conversation>\n[User]: new work\n</conversation>") {
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
		if len(req.Messages) != 1 {
			t.Fatalf("messages = %d, want 1", len(req.Messages))
		}
		prompt = req.Messages[0].Content
		return ai.ScriptedStreamFn("## Goal\nFirst.", 0)(ctx, req, opts)
	}
	msgs := []ai.Message{{Role: ai.RoleUser, Content: "hello"}}
	if _, err := Summarize(context.Background(), sf, "test", msgs, "", "", "", 0); err != nil {
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

	const boundary = "# Conversation\n[User]: try something\n\n[Assistant]: did it\n\n# Instructions\n"
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
	const boundary = "# Conversation\n[User]: try something\n\n# Instructions\n"
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

func TestSerializeConversation(t *testing.T) {
	msgs := []ai.Message{
		{Role: ai.RoleUser, Content: "do it"},
		{
			Role: ai.RoleAssistant,
			Assistant: &ai.AssistantMessage{Content: []*ai.Content{
				{Type: ai.KindThinking, Thinking: "plan"},
				{Type: ai.KindText, Text: "running"},
				{Type: ai.KindToolCall, ToolName: "read", Arguments: map[string]any{"path": "foo.ts"}},
				{Type: ai.KindToolCall, ToolName: "bash", Arguments: map[string]any{"command": "ls", "block": false}},
			}},
		},
		{Role: ai.RoleToolResult, Content: "file contents"},
		{Role: ai.RoleUser, Content: ""},
		{Role: ai.RoleToolResult, Content: ""},
	}
	got := serializeConversation(msgs)
	want := strings.Join([]string{
		"[User]: do it",
		"[Assistant thinking]: plan",
		"[Assistant]: running",
		`[Assistant tool calls]: read(path="foo.ts"); bash(block=false, command="ls")`,
		"[Tool result]: file contents",
	}, "\n\n")
	if got != want {
		t.Fatalf("serialized =\n%s\nwant\n%s", got, want)
	}
}

func TestSerializeConversationTruncatesToolResults(t *testing.T) {
	exact := strings.Repeat("b", toolResultMaxChars)
	got := serializeConversation([]ai.Message{{Role: ai.RoleToolResult, Content: exact}})
	if got != "[Tool result]: "+exact {
		t.Fatalf("exact result was truncated: len=%d", len(got))
	}
	if strings.Contains(got, "truncated") {
		t.Fatalf("exact result included a truncation marker: %q", got[len(got)-40:])
	}

	body := strings.Repeat("a", toolResultMaxChars+1)
	got = serializeConversation([]ai.Message{{Role: ai.RoleToolResult, Content: body}})
	prefix := "[Tool result]: " + strings.Repeat("a", toolResultMaxChars)
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("truncated result does not keep the first %d characters", toolResultMaxChars)
	}
	if !strings.HasSuffix(got, "\n\n[... 1 more characters truncated]") {
		t.Fatalf("marker = %q", got[len(prefix):])
	}
}

func TestSummarizeRequestShape(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "sum-cap", DefaultAPI: "anthropic-messages", DefaultID: "small",
		Models: []models.Model{{
			Provider: "sum-cap", ID: "small", API: "anthropic-messages", MaxTokens: 4096,
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("sum-cap") })

	var req ai.Context
	var opts ai.Options
	sf := func(ctx context.Context, gotReq ai.Context, gotOpts ai.Options) (*ai.EventStream, error) {
		req = gotReq
		opts = gotOpts
		return ai.ScriptedStreamFn("## Goal\nDone.", 0)(ctx, gotReq, gotOpts)
	}
	msgs := []ai.Message{{Role: ai.RoleUser, Content: "hello"}}
	if _, err := Summarize(context.Background(), sf, "small", msgs, "", "sum-cap", "", 16384); err != nil {
		t.Fatal(err)
	}
	if req.System != SummarizationSystemPrompt {
		t.Fatalf("system = %q", req.System)
	}
	if len(req.Messages) != 1 || len(req.Tools) != 0 {
		t.Fatalf("messages=%d tools=%d", len(req.Messages), len(req.Tools))
	}
	if !strings.Contains(req.Messages[0].Content, "<conversation>\n[User]: hello\n</conversation>") {
		t.Fatalf("user = %q", req.Messages[0].Content)
	}
	if opts.CacheRetention != "none" {
		t.Fatalf("cacheRetention = %q", opts.CacheRetention)
	}
	if opts.MaxTokens != 4096 {
		t.Fatalf("maxTokens = %d, want model cap 4096", opts.MaxTokens)
	}

	if _, err := Summarize(context.Background(), sf, "missing", msgs, "", "sum-cap", "", 16384); err != nil {
		t.Fatal(err)
	}
	if opts.MaxTokens != 13107 {
		t.Fatalf("maxTokens = %d, want floor(0.8*16384)=13107", opts.MaxTokens)
	}
}

func TestSummarizeStopLengthIsFailure(t *testing.T) {
	sf := func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
		return ai.EmitMessage(ctx, &ai.AssistantMessage{
			Role:       ai.RoleAssistant,
			StopReason: ai.StopLength,
			Content:    []*ai.Content{{Type: ai.KindText, Text: "partial summary"}},
		}), nil
	}
	text, err := Summarize(context.Background(), sf, "test", []ai.Message{{Role: ai.RoleUser, Content: "hello"}}, "", "", "", 0)
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
	if err == nil || err.Error() != "Summarization failed: generation hit the token cap and the summary is incomplete" {
		t.Fatalf("err = %v", err)
	}
}

func TestGenerateBranchSummarySerializesToolResults(t *testing.T) {
	var req ai.Context
	var opts ai.Options
	sf := func(ctx context.Context, gotReq ai.Context, gotOpts ai.Options) (*ai.EventStream, error) {
		req = gotReq
		opts = gotOpts
		return ai.ScriptedStreamFn("## Goal\nExplore.", 0)(ctx, gotReq, gotOpts)
	}
	msgs := []ai.Message{
		{Role: ai.RoleUser, Content: "run it"},
		assistantTools(toolCall("bash", "script.sh")),
		{Role: ai.RoleToolResult, Content: strings.Repeat("z", toolResultMaxChars+25)},
	}
	if _, _, err := GenerateBranchSummary(context.Background(), sf, "test", msgs, BranchSummaryOpts{ReserveTokens: 100}); err != nil {
		t.Fatal(err)
	}
	prompt := req.Messages[0].Content
	if strings.Contains(prompt, "<conversation>") {
		t.Fatalf("branch prompt restored conversation XML:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[User]: run it") || !strings.Contains(prompt, `[Assistant tool calls]: bash(path="script.sh")`) {
		t.Fatalf("prompt = %q", prompt)
	}
	if !strings.Contains(prompt, "[... 25 more characters truncated]") {
		t.Fatalf("prompt missing truncation marker:\n%s", prompt)
	}
	if !strings.HasPrefix(prompt, "# Conversation\n") || !strings.Contains(prompt, "\n\n# Instructions\n") {
		t.Fatalf("prompt boundary = %q", prompt)
	}
	if req.System != SummarizationSystemPrompt {
		t.Fatalf("system = %q", req.System)
	}
	if opts.CacheRetention != "none" || opts.MaxTokens != 80 {
		t.Fatalf("opts max=%d cache=%q", opts.MaxTokens, opts.CacheRetention)
	}
}

func TestGenerateBranchSummaryStopLengthIsFailure(t *testing.T) {
	sf := func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
		return ai.EmitMessage(ctx, &ai.AssistantMessage{
			Role:       ai.RoleAssistant,
			StopReason: ai.StopLength,
			Content:    []*ai.Content{{Type: ai.KindText, Text: "partial branch"}},
		}), nil
	}
	text, files, err := GenerateBranchSummary(context.Background(), sf, "test", []ai.Message{{Role: ai.RoleUser, Content: "x"}}, BranchSummaryOpts{})
	if text != "" || len(files.ReadFiles) != 0 || len(files.ModifiedFiles) != 0 {
		t.Fatalf("text=%q files=%+v", text, files)
	}
	if err == nil || err.Error() != "Branch summary failed: generation hit the token cap and the summary is incomplete" {
		t.Fatalf("err = %v", err)
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
