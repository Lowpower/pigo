package ai

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const mergedReasoningDetails = `[
  {"type":"reasoning.text","text":"The user wants the time.","index":0,"signature":"sha256:first","format":"openai-responses-v1","source":"openrouter"},
  {"type":"reasoning.summary","summary":"Looked up time.","index":0,"format":"openai-responses-v1"},
  {"type":"reasoning.encrypted","id":"enc-1","data":"cipher-secret","format":"openai-responses-v1","index":1},
  {"type":"reasoning.summary","summary":"After encrypted block.","format":"openai-responses-v1","index":0}
]`

const openAIReasoningDetailsFixture = `data: {"choices":[{"delta":{"reasoning_content":"plan ","reasoning_details":[{"type":"reasoning.text","text":"The","index":0,"signature":"sha256:first","source":"openrouter"}]}}]}

data: {"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.text","text":" user wants the time.","signature":"sha256:text-signature","format":"openai-responses-v1","index":0}]}}]}

data: {"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.summary","summary":"Looked","index":0}]}}]}

data: {"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.summary","summary":" up time.","format":"openai-responses-v1","index":0}]}}]}

data: {"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.encrypted","id":"enc-1","data":"cipher-secret","format":"openai-responses-v1","index":1}],"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_time","arguments":"{}"}}]}}]}

data: {"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.summary","summary":"After encrypted block.","format":"openai-responses-v1","index":0}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]
`

func TestStreamOpenAIReasoningDetailsMerge(t *testing.T) {
	_, final := StreamOpenAIReader(context.Background(), strings.NewReader(openAIReasoningDetailsFixture), "gpt-test").Collect()
	if final == nil {
		t.Fatal("no final message")
	}
	if len(final.Content) < 2 || final.Content[0].Type != KindThinking || final.Content[1].Type != KindToolCall {
		t.Fatalf("content = %#v", final.Content)
	}
	if final.Content[0].Thinking != "plan " {
		t.Fatalf("thinking = %q", final.Content[0].Thinking)
	}
	if final.Content[1].ToolID != "call_1" || final.Content[1].ToolName != "get_time" {
		t.Fatalf("tool call = %#v", final.Content[1])
	}
	assertReasoningDetails(t, final.Content[0].ThinkingSignature, mergedReasoningDetails)
}

const openAIEncryptedBeforeToolFixture = `data: {"choices":[{"delta":{"reasoning_details":[{"type":"reasoning.encrypted","id":"enc-early","data":"cipher-secret","index":0}],"tool_calls":[{"index":0,"id":"call_early","function":{"name":"read","arguments":"{}"}}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]
`

func TestStreamOpenAIEncryptedReasoningDetailsBeforeToolCall(t *testing.T) {
	_, final := StreamOpenAIReader(context.Background(), strings.NewReader(openAIEncryptedBeforeToolFixture), "gpt-test").Collect()
	if final == nil {
		t.Fatal("no final message")
	}
	if len(final.Content) != 2 || final.Content[0].Type != KindThinking || final.Content[1].Type != KindToolCall {
		t.Fatalf("content = %#v", final.Content)
	}
	if final.Content[0].Thinking != "" {
		t.Fatalf("thinking = %q", final.Content[0].Thinking)
	}
	if final.Content[1].ToolID != "call_early" || final.Content[1].ToolName != "read" {
		t.Fatalf("tool call = %#v", final.Content[1])
	}
	assertReasoningDetails(t, final.Content[0].ThinkingSignature, `[{"type":"reasoning.encrypted","id":"enc-early","data":"cipher-secret","index":0}]`)
}

func TestBuildOpenAIRequestReplaysReasoningDetails(t *testing.T) {
	asst := &AssistantMessage{
		Provider: "openai", API: "openai-completions", Model: "gpt-test",
		Content: []*Content{
			{Type: KindThinking, Thinking: "plan ", ThinkingSignature: mergedReasoningDetails},
			{Type: KindText, Text: "ok"},
			{Type: KindToolCall, ToolID: "call_1", ToolName: "get_time", Arguments: map[string]any{}},
		},
	}
	body := openAICompatBody(t, Context{Messages: []Message{{Assistant: asst}}}, Options{Provider: "openai", Model: "gpt-test"}, "")
	msg := messageAt(t, body, 0)
	if msg["reasoning_content"] != "plan " {
		t.Fatalf("reasoning_content = %#v", msg["reasoning_content"])
	}
	assertReasoningDetails(t, msg["reasoning_details"], mergedReasoningDetails)
	calls, _ := msg["tool_calls"].([]any)
	if len(calls) != 1 {
		t.Fatalf("tool_calls = %#v", msg["tool_calls"])
	}
}

