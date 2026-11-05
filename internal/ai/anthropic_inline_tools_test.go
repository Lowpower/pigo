package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func TestAnthropicInlineToolsFreezeInitialList(t *testing.T) {
	registerInlineToolsModel(t, true, true)
	base := inlineTool("base_tool", "base_tool tool")
	late := inlineTool("late_tool", "late_tool tool")
	opts := Options{Provider: "inline-ant", Model: "claude", MaxTokens: 100}

	firstBody, err := buildAnthropicRequest(Context{
		System: "base prompt",
		Messages: []Message{
			{Role: RoleSystem, ToolsAdded: []Tool{base}},
			{Role: RoleUser, Content: "before"},
		},
		Tools: []Tool{base},
	}, opts)
	if err != nil {
		t.Fatal(err)
	}
	first := decodeAnthropicRequest(t, firstBody)
	if names := anthropicToolNames(t, first); !sameStrings(names, []string{"base_tool", inlineDeferredPlaceholder}) {
		t.Fatalf("initial tools = %#v", names)
	}
	if anthropicMessageTypes(t, first) != nil && containsString(anthropicMessageTypes(t, first), "tool_addition") {
		t.Fatalf("unchanged request inserted tool_addition: %#v", first["messages"])
	}
	assertInlinePlaceholder(t, first)
	if got := anthropicExtraBetas(opts, Context{Messages: firstMessages(base), Tools: []Tool{base}}); !containsString(got, inlineToolsBeta) {
		t.Fatalf("unchanged betas = %#v", got)
	}

	secondBody, err := buildAnthropicRequest(Context{
		System: "base prompt",
		Messages: []Message{
			{Role: RoleSystem, ToolsAdded: []Tool{base}},
			{Role: RoleUser, Content: "before"},
			{Role: RoleSystem, ToolsAdded: []Tool{late}},
		},
		Tools: []Tool{base, late},
	}, opts)
	if err != nil {
		t.Fatal(err)
	}
	second := decodeAnthropicRequest(t, secondBody)
	if names := anthropicToolNames(t, second); !sameStrings(names, []string{"base_tool", inlineDeferredPlaceholder}) {
		t.Fatalf("tools after addition = %#v", names)
	}
	addition := lastSystemBlocks(t, second)
	if len(addition) != 1 || addition[0]["type"] != "tool_addition" {
		t.Fatalf("addition = %#v", addition)
	}
	def := inlineDefinition(t, addition[0])
	if def["name"] != "late_tool" || def["description"] != "late_tool tool" {
		t.Fatalf("definition = %#v", def)
	}
	if _, ok := def["cache_control"]; ok {
		t.Fatalf("cache_control on definition: %#v", def)
	}
	if _, ok := def["defer_loading"]; ok {
		t.Fatalf("defer_loading on definition: %#v", def)
	}
	if addition[0]["cache_control"] == nil {
		t.Fatalf("cache_control missing on tool_addition: %#v", addition[0])
	}
}

func TestAnthropicInlineToolsRedefineWithoutRemoval(t *testing.T) {
	registerInlineToolsModel(t, true, true)
	base := inlineTool("base_tool", "base_tool tool")
	changed := inlineTool("base_tool", "changed")
	body, err := buildAnthropicRequest(Context{
		Messages: []Message{
			{Role: RoleSystem, ToolsAdded: []Tool{base}},
			{Role: RoleUser, Content: "before"},
			{Role: RoleSystem, ToolsRemoved: []string{"base_tool"}, ToolsAdded: []Tool{changed}},
		},
		Tools: []Tool{changed},
	}, Options{Provider: "inline-ant", Model: "claude", MaxTokens: 20})
	if err != nil {
		t.Fatal(err)
	}
	req := decodeAnthropicRequest(t, body)
	tools := anthropicTools(t, req)
	if tools[0]["description"] != "base_tool tool" {
		t.Fatalf("top-level definition changed: %#v", tools[0])
	}
	blocks := lastSystemBlocks(t, req)
	if len(blocks) != 1 || blocks[0]["type"] != "tool_addition" {
		t.Fatalf("redefine = %#v", blocks)
	}
	if inlineDefinition(t, blocks[0])["description"] != "changed" {
		t.Fatalf("new definition = %#v", blocks[0])
	}
}

