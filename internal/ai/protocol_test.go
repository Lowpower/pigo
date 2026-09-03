package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"google.golang.org/genai"
)

func TestGuessOpenCodeAPI(t *testing.T) {
	cases := map[string]string{
		"claude-sonnet-4":   "anthropic-messages",
		"gemini-2.5-flash":  "google-generative-ai",
		"gpt-5":             "openai-responses",
		"gpt-5-mini":        "openai-responses",
		"gpt-4o":            "openai-responses",
		"o3-mini":           "openai-responses",
		"gpt-5.3-codex":     "openai-responses",
		"deepseek-v4-flash": "openai-completions",
		"qwen-2.5":          "openai-completions",
	}
	for model, want := range cases {
		if got := guessOpenCodeAPI(model); got != want {
			t.Errorf("guessOpenCodeAPI(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestGuessOpenCodeGoAPISkipsGoogle(t *testing.T) {
	if got := guessOpenCodeGoAPI("gemini-2.5-pro"); got != "" {
		t.Fatalf("got %q, want empty (no google on opencode-go)", got)
	}
	if got := guessOpenCodeGoAPI("gpt-5"); got != "openai-responses" {
		t.Fatalf("got %q", got)
	}
}

func TestGuessCloudflareAndFireworksAPI(t *testing.T) {
	if got := guessCloudflareGatewayAPI("claude-sonnet-4"); got != "anthropic-messages" {
		t.Fatalf("cf claude = %q", got)
	}
	if got := guessCloudflareGatewayAPI("gpt-5"); got != "openai-responses" {
		t.Fatalf("cf gpt-5 = %q", got)
	}
	if got := guessCloudflareGatewayAPI("llama-3"); got != "openai-completions" {
		t.Fatalf("cf llama = %q", got)
	}
	if got := guessFireworksAPI("accounts/fireworks/models/claude-sonnet"); got != "anthropic-messages" {
		t.Fatalf("fw claude = %q", got)
	}
	if got := guessFireworksAPI("accounts/fireworks/models/llama-3"); got != "openai-completions" {
		t.Fatalf("fw llama = %q", got)
	}
}

func TestClampPromptCacheKey(t *testing.T) {
	if got := clampPromptCacheKey("abc"); got != "abc" {
		t.Fatalf("%q", got)
	}
	long := strings.Repeat("x", 80)
	if got := clampPromptCacheKey(long); got != strings.Repeat("x", 64) {
		t.Fatalf("len=%d", len(got))
	}
}

func TestAzureDeploymentNameAndAPIVersion(t *testing.T) {
	t.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", "")
	t.Setenv("AZURE_OPENAI_API_VERSION", "")
	if got := azureDeploymentName("gpt-4"); got != "gpt-4" {
		t.Fatalf("%q", got)
	}
	if got := azureAPIVersion(); got != "v1" {
		t.Fatalf("default api-version = %q", got)
	}
	t.Setenv("AZURE_OPENAI_API_VERSION", "2024-12-01-preview")
	if got := azureAPIVersion(); got != "2024-12-01-preview" {
		t.Fatalf("%q", got)
	}
	t.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", "gpt-4=dep-a,gpt-4o=dep-b")
	if got := azureDeploymentName("gpt-4"); got != "dep-a" {
		t.Fatalf("csv map = %q", got)
	}
	t.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", `{"gpt-4":"json-dep"}`)
	if got := azureDeploymentName("gpt-4"); got != "json-dep" {
		t.Fatalf("json map = %q", got)
	}
}

func TestBuildAnthropicRequestCacheThinkingAndTools(t *testing.T) {
	body, err := buildAnthropicRequest(Context{
		System:   "sys",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools:    []Tool{{Name: "read", Description: "r", Parameters: map[string]any{"type": "object"}}},
	}, Options{Model: "claude-sonnet-4", Thinking: "high", MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatal(err)
	}
	sys, _ := req["system"].([]any)
	if len(sys) != 1 {
		t.Fatalf("system = %#v", req["system"])
	}
	sys0, _ := sys[0].(map[string]any)
	if _, ok := sys0["cache_control"]; !ok {
		t.Fatalf("system missing cache_control: %#v", sys0)
	}
	msgs, _ := req["messages"].([]any)
	last, _ := msgs[len(msgs)-1].(map[string]any)
	content, _ := last["content"].([]any)
	lastBlock, _ := content[len(content)-1].(map[string]any)
	if _, ok := lastBlock["cache_control"]; !ok {
		t.Fatalf("last user missing cache_control: %#v", lastBlock)
	}
	tools, _ := req["tools"].([]any)
	tool, _ := tools[len(tools)-1].(map[string]any)
	if _, ok := tool["cache_control"]; !ok {
		t.Fatalf("last tool missing cache_control: %#v", tool)
	}
	if _, ok := req["tool_choice"]; !ok {
		t.Fatal("missing tool_choice")
	}
	thinking, _ := req["thinking"].(map[string]any)
	if thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v", thinking)
	}
}

func TestBuildAnthropicRequestAdaptiveThinking(t *testing.T) {
	t.Cleanup(func() { models.SetThinkingBudgets(nil) })
	models.SetThinkingBudgets(map[string]int{"low": 0})
	body, err := buildAnthropicRequest(Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "claude-sonnet-4", Thinking: "low"})
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	thinking, _ := req["thinking"].(map[string]any)
	if thinking["type"] != "adaptive" {
		t.Fatalf("thinking = %#v, want adaptive when budget is 0", thinking)
	}
}

func TestAnthropicRedactedThinkingReplay(t *testing.T) {
	wire := AnthropicWireMessages([]Message{{
		Assistant: &AssistantMessage{Content: []*Content{{
			Type: KindThinking, Redacted: true, ThinkingSignature: "opaque-sig", Thinking: "[Reasoning redacted]",
		}}},
	}})
	blocks, _ := wire[0]["content"].([]map[string]any)
	if len(blocks) != 1 || blocks[0]["type"] != "redacted_thinking" || blocks[0]["data"] != "opaque-sig" {
		t.Fatalf("replay = %#v", wire[0]["content"])
	}
}

func TestBuildOpenAIRequestReasoningCacheStore(t *testing.T) {
	body, err := buildOpenAIRequest(Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "gpt-4o", Thinking: "high", SessionID: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	if req["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v", req["reasoning_effort"])
	}
	if req["prompt_cache_key"] != "sess-1" {
		t.Fatalf("prompt_cache_key = %#v", req["prompt_cache_key"])
	}
	if req["store"] != false {
		t.Fatalf("store = %#v", req["store"])
	}
}

func TestOpenAIWireToolResultImages(t *testing.T) {
	wire := OpenAIWireMessages([]Message{{
		Role: RoleToolResult, ToolCallID: "c1", ToolName: "read",
		Images:  []ImageContent{{Type: "image", Data: "AAA", MimeType: "image/png"}},
		Content: "ok",
	}})
	content, _ := wire[0]["content"].([]map[string]any)
	if len(content) != 2 || content[1]["type"] != "image_url" {
		t.Fatalf("tool result = %#v", wire[0]["content"])
	}
}

func TestBuildMistralRequestReasoningAndCache(t *testing.T) {
	body, err := buildMistralRequest(Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "codestral-latest", Thinking: "high", SessionID: "sess-9"})
	if err != nil {
		t.Fatal(err)
	}
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	if req["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v", req["reasoning_effort"])
	}
	if req["prompt_mode"] != "reasoning" {
		t.Fatalf("prompt_mode = %#v", req["prompt_mode"])
	}
	if req["prompt_cache_key"] != "sess-9" {
		t.Fatalf("prompt_cache_key = %#v", req["prompt_cache_key"])
	}
}

