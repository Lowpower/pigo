package ai

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func testTool(name string) Tool {
	return Tool{Name: name, Description: name, Parameters: map[string]any{"type": "object"}}
}

func deferredContext() Context {
	return Context{
		Messages: []Message{
			{Role: RoleUser, Content: "look up alpha"},
			{Assistant: &AssistantMessage{Content: []*Content{
				{Type: KindToolCall, ToolID: "search1", ToolName: "tool_search", Arguments: map[string]any{"query": "lookup"}},
			}}},
			{Role: RoleToolResult, ToolCallID: "search1", ToolName: "tool_search", Content: "Found lookup.", AddedToolNames: []string{"lookup"}},
		},
		Tools: []Tool{testTool("tool_search"), testTool("lookup")},
	}
}

func TestSplitDeferredTools(t *testing.T) {
	ctx := deferredContext()
	added := func(names ...string) []Message {
		return []Message{{Role: RoleToolResult, ToolCallID: "c1", AddedToolNames: names}}
	}
	usedEarly := []Message{
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindToolCall, ToolID: "c1", ToolName: "lookup"},
		}}},
		{Role: RoleToolResult, ToolCallID: "c1", ToolName: "lookup", Content: "early", AddedToolNames: []string{"lookup"}},
	}
	tests := []struct {
		name     string
		msgs     []Message
		tools    []Tool
		enabled  bool
		imm, def []string
	}{
		{"places late tool", ctx.Messages, ctx.Tools, true, []string{"tool_search"}, []string{"lookup"}},
		{"disabled keeps prefix", ctx.Messages, ctx.Tools, false, []string{"tool_search", "lookup"}, nil},
		{"used before marker stays immediate", usedEarly, []Tool{testTool("base"), testTool("lookup")}, true, []string{"base", "lookup"}, nil},
		{"all deferred falls back", added("only"), []Tool{testTool("only")}, true, []string{"only"}, nil},
		{"ignores unknown names", added("ghost", "lookup"), []Tool{testTool("base"), testTool("lookup")}, true, []string{"base"}, []string{"lookup"}},
	}
	toolNames := func(ts []Tool) []string {
		var out []string
		for _, tl := range ts {
			out = append(out, tl.Name)
		}
		return out
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imm, def := splitDeferredTools(tt.msgs, tt.tools, tt.enabled)
			if !slices.Equal(toolNames(imm), tt.imm) || !slices.Equal(toolNames(def), tt.def) {
				t.Fatalf("immediate=%v deferred=%v, want %v / %v", toolNames(imm), toolNames(def), tt.imm, tt.def)
			}
		})
	}
}

func TestBuildAnthropicRequestFireworksDeferredTools(t *testing.T) {
	body, err := buildAnthropicRequest(deferredContext(), Options{Provider: "fireworks", Model: "accounts/fireworks/models/deepseek-v4-flash-0731"})
	if err != nil {
		t.Fatal(err)
	}
	req := decodeReq(t, body)
	tools, _ := req["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools = %#v", req["tools"])
	}
	first, _ := tools[0].(map[string]any)
	second, _ := tools[1].(map[string]any)
	if first["name"] != "tool_search" {
		t.Fatalf("first tool = %#v", first)
	}
	if _, ok := first["defer_loading"]; ok {
		t.Fatalf("prefix tool must not defer: %#v", first)
	}
	if second["name"] != "lookup" || second["defer_loading"] != true {
		t.Fatalf("deferred tool = %#v", second)
	}
	tr := findToolResult(t, req)
	content, _ := tr["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("tool_result content = %#v", tr["content"])
	}
	ref, _ := content[0].(map[string]any)
	if ref["type"] != "tool_reference" || ref["tool_name"] != "lookup" {
		t.Fatalf("reference = %#v", ref)
	}
	sib := findSiblingText(t, req)
	if sib != "Found lookup." {
		t.Fatalf("sibling = %q", sib)
	}
}

func TestBuildAnthropicRequestNoDeferredWithoutSupport(t *testing.T) {
	body, err := buildAnthropicRequest(deferredContext(), Options{Provider: "anthropic", Model: "claude-sonnet-4"})
	if err != nil {
		t.Fatal(err)
	}
	req := decodeReq(t, body)
	tools, _ := req["tools"].([]any)
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		if _, ok := tool["defer_loading"]; ok {
			t.Fatalf("unexpected defer_loading: %#v", tool)
		}
	}
	tr := findToolResult(t, req)
	if _, ok := tr["content"].(string); !ok {
		t.Fatalf("unsupported provider must keep string tool_result, got %#v", tr["content"])
	}
	if hasToolReference(req) {
		t.Fatal("unexpected tool_reference")
	}
}

