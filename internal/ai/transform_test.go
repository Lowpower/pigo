package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransformOpenAIThinkingToAnthropic(t *testing.T) {
	asst := &AssistantMessage{
		Provider: "openai", API: "openai-completions", Model: "gpt-4o",
		Content: []*Content{
			{Type: KindThinking, Thinking: "plan the answer", ThinkingSignature: "reasoning_content"},
			{Type: KindText, Text: "ok"},
			{Type: KindText, Text: " "},
		},
	}
	body, err := buildAnthropicRequest(Context{Messages: []Message{{Assistant: asst}}}, Options{
		Provider: "anthropic", Model: "claude-sonnet-4",
	})
	if err != nil {
		t.Fatal(err)
	}
	blocks := assistantWireBlocks(t, body)
	for _, b := range blocks {
		if b["type"] == "thinking" || b["signature"] != nil {
			t.Fatalf("thinking replayed: %#v", blocks)
		}
		if b["type"] == "text" && strings.TrimSpace(b["text"].(string)) == "" {
			t.Fatalf("blank text replayed: %#v", blocks)
		}
	}
	if !hasTextBlock(blocks, "plan the answer") || !hasTextBlock(blocks, "ok") {
		t.Fatalf("blocks = %#v", blocks)
	}
	if asst.Content[0].Type != KindThinking || asst.Content[0].ThinkingSignature != "reasoning_content" {
		t.Fatalf("session message changed: %+v", asst.Content[0])
	}
	if strings.Contains(string(body), "reasoning_content") {
		t.Fatalf("signature leaked: %s", body)
	}
}

func TestTransformAnthropicThinkingToResponses(t *testing.T) {
	asst := &AssistantMessage{
		Provider: "anthropic", API: "anthropic-messages", Model: "claude-sonnet-4",
		Content: []*Content{
			{Type: KindThinking, Redacted: true, ThinkingSignature: "opaque-secret", Thinking: "[Reasoning redacted]"},
			{Type: KindThinking, Thinking: "plan", ThinkingSignature: "anth-sig"},
			{Type: KindToolCall, ToolID: "toolu_abc|xyz", ToolName: "read", Arguments: map[string]any{"p": "a"}},
		},
	}
	items := buildResponsesInput(Context{Messages: []Message{
		{Assistant: asst},
		{Role: RoleToolResult, ToolCallID: "toolu_abc|xyz", Content: "ok"},
	}}, replayTarget{Provider: "openai", API: "openai-responses", Model: "gpt-4o"})
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if strings.Contains(got, "opaque-secret") || strings.Contains(got, "anth-sig") || strings.Contains(got, "redacted_thinking") {
		t.Fatalf("signature replayed: %s", got)
	}
	if !strings.Contains(got, "plan") {
		t.Fatalf("thinking text missing: %s", got)
	}
	if !strings.Contains(got, "toolu_abc|fc_xyz") {
		t.Fatalf("tool id = %s", got)
	}
	if asst.Content[1].ThinkingSignature != "anth-sig" || asst.Content[2].ToolID != "toolu_abc|xyz" {
		t.Fatal("session message changed")
	}
}

func TestTransformGoogleToolSignatureToAnthropic(t *testing.T) {
	asst := &AssistantMessage{
		Provider: "google", API: "google-generative-ai", Model: "gemini-2.5-pro",
		Content: []*Content{
			{Type: KindThinking, Thinking: "hmm", ThinkingSignature: "tsig"},
			{Type: KindToolCall, ToolID: "call|abc+1", ToolName: "read", Arguments: map[string]any{"p": "a"}, ThinkingSignature: "csig"},
		},
	}
	body, err := buildAnthropicRequest(Context{Messages: []Message{
		{Assistant: asst},
		{Role: RoleToolResult, ToolCallID: "call|abc+1", Content: "ok"},
	}}, Options{Provider: "anthropic", Model: "claude-sonnet-4"})
	if err != nil {
		t.Fatal(err)
	}
	raw := string(body)
	if strings.Contains(raw, "tsig") || strings.Contains(raw, "csig") || strings.Contains(raw, `"type":"thinking"`) {
		t.Fatalf("signature replayed: %s", raw)
	}
	if !strings.Contains(raw, "hmm") {
		t.Fatalf("thinking text missing: %s", raw)
	}
	if !strings.Contains(raw, "call_abc_1") {
		t.Fatalf("tool id not rewritten: %s", raw)
	}
	if strings.Contains(raw, "call|abc+1") {
		t.Fatalf("original tool id replayed: %s", raw)
	}
	if asst.Content[1].ToolID != "call|abc+1" || asst.Content[1].ThinkingSignature != "csig" {
		t.Fatal("session message changed")
	}
}

