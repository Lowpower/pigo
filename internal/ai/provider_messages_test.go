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

func TestOpenAIWireMessagesIncludesReasoningContent(t *testing.T) {
	asst := &AssistantMessage{
		Role: RoleAssistant,
		Content: []*Content{
			{Type: KindThinking, Thinking: "secret plan"},
			{Type: KindText, Text: "hello"},
		},
	}
	wire := OpenAIWireMessages([]Message{{Role: RoleAssistant, Assistant: asst}})
	if len(wire) != 1 {
		t.Fatalf("len=%d", len(wire))
	}
	if wire[0]["content"] != "hello" {
		t.Fatalf("content = %#v, want hello", wire[0]["content"])
	}
	if wire[0]["reasoning_content"] != "secret plan" {
		t.Fatalf("reasoning_content = %#v", wire[0]["reasoning_content"])
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

func TestOpenAIWireMessagesUserTextAndImages(t *testing.T) {
	img := []ImageContent{{Type: "image", Data: "AAA", MimeType: "image/png"}}

	t.Run("image only", func(t *testing.T) {
		wire := OpenAIWireMessages([]Message{{Role: RoleUser, Images: img}})
		blocks, ok := wire[0]["content"].([]map[string]any)
		if !ok || len(blocks) != 1 || blocks[0]["type"] != "image_url" {
			t.Fatalf("content = %#v", wire[0]["content"])
		}
		for _, b := range blocks {
			if b["type"] == "text" {
				t.Fatalf("empty text part = %#v", b)
			}
		}
	})

	t.Run("text and image", func(t *testing.T) {
		wire := OpenAIWireMessages([]Message{{
			Role:    RoleUser,
			Content: "look",
			Images:  img,
		}})
		blocks, ok := wire[0]["content"].([]map[string]any)
		if !ok || len(blocks) != 2 || blocks[0]["type"] != "text" || blocks[0]["text"] != "look" || blocks[1]["type"] != "image_url" {
			t.Fatalf("content = %#v", wire[0]["content"])
		}
	})

	t.Run("text only", func(t *testing.T) {
		wire := OpenAIWireMessages([]Message{{Role: RoleUser, Content: "look"}})
		if wire[0]["content"] != "look" {
			t.Fatalf("content = %#v", wire[0]["content"])
		}
	})
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

func TestAnthropicWireMessagesRelocatesErrorToolImages(t *testing.T) {
	success := `{"content":[{"type":"text","text":"see"},{"type":"image","data":"AAA","mimeType":"image/png"}]}`
	errJSON := `{"content":[{"type":"text","text":"before"},{"type":"image","data":"BBB","mimeType":"image/png"},{"type":"image","data":"CCC","mimeType":"image/jpeg"},{"type":"text","text":"after"},{"type":"image","data":"DDD","mimeType":"image/gif"}]}`
	wire := AnthropicWireMessages([]Message{
		{Role: RoleUser, Content: "look"},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindToolCall, ToolID: "a", ToolName: "read"},
			{Type: KindToolCall, ToolID: "b", ToolName: "snap"},
			{Type: KindToolCall, ToolID: "c", ToolName: "bash"},
		}}},
		{Role: RoleToolResult, ToolCallID: "a", ToolName: "read", Content: success},
		{Role: RoleToolResult, ToolCallID: "b", ToolName: "snap", Content: errJSON, IsError: true},
		{Role: RoleToolResult, ToolCallID: "c", ToolName: "bash", Content: "nope", IsError: true},
		{Role: RoleUser, Content: "next"},
	})
	if len(wire) != 4 {
		t.Fatalf("len=%d, want user + assistant + one tool user + follow-up", len(wire))
	}
	if wire[3]["role"] != "user" || wire[3]["content"] != "next" {
		t.Fatalf("follow-up = %#v", wire[3])
	}
	content, _ := wire[2]["content"].([]map[string]any)
	if len(content) != 7 {
		t.Fatalf("tool user blocks = %d %#v", len(content), content)
	}
	okResult := content[0]
	inner, _ := okResult["content"].([]map[string]any)
	if okResult["type"] != "tool_result" || okResult["tool_use_id"] != "a" || okResult["is_error"] == true || len(inner) != 2 || inner[1]["type"] != "image" {
		t.Fatalf("success result = %#v", okResult)
	}
	src, _ := inner[1]["source"].(map[string]any)
	if src["data"] != "AAA" {
		t.Fatalf("success image = %#v", src)
	}
	bad := content[1]
	badText, _ := bad["content"].(string)
	if bad["type"] != "tool_result" || bad["tool_use_id"] != "b" || bad["is_error"] != true {
		t.Fatalf("error result = %#v", bad)
	}
	if _, isBlocks := bad["content"].([]map[string]any); isBlocks || strings.Count(badText, "(image attached after the tool results)") != 2 || !strings.Contains(badText, "before") || !strings.Contains(badText, "after") {
		t.Fatalf("error text = %#v", bad["content"])
	}
	plain := content[2]
	if plain["type"] != "tool_result" || plain["tool_use_id"] != "c" || plain["is_error"] != true || plain["content"] != "nope" {
		t.Fatalf("plain error = %#v", plain)
	}
	if content[3]["type"] != "text" || content[3]["text"] != "Images from the error result of tool call b (snap):" {
		t.Fatalf("caption = %#v", content[3])
	}
	want := []struct{ data, mime string }{{"BBB", "image/png"}, {"CCC", "image/jpeg"}, {"DDD", "image/gif"}}
	for i, img := range want {
		block := content[4+i]
		if block["type"] != "image" {
			t.Fatalf("image %d = %#v", i, block)
		}
		source, _ := block["source"].(map[string]any)
		if source["data"] != img.data || source["media_type"] != img.mime {
			t.Fatalf("image %d source = %#v", i, source)
		}
	}
}

