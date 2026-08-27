package ai

import (
	"strings"
	"testing"
)

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

func TestAnthropicWireMessagesIncludesUserImages(t *testing.T) {
	wire := AnthropicWireMessages([]Message{{
		Role:    RoleUser,
		Content: "look",
		Images:  []ImageContent{{Type: "image", Data: "AAA", MimeType: "image/png"}},
	}})
	if len(wire) != 1 {
		t.Fatalf("len=%d", len(wire))
	}
	blocks, _ := wire[0]["content"].([]map[string]any)
	if len(blocks) != 2 || blocks[0]["type"] != "text" || blocks[1]["type"] != "image" {
		t.Fatalf("content = %#v", wire[0]["content"])
	}
	src, _ := blocks[1]["source"].(map[string]any)
	if src["type"] != "base64" || src["media_type"] != "image/png" || src["data"] != "AAA" {
		t.Fatalf("source = %#v", src)
	}
}

func TestOpenAIWireMessagesIncludesUserImages(t *testing.T) {
	wire := OpenAIWireMessages([]Message{{
		Role:    RoleUser,
		Content: "look",
		Images:  []ImageContent{{Type: "image", Data: "AAA", MimeType: "image/png"}},
	}})
	blocks, _ := wire[0]["content"].([]map[string]any)
	if len(blocks) != 2 || blocks[0]["type"] != "text" || blocks[1]["type"] != "image_url" {
		t.Fatalf("content = %#v", wire[0]["content"])
	}
	url, _ := blocks[1]["image_url"].(map[string]any)
	if url["url"] != "data:image/png;base64,AAA" {
		t.Fatalf("url = %#v", url)
	}
}

func TestAnthropicWireMessagesIncludesToolResultImages(t *testing.T) {
	raw := `{"content":[{"type":"text","text":"Read image file [image/png]"},{"type":"image","data":"AAA","mimeType":"image/png"}]}`
	wire := AnthropicWireMessages([]Message{{
		Role:       RoleToolResult,
		ToolCallID: "tu_1",
		ToolName:   "read",
		Content:    raw,
	}})
	trBlocks, _ := wire[0]["content"].([]map[string]any)
	if len(trBlocks) != 1 {
		t.Fatalf("tool result wrapper = %#v", wire[0]["content"])
	}
	inner, _ := trBlocks[0]["content"].([]map[string]any)
	if len(inner) != 2 || inner[0]["type"] != "text" || inner[1]["type"] != "image" {
		t.Fatalf("tool_result content = %#v", trBlocks[0]["content"])
	}
	src, _ := inner[1]["source"].(map[string]any)
	if src["data"] != "AAA" || src["media_type"] != "image/png" {
		t.Fatalf("image source = %#v", src)
	}
}

func TestBlockImagesStripsToolAndUserImages(t *testing.T) {
	raw := `{"content":[{"type":"text","text":"Read image file [image/png]"},{"type":"image","data":"AAA","mimeType":"image/png"}]}`
	got := BlockImages([]Message{
		{Role: RoleUser, Content: "look", Images: []ImageContent{{Type: "image", Data: "AAA", MimeType: "image/png"}}},
		{Role: RoleToolResult, ToolCallID: "tu_1", Content: raw},
	})
	if len(got[0].Images) != 0 {
		t.Fatalf("user images = %#v", got[0].Images)
	}
	if strings.Contains(got[1].Content, `"type":"image"`) {
		t.Fatalf("tool content still has image JSON: %q", got[1].Content)
	}
	if !strings.Contains(got[1].Content, "Read image file") || !strings.Contains(got[1].Content, "blockImages") {
		t.Fatalf("blocked tool content = %q", got[1].Content)
	}
	wire := AnthropicWireMessages(got[1:])
	trBlocks, _ := wire[0]["content"].([]map[string]any)
	if _, isSlice := trBlocks[0]["content"].([]map[string]any); isSlice {
		t.Fatalf("blocked tool_result should be text, got %#v", trBlocks[0]["content"])
	}
}
