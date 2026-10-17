package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/models"
)

func TestAnthropicEagerToolInputDefault(t *testing.T) {
	body := mustAnthropicBody(t, Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools:    []Tool{testTool("read"), testTool("grep")},
	}, Options{Model: "claude-sonnet-4"})
	tools := requestTools(t, body)
	if len(tools) != 2 {
		t.Fatalf("tools = %#v", tools)
	}
	for _, tool := range tools {
		if tool["eager_input_streaming"] != true {
			t.Fatalf("tool = %#v, want eager_input_streaming", tool)
		}
	}
}

func TestAnthropicEagerToolInputFlag(t *testing.T) {
	on := true
	off := false
	registerAnthropicCompat(t, "eager-flag", map[string]*models.Compat{
		"on":  {SupportsEagerToolInputStreaming: &on},
		"off": {SupportsEagerToolInputStreaming: &off},
	})

	onBody := mustAnthropicBody(t, toolContext(), Options{Provider: "eager-flag", Model: "on"})
	if requestTools(t, onBody)[0]["eager_input_streaming"] != true {
		t.Fatalf("on tools = %#v", requestTools(t, onBody))
	}
	offTools := requestTools(t, mustAnthropicBody(t, toolContext(), Options{Provider: "eager-flag", Model: "off"}))
	if _, ok := offTools[0]["eager_input_streaming"]; ok {
		t.Fatalf("off tool kept eager_input_streaming: %#v", offTools[0])
	}
}

func TestAnthropicFineGrainedBeta(t *testing.T) {
	off := false
	registerAnthropicCompat(t, "beta-flag", map[string]*models.Compat{
		"off": {SupportsEagerToolInputStreaming: &off},
	})

	if beta := anthropicBeta(t, toolContext(), Options{Provider: "beta-flag", Model: "off"}); !strings.Contains(beta, "fine-grained-tool-streaming-2025-05-14") {
		t.Fatalf("off+tools beta = %q", beta)
	}
	if beta := anthropicBeta(t, toolContext(), Options{Provider: "beta-flag", Model: "unset"}); strings.Contains(beta, "fine-grained-tool-streaming") {
		t.Fatalf("default+tools beta = %q", beta)
	}
	plain := Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}
	if beta := anthropicBeta(t, plain, Options{Provider: "beta-flag", Model: "off"}); strings.Contains(beta, "fine-grained-tool-streaming") {
		t.Fatalf("off without tools beta = %q", beta)
	}
	if beta := anthropicBeta(t, plain, Options{Model: "claude-sonnet-4"}); strings.Contains(beta, "fine-grained-tool-streaming") {
		t.Fatalf("default without tools beta = %q", beta)
	}

	kept := anthropicBetaWithHeader(t, toolContext(), Options{Provider: "beta-flag", Model: "unset"}, "custom-beta")
	if kept != "custom-beta" {
		t.Fatalf("caller beta = %q", kept)
	}
}

func TestAnthropicToolCacheControlFlag(t *testing.T) {
	off := false
	registerAnthropicCompat(t, "cache-flag", map[string]*models.Compat{
		"off": {SupportsCacheControlOnTools: &off},
	})

	offBody := mustAnthropicBody(t, Context{
		System:   "sys",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools:    []Tool{testTool("read"), testTool("grep")},
	}, Options{Provider: "cache-flag", Model: "off"})
	req := decodeReq(t, offBody)
	for _, tool := range requestTools(t, offBody) {
		if _, ok := tool["cache_control"]; ok {
			t.Fatalf("tool cache_control = %#v", tool)
		}
	}
	if !strings.Contains(string(offBody), "cache_control") {
		t.Fatal("system and user cache_control dropped")
	}
	sys := req["system"].([]any)[0].(map[string]any)
	if _, ok := sys["cache_control"]; !ok {
		t.Fatalf("system = %#v", sys)
	}

	onBody := mustAnthropicBody(t, Context{
		System:   "sys",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools:    []Tool{testTool("read"), testTool("grep")},
	}, Options{Model: "claude-sonnet-4"})
	onTools := requestTools(t, onBody)
	if _, ok := onTools[0]["cache_control"]; ok {
		t.Fatalf("first tool cached: %#v", onTools[0])
	}
	if _, ok := onTools[1]["cache_control"]; !ok {
		t.Fatalf("last tool missing cache_control: %#v", onTools[1])
	}

	deferred := mustAnthropicBody(t, deferredContext(), Options{Provider: "fireworks", Model: "fw"})
	deferredTools := requestTools(t, deferred)
	if _, ok := deferredTools[0]["cache_control"]; !ok {
		t.Fatalf("immediate tool missing cache_control: %#v", deferredTools[0])
	}
	if _, ok := deferredTools[1]["cache_control"]; ok {
		t.Fatalf("deferred tool cached: %#v", deferredTools[1])
	}
}

