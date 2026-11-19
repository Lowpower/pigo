package bugreport

import (
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/compaction"
)

func TestSummaryUserPromptUsesSerializeConversation(t *testing.T) {
	msgs := []ai.Message{
		{Role: ai.RoleUser, Content: "do it"},
		{
			Role: ai.RoleAssistant,
			Assistant: &ai.AssistantMessage{Content: []*ai.Content{
				{Type: ai.KindThinking, Thinking: "private plan"},
				{Type: ai.KindText, Text: "running"},
				{Type: ai.KindToolCall, ToolName: "read", Arguments: map[string]any{"path": "foo.ts"}},
			}},
		},
		{Role: ai.RoleToolResult, Content: "file contents"},
	}
	got := SummaryUserPrompt(msgs, "it hung", len(msgs)+1)
	body := compaction.SerializeConversation(msgs)
	wantConv := "<conversation>\n" + body + "\n</conversation>"
	if !strings.Contains(got, wantConv) {
		t.Fatalf("prompt missing serialized conversation:\n%s", got)
	}
	if strings.Contains(got, "private plan") || strings.Contains(got, "[Assistant thinking]") {
		t.Fatalf("thinking leaked into bug report prompt:\n%s", got)
	}
	if !strings.Contains(got, `[Assistant tool calls]: read(path="foo.ts")`) {
		t.Fatalf("tool call missing from bug report prompt:\n%s", got)
	}
	if !strings.HasPrefix(got, "Note: only the last 3 of 4 messages are shown.\n\n") {
		t.Fatalf("message note changed:\n%s", got)
	}
	if !strings.Contains(got, "<user-report>\nit hung\n</user-report>") {
		t.Fatalf("user report missing:\n%s", got)
	}
	if !strings.Contains(got, summaryInstructions) {
		t.Fatalf("instructions missing:\n%s", got)
	}
}