func TestPiMessagesOptionsIncludesSessionAndToolChoice(t *testing.T) {
	got := piMessagesOptions(Options{
		MaxTokens: 10, Thinking: "low", SessionID: "s1", CacheRetention: "short", ToolChoice: "auto",
	})
	if got["sessionId"] != "s1" || got["cacheRetention"] != "short" || got["toolChoice"] != "auto" || got["reasoning"] != "low" {
		t.Fatalf("%#v", got)
	}
}

func TestGoogleContentsReplayThoughtSignaturesAndImages(t *testing.T) {
	got := googleContents(Context{Messages: []Message{
		{Role: RoleUser, Content: "look", Images: []ImageContent{{Type: "image", Data: "QQ==", MimeType: "image/png"}}},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindThinking, Thinking: "hmm", ThinkingSignature: "tsig"},
			{Type: KindText, Text: "yo", TextSignature: "xsig"},
			{Type: KindToolCall, ToolID: "1", ToolName: "read", Arguments: map[string]any{"p": "a"}, ThinkingSignature: "csig"},
		}}},
	}})
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].Parts[0].Text != "look" {
		t.Fatalf("user text = %#v", got[0].Parts[0])
	}
	if got[0].Parts[1].InlineData == nil || got[0].Parts[1].InlineData.MIMEType != "image/png" {
		t.Fatalf("user image = %#v", got[0].Parts[1])
	}
	if string(got[1].Parts[0].ThoughtSignature) != "tsig" || !got[1].Parts[0].Thought {
		t.Fatalf("thinking part = %#v", got[1].Parts[0])
	}
	if string(got[1].Parts[1].ThoughtSignature) != "xsig" {
		t.Fatalf("text signature = %q", got[1].Parts[1].ThoughtSignature)
	}
	if got[1].Parts[2].FunctionCall == nil || string(got[1].Parts[2].ThoughtSignature) != "csig" {
		t.Fatalf("tool part = %#v", got[1].Parts[2])
	}
}

