package ai

import (
	"context"
	"testing"

	"google.golang.org/genai"
)

func TestGoogleContentsRolesAndTools(t *testing.T) {
	got := googleContents(Context{Messages: []Message{
		{Role: RoleUser, Content: "hi"},
		{Assistant: &AssistantMessage{Content: []*Content{
			{Type: KindText, Text: "yo"},
			{Type: KindToolCall, ToolID: "1", ToolName: "read", Arguments: map[string]any{"path": "a"}},
		}}},
		{Role: RoleToolResult, ToolCallID: "1", ToolName: "read", Content: "ok"},
	}})
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Role != "user" || got[1].Role != "model" || got[2].Role != "user" {
		t.Fatalf("roles = %q %q %q", got[0].Role, got[1].Role, got[2].Role)
	}
	if got[1].Parts[1].FunctionCall == nil || got[1].Parts[1].FunctionCall.Name != "read" {
		t.Fatalf("tool part = %+v", got[1].Parts)
	}
	if got[2].Parts[0].FunctionResponse == nil {
		t.Fatal("missing function response")
	}
}

func TestGoogleVertexClientConfig(t *testing.T) {
	c := &GoogleClient{APIKey: "k", Project: "p", Location: "us-central1", Vertex: true}
	cfg := c.genaiConfig()
	if cfg.Backend != genai.BackendVertexAI || cfg.APIKey != "k" || cfg.Project != "p" || cfg.Location != "us-central1" {
		t.Fatalf("%+v", cfg)
	}
	if msg := c.missingAuth(); msg != "" {
		t.Fatalf("unexpected missingAuth %q", msg)
	}
}

func TestGoogleBaseURLHTTPOptions(t *testing.T) {
	c := &GoogleClient{APIKey: "k", BaseURL: "https://opencode.ai/zen"}
	cfg := c.genaiConfig()
	if cfg.HTTPOptions.BaseURL != "https://opencode.ai/zen" {
		t.Fatalf("HTTPOptions.BaseURL = %q", cfg.HTTPOptions.BaseURL)
	}
}

func TestGoogleVertexMissingAuth(t *testing.T) {
	c := &GoogleClient{Vertex: true, Project: "p"}
	if msg := c.missingAuth(); msg == "" {
		t.Fatal("expected missing location")
	}
	stream, err := c.StreamFn()(context.Background(), Context{}, Options{Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	_, final := stream.Collect()
	if final == nil || final.StopReason != StopError || final.API != "google-vertex" {
		t.Fatalf("%+v", final)
	}
}
