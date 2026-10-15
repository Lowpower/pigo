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

func registerCompat(t *testing.T, id, api string, compat *models.Compat) {
	t.Helper()
	models.RegisterProvider(models.ProviderSpec{
		ID: id, DefaultAPI: api, DefaultID: "m",
		Models: []models.Model{{Provider: id, ID: "m", API: api, Compat: compat}},
	})
	t.Cleanup(func() { models.UnregisterProvider(id) })
}

func twoStepDeferredContext() (short, full Context) {
	base := []Message{
		{Role: RoleUser, Content: "look up alpha"},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindToolCall, ToolID: "search1", ToolName: "tool_search", Arguments: map[string]any{"query": "lookup"}},
		}}},
		{Role: RoleToolResult, ToolCallID: "search1", ToolName: "tool_search", Content: "Found lookup.", AddedToolNames: []string{"lookup"}},
	}
	short = Context{
		Messages: append([]Message(nil), base...),
		Tools:    []Tool{testTool("tool_search"), testTool("lookup")},
	}
	full = Context{Messages: append(append([]Message(nil), base...),
		Message{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindToolCall, ToolID: "search2", ToolName: "tool_search", Arguments: map[string]any{"query": "extra"}},
		}}},
		Message{Role: RoleToolResult, ToolCallID: "search2", ToolName: "tool_search", Content: "Found extra.", AddedToolNames: []string{"extra"}},
	), Tools: []Tool{testTool("tool_search"), testTool("lookup"), testTool("extra")}}
	return short, full
}

func responsesItemMaps(t *testing.T, ctx Context, anchor deferredAnchor) []map[string]any {
	t.Helper()
	raw, err := json.Marshal(buildResponsesInput(ctx, replayTarget{
		Provider: "openai", API: "openai-responses", Model: "gpt",
	}, anchor))
	if err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatal(err)
	}
	return items
}

func itemsOfType(items []map[string]any, typ string) []map[string]any {
	var out []map[string]any
	for _, item := range items {
		if item["type"] == typ {
			out = append(out, item)
		}
	}
	return out
}

