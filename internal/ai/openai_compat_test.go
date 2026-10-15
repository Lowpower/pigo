package ai

import (
	"encoding/json"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func openAICompatBody(t *testing.T, ctx Context, opts Options, baseURL string) map[string]any {
	t.Helper()
	raw, err := buildOpenAIRequestAt(ctx, opts, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	return req
}

func TestOpenAICompatUnknownModelUsesStandardDefaults(t *testing.T) {
	body := openAICompatBody(t, Context{
		System:   "be brief",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-4o", MaxTokens: 50, Thinking: "high", SessionID: "sess-1"}, "")

	if body["store"] != false {
		t.Fatalf("store = %#v", body["store"])
	}
	if body["max_completion_tokens"] != float64(50) {
		t.Fatalf("max_completion_tokens = %#v", body["max_completion_tokens"])
	}
	if _, ok := body["max_tokens"]; ok {
		t.Fatalf("max_tokens leaked: %#v", body["max_tokens"])
	}
	stream, _ := body["stream_options"].(map[string]any)
	if stream["include_usage"] != true {
		t.Fatalf("stream_options = %#v", body["stream_options"])
	}
	msgs, _ := body["messages"].([]any)
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "be brief" {
		t.Fatalf("system = %#v", msgs[0])
	}
	if body["prompt_cache_key"] != "sess-1" {
		t.Fatalf("prompt_cache_key = %#v", body["prompt_cache_key"])
	}
	if _, ok := body["provider"]; ok {
		t.Fatalf("provider routing leaked: %#v", body["provider"])
	}
	if _, ok := body["tool_stream"]; ok {
		t.Fatalf("tool_stream leaked: %#v", body["tool_stream"])
	}
}

func TestOpenAICompatDetectedDefaults(t *testing.T) {
	on := true
	off := false
	models.RegisterProvider(models.ProviderSpec{
		ID: "compat-detect", DefaultAPI: "openai-completions", DefaultID: "plain",
		Models: []models.Model{
			{Provider: "compat-detect", ID: "plain", Reasoning: &on},
			{Provider: "compat-detect", ID: "quiet", Reasoning: &off},
			{Provider: "compat-detect", ID: "together", BaseURL: "https://api.together.xyz/v1", Reasoning: &on},
			{Provider: "compat-detect", ID: "workers", BaseURL: "https://api.cloudflare.com/client/v4", Reasoning: &on},
		},
	})
	models.SetUserOverlay("deepseek", []models.Model{{ID: "compat-chat", Reasoning: &on}})
	models.SetUserOverlay("xai", []models.Model{{ID: "compat-grok", Reasoning: &on}})
	models.SetUserOverlay("openrouter", []models.Model{
		{ID: "anthropic/claude-compat", Reasoning: &on},
		{ID: "z-ai/glm-compat", Reasoning: &on},
		{ID: "openai/gpt-compat", Reasoning: &on},
	})
	t.Cleanup(func() {
		models.ClearOverlays()
		models.UnregisterProvider("compat-detect")
	})

	std := openAICompatBody(t, Context{System: "sys", Messages: []Message{{Role: RoleUser, Content: "hi"}}},
		Options{Provider: "compat-detect", Model: "plain", MaxTokens: 20}, "")
	msgs, _ := std["messages"].([]any)
	role, _ := msgs[0].(map[string]any)["role"].(string)
	if role != "developer" {
		t.Fatalf("reasoning model role = %q", role)
	}
	if std["max_completion_tokens"] != float64(20) || std["store"] != false {
		t.Fatalf("standard body = %#v", std)
	}

	quiet := openAICompatBody(t, Context{System: "sys", Messages: []Message{{Role: RoleUser, Content: "hi"}}},
		Options{Provider: "compat-detect", Model: "quiet"}, "")
	msgs, _ = quiet["messages"].([]any)
	role, _ = msgs[0].(map[string]any)["role"].(string)
	if role != "system" {
		t.Fatalf("non-reasoning role = %q", role)
	}

	dsCtx := Context{
		Messages: []Message{{
			Role:      RoleAssistant,
			Assistant: &AssistantMessage{Content: []*Content{{Type: KindText, Text: "answer"}}},
		}},
	}
	ds := openAICompatBody(t, dsCtx, Options{Provider: "deepseek", Model: "compat-chat", MaxTokens: 30}, "")
	if _, ok := ds["store"]; ok {
		t.Fatalf("deepseek store = %#v", ds["store"])
	}
	if ds["max_tokens"] != float64(30) {
		t.Fatalf("deepseek max = %#v", ds["max_tokens"])
	}
	if _, ok := ds["max_completion_tokens"]; ok {
		t.Fatalf("deepseek completion field = %#v", ds["max_completion_tokens"])
	}
	dsMsgs, _ := ds["messages"].([]any)
	asst, _ := dsMsgs[0].(map[string]any)
	if asst["reasoning_content"] != "" {
		t.Fatalf("deepseek reasoning_content = %#v", asst["reasoning_content"])
	}

	viaURL := openAICompatBody(t, Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}},
		Options{Provider: "compat-detect", Model: "plain", MaxTokens: 8}, "https://api.deepseek.com/v1")
	if _, ok := viaURL["store"]; ok || viaURL["max_tokens"] != float64(8) {
		t.Fatalf("client deepseek url = %#v", viaURL)
	}

	catalogURL := openAICompatBody(t, Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}},
		Options{Provider: "compat-detect", Model: "together", MaxTokens: 8}, "")
	if _, ok := catalogURL["store"]; ok || catalogURL["max_tokens"] != float64(8) {
		t.Fatalf("together catalog url = %#v", catalogURL)
	}
	clientWins := openAICompatBody(t, Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}},
		Options{Provider: "compat-detect", Model: "together", MaxTokens: 8}, "https://api.openai.com/v1")
	if clientWins["store"] != false || clientWins["max_completion_tokens"] != float64(8) {
		t.Fatalf("client url should win over catalog = %#v", clientWins)
	}
	if _, ok := clientWins["max_tokens"]; ok {
		t.Fatalf("client url kept max_tokens: %#v", clientWins["max_tokens"])
	}

	xaiBody := openAICompatBody(t, Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}},
		Options{Provider: "xai", Model: "compat-grok", MaxTokens: 8}, "")
	if _, ok := xaiBody["store"]; ok || xaiBody["max_completion_tokens"] != float64(8) {
		t.Fatalf("xai = %#v", xaiBody)
	}

	workers := openAICompatBody(t, Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}},
		Options{Provider: "compat-detect", Model: "workers", MaxTokens: 8}, "")
	if _, ok := workers["store"]; ok || workers["max_completion_tokens"] != float64(8) {
		t.Fatalf("cloudflare workers = %#v", workers)
	}

	orClaude := openAICompatBody(t, Context{
		System:   "sys",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools:    []Tool{{Name: "read", Description: "d", Parameters: map[string]any{"type": "object"}}},
	}, Options{Provider: "openrouter", Model: "anthropic/claude-compat", CacheRetention: "long"}, "")
	orMsgs, _ := orClaude["messages"].([]any)
	sys, _ := orMsgs[0].(map[string]any)
	if sys["role"] != "developer" {
		t.Fatalf("openrouter claude role = %#v", sys["role"])
	}
	sysParts, _ := sys["content"].([]any)
	sysPart, _ := sysParts[0].(map[string]any)
	cc, _ := sysPart["cache_control"].(map[string]any)
	if sysPart["text"] != "sys" || cc["type"] != "ephemeral" || cc["ttl"] != "1h" {
		t.Fatalf("system cache = %#v", sys["content"])
	}
	user, _ := orMsgs[1].(map[string]any)
	userParts, _ := user["content"].([]any)
	userPart, _ := userParts[0].(map[string]any)
	ucc, _ := userPart["cache_control"].(map[string]any)
	if ucc["type"] != "ephemeral" || ucc["ttl"] != "1h" {
		t.Fatalf("user cache = %#v", user["content"])
	}
	tools, _ := orClaude["tools"].([]any)
	tool, _ := tools[0].(map[string]any)
	tcc, _ := tool["cache_control"].(map[string]any)
	if tcc["type"] != "ephemeral" || tcc["ttl"] != "1h" {
		t.Fatalf("tool cache = %#v", tool)
	}

	short := openAICompatBody(t, Context{System: "sys", Messages: []Message{{Role: RoleUser, Content: "hi"}}},
		Options{Provider: "openrouter", Model: "anthropic/claude-compat", CacheRetention: "short"}, "")
	shortMsgs, _ := short["messages"].([]any)
	shortSys, _ := shortMsgs[0].(map[string]any)
	shortParts, _ := shortSys["content"].([]any)
	shortPart, _ := shortParts[0].(map[string]any)
	shortCC, _ := shortPart["cache_control"].(map[string]any)
	if shortCC["type"] != "ephemeral" {
		t.Fatalf("short cache = %#v", shortPart)
	}
	if _, ok := shortCC["ttl"]; ok {
		t.Fatalf("short ttl = %#v", shortCC["ttl"])
	}

	none := openAICompatBody(t, Context{System: "sys", Messages: []Message{{Role: RoleUser, Content: "hi"}}},
		Options{Provider: "openrouter", Model: "anthropic/claude-compat", CacheRetention: "none"}, "")
	noneMsgs, _ := none["messages"].([]any)
	noneSys, _ := noneMsgs[0].(map[string]any)
	if noneSys["content"] != "sys" {
		t.Fatalf("cache none rewrote system: %#v", noneSys["content"])
	}

	glm := openAICompatBody(t, Context{System: "sys", Messages: []Message{{Role: RoleUser, Content: "hi"}}},
		Options{Provider: "openrouter", Model: "z-ai/glm-compat"}, "")
	glmMsgs, _ := glm["messages"].([]any)
	glmRole, _ := glmMsgs[0].(map[string]any)["role"].(string)
	if glmRole != "system" {
		t.Fatalf("openrouter glm role = %q", glmRole)
	}
	if _, ok := glm["tools"]; ok {
		t.Fatal("unexpected tools")
	}
	glmContent := glmMsgs[0].(map[string]any)["content"]
	if glmContent != "sys" {
		t.Fatalf("glm should not get cache_control: %#v", glmContent)
	}
}

