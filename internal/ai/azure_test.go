package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const azureChatFixture = `data: {"choices":[{"delta":{"content":"Hello, world"}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]
`

func TestResolveAzureEndpoint(t *testing.T) {
	t.Setenv("AZURE_OPENAI_BASE_URL", "")
	t.Setenv("AZURE_OPENAI_RESOURCE_NAME", "")
	if _, err := resolveAzureEndpoint(""); err == nil || !strings.Contains(err.Error(), "base URL is required") {
		t.Fatalf("missing endpoint = %v", err)
	}
	t.Setenv("AZURE_OPENAI_RESOURCE_NAME", "myres")
	got, err := resolveAzureEndpoint("")
	if err != nil || got != "https://myres.openai.azure.com/openai/v1" {
		t.Fatalf("resource = %q err=%v", got, err)
	}
	got, err = resolveAzureEndpoint("https://my-resource.services.ai.azure.com")
	if err != nil || got != "https://my-resource.services.ai.azure.com/openai/v1" {
		t.Fatalf("services host = %q err=%v", got, err)
	}
	got, err = resolveAzureEndpoint("https://x.openai.azure.com/openai/v1/responses")
	if err != nil || got != "https://x.openai.azure.com/openai/v1" {
		t.Fatalf("responses root = %q err=%v", got, err)
	}
	got, err = resolveAzureEndpoint("https://x.cognitiveservices.azure.com/openai")
	if err != nil || got != "https://x.cognitiveservices.azure.com/openai/v1" {
		t.Fatalf("cognitive = %q err=%v", got, err)
	}
	got, err = resolveAzureEndpoint("http://127.0.0.1:9/custom")
	if err != nil || got != "http://127.0.0.1:9/custom" {
		t.Fatalf("non-azure = %q err=%v", got, err)
	}
}

func TestStreamForAzureDeepSeekChat(t *testing.T) {
	t.Setenv("AZURE_OPENAI_API_VERSION", "")
	t.Setenv("AZURE_OPENAI_BASE_URL", "")
	t.Setenv("AZURE_OPENAI_RESOURCE_NAME", "")
	t.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", "deepseek-v4-pro=my-deepseek")
	var payload map[string]any
	var path, version string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		version = r.Header.Get("api-version")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(azureChatFixture))
	}))
	defer srv.Close()

	stream, err := StreamFor("azure", ClientConfig{
		APIKey: "k", BaseURL: srv.URL + "/openai/v1", HTTPClient: srv.Client(),
	})(context.Background(), Context{
		System: "sys",
		Messages: []Message{
			{Role: RoleUser, Content: "first"},
			{Assistant: &AssistantMessage{
				Provider: "azure", API: "openai-completions", Model: "deepseek-v4-pro",
				Content: []*Content{
					{Type: KindThinking, Thinking: "internal reasoning", ThinkingSignature: "reasoning_content"},
					{Type: KindText, Text: "answer"},
				},
			}},
			{Role: RoleUser, Content: "second"},
		},
	}, Options{
		Model: "deepseek-v4-pro", Thinking: "max", SessionID: "session-1", CacheRetention: "long",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.Text() != "Hello, world" {
		t.Fatalf("text = %v", final)
	}
	if final.API != "openai-completions" || final.Model != "deepseek-v4-pro" || final.Provider != "azure" {
		t.Fatalf("final = %+v", final)
	}
	if path != "/openai/v1/chat/completions" {
		t.Fatalf("path = %q", path)
	}
	if version != "v1" {
		t.Fatalf("api-version = %q", version)
	}
	if payload["model"] != "my-deepseek" {
		t.Fatalf("model = %#v", payload["model"])
	}
	if payload["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v", payload["reasoning_effort"])
	}
	if _, ok := payload["thinking"]; ok {
		t.Fatalf("thinking = %#v", payload["thinking"])
	}
	if _, ok := payload["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key = %#v", payload["prompt_cache_key"])
	}
	if _, ok := payload["prompt_cache_retention"]; ok {
		t.Fatalf("prompt_cache_retention = %#v", payload["prompt_cache_retention"])
	}
	if _, ok := payload["input"]; ok {
		t.Fatalf("chat body has input: %#v", payload)
	}
	msgs, _ := payload["messages"].([]any)
	if len(msgs) == 0 {
		t.Fatalf("messages = %#v", payload["messages"])
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "sys" {
		t.Fatalf("system = %#v", first)
	}
	var assistant map[string]any
	for _, raw := range msgs {
		msg, _ := raw.(map[string]any)
		if msg["role"] == "assistant" {
			assistant = msg
		}
	}
	if assistant["reasoning_content"] != "internal reasoning" {
		t.Fatalf("reasoning_content = %#v", assistant)
	}
}

func TestStreamForAzureMissingEndpoint(t *testing.T) {
	t.Setenv("AZURE_OPENAI_BASE_URL", "")
	t.Setenv("AZURE_OPENAI_RESOURCE_NAME", "")
	stream, err := StreamFor("azure", ClientConfig{})(context.Background(), Context{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}, Options{Model: "deepseek-v4-pro"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.StopReason != StopError || !strings.Contains(final.ErrorMessage, "base URL is required") {
		t.Fatalf("final = %+v", final)
	}
}
