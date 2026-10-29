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

func TestErrorImagesInProviderRequests(t *testing.T) {
	// Regression for #339: inspect the actual serialized provider requests.
	messages := []Message{
		{Role: RoleAssistant, Assistant: &AssistantMessage{Content: []*Content{{Type: KindToolCall, ToolID: "a", ToolName: "read"}, {Type: KindToolCall, ToolID: "b", ToolName: "read"}}}},
		{Role: RoleToolResult, ToolCallID: "a", ToolName: "read", IsError: true, Content: "failed", Images: []ImageContent{{Data: "QQ==", MimeType: "image/png"}}},
		{Role: RoleToolResult, ToolCallID: "b", ToolName: "read", Content: "ok", Images: []ImageContent{{Data: "Qg==", MimeType: "image/png"}}},
	}
	t.Run("anthropic", func(t *testing.T) {
		req, _, _ := captureAnthropicRequest(t, &AnthropicClient{APIKey: "test"}, Context{Messages: messages}, Options{Provider: "anthropic", Model: "claude-sonnet-4-6"}, anthropicFixture)
		wire := req["messages"].([]any)
		if len(wire) != 3 {
			t.Fatalf("messages = %#v", wire)
		}
		results := wire[1].(map[string]any)["content"].([]any)
		images := wire[2].(map[string]any)["content"].([]any)
		if len(results) != 2 || len(images) != 2 || results[0].(map[string]any)["content"] != "failed\n(image attached after the tool results)" || images[1].(map[string]any)["type"] != "image" {
			t.Fatalf("messages = %#v", wire)
		}
		success := results[1].(map[string]any)["content"].([]any)
		if success[1].(map[string]any)["type"] != "image" {
			t.Fatalf("success = %#v", success)
		}
	})
	t.Run("bedrock claude", func(t *testing.T) {
		t.Setenv("AWS_REGION", "us-east-1")
		t.Setenv("AWS_ACCESS_KEY_ID", "test")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
		var body []byte
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer srv.Close()
		t.Setenv("AWS_ENDPOINT_URL_BEDROCK_RUNTIME", srv.URL)
		client := &BedrockClient{HTTPClient: srv.Client()}
		stream, err := client.StreamFn()(context.Background(), Context{Messages: messages}, Options{Provider: "amazon-bedrock", Model: "anthropic.claude-sonnet-4-6", CacheRetention: "none"})
		if err != nil {
			t.Fatal(err)
		}
		stream.Collect()
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("request: %v: %s", err, body)
		}
		wire := req["messages"].([]any)
		if len(wire) != 3 {
			t.Fatalf("messages = %#v", wire)
		}
		results := wire[1].(map[string]any)["content"].([]any)
		images := wire[2].(map[string]any)["content"].([]any)
		errorResult := results[0].(map[string]any)["toolResult"].(map[string]any)
		encoded, _ := json.Marshal(errorResult)
		if len(results) != 2 || len(images) != 2 || strings.Contains(string(encoded), `"image"`) || images[1].(map[string]any)["image"] == nil {
			t.Fatalf("messages = %#v", wire)
		}
		success := results[1].(map[string]any)["toolResult"].(map[string]any)["content"].([]any)
		if success[1].(map[string]any)["image"] == nil {
			t.Fatalf("success = %#v", success)
		}
	})
}