func TestBuildAnthropicRequestCompatOptIn(t *testing.T) {
	models.RegisterProvider(models.ProviderSpec{
		ID: "ref-proxy", DefaultAPI: "anthropic-messages", DefaultID: "m1",
		Models: []models.Model{{Provider: "ref-proxy", ID: "m1", Compat: &models.Compat{SupportsToolReferences: true}}},
	})
	t.Cleanup(func() { models.UnregisterProvider("ref-proxy") })

	body, err := buildAnthropicRequest(deferredContext(), Options{Provider: "ref-proxy", Model: "m1"})
	if err != nil {
		t.Fatal(err)
	}
	req := decodeReq(t, body)
	if !hasToolReference(req) {
		t.Fatal("compat.supportsToolReferences must emit tool_reference")
	}
	tools, _ := req["tools"].([]any)
	second, _ := tools[1].(map[string]any)
	if second["defer_loading"] != true {
		t.Fatalf("compat deferred tool = %#v", second)
	}
}

func TestBuildAnthropicRequestSiblingAfterGroupedResults(t *testing.T) {
	ctx := deferredContext()
	ctx.Messages[1].Assistant.Content = append(ctx.Messages[1].Assistant.Content,
		&Content{Type: KindToolCall, ToolID: "search2", ToolName: "tool_search", Arguments: map[string]any{}})
	ctx.Messages = append(ctx.Messages, Message{Role: RoleToolResult, ToolCallID: "search2", ToolName: "tool_search", Content: "second result"})
	body, err := buildAnthropicRequest(ctx, Options{Provider: "fireworks", Model: "fw"})
	if err != nil {
		t.Fatal(err)
	}
	req := decodeReq(t, body)
	content := findUserContent(t, req)
	if len(content) != 3 {
		t.Fatalf("grouped content = %#v", content)
	}
	if content[0].(map[string]any)["type"] != "tool_result" || content[1].(map[string]any)["type"] != "tool_result" {
		t.Fatalf("want two tool_results then sibling, got %#v", content)
	}
	if content[2].(map[string]any)["type"] != "text" || content[2].(map[string]any)["text"] != "Found lookup." {
		t.Fatalf("sibling placement = %#v", content)
	}
}

func TestStreamAnthropicReaderSkipsToolReference(t *testing.T) {
	const fixture = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","model":"fw-test","usage":{"input_tokens":4,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_reference","tool_name":"lookup"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu_1","name":"lookup","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"key\":\"alpha\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":8}}

event: message_stop
data: {"type":"message_stop"}
`
	stream := StreamAnthropicReader(context.Background(), strings.NewReader(fixture), "fw-test")
	_, final := stream.Collect()
	if final == nil {
		t.Fatal("no final message")
	}
	calls := final.ToolCalls()
	if len(calls) != 1 || calls[0].ToolName != "lookup" {
		t.Fatalf("tool calls = %+v", calls)
	}
	if calls[0].Arguments["key"] != "alpha" {
		t.Fatalf("args = %#v", calls[0].Arguments)
	}
	if final.StopReason != StopToolUse {
		t.Fatalf("stop = %q", final.StopReason)
	}
}

func decodeReq(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	return req
}

func findUserContent(t *testing.T, req map[string]any) []any {
	t.Helper()
	msgs, _ := req["messages"].([]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		m, _ := msgs[i].(map[string]any)
		if m["role"] != "user" {
			continue
		}
		content, ok := m["content"].([]any)
		if !ok {
			continue
		}
		return content
	}
	t.Fatalf("no user content: %#v", req["messages"])
	return nil
}

func findToolResult(t *testing.T, req map[string]any) map[string]any {
	t.Helper()
	for _, raw := range findUserContent(t, req) {
		block, _ := raw.(map[string]any)
		if block["type"] == "tool_result" {
			return block
		}
	}
	t.Fatal("no tool_result")
	return nil
}

func findSiblingText(t *testing.T, req map[string]any) string {
	t.Helper()
	for _, raw := range findUserContent(t, req) {
		block, _ := raw.(map[string]any)
		if block["type"] == "text" {
			s, _ := block["text"].(string)
			return s
		}
	}
	return ""
}

func hasToolReference(req map[string]any) bool {
	msgs, _ := req["messages"].([]any)
	for _, raw := range msgs {
		m, _ := raw.(map[string]any)
		content, _ := m["content"].([]any)
		for _, c := range content {
			block, _ := c.(map[string]any)
			if block["type"] != "tool_result" {
				continue
			}
			inner, ok := block["content"].([]any)
			if !ok {
				continue
			}
			for _, item := range inner {
				ref, _ := item.(map[string]any)
				if ref["type"] == "tool_reference" {
					return true
				}
			}
		}
	}
	return false
}