func TestAnthropicInlineToolsRemovalAndHold(t *testing.T) {
	registerInlineToolsModel(t, true, true)
	base := inlineTool("base_tool", "base_tool tool")
	body, err := buildAnthropicRequest(Context{
		Messages: []Message{
			{Role: RoleSystem, ToolsAdded: []Tool{base}},
			{Role: RoleUser, Content: "before"},
			{Role: RoleAssistant, Assistant: &AssistantMessage{Role: RoleAssistant, Content: []*Content{{Type: KindText, Text: "ok"}}}},
			{Role: RoleSystem, ToolsRemoved: []string{"base_tool"}},
			{Role: RoleUser, Content: "after"},
		},
		Tools: nil,
	}, Options{Provider: "inline-ant", Model: "claude", MaxTokens: 20})
	if err != nil {
		t.Fatal(err)
	}
	req := decodeAnthropicRequest(t, body)
	if names := anthropicToolNames(t, req); !sameStrings(names, []string{"base_tool", inlineDeferredPlaceholder}) {
		t.Fatalf("tools = %#v", names)
	}
	if roles := messageRoles(req["messages"].([]any)); !sameStrings(roles, []string{"user", "assistant", "user", "system"}) {
		t.Fatalf("roles = %#v", roles)
	}
	blocks := lastSystemBlocks(t, req)
	if len(blocks) != 1 || blocks[0]["type"] != "tool_removal" {
		t.Fatalf("removal = %#v", blocks)
	}
	tool, _ := blocks[0]["tool"].(map[string]any)
	if tool["type"] != "tool_reference" || tool["name"] != "base_tool" {
		t.Fatalf("reference = %#v", tool)
	}
}