func TestAnthropicAllowEmptySignature(t *testing.T) {
	on := true
	registerAnthropicCompat(t, "sig-flag", map[string]*models.Compat{
		"on": {AllowEmptySignature: &on},
	})
	msg := Message{Assistant: &AssistantMessage{
		Provider: "sig-flag", API: "anthropic-messages", Model: "on",
		Content: []*Content{
			{Type: KindThinking, Thinking: "plan"},
			{Type: KindThinking, Thinking: "   ", ThinkingSignature: "  "},
			{Type: KindText, Text: "ok"},
		},
	}}
	blocks := assistantWireBlocks(t, mustAnthropicBody(t, Context{Messages: []Message{msg}}, Options{Provider: "sig-flag", Model: "on"}))
	var thinking map[string]any
	for _, b := range blocks {
		if b["type"] == "thinking" {
			thinking = b
		}
	}
	if thinking == nil || thinking["thinking"] != "plan" || thinking["signature"] != "" {
		t.Fatalf("blocks = %#v", blocks)
	}
	if hasTextBlock(blocks, "plan") || hasTextBlock(blocks, "   ") {
		t.Fatalf("empty signature downgraded: %#v", blocks)
	}

	offBlocks := assistantWireBlocks(t, mustAnthropicBody(t, Context{Messages: []Message{{
		Assistant: &AssistantMessage{
			Provider: "sig-flag", API: "anthropic-messages", Model: "unset",
			Content: []*Content{{Type: KindThinking, Thinking: "plan"}},
		},
	}}}, Options{Provider: "sig-flag", Model: "unset"}))
	for _, b := range offBlocks {
		if b["type"] == "thinking" {
			t.Fatalf("default replayed thinking: %#v", offBlocks)
		}
	}
	if !hasTextBlock(offBlocks, "plan") {
		t.Fatalf("blocks = %#v", offBlocks)
	}

	cross := assistantWireBlocks(t, mustAnthropicBody(t, Context{Messages: []Message{{
		Assistant: &AssistantMessage{
			Provider: "other", API: "anthropic-messages", Model: "on",
			Content: []*Content{{Type: KindThinking, Thinking: "plan", ThinkingSignature: ""}},
		},
	}}}, Options{Provider: "sig-flag", Model: "on"}))
	for _, b := range cross {
		if b["type"] == "thinking" {
			t.Fatalf("cross-model thinking kept: %#v", cross)
		}
	}
	if !hasTextBlock(cross, "plan") {
		t.Fatalf("cross blocks = %#v", cross)
	}
}

func TestAnthropicTemperatureCompat(t *testing.T) {
	off := false
	registerAnthropicModels(t, "temp-flag", []models.Model{
		{Provider: "temp-flag", ID: "plain", SamplingParams: map[string]any{"temperature": 0.2, "top_p": 0.9}},
		{Provider: "temp-flag", ID: "off", SamplingParams: map[string]any{"temperature": 0.2}, Compat: &models.Compat{SupportsTemperature: &off}},
		{Provider: "temp-flag", ID: "effort", SamplingParams: map[string]any{"temperature": 0.2}, Compat: &models.Compat{SupportsMidConvoEffort: true}},
	})

	sent := decodeReq(t, mustAnthropicBody(t, userHi(), Options{
		Provider: "temp-flag", Model: "plain", Thinking: "off",
		SamplingParams: map[string]any{"temperature": 0.9},
	}))
	if sent["temperature"] != 0.9 {
		t.Fatalf("temperature = %#v", sent["temperature"])
	}
	if _, ok := sent["top_p"]; ok {
		t.Fatalf("top_p leaked: %#v", sent["top_p"])
	}

	catalog := decodeReq(t, mustAnthropicBody(t, userHi(), Options{Provider: "temp-flag", Model: "plain"}))
	if catalog["temperature"] != 0.2 {
		t.Fatalf("catalog temperature = %#v", catalog["temperature"])
	}
	if _, ok := catalog["top_p"]; ok {
		t.Fatalf("catalog top_p leaked: %#v", catalog)
	}

	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"flag off", Options{Provider: "temp-flag", Model: "off", SamplingParams: map[string]any{"temperature": 0.9}}},
		{"thinking on", Options{Provider: "temp-flag", Model: "plain", Thinking: "high", SamplingParams: map[string]any{"temperature": 0.9}}},
		{"mid convo", Options{Provider: "temp-flag", Model: "effort", Thinking: "off", SamplingParams: map[string]any{"temperature": 0.9}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := decodeReq(t, mustAnthropicBody(t, userHi(), tc.opts))
			if _, ok := req["temperature"]; ok {
				t.Fatalf("temperature = %#v", req["temperature"])
			}
		})
	}
}