func TestGoogleThinkingConfig(t *testing.T) {
	if googleThinkingConfig(Options{}) != nil {
		t.Fatal("off should omit thinking config")
	}
	cfg := googleThinkingConfig(Options{Thinking: "low"})
	if cfg == nil || cfg.ThinkingLevel != genai.ThinkingLevelLow || !cfg.IncludeThoughts {
		t.Fatalf("%+v", cfg)
	}
	cfg = googleThinkingConfig(Options{ThinkingBudget: 80})
	if cfg == nil || cfg.ThinkingBudget == nil || *cfg.ThinkingBudget != 80 {
		t.Fatalf("%+v", cfg)
	}
}

func TestBedrockMessagesReplayReasoningAndImages(t *testing.T) {
	got := bedrockMessages(Context{Messages: []Message{
		{Role: RoleUser, Content: "look", Images: []ImageContent{{Type: "image", Data: "QQ==", MimeType: "image/png"}}},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindThinking, Thinking: "plan", ThinkingSignature: "sig"},
			{Type: KindText, Text: "yo"},
		}}},
	}})
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if len(got[0].Content) != 2 {
		t.Fatalf("user blocks=%d", len(got[0].Content))
	}
	if _, ok := got[0].Content[1].(*types.ContentBlockMemberImage); !ok {
		t.Fatalf("user image = %T", got[0].Content[1])
	}
	if _, ok := got[1].Content[0].(*types.ContentBlockMemberReasoningContent); !ok {
		t.Fatalf("assistant reasoning = %T", got[1].Content[0])
	}
}

func TestBedrockThinkingFields(t *testing.T) {
	if bedrockThinkingFields(Options{}) != nil {
		t.Fatal("off")
	}
	got := bedrockThinkingFields(Options{Thinking: "high"})
	thinking, _ := got["thinking"].(map[string]any)
	if thinking["type"] != "enabled" {
		t.Fatalf("%#v", got)
	}
}

func TestFireworksDefaultIDUpdated(t *testing.T) {
	spec, ok := models.LookupProvider("fireworks")
	if !ok {
		t.Fatal("missing fireworks")
	}
	want := "accounts/fireworks/models/deepseek-v4-flash-0731"
	if spec.DefaultID != want {
		t.Fatalf("DefaultID = %q, want %q", spec.DefaultID, want)
	}
}

func TestMapAnthropicStopRefusal(t *testing.T) {
	if mapAnthropicStopReason("refusal") != StopError {
		t.Fatal("refusal")
	}
	if mapAnthropicStopReason("sensitive") != StopError {
		t.Fatal("sensitive")
	}
}

func TestResolveWireAPIOpenCode(t *testing.T) {
	if got := resolveWireAPI("opencode", "gpt-5", "opencode"); got != "openai-responses" {
		t.Fatalf("%q", got)
	}
	if got := resolveWireAPI("opencode", "claude-sonnet-4", "opencode"); got != "anthropic-messages" {
		t.Fatalf("%q", got)
	}
}
