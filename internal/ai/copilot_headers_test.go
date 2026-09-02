package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInferCopilotInitiator(t *testing.T) {
	if got := inferCopilotInitiator([]Message{{Role: RoleUser, Content: "hi"}}); got != "user" {
		t.Fatalf("%s", got)
	}
	if got := inferCopilotInitiator([]Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, Content: "ok"},
	}); got != "agent" {
		t.Fatalf("%s", got)
	}
	if got := inferCopilotInitiator([]Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleToolResult, ToolCallID: "1", Content: "out"},
	}); got != "agent" {
		t.Fatalf("%s", got)
	}
}

func TestHasCopilotVisionInput(t *testing.T) {
	if hasCopilotVisionInput([]Message{{Role: RoleUser, Content: "hi"}}) {
		t.Fatal("text-only")
	}
	if !hasCopilotVisionInput([]Message{{
		Role: RoleUser, Content: "look",
		Images: []ImageContent{{Type: "image", Data: "AAA", MimeType: "image/png"}},
	}}) {
		t.Fatal("user image")
	}
	if !hasCopilotVisionInput([]Message{{
		Role: RoleToolResult, ToolCallID: "1", Content: "out",
		Images: []ImageContent{{Type: "image", Data: "BBB", MimeType: "image/png"}},
	}}) {
		t.Fatal("tool image")
	}
}

func TestStreamForCopilotDynamicHeaders(t *testing.T) {
	var initiator, intent, vision string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		initiator = r.Header.Get("X-Initiator")
		intent = r.Header.Get("Openai-Intent")
		vision = r.Header.Get("Copilot-Vision-Request")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	t.Cleanup(srv.Close)

	stream, err := StreamFor("github-copilot", ClientConfig{
		APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client(),
	})(context.Background(), Context{Messages: []Message{
		{Role: RoleUser, Content: "hi"},
		{Role: RoleAssistant, Content: "ok"},
	}}, Options{Model: "claude-fable-5"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	if initiator != "agent" || intent != "conversation-edits" {
		t.Fatalf("initiator=%q intent=%q", initiator, intent)
	}
	if vision != "" {
		t.Fatalf("unexpected vision header %q", vision)
	}

	stream, err = StreamFor("github-copilot", ClientConfig{
		APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client(),
	})(context.Background(), Context{Messages: []Message{{
		Role: RoleUser, Content: "look",
		Images: []ImageContent{{Type: "image", Data: "AAA", MimeType: "image/png"}},
	}}}, Options{Model: "claude-fable-5"})
	if err != nil {
		t.Fatal(err)
	}
	stream.Collect()
	if initiator != "user" || vision != "true" {
		t.Fatalf("initiator=%q vision=%q", initiator, vision)
	}
}