func TestAnthropicStrictTools(t *testing.T) {
	on := true
	registerAnthropicCompat(t, "strict-flag", map[string]*models.Compat{
		"on": {SupportsStrictTools: &on},
	})
	read := readLikeSchema()
	before := mustJSON(t, read)
	limited := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"n": map[string]any{"type": "integer", "minimum": 1},
		},
		"required": []any{"n"},
	}
	beforeLimited := mustJSON(t, limited)
	ctx := Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools: []Tool{
			{Name: "read", Description: "r", Parameters: read, ConstrainedSampling: &ConstrainedSampling{Type: "json_schema", Strict: "prefer"}},
			{Name: "grep", Description: "g", Parameters: map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}}},
			{Name: "limit", Description: "l", Parameters: limited, ConstrainedSampling: &ConstrainedSampling{Type: "json_schema", Strict: "prefer"}},
		},
	}

	plain := toolsByName(t, mustAnthropicBody(t, ctx, Options{Provider: "strict-flag", Model: "unset"}))
	if _, ok := plain["read"]["strict"]; ok {
		t.Fatalf("default strict = %#v", plain["read"])
	}
	plainRequired, _ := plain["read"]["input_schema"].(map[string]any)["required"].([]any)
	if len(plainRequired) != 1 || plainRequired[0] != "path" {
		t.Fatalf("default schema changed: %#v", plain["read"]["input_schema"])
	}

	strict := toolsByName(t, mustAnthropicBody(t, ctx, Options{Provider: "strict-flag", Model: "on"}))
	if strict["read"]["strict"] != true {
		t.Fatalf("read = %#v", strict["read"])
	}
	readSchema, _ := strict["read"]["input_schema"].(map[string]any)
	assertRequired(t, readSchema, "limit", "offset", "path")
	if readSchema["additionalProperties"] != false {
		t.Fatalf("read schema = %#v", readSchema)
	}
	if _, ok := strict["grep"]["strict"]; ok {
		t.Fatalf("unmarked tool strict = %#v", strict["grep"])
	}
	if _, ok := strict["limit"]["strict"]; ok {
		t.Fatalf("unsupported keyword stayed strict: %#v", strict["limit"])
	}
	limitSchema, _ := strict["limit"]["input_schema"].(map[string]any)
	n, _ := limitSchema["properties"].(map[string]any)["n"].(map[string]any)
	if n["minimum"] != float64(1) && n["minimum"] != 1 {
		t.Fatalf("limit schema = %#v", limitSchema)
	}

	if got := mustJSON(t, read); got != before {
		t.Fatalf("read mutated\n%s", got)
	}
	if got := mustJSON(t, limited); got != beforeLimited {
		t.Fatalf("limit mutated\n%s", got)
	}
}

func registerAnthropicCompat(t *testing.T, provider string, flags map[string]*models.Compat) {
	t.Helper()
	modelsList := make([]models.Model, 0, len(flags)+1)
	for id, compat := range flags {
		modelsList = append(modelsList, models.Model{Provider: provider, ID: id, API: "anthropic-messages", Compat: compat})
	}
	modelsList = append(modelsList, models.Model{Provider: provider, ID: "unset", API: "anthropic-messages"})
	registerAnthropicModels(t, provider, modelsList)
}

func registerAnthropicModels(t *testing.T, provider string, list []models.Model) {
	t.Helper()
	models.RegisterProvider(models.ProviderSpec{
		ID: provider, DefaultAPI: "anthropic-messages", DefaultID: list[0].ID, Models: list,
	})
	t.Cleanup(func() { models.UnregisterProvider(provider) })
}

func toolContext() Context {
	return Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
		Tools:    []Tool{testTool("read")},
	}
}

func userHi() Context {
	return Context{Messages: []Message{{Role: RoleUser, Content: "hi"}}}
}

func mustAnthropicBody(t *testing.T, ctx Context, opts Options) []byte {
	t.Helper()
	body, err := buildAnthropicRequest(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func requestTools(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	req := decodeReq(t, body)
	raw, _ := req["tools"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		tool, _ := item.(map[string]any)
		out = append(out, tool)
	}
	return out
}

func toolsByName(t *testing.T, body []byte) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, tool := range requestTools(t, body) {
		name, _ := tool["name"].(string)
		out[name] = tool
	}
	return out
}

func anthropicBeta(t *testing.T, ctx Context, opts Options) string {
	t.Helper()
	return anthropicBetaWithHeader(t, ctx, opts, "")
}

func anthropicBetaWithHeader(t *testing.T, ctx Context, opts Options, beta string) string {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("anthropic-beta")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(anthropicFixture))
	}))
	t.Cleanup(srv.Close)
	client := &AnthropicClient{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()}
	if beta != "" {
		client.Headers = map[string]string{"anthropic-beta": beta}
	}
	stream, err := client.StreamFn()(context.Background(), ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	return got
}