func TestAnthropicInlineToolsRequireBothFlagsAndInitialTool(t *testing.T) {
	late := inlineTool("late_tool", "late_tool tool")
	cases := []struct {
		name   string
		system bool
		tools  bool
		msgs   []Message
	}{
		{
			name: "tool flag only", system: false, tools: true,
			msgs: []Message{
				{Role: RoleSystem, ToolsAdded: []Tool{inlineTool("base_tool", "base")}},
				{Role: RoleUser, Content: "before"},
				{Role: RoleSystem, ToolsAdded: []Tool{late}},
			},
		},
		{
			name: "no initial tool", system: true, tools: true,
			msgs: []Message{
				{Role: RoleSystem, Content: "base prompt"},
				{Role: RoleUser, Content: "before"},
				{Role: RoleSystem, ToolsAdded: []Tool{late}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			registerInlineToolsModel(t, tc.system, tc.tools)
			body, err := buildAnthropicRequest(Context{
				Messages: tc.msgs,
				Tools:    []Tool{late},
			}, Options{Provider: "inline-ant", Model: "claude", MaxTokens: 20})
			if err != nil {
				t.Fatal(err)
			}
			req := decodeAnthropicRequest(t, body)
			if names := anthropicToolNames(t, req); !sameStrings(names, []string{"late_tool"}) {
				t.Fatalf("tools = %#v", names)
			}
			if types := anthropicMessageTypes(t, req); containsString(types, "tool_addition") || containsString(types, "tool_removal") {
				t.Fatalf("native blocks = %#v", req["messages"])
			}
			betas := anthropicExtraBetas(Options{Provider: "inline-ant", Model: "claude"}, Context{Messages: tc.msgs, Tools: []Tool{late}})
			if containsString(betas, inlineToolsBeta) {
				t.Fatalf("betas = %#v", betas)
			}
		})
	}
}

func TestAnthropicInlineToolsBetaHeader(t *testing.T) {
	registerInlineToolsModel(t, true, true)
	var beta string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beta = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicFixture))
	}))
	defer srv.Close()
	base := inlineTool("base_tool", "base")
	client := &AnthropicClient{BaseURL: srv.URL, APIKey: "sk-test", HTTPClient: srv.Client()}
	stream, err := client.StreamFn()(context.Background(), Context{
		Messages: []Message{
			{Role: RoleSystem, ToolsAdded: []Tool{base}},
			{Role: RoleUser, Content: "hi"},
		},
		Tools: []Tool{base},
	}, Options{Provider: "inline-ant", Model: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	if !strings.Contains(beta, inlineToolsBeta) {
		t.Fatalf("beta = %q", beta)
	}
}

func registerInlineToolsModel(t *testing.T, system, tools bool) {
	t.Helper()
	models.RegisterProvider(models.ProviderSpec{
		ID: "inline-ant", DefaultAPI: "anthropic-messages", DefaultID: "claude",
		Models: []models.Model{{
			Provider: "inline-ant",
			ID:       "claude",
			Compat: &models.Compat{
				SupportsMidConvoSystemMessages: system,
				SupportsMidConvoToolChanges:    tools,
			},
		}},
	})
	t.Cleanup(func() { models.UnregisterProvider("inline-ant") })
}

func inlineDefinition(t *testing.T, block map[string]any) map[string]any {
	t.Helper()
	tool, _ := block["tool"].(map[string]any)
	def, _ := tool["definition"].(map[string]any)
	if def == nil {
		t.Fatalf("definition = %#v", block)
	}
	return def
}

func inlineTool(name, desc string) Tool {
	return Tool{Name: name, Description: desc, Parameters: map[string]any{"type": "object"}}
}

func firstMessages(base Tool) []Message {
	return []Message{
		{Role: RoleSystem, ToolsAdded: []Tool{base}},
		{Role: RoleUser, Content: "before"},
	}
}

func anthropicTools(t *testing.T, req map[string]any) []map[string]any {
	t.Helper()
	raw, _ := req["tools"].([]any)
	out := make([]map[string]any, len(raw))
	for i, item := range raw {
		out[i], _ = item.(map[string]any)
		if out[i] == nil {
			t.Fatalf("tool %d = %#v", i, item)
		}
	}
	return out
}

func anthropicToolNames(t *testing.T, req map[string]any) []string {
	t.Helper()
	tools := anthropicTools(t, req)
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i], _ = tool["name"].(string)
	}
	return names
}

func assertInlinePlaceholder(t *testing.T, req map[string]any) {
	t.Helper()
	tools := anthropicTools(t, req)
	last := tools[len(tools)-1]
	if last["defer_loading"] != true {
		t.Fatalf("placeholder = %#v", last)
	}
	if _, ok := last["cache_control"]; ok {
		t.Fatalf("placeholder cache_control = %#v", last)
	}
	prev := tools[len(tools)-2]
	if prev["cache_control"] == nil {
		t.Fatalf("initial tool missing cache_control: %#v", prev)
	}
}

func lastSystemBlocks(t *testing.T, req map[string]any) []map[string]any {
	t.Helper()
	msgs, _ := req["messages"].([]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		msg, _ := msgs[i].(map[string]any)
		if msg["role"] != "system" {
			continue
		}
		raw, _ := msg["content"].([]any)
		out := make([]map[string]any, len(raw))
		for j, block := range raw {
			out[j], _ = block.(map[string]any)
		}
		return out
	}
	t.Fatal("no system message")
	return nil
}

func anthropicMessageTypes(t *testing.T, req map[string]any) []string {
	t.Helper()
	var types []string
	msgs, _ := req["messages"].([]any)
	for _, item := range msgs {
		msg, _ := item.(map[string]any)
		raw, _ := msg["content"].([]any)
		for _, block := range raw {
			m, _ := block.(map[string]any)
			if typ, _ := m["type"].(string); typ != "" {
				types = append(types, typ)
			}
		}
	}
	return types
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestAnthropicInlineToolsJSONShape(t *testing.T) {
	raw, err := json.Marshal(Message{Role: RoleSystem, ToolsRemoved: []string{"write"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"toolsRemoved":["write"]`) {
		t.Fatalf("json = %s", raw)
	}
}