func TestAnthropicWireMessagesRelocatesErrorImagesField(t *testing.T) {
	wire := AnthropicWireMessages([]Message{{
		Role:       RoleToolResult,
		ToolCallID: "b",
		ToolName:   "shot",
		Content:    "plain",
		IsError:    true,
		Images: []ImageContent{
			{Type: "image", Data: "EEE", MimeType: "image/png"},
			{Type: "image", Data: "FFF", MimeType: "image/webp"},
		},
	}})
	if len(wire) != 1 {
		t.Fatalf("len=%d", len(wire))
	}
	content, _ := wire[0]["content"].([]map[string]any)
	if len(content) != 4 {
		t.Fatalf("blocks = %#v", content)
	}
	text, _ := content[0]["content"].(string)
	if content[0]["is_error"] != true || strings.Count(text, "(image attached after the tool results)") != 1 || !strings.Contains(text, "plain") {
		t.Fatalf("tool result = %#v", content[0])
	}
	if content[1]["text"] != "Images from the error result of tool call b (shot):" {
		t.Fatalf("caption = %#v", content[1])
	}
	for i, data := range []string{"EEE", "FFF"} {
		source, _ := content[2+i]["source"].(map[string]any)
		if content[2+i]["type"] != "image" || source["data"] != data {
			t.Fatalf("image %d = %#v", i, content[2+i])
		}
	}
}

func TestAnthropicWireMessagesRelocatesErrorImagesAfterRefs(t *testing.T) {
	raw := `{"content":[{"type":"text","text":"boom"},{"type":"image","data":"BBB","mimeType":"image/png"}]}`
	wire := anthropicWireMessages([]Message{{
		Role:           RoleToolResult,
		ToolCallID:     "b",
		ToolName:       "snap",
		Content:        raw,
		IsError:        true,
		AddedToolNames: []string{"lookup"},
	}}, true, map[string]struct{}{"lookup": {}}, nil, "", false)
	content, _ := wire[0]["content"].([]map[string]any)
	if len(content) != 4 {
		t.Fatalf("blocks = %#v", content)
	}
	refs, _ := content[0]["content"].([]map[string]any)
	if content[0]["is_error"] != true || len(refs) != 1 || refs[0]["type"] != "tool_reference" || refs[0]["tool_name"] != "lookup" {
		t.Fatalf("tool result = %#v", content[0])
	}
	if content[1]["type"] != "text" || content[1]["text"] != "boom\n(image attached after the tool results)" {
		t.Fatalf("sibling = %#v", content[1])
	}
	if content[2]["type"] != "text" || content[2]["text"] != "Images from the error result of tool call b (snap):" {
		t.Fatalf("caption = %#v", content[2])
	}
	source, _ := content[3]["source"].(map[string]any)
	if content[3]["type"] != "image" || source["data"] != "BBB" {
		t.Fatalf("image = %#v", content[3])
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