func TestOpenAICompatDetectMatrix(t *testing.T) {
	cases := []struct {
		provider, model, base string
		store                 bool
		field                 string
		developer             bool
		reasoning             bool
		cache                 string
	}{
		{"openai", "gpt-4o", "https://api.openai.com/v1", true, "max_completion_tokens", true, false, ""},
		{"deepseek", "x", "", false, "max_tokens", false, true, ""},
		{"xai", "grok", "", false, "max_completion_tokens", false, false, ""},
		{"cerebras", "x", "", false, "max_completion_tokens", false, false, ""},
		{"together", "x", "", false, "max_tokens", false, false, ""},
		{"nvidia", "x", "", false, "max_tokens", false, false, ""},
		{"moonshotai", "x", "", false, "max_tokens", false, false, ""},
		{"moonshotai-cn", "x", "", false, "max_tokens", false, false, ""},
		{"zai", "x", "", false, "max_tokens", false, false, ""},
		{"zai-coding-cn", "x", "", false, "max_tokens", false, false, ""},
		{"opencode", "x", "", false, "max_completion_tokens", false, false, ""},
		{"cloudflare-workers-ai", "x", "", false, "max_completion_tokens", false, false, ""},
		{"cloudflare-ai-gateway", "x", "", false, "max_tokens", false, false, ""},
		{"ant-ling", "x", "", false, "max_tokens", false, false, ""},
		{"custom", "x", "https://chutes.ai/v1", false, "max_tokens", false, false, ""},
		{"custom", "x", "https://api.together.xyz/v1", false, "max_tokens", false, false, ""},
		{"custom", "x", "https://api.together.ai/v1", false, "max_tokens", false, false, ""},
		{"custom", "x", "https://api.moonshot.ai/v1", false, "max_tokens", false, false, ""},
		{"custom", "x", "https://open.bigmodel.cn/api", false, "max_tokens", false, false, ""},
		{"custom", "x", "https://api.z.ai/api", false, "max_tokens", false, false, ""},
		{"custom", "x", "https://integrate.api.nvidia.com/v1", false, "max_tokens", false, false, ""},
		{"custom", "x", "https://api.ant-ling.com/v1", false, "max_tokens", false, false, ""},
		{"custom", "x", "https://gateway.ai.cloudflare.com/v1", false, "max_tokens", false, false, ""},
		{"custom", "x", "https://api.cloudflare.com/v1", false, "max_completion_tokens", false, false, ""},
		{"custom", "x", "https://api.cerebras.ai/v1", false, "max_completion_tokens", false, false, ""},
		{"custom", "x", "https://api.x.ai/v1", false, "max_completion_tokens", false, false, ""},
		{"custom", "x", "https://opencode.ai/v1", false, "max_completion_tokens", false, false, ""},
		{"custom", "x", "https://api.deepseek.com/v1", false, "max_tokens", false, true, ""},
		{"openrouter", "anthropic/claude", "", true, "max_completion_tokens", true, false, "anthropic"},
		{"openrouter", "openai/gpt", "", true, "max_completion_tokens", true, false, ""},
		{"openrouter", "z-ai/glm", "", true, "max_completion_tokens", false, false, ""},
		{"proxy", "anthropic/claude", "https://openrouter.ai/api/v1", true, "max_completion_tokens", true, false, ""},
	}
	for _, tc := range cases {
		got := detectOpenAICompat(tc.provider, tc.model, tc.base)
		if got.supportsStore != tc.store || got.maxTokensField != tc.field || got.supportsDeveloperRole != tc.developer ||
			got.requiresReasoningContent != tc.reasoning || got.cacheControlFormat != tc.cache || !got.supportsUsageInStreaming {
			t.Fatalf("%s %s %s: %+v", tc.provider, tc.model, tc.base, got)
		}
	}
}