func TestTransformSameModelKeepsSignedThinking(t *testing.T) {
	body, err := buildAnthropicRequest(Context{Messages: []Message{{
		Assistant: &AssistantMessage{
			Provider: "anthropic", API: "anthropic-messages", Model: "claude-sonnet-4",
			Content: []*Content{
				{Type: KindThinking, Thinking: "", ThinkingSignature: "sig-keep"},
				{Type: KindText, Text: "ok"},
			},
		},
	}}}, Options{Provider: "anthropic", Model: "claude-sonnet-4"})
	if err != nil {
		t.Fatal(err)
	}
	blocks := assistantWireBlocks(t, body)
	var thinking map[string]any
	for _, b := range blocks {
		if b["type"] == "thinking" {
			thinking = b
		}
	}
	if thinking == nil || thinking["signature"] != "sig-keep" {
		t.Fatalf("blocks = %#v", blocks)
	}
}

func TestAnthropicDropsBlankAssistantText(t *testing.T) {
	body, err := buildAnthropicRequest(Context{Messages: []Message{
		{Role: RoleUser, Content: "hi"},
		{Assistant: &AssistantMessage{Content: []*Content{{Type: KindText, Text: " "}}}},
	}}, Options{Provider: "anthropic", Model: "claude-sonnet-4"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), `"text":""`) || strings.Contains(string(body), `"text":" "`) {
		t.Fatalf("blank text replayed: %s", body)
	}
	blocks := assistantWireBlocks(t, body)
	if len(blocks) != 0 {
		t.Fatalf("blocks = %#v", blocks)
	}
}

func TestAnthropicUnsignedThinkingBecomesText(t *testing.T) {
	body, err := buildAnthropicRequest(Context{Messages: []Message{{
		Assistant: &AssistantMessage{
			Provider: "anthropic", API: "anthropic-messages", Model: "claude-sonnet-4",
			Content: []*Content{
				{Type: KindThinking, Thinking: "plan"},
				{Type: KindText, Text: "ok"},
			},
		},
	}}}, Options{Provider: "anthropic", Model: "claude-sonnet-4"})
	if err != nil {
		t.Fatal(err)
	}
	blocks := assistantWireBlocks(t, body)
	for _, b := range blocks {
		if b["type"] == "thinking" {
			t.Fatalf("unsigned thinking replayed: %#v", blocks)
		}
	}
	if !hasTextBlock(blocks, "plan") || !hasTextBlock(blocks, "ok") {
		t.Fatalf("blocks = %#v", blocks)
	}
}

func TestAnthropicRecordsActualProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicFixture))
	}))
	t.Cleanup(srv.Close)
	client := &AnthropicClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Provider: "fake", Model: "m1"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Provider != "fake" {
		t.Fatalf("provider = %+v", final)
	}
}

func assistantWireBlocks(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	msgs, _ := req["messages"].([]any)
	var blocks []map[string]any
	for _, raw := range msgs {
		msg, _ := raw.(map[string]any)
		if msg["role"] != "assistant" {
			continue
		}
		content, _ := msg["content"].([]any)
		for _, c := range content {
			block, _ := c.(map[string]any)
			blocks = append(blocks, block)
		}
	}
	return blocks
}

func hasTextBlock(blocks []map[string]any, text string) bool {
	for _, b := range blocks {
		if b["type"] == "text" && b["text"] == text {
			return true
		}
	}
	return false
}