func TestBuildOpenAIRequestLegacySignatureOmitsReasoningDetails(t *testing.T) {
	asst := &AssistantMessage{
		Provider: "openai", API: "openai-completions", Model: "gpt-test",
		Content: []*Content{
			{Type: KindThinking, Thinking: "secret plan", ThinkingSignature: "reasoning_content"},
			{Type: KindText, Text: "hello"},
		},
	}
	body := openAICompatBody(t, Context{Messages: []Message{{Assistant: asst}}}, Options{Provider: "openai", Model: "gpt-test"}, "")
	msg := messageAt(t, body, 0)
	if msg["reasoning_content"] != "secret plan" {
		t.Fatalf("reasoning_content = %#v", msg["reasoning_content"])
	}
	if _, ok := msg["reasoning_details"]; ok {
		t.Fatalf("legacy signature emitted reasoning_details: %#v", msg["reasoning_details"])
	}
}

func TestBuildOpenAIRequestCrossModelDropsReasoningDetails(t *testing.T) {
	visible := &AssistantMessage{
		Provider: "openai", API: "openai-completions", Model: "gpt-test",
		Content: []*Content{
			{Type: KindThinking, Thinking: "visible plan", ThinkingSignature: mergedReasoningDetails},
			{Type: KindText, Text: "ok"},
		},
	}
	body := openAICompatBody(t, Context{Messages: []Message{{Assistant: visible}}}, Options{Provider: "openai", Model: "gpt-other"}, "")
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "cipher-secret") || strings.Contains(string(raw), "reasoning_details") {
		t.Fatalf("cross-model kept details: %s", raw)
	}
	if messageAt(t, body, 0)["content"] != "visible planok" {
		t.Fatalf("downgraded content = %#v", messageAt(t, body, 0)["content"])
	}

	encrypted := &AssistantMessage{
		Provider: "openai", API: "openai-completions", Model: "gpt-test",
		Content: []*Content{
			{Type: KindThinking, ThinkingSignature: `[{"type":"reasoning.encrypted","data":"cipher-secret"}]`},
			{Type: KindText, Text: "ok"},
		},
	}
	anth, err := buildAnthropicRequest(Context{Messages: []Message{{Assistant: encrypted}}}, Options{
		Provider: "anthropic", Model: "claude-sonnet-4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(anth), "cipher-secret") {
		t.Fatalf("cross-provider kept details: %s", anth)
	}
}

func TestOpenAIAssistantThinkingAsTextKeepsReasoningDetails(t *testing.T) {
	msg := &AssistantMessage{Content: []*Content{
		{Type: KindThinking, Thinking: "plan", ThinkingSignature: mergedReasoningDetails},
		{Type: KindText, Text: "hello"},
	}}
	out := openaiAssistant(msg, openAIWireOpts{thinkingAsText: true})
	if _, ok := out["reasoning_content"]; ok {
		t.Fatalf("thinking as text kept reasoning_content: %#v", out["reasoning_content"])
	}
	parts, _ := out["content"].([]map[string]any)
	if len(parts) != 2 || parts[0]["text"] != "plan" || parts[1]["text"] != "hello" {
		t.Fatalf("content = %#v", out["content"])
	}
	assertReasoningDetails(t, out["reasoning_details"], mergedReasoningDetails)
}

func assertReasoningDetails(t *testing.T, got any, wantJSON string) {
	t.Helper()
	raw, err := reasoningDetailsJSON(got)
	if err != nil {
		t.Fatal(err)
	}
	var gotNorm, wantNorm []map[string]any
	if err := json.Unmarshal(raw, &gotNorm); err != nil {
		t.Fatalf("details: %v\n%s", err, raw)
	}
	if err := json.Unmarshal([]byte(wantJSON), &wantNorm); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotNorm, wantNorm) {
		t.Fatalf("details = %s\nwant %s", raw, wantJSON)
	}
}

func reasoningDetailsJSON(got any) ([]byte, error) {
	if s, ok := got.(string); ok {
		return []byte(s), nil
	}
	return json.Marshal(got)
}