func TestResponsesDeferredModes(t *testing.T) {
	ctx := deferredContext()
	short, full := twoStepDeferredContext()
	additional := func(c Context) deferredAnchor {
		_, deferred := splitDeferredTools(c.Messages, c.Tools, true)
		return deferredAnchor{mode: toolAnchorAdditional, deferred: deferred}
	}
	search := func(c Context) deferredAnchor {
		_, deferred := splitDeferredTools(c.Messages, c.Tools, true)
		return deferredAnchor{mode: toolAnchorSearch, deferred: deferred}
	}

	t.Run("additional tools anchor after the result", func(t *testing.T) {
		items := responsesItemMaps(t, ctx, additional(ctx))
		added := itemsOfType(items, "additional_tools")
		if len(added) != 1 || added[0]["role"] != "developer" {
			t.Fatalf("additional_tools = %#v", added)
		}
		tools, _ := added[0]["tools"].([]any)
		if len(tools) != 1 || tools[0].(map[string]any)["name"] != "lookup" {
			t.Fatalf("anchored tools = %#v", tools)
		}
		if len(itemsOfType(items, "tool_search_call")) != 0 {
			t.Fatal("additional mode must not emit tool search")
		}
		outIdx := -1
		addIdx := -1
		for i, item := range items {
			if item["type"] == "function_call_output" && item["call_id"] == "search1" {
				outIdx = i
			}
			if item["type"] == "additional_tools" {
				addIdx = i
			}
		}
		if outIdx < 0 || addIdx != outIdx+1 {
			t.Fatalf("anchor index output=%d additional=%d items=%#v", outIdx, addIdx, items)
		}
	})

	t.Run("tool search when additional tools are off", func(t *testing.T) {
		items := responsesItemMaps(t, ctx, search(ctx))
		if len(itemsOfType(items, "additional_tools")) != 0 {
			t.Fatal("tool search mode must not emit additional_tools")
		}
		calls := itemsOfType(items, "tool_search_call")
		outs := itemsOfType(items, "tool_search_output")
		if len(calls) != 1 || len(outs) != 1 {
			t.Fatalf("calls=%#v outs=%#v", calls, outs)
		}
		if calls[0]["execution"] != "client" || calls[0]["status"] != "completed" {
			t.Fatalf("call = %#v", calls[0])
		}
		if calls[0]["call_id"] != outs[0]["call_id"] {
			t.Fatalf("call ids %v vs %v", calls[0]["call_id"], outs[0]["call_id"])
		}
		args, _ := calls[0]["arguments"].(map[string]any)
		if args["query"] != "lookup" || args["limit"] != float64(1) {
			t.Fatalf("arguments = %#v", calls[0]["arguments"])
		}
		tools, _ := outs[0]["tools"].([]any)
		tool, _ := tools[0].(map[string]any)
		if tool["name"] != "lookup" || tool["defer_loading"] != true {
			t.Fatalf("search tool = %#v", tool)
		}
		again := responsesItemMaps(t, ctx, search(ctx))
		if againJSON, _ := json.Marshal(again); string(againJSON) != mustJSON(t, items) {
			t.Fatal("tool search call id changed between replays")
		}
	})

	t.Run("unsupported leaves the transcript unchanged", func(t *testing.T) {
		items := responsesItemMaps(t, ctx, deferredAnchor{})
		if len(itemsOfType(items, "additional_tools")) != 0 || len(itemsOfType(items, "tool_search_call")) != 0 {
			t.Fatalf("items = %#v", items)
		}
	})

	t.Run("later tools do not rewrite the prefix", func(t *testing.T) {
		shortItems := responsesItemMaps(t, short, additional(short))
		fullItems := responsesItemMaps(t, full, additional(full))
		if len(fullItems) <= len(shortItems) {
			t.Fatalf("full = %#v short = %#v", fullItems, shortItems)
		}
		if mustJSON(t, fullItems[:len(shortItems)]) != mustJSON(t, shortItems) {
			t.Fatalf("prefix changed\nshort %s\nfull  %s", mustJSON(t, shortItems), mustJSON(t, fullItems[:len(shortItems)]))
		}
		added := itemsOfType(fullItems, "additional_tools")
		if len(added) != 2 {
			t.Fatalf("anchors = %#v", added)
		}
		firstTools, _ := added[0]["tools"].([]any)
		secondTools, _ := added[1]["tools"].([]any)
		if firstTools[0].(map[string]any)["name"] != "lookup" || secondTools[0].(map[string]any)["name"] != "extra" {
			t.Fatalf("anchors = %#v", added)
		}
	})
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestResponsesDeferredListing(t *testing.T) {
	ctx := deferredContext()
	registerCompat(t, "resp-both", "openai-responses", &models.Compat{
		SupportsAdditionalTools: true,
		SupportsToolSearch:      true,
		ThinkingFormat:          "keep",
	})
	registerCompat(t, "resp-search", "openai-responses", &models.Compat{SupportsToolSearch: true})
	opts := func(provider string) Options { return Options{Provider: provider, Model: "m"} }

	anchor, listed := responsesDeferred(ctx, opts("resp-both"))
	if anchor.mode != toolAnchorAdditional {
		t.Fatalf("mode = %v", anchor.mode)
	}
	if names := toolNames(listed); len(names) != 1 || names[0] != "tool_search" {
		t.Fatalf("listed = %v", names)
	}
	anchor, listed = responsesDeferred(ctx, opts("resp-search"))
	if anchor.mode != toolAnchorSearch {
		t.Fatalf("search mode = %v", anchor.mode)
	}
	if names := toolNames(listed); len(names) != 1 || names[0] != "tool_search" {
		t.Fatalf("search listed = %v", names)
	}
	anchor, listed = responsesDeferred(ctx, opts("missing"))
	if anchor.mode != toolAnchorOff || len(listed) != 2 {
		t.Fatalf("off mode=%v listed=%v", anchor.mode, toolNames(listed))
	}
}

func TestBuildOpenAIRequestKimiDeferredTools(t *testing.T) {
	registerCompat(t, "kimi-defer", "openai-completions", &models.Compat{DeferredToolsMode: "kimi"})
	short, full := twoStepDeferredContext()
	shortReq := decodeReq(t, mustBuildOpenAI(t, short, Options{Provider: "kimi-defer", Model: "m"}))
	fullReq := decodeReq(t, mustBuildOpenAI(t, full, Options{Provider: "kimi-defer", Model: "m"}))

	if names := chatToolNames(t, shortReq["tools"]); len(names) != 1 || names[0] != "tool_search" {
		t.Fatalf("short tools = %v", names)
	}
	if names := chatToolNames(t, fullReq["tools"]); len(names) != 1 || names[0] != "tool_search" {
		t.Fatalf("full tools = %v", names)
	}
	shortMsgs, _ := shortReq["messages"].([]any)
	fullMsgs, _ := fullReq["messages"].([]any)
	if mustJSON(t, fullMsgs[:len(shortMsgs)]) != mustJSON(t, shortMsgs) {
		t.Fatalf("prefix changed\nshort %s\nfull %s", mustJSON(t, shortMsgs), mustJSON(t, fullMsgs[:len(shortMsgs)]))
	}
	batches := kimiToolBatches(t, fullMsgs)
	if len(batches) != 2 || batches[0][0] != "lookup" || batches[1][0] != "extra" {
		t.Fatalf("batches = %#v", batches)
	}

	plain := decodeReq(t, mustBuildOpenAI(t, full, Options{Provider: "openai", Model: "gpt-4o"}))
	if names := chatToolNames(t, plain["tools"]); len(names) != 3 {
		t.Fatalf("plain tools = %v", names)
	}
	if batches := kimiToolBatches(t, plain["messages"].([]any)); len(batches) != 0 {
		t.Fatalf("plain batches = %#v", batches)
	}
}

func mustBuildOpenAI(t *testing.T, ctx Context, opts Options) []byte {
	t.Helper()
	body, err := buildOpenAIRequest(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func chatToolNames(t *testing.T, raw any) []string {
	t.Helper()
	tools, _ := raw.([]any)
	var names []string
	for _, item := range tools {
		tool, _ := item.(map[string]any)
		fn, _ := tool["function"].(map[string]any)
		name, _ := fn["name"].(string)
		names = append(names, name)
	}
	return names
}

func kimiToolBatches(t *testing.T, msgs []any) [][]string {
	t.Helper()
	var batches [][]string
	for _, raw := range msgs {
		msg, _ := raw.(map[string]any)
		tools, ok := msg["tools"].([]any)
		if !ok {
			continue
		}
		if msg["role"] != "system" {
			t.Fatalf("tools message role = %v", msg["role"])
		}
		var names []string
		for _, item := range tools {
			tool, _ := item.(map[string]any)
			fn, _ := tool["function"].(map[string]any)
			name, _ := fn["name"].(string)
			names = append(names, name)
		}
		batches = append(batches, names)
	}
	return batches
}

func toolNames(ts []Tool) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return out
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
	stream := StreamAnthropicReader(context.Background(), strings.NewReader(fixture), "fw-test", nil)
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
