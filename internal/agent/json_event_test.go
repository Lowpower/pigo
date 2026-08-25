package agent

import (
	"encoding/json"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
)

func TestToJSONMessageUpdateStripsPartialAndCumulativeMessage(t *testing.T) {
	msg := &ai.AssistantMessage{
		Role:       ai.RoleAssistant,
		StopReason: ai.StopStop,
		Usage:      ai.Usage{Input: 3, Output: 5, TotalTokens: 8},
		Content:    []*ai.Content{{Type: ai.KindText, Text: "hi"}},
	}
	aiEv := &ai.Event{
		Type:         ai.EventTextDelta,
		ContentIndex: 0,
		Delta:        "hi",
		Partial:      msg,
	}
	got, err := ToJSON(Event{Type: EventMessageUpdate, Assistant: msg, AIEvent: aiEv})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["type"] != "message_update" {
		t.Fatalf("type = %v", payload["type"])
	}
	if _, ok := payload["message"]; ok {
		t.Fatalf("message_update must not include cumulative message: %s", raw)
	}
	if _, ok := payload["text"]; ok {
		t.Fatalf("message_update must not include shortcut text: %s", raw)
	}
	usage, _ := payload["usage"].(map[string]any)
	if usage["input"] != float64(3) || usage["output"] != float64(5) {
		t.Fatalf("usage = %v", payload["usage"])
	}
	ame, _ := payload["assistantMessageEvent"].(map[string]any)
	if ame["type"] != "text_delta" || ame["delta"] != "hi" || ame["contentIndex"] != float64(0) {
		t.Fatalf("assistantMessageEvent = %v", ame)
	}
	if _, ok := ame["partial"]; ok {
		t.Fatalf("partial must be stripped: %s", raw)
	}
}

func TestToJSONToolCallStartIncludesIDAndName(t *testing.T) {
	msg := &ai.AssistantMessage{
		Role: ai.RoleAssistant,
		Content: []*ai.Content{{
			Type:     ai.KindToolCall,
			ToolID:   "call_1",
			ToolName: "bash",
			Arguments: map[string]any{"command": "ls"},
		}},
	}
	aiEv := &ai.Event{Type: ai.EventToolCallStart, ContentIndex: 0, Partial: msg}
	got, err := ToJSON(Event{Type: EventMessageUpdate, Assistant: msg, AIEvent: aiEv})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	ame, _ := payload["assistantMessageEvent"].(map[string]any)
	if ame["type"] != "toolcall_start" || ame["id"] != "call_1" || ame["toolName"] != "bash" {
		t.Fatalf("toolcall_start = %v", ame)
	}
	if _, ok := ame["partial"]; ok {
		t.Fatalf("partial leaked: %s", raw)
	}
}

func TestToJSONAgentEndIncludesWillRetry(t *testing.T) {
	got, err := ToJSON(Event{
		Type: EventAgentEnd,
		Messages: []Msg{
			{Role: RoleUser, Text: "hi"},
			{Role: RoleAssistant, Assistant: &ai.AssistantMessage{
				Role:       ai.RoleAssistant,
				StopReason: ai.StopStop,
				Content:    []*ai.Content{{Type: ai.KindText, Text: "yo"}},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	if payload["type"] != "agent_end" {
		t.Fatalf("type = %v", payload["type"])
	}
	if payload["willRetry"] != false {
		t.Fatalf("willRetry = %v, want false", payload["willRetry"])
	}
	msgs, _ := payload["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %v", payload["messages"])
	}
	u, _ := msgs[0].(map[string]any)
	if u["role"] != "user" || u["content"] != "hi" {
		t.Fatalf("user message = %v", u)
	}
}