func TestOpenAICompatFlagsToggleRequestFields(t *testing.T) {
	on := true
	off := false
	models.RegisterProvider(models.ProviderSpec{
		ID: "compat-flags", DefaultAPI: "openai-completions", DefaultID: "m",
		Models: []models.Model{
			{Provider: "compat-flags", ID: "store-off", Reasoning: &on, Compat: &models.Compat{SupportsStore: &off}},
			{Provider: "compat-flags", ID: "store-on", BaseURL: "https://api.deepseek.com", Reasoning: &on, Compat: &models.Compat{SupportsStore: &on}},
			{Provider: "compat-flags", ID: "usage-off", Compat: &models.Compat{SupportsUsageInStreaming: &off}},
			{Provider: "compat-flags", ID: "usage-on", Compat: &models.Compat{SupportsUsageInStreaming: &on}},
			{Provider: "compat-flags", ID: "dev-off", Reasoning: &on, Compat: &models.Compat{SupportsDeveloperRole: &off}},
			{Provider: "compat-flags", ID: "dev-on", Reasoning: &on, BaseURL: "https://api.deepseek.com", Compat: &models.Compat{SupportsDeveloperRole: &on}},
			{Provider: "compat-flags", ID: "max-legacy", Compat: &models.Compat{MaxTokensField: "max_tokens"}},
			{Provider: "compat-flags", ID: "max-new", BaseURL: "https://api.deepseek.com", Compat: &models.Compat{MaxTokensField: "max_completion_tokens"}},
			{Provider: "compat-flags", ID: "name-on", Compat: &models.Compat{RequiresToolResultName: &on}},
			{Provider: "compat-flags", ID: "name-off", Compat: &models.Compat{RequiresToolResultName: &off}},
			{Provider: "compat-flags", ID: "bridge-on", Compat: &models.Compat{RequiresAssistantAfterToolResult: &on}},
			{Provider: "compat-flags", ID: "bridge-off", Compat: &models.Compat{RequiresAssistantAfterToolResult: &off}},
			{Provider: "compat-flags", ID: "think-text", Reasoning: &on, Compat: &models.Compat{RequiresThinkingAsText: &on}},
			{Provider: "compat-flags", ID: "think-field", Reasoning: &on, Compat: &models.Compat{RequiresThinkingAsText: &off}},
			{Provider: "compat-flags", ID: "reason-on", Reasoning: &on, Compat: &models.Compat{RequiresReasoningContentOnAssistantMessages: &on}},
			{Provider: "compat-flags", ID: "reason-off", Reasoning: &on, BaseURL: "https://api.deepseek.com", Compat: &models.Compat{RequiresReasoningContentOnAssistantMessages: &off}},
			{Provider: "compat-flags", ID: "reason-quiet", Reasoning: &off, Compat: &models.Compat{RequiresReasoningContentOnAssistantMessages: &on}},
			{Provider: "compat-flags", ID: "route", Compat: &models.Compat{OpenRouterRouting: map[string]any{"only": []any{"anthropic"}, "allow_fallbacks": false}}},
			{Provider: "compat-flags", ID: "route-empty", Compat: &models.Compat{OpenRouterRouting: map[string]any{}}},
			{Provider: "compat-flags", ID: "vercel", Compat: &models.Compat{VercelGatewayRouting: &models.VercelGatewayRouting{Only: []string{"bedrock"}, Order: []string{"anthropic"}}}},
			{Provider: "compat-flags", ID: "vercel-empty", Compat: &models.Compat{VercelGatewayRouting: &models.VercelGatewayRouting{}}},
			{Provider: "compat-flags", ID: "zai-on", Compat: &models.Compat{ZaiToolStream: &on}},
			{Provider: "compat-flags", ID: "zai-off", Compat: &models.Compat{ZaiToolStream: &off}},
			{Provider: "compat-flags", ID: "budget-field", Reasoning: &on, MaxTokens: 3000, Compat: &models.Compat{ThinkingTokenBudgetField: "thinking_budget"}},
			{Provider: "compat-flags", ID: "budget-alias", Reasoning: &on, Compat: &models.Compat{SupportsThinkingTokenBudget: &on}},
			{Provider: "compat-flags", ID: "budget-off", Reasoning: &on, Compat: &models.Compat{SupportsThinkingTokenBudget: &off, ThinkingTokenBudgetField: "thinking_budget_tokens"}},
			{Provider: "compat-flags", ID: "budget-quiet", Reasoning: &off, Compat: &models.Compat{ThinkingTokenBudgetField: "thinking_token_budget"}},
			{Provider: "compat-flags", ID: "cache-on", Compat: &models.Compat{CacheControlFormat: "anthropic"}},
			{Provider: "compat-flags", ID: "cache-long-off", Compat: &models.Compat{CacheControlFormat: "anthropic", SupportsLongCacheRetention: &off}},
			{Provider: "compat-flags", ID: "both-think", Reasoning: &on, Compat: &models.Compat{RequiresThinkingAsText: &on, RequiresReasoningContentOnAssistantMessages: &on}},
		},
	})
	t.Cleanup(func() { models.UnregisterProvider("compat-flags") })

	hi := Context{System: "sys", Messages: []Message{{Role: RoleUser, Content: "hi"}}}
	toolCtx := Context{Messages: []Message{
		{Role: RoleToolResult, ToolCallID: "c1", ToolName: "bash", Content: "ok"},
		{Role: RoleUser, Content: "next"},
	}}
	thinkCtx := func(model string) Context {
		return Context{Messages: []Message{{
			Role: RoleAssistant,
			Assistant: &AssistantMessage{
				Provider: "compat-flags",
				API:      "openai-completions",
				Model:    model,
				Content: []*Content{
					{Type: KindThinking, Thinking: "  "},
					{Type: KindThinking, Thinking: "plan"},
					{Type: KindThinking, Thinking: "more"},
					{Type: KindText, Text: "hello"},
				},
			},
		}}}
	}
	plainAsst := Context{Messages: []Message{{
		Role:      RoleAssistant,
		Assistant: &AssistantMessage{Content: []*Content{{Type: KindText, Text: "answer"}}},
	}}}

	storeOff := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "store-off"}, "")
	if _, ok := storeOff["store"]; ok {
		t.Fatalf("supportsStore false still sent store: %#v", storeOff["store"])
	}
	storeOn := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "store-on"}, "")
	if storeOn["store"] != false {
		t.Fatalf("supportsStore true = %#v", storeOn["store"])
	}

	usageOff := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "usage-off"}, "")
	if _, ok := usageOff["stream_options"]; ok {
		t.Fatalf("usage off = %#v", usageOff["stream_options"])
	}
	usageOn := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "usage-on"}, "")
	stream, _ := usageOn["stream_options"].(map[string]any)
	if stream["include_usage"] != true {
		t.Fatalf("usage on = %#v", usageOn["stream_options"])
	}

	devOff := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "dev-off"}, "")
	if roleOf(t, devOff, 0) != "system" {
		t.Fatalf("developer off role = %q", roleOf(t, devOff, 0))
	}
	devOn := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "dev-on"}, "")
	if roleOf(t, devOn, 0) != "developer" {
		t.Fatalf("developer on role = %q", roleOf(t, devOn, 0))
	}

	legacy := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "max-legacy", MaxTokens: 12, SamplingParams: map[string]any{"max_completion_tokens": 3}}, "")
	if legacy["max_tokens"] != float64(12) {
		t.Fatalf("max_tokens = %#v", legacy["max_tokens"])
	}
	if _, ok := legacy["max_completion_tokens"]; ok {
		t.Fatalf("sampling max_completion_tokens leaked: %#v", legacy["max_completion_tokens"])
	}
	newer := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "max-new", MaxTokens: 12, SamplingParams: map[string]any{"max_tokens": 3}}, "")
	if newer["max_completion_tokens"] != float64(12) {
		t.Fatalf("max_completion_tokens = %#v", newer["max_completion_tokens"])
	}
	if _, ok := newer["max_tokens"]; ok {
		t.Fatalf("sampling max_tokens leaked: %#v", newer["max_tokens"])
	}

	named := openAICompatBody(t, toolCtx, Options{Provider: "compat-flags", Model: "name-on"}, "")
	toolMsg := messageAt(t, named, 0)
	if toolMsg["name"] != "bash" {
		t.Fatalf("tool name = %#v", toolMsg["name"])
	}
	unnamed := openAICompatBody(t, Context{Messages: []Message{{Role: RoleToolResult, ToolCallID: "c1", Content: "ok"}}},
		Options{Provider: "compat-flags", Model: "name-on"}, "")
	if _, ok := messageAt(t, unnamed, 0)["name"]; ok {
		t.Fatalf("empty tool name sent: %#v", messageAt(t, unnamed, 0))
	}
	nameOff := openAICompatBody(t, toolCtx, Options{Provider: "compat-flags", Model: "name-off"}, "")
	if _, ok := messageAt(t, nameOff, 0)["name"]; ok {
		t.Fatalf("name off = %#v", messageAt(t, nameOff, 0)["name"])
	}

	bridged := openAICompatBody(t, toolCtx, Options{Provider: "compat-flags", Model: "bridge-on"}, "")
	if messageAt(t, bridged, 1)["role"] != "assistant" || messageAt(t, bridged, 1)["content"] != "I have processed the tool results." {
		t.Fatalf("bridge = %#v", messageAt(t, bridged, 1))
	}
	if messageAt(t, bridged, 2)["role"] != "user" {
		t.Fatalf("user moved = %#v", messageAt(t, bridged, 2))
	}
	toolsOnly := openAICompatBody(t, Context{Messages: []Message{
		{Role: RoleToolResult, ToolCallID: "c1", ToolName: "bash", Content: "one"},
		{Role: RoleToolResult, ToolCallID: "c2", ToolName: "bash", Content: "two"},
	}}, Options{Provider: "compat-flags", Model: "bridge-on"}, "")
	if len(messagesOf(t, toolsOnly)) != 2 {
		t.Fatalf("bridge inserted between tools: %#v", messagesOf(t, toolsOnly))
	}
	noBridge := openAICompatBody(t, toolCtx, Options{Provider: "compat-flags", Model: "bridge-off"}, "")
	if len(messagesOf(t, noBridge)) != 2 || messageAt(t, noBridge, 1)["role"] != "user" {
		t.Fatalf("bridge off = %#v", messagesOf(t, noBridge))
	}

	asText := openAICompatBody(t, thinkCtx("think-text"), Options{Provider: "compat-flags", Model: "think-text"}, "")
	parts, _ := messageAt(t, asText, 0)["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("thinking as text = %#v", messageAt(t, asText, 0)["content"])
	}
	first, _ := parts[0].(map[string]any)
	second, _ := parts[1].(map[string]any)
	if first["type"] != "text" || first["text"] != "plan\n\nmore" || second["text"] != "hello" {
		t.Fatalf("parts = %#v", parts)
	}
	if _, ok := messageAt(t, asText, 0)["reasoning_content"]; ok {
		t.Fatalf("thinking as text kept reasoning_content: %#v", messageAt(t, asText, 0)["reasoning_content"])
	}
	asField := openAICompatBody(t, thinkCtx("think-field"), Options{Provider: "compat-flags", Model: "think-field"}, "")
	if messageAt(t, asField, 0)["content"] != "hello" {
		t.Fatalf("default content = %#v", messageAt(t, asField, 0)["content"])
	}
	if messageAt(t, asField, 0)["reasoning_content"] == nil {
		t.Fatal("default reasoning_content missing")
	}

	reasonOn := openAICompatBody(t, plainAsst, Options{Provider: "compat-flags", Model: "reason-on"}, "")
	if messageAt(t, reasonOn, 0)["reasoning_content"] != "" {
		t.Fatalf("empty reasoning = %#v", messageAt(t, reasonOn, 0)["reasoning_content"])
	}
	reasonOff := openAICompatBody(t, plainAsst, Options{Provider: "compat-flags", Model: "reason-off"}, "")
	if _, ok := messageAt(t, reasonOff, 0)["reasoning_content"]; ok {
		t.Fatalf("deepseek override still set reasoning_content: %#v", messageAt(t, reasonOff, 0))
	}
	reasonQuiet := openAICompatBody(t, plainAsst, Options{Provider: "compat-flags", Model: "reason-quiet"}, "")
	if _, ok := messageAt(t, reasonQuiet, 0)["reasoning_content"]; ok {
		t.Fatalf("non-reasoning model set reasoning_content: %#v", messageAt(t, reasonQuiet, 0))
	}
	both := openAICompatBody(t, thinkCtx("both-think"), Options{Provider: "compat-flags", Model: "both-think"}, "")
	if messageAt(t, both, 0)["reasoning_content"] != "" {
		t.Fatalf("both flags reasoning_content = %#v", messageAt(t, both, 0)["reasoning_content"])
	}
	if _, ok := messageAt(t, both, 0)["content"].([]any); !ok {
		t.Fatalf("both flags content = %#v", messageAt(t, both, 0)["content"])
	}

	routed := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "route"}, "")
	provider, _ := routed["provider"].(map[string]any)
	only, _ := provider["only"].([]any)
	if len(only) != 1 || only[0] != "anthropic" || provider["allow_fallbacks"] != false {
		t.Fatalf("provider = %#v", routed["provider"])
	}
	emptyRoute := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "route-empty"}, "")
	emptyProvider, ok := emptyRoute["provider"].(map[string]any)
	if !ok || len(emptyProvider) != 0 {
		t.Fatalf("empty routing = %#v", emptyRoute["provider"])
	}
	plain := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "usage-on"}, "")
	if _, ok := plain["provider"]; ok {
		t.Fatalf("unset routing sent: %#v", plain["provider"])
	}

	vercel := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "vercel"}, "")
	popts, _ := vercel["providerOptions"].(map[string]any)
	gateway, _ := popts["gateway"].(map[string]any)
	gOnly, _ := gateway["only"].([]any)
	gOrder, _ := gateway["order"].([]any)
	if len(gOnly) != 1 || gOnly[0] != "bedrock" || len(gOrder) != 1 || gOrder[0] != "anthropic" {
		t.Fatalf("gateway = %#v", vercel["providerOptions"])
	}
	vercelEmpty := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "vercel-empty"}, "")
	if _, ok := vercelEmpty["providerOptions"]; ok {
		t.Fatalf("empty vercel sent: %#v", vercelEmpty["providerOptions"])
	}

	withTools := Context{Tools: []Tool{{Name: "read", Parameters: map[string]any{"type": "object"}}}}
	zaiOn := openAICompatBody(t, withTools, Options{Provider: "compat-flags", Model: "zai-on"}, "")
	if zaiOn["tool_stream"] != true {
		t.Fatalf("tool_stream = %#v", zaiOn["tool_stream"])
	}
	zaiNoTools := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "zai-on"}, "")
	if _, ok := zaiNoTools["tool_stream"]; ok {
		t.Fatalf("tool_stream without tools = %#v", zaiNoTools["tool_stream"])
	}
	zaiOff := openAICompatBody(t, withTools, Options{Provider: "compat-flags", Model: "zai-off"}, "")
	if _, ok := zaiOff["tool_stream"]; ok {
		t.Fatalf("zai off = %#v", zaiOff["tool_stream"])
	}

	clamped := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "budget-field", Thinking: "high", ThinkingBudget: 10000, MaxTokens: 2000}, "")
	if clamped["thinking_budget"] != float64(976) {
		t.Fatalf("thinking_budget = %#v", clamped["thinking_budget"])
	}
	fromModel := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "budget-field", Thinking: "high", ThinkingBudget: 10000}, "")
	if fromModel["thinking_budget"] != float64(1976) {
		t.Fatalf("model ceiling budget = %#v", fromModel["thinking_budget"])
	}
	alias := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "budget-alias", Thinking: "low", ThinkingBudget: 1500, MaxTokens: 4000}, "")
	if alias["thinking_token_budget"] != float64(1500) {
		t.Fatalf("alias budget = %#v", alias["thinking_token_budget"])
	}
	fieldWins := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "budget-off", Thinking: "high", ThinkingBudget: 2000, MaxTokens: 5000}, "")
	if fieldWins["thinking_budget_tokens"] != float64(2000) {
		t.Fatalf("field wins = %#v", fieldWins["thinking_budget_tokens"])
	}
	if _, ok := fieldWins["thinking_token_budget"]; ok {
		t.Fatalf("alias also sent: %#v", fieldWins["thinking_token_budget"])
	}
	offBudget := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "budget-field", Thinking: "off", ThinkingBudget: 10000, MaxTokens: 4000}, "")
	if _, ok := offBudget["thinking_budget"]; ok {
		t.Fatalf("thinking off still budgeted: %#v", offBudget["thinking_budget"])
	}
	tooSmall := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "budget-field", Thinking: "high", ThinkingBudget: 5000, MaxTokens: 100}, "")
	if _, ok := tooSmall["thinking_budget"]; ok {
		t.Fatalf("no answer room still sent budget: %#v", tooSmall["thinking_budget"])
	}
	quietBudget := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "budget-quiet", Thinking: "high", ThinkingBudget: 2000, MaxTokens: 4000}, "")
	if _, ok := quietBudget["thinking_token_budget"]; ok {
		t.Fatalf("non-reasoning budget = %#v", quietBudget["thinking_token_budget"])
	}

	cached := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "cache-on", CacheRetention: "short"}, "")
	cachedSys := messageAt(t, cached, 0)
	cachedParts, _ := cachedSys["content"].([]any)
	cachedPart, _ := cachedParts[0].(map[string]any)
	cachedCC, _ := cachedPart["cache_control"].(map[string]any)
	if cachedCC["type"] != "ephemeral" {
		t.Fatalf("explicit cache = %#v", cachedSys["content"])
	}
	if _, ok := cachedCC["ttl"]; ok {
		t.Fatalf("short explicit ttl = %#v", cachedCC)
	}
	longOff := openAICompatBody(t, hi, Options{Provider: "compat-flags", Model: "cache-long-off", CacheRetention: "long"}, "")
	longParts, _ := messageAt(t, longOff, 0)["content"].([]any)
	longPart, _ := longParts[0].(map[string]any)
	longCC, _ := longPart["cache_control"].(map[string]any)
	if longCC["type"] != "ephemeral" {
		t.Fatalf("long disabled cache = %#v", longPart)
	}
	if _, ok := longCC["ttl"]; ok {
		t.Fatalf("long retention disabled still set ttl: %#v", longCC)
	}
}

func messagesOf(t *testing.T, body map[string]any) []any {
	t.Helper()
	msgs, _ := body["messages"].([]any)
	return msgs
}

func messageAt(t *testing.T, body map[string]any, i int) map[string]any {
	t.Helper()
	msgs := messagesOf(t, body)
	if i >= len(msgs) {
		t.Fatalf("message %d missing in %#v", i, msgs)
	}
	msg, _ := msgs[i].(map[string]any)
	return msg
}

func roleOf(t *testing.T, body map[string]any, i int) string {
	t.Helper()
	role, _ := messageAt(t, body, i)["role"].(string)
	return role
}
