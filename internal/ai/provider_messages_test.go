package ai

import "testing"

func TestAnthropicWireMessagesPreservesToolPairing(t *testing.T) {
	asst := &AssistantMessage{
		Role:       RoleAssistant,
		StopReason: StopToolUse,
		Content: []*Content{
			{Type: KindText, Text: "I'll read it."},
			{Type: KindToolCall, ToolID: "tu_1", ToolName: "read", Arguments: map[string]any{"path": "README.md"}},
		},
	}
	msgs := []Message{
		{Role: RoleUser, Content: "read the readme"},
		{Role: RoleAssistant, Assistant: asst, Content: asst.Text()},
		{Role: RoleToolResult, ToolCallID: "tu_1", ToolName: "read", Content: "file body"},
	}
	wire := AnthropicWireMessages(msgs)
	if len(wire) != 3 {
		t.Fatalf("len=%d, want 3", len(wire))
	}
	asstMsg := wire[1]
	if asstMsg["role"] != "assistant" {
		t.Fatalf("assistant role = %v", asstMsg["role"])
	}
	blocks, _ := asstMsg["content"].([]map[string]any)
	if len(blocks) != 2 || blocks[1]["type"] != "tool_use" || blocks[1]["id"] != "tu_1" {
		t.Fatalf("assistant content = %#v", asstMsg["content"])
	}
	tr := wire[2]
	if tr["role"] != "user" {
		t.Fatalf("tool result role = %v, want user (Anthropic convention)", tr["role"])
	}
	trBlocks, _ := tr["content"].([]map[string]any)
	if len(trBlocks) != 1 || trBlocks[0]["type"] != "tool_result" || trBlocks[0]["tool_use_id"] != "tu_1" {
		t.Fatalf("tool result = %#v", tr["content"])
	}
}

func TestOpenAIWireMessagesPreservesToolPairing(t *testing.T) {
	asst := &AssistantMessage{
		Role: RoleAssistant,
		Content: []*Content{
			{Type: KindToolCall, ToolID: "call_1", ToolName: "bash", Arguments: map[string]any{"command": "pwd"}},
		},
	}
	wire := OpenAIWireMessages([]Message{
		{Role: RoleAssistant, Assistant: asst},
		{Role: RoleToolResult, ToolCallID: "call_1", ToolName: "bash", Content: "/tmp"},
	})
	if len(wire) != 2 {
		t.Fatalf("len=%d", len(wire))
	}
	calls, _ := wire[0]["tool_calls"].([]map[string]any)
	if len(calls) != 1 || calls[0]["id"] != "call_1" {
		t.Fatalf("tool_calls = %#v", wire[0]["tool_calls"])
	}
	if wire[1]["role"] != "tool" || wire[1]["tool_call_id"] != "call_1" {
		t.Fatalf("tool msg = %#v", wire[1])
	}
}
